package plugin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"

	"github.com/flow-software/Timebase-Grafana/Historian/pkg/auth"
	"github.com/flow-software/Timebase-Grafana/Historian/pkg/i3x"
	"github.com/flow-software/Timebase-Grafana/Historian/pkg/models"
	"github.com/flow-software/Timebase-Grafana/Historian/pkg/series"
	"github.com/flow-software/Timebase-Grafana/Historian/pkg/timebase"
	"github.com/grafana/grafana-plugin-sdk-go/backend"
	"github.com/grafana/grafana-plugin-sdk-go/backend/httpclient"
	"github.com/grafana/grafana-plugin-sdk-go/backend/instancemgmt"
	"github.com/grafana/grafana-plugin-sdk-go/data"
)

var (
	_ backend.QueryDataHandler      = (*Datasource)(nil)
	_ backend.CheckHealthHandler    = (*Datasource)(nil)
	_ backend.CallResourceHandler   = (*Datasource)(nil)
	_ instancemgmt.InstanceDisposer = (*Datasource)(nil)
)

// NewDatasource builds one instance per configured datasource. Grafana
// disposes it and builds a new one whenever the settings change, which also
// discards the cached token.
//
// Invalid settings do not fail construction: the error is kept and reported
// by the health check and by every query, which is clearer than a generic
// "plugin unavailable".
func NewDatasource(ctx context.Context, settings backend.DataSourceInstanceSettings) (instancemgmt.Instance, error) {
	cfg, err := models.LoadPluginSettings(settings)
	if err != nil {
		return &Datasource{configErr: err}, nil
	}
	if err := cfg.Validate(); err != nil {
		return &Datasource{configErr: err}, nil
	}

	opts, err := settings.HTTPClientOptions(ctx)
	if err != nil {
		return nil, fmt.Errorf("http client options: %w", err)
	}
	httpClient, err := httpclient.New(opts)
	if err != nil {
		return nil, fmt.Errorf("http client: %w", err)
	}

	// Left nil (not a typed nil) for "no authentication", so no header is sent.
	var tokens auth.TokenSource
	switch cfg.AuthType {
	case models.AuthClientCredentials:
		tokens = &auth.ClientCredentials{
			TokenURL:     cfg.TokenURL,
			ClientID:     cfg.ClientID,
			ClientSecret: cfg.Secrets.ClientSecret,
			Scope:        cfg.Scope,
			HTTP:         httpClient,
		}
	case models.AuthBearerToken:
		tokens = auth.StaticToken(cfg.Secrets.BearerToken)
	}

	// Live streams stay open indefinitely, so they need a client without the
	// overall request timeout; it shares the transport and TLS settings.
	streamClient := *httpClient
	streamClient.Timeout = 0

	var h historian
	switch cfg.APIType {
	case models.APITimebase:
		h = nativeHistorian{c: &timebase.Client{BaseURL: cfg.URL, HTTP: httpClient, StreamHTTP: &streamClient, Auth: tokens}}
	default:
		h = i3xHistorian{c: &i3x.Client{BaseURL: cfg.URL, HTTP: httpClient, StreamHTTP: &streamClient, Auth: tokens}}
	}

	return &Datasource{historian: h, tokens: tokens, httpClient: httpClient, authType: cfg.AuthType}, nil
}

type Datasource struct {
	historian  historian
	tokens     auth.TokenSource
	httpClient *http.Client
	authType   string
	configErr  error
}

func (d *Datasource) Dispose() {
	if d.httpClient != nil {
		d.httpClient.CloseIdleConnections()
	}
}

// QueryType values sent by the query editor.
const queryTypeHistory = "history"

type queryModel struct {
	QueryType        string   `json:"queryType"`
	ElementIDs       []string `json:"elementIds"`
	IncludeQuality   bool     `json:"includeQuality"`
	BadQualityAsNull bool     `json:"badQualityAsNull"`
}

func (d *Datasource) QueryData(ctx context.Context, req *backend.QueryDataRequest) (*backend.QueryDataResponse, error) {
	response := backend.NewQueryDataResponse()
	for _, q := range req.Queries {
		response.Responses[q.RefID] = d.query(ctx, q)
	}
	return response, nil
}

func (d *Datasource) query(ctx context.Context, query backend.DataQuery) backend.DataResponse {
	if d.configErr != nil {
		return backend.ErrDataResponseWithSource(backend.StatusBadRequest, backend.ErrorSourcePlugin, "datasource is not configured: "+d.configErr.Error())
	}

	var qm queryModel
	if err := json.Unmarshal(query.JSON, &qm); err != nil {
		return backend.ErrDataResponseWithSource(backend.StatusBadRequest, backend.ErrorSourcePlugin, fmt.Sprintf("invalid query: %v", err))
	}
	if qm.QueryType != "" && qm.QueryType != queryTypeHistory {
		return backend.ErrDataResponseWithSource(backend.StatusBadRequest, backend.ErrorSourcePlugin, fmt.Sprintf("unsupported query type %q", qm.QueryType))
	}

	ids := cleanIDs(qm.ElementIDs)
	if len(ids) == 0 {
		return backend.DataResponse{}
	}

	results, err := d.historian.History(ctx, ids, query.TimeRange.From, query.TimeRange.To)
	if err != nil {
		return errorResponse(err)
	}

	opts := series.Options{IncludeQuality: qm.IncludeQuality, BadQualityAsNull: qm.BadQualityAsNull}
	var resp backend.DataResponse
	var failures []string
	for _, r := range results {
		if r.Err != "" {
			failures = append(failures, fmt.Sprintf("%s: %s", r.ElementID, r.Err))
			continue
		}
		resp.Frames = append(resp.Frames, series.Frame(r.ElementID, r.Samples, r.Meta, opts))
	}

	if len(resp.Frames) == 0 && len(failures) > 0 {
		return backend.ErrDataResponseWithSource(backend.StatusBadRequest, backend.ErrorSourceDownstream, strings.Join(failures, "; "))
	}
	if len(failures) > 0 {
		// Show partial failures as a warning on the panel without dropping
		// the series that did load.
		first := resp.Frames[0]
		if first.Meta == nil {
			first.Meta = &data.FrameMeta{}
		}
		for _, f := range failures {
			first.Meta.Notices = append(first.Meta.Notices, data.Notice{Severity: data.NoticeSeverityWarning, Text: f})
		}
	}
	return resp
}

func cleanIDs(in []string) []string {
	seen := make(map[string]bool, len(in))
	out := make([]string, 0, len(in))
	for _, id := range in {
		id = strings.TrimSpace(id)
		if id != "" && !seen[id] {
			seen[id] = true
			out = append(out, id)
		}
	}
	return out
}

func errorResponse(err error) backend.DataResponse {
	var withStatus interface{ HTTPStatus() int }
	if errors.As(err, &withStatus) {
		return backend.ErrDataResponseWithSource(backend.Status(withStatus.HTTPStatus()), backend.ErrorSourceDownstream, err.Error())
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return backend.ErrDataResponseWithSource(backend.StatusTimeout, backend.ErrorSourceDownstream, err.Error())
	}
	return backend.ErrDataResponseWithSource(backend.StatusBadGateway, backend.ErrorSourceDownstream, err.Error())
}

// CheckHealth backs the "Save & test" button. It fetches a token first so a
// credentials problem is reported as such, then calls the API to prove the
// URL and token both work.
func (d *Datasource) CheckHealth(ctx context.Context, _ *backend.CheckHealthRequest) (*backend.CheckHealthResult, error) {
	fail := func(msg string) (*backend.CheckHealthResult, error) {
		return &backend.CheckHealthResult{Status: backend.HealthStatusError, Message: msg}, nil
	}
	if d.configErr != nil {
		return fail(d.configErr.Error())
	}

	if cc, ok := d.tokens.(*auth.ClientCredentials); ok {
		cc.Invalidate()
		if _, err := cc.Token(ctx); err != nil {
			return fail("Could not get a token: " + err.Error())
		}
	}

	msg, err := d.historian.Check(ctx)
	if err != nil {
		if d.tokens != nil {
			return fail("Got a token, but the API call failed: " + err.Error())
		}
		return fail("The API call failed: " + err.Error())
	}
	if d.authType == models.AuthBearerToken {
		msg += ". This token is not renewed; queries and alerts stop when it expires. Use client credentials to renew automatically"
	}
	return &backend.CheckHealthResult{Status: backend.HealthStatusOk, Message: msg}, nil
}

// CallResource serves GET objects, the object list behind the query editor's
// tag picker and tree browser, sorted by ElementId.
func (d *Datasource) CallResource(ctx context.Context, req *backend.CallResourceRequest, sender backend.CallResourceResponseSender) error {
	reply := func(status int, body any) error {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		return sender.Send(&backend.CallResourceResponse{
			Status:  status,
			Headers: map[string][]string{"Content-Type": {"application/json"}},
			Body:    b,
		})
	}

	if req.Path != "objects" || req.Method != http.MethodGet {
		return reply(http.StatusNotFound, map[string]string{"message": "not found"})
	}
	if d.configErr != nil {
		return reply(http.StatusBadRequest, map[string]string{"message": d.configErr.Error()})
	}

	objects, err := d.historian.Objects(ctx)
	if err != nil {
		return reply(http.StatusBadGateway, map[string]string{"message": err.Error()})
	}
	sort.Slice(objects, func(i, j int) bool { return objects[i].ElementID < objects[j].ElementID })
	return reply(http.StatusOK, objects)
}
