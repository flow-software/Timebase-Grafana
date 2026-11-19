package plugin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/flow-software/Timebase-Grafana/Atlas/pkg/atlas"
	"github.com/flow-software/Timebase-Grafana/Atlas/pkg/auth"
	"github.com/flow-software/Timebase-Grafana/Atlas/pkg/models"
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

// Limits that keep one panel from pulling a whole large model by accident.
const (
	defaultGraphLimit = 2000  // relationships in a whole-domain graph
	maxGraphLimit     = 20000 // ceiling for any user-set limit
	defaultTableLimit = 1000  // instances in an instances table
	maxCypherRows     = 50000 // rows kept from a raw Cypher result
	maxDepth          = 4     // hops around an instance
	resourceListLimit = 20000 // instances read for the editor's pickers
)

// NewDatasource builds one instance per configured datasource. Grafana
// disposes it and builds a new one whenever the settings change, which also
// discards the cached token. Invalid settings do not fail construction: the
// error is reported by the health check and by every query.
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

	return &Datasource{
		client:     &atlas.Client{BaseURL: cfg.URL, HTTP: httpClient, Auth: tokens},
		cfg:        cfg,
		tokens:     tokens,
		httpClient: httpClient,
	}, nil
}

type Datasource struct {
	client     *atlas.Client
	cfg        *models.PluginSettings
	tokens     auth.TokenSource
	httpClient *http.Client
	configErr  error
	trees      treeCache
	kindCache  kindCache
}

func (d *Datasource) Dispose() {
	if d.httpClient != nil {
		d.httpClient.CloseIdleConnections()
	}
}

// Query types sent by the query editor.
const (
	queryGraph     = "graph"
	queryInstances = "instances"
	queryValues    = "values"
	queryCypher    = "cypher"
	queryHistory   = "history"
)

// Output formats for Cypher queries.
const (
	formatAuto  = "auto"
	formatTable = "table"
	formatGraph = "graph"
	// formatSeries splits a long result (a time column, text columns, value
	// columns) into one time series per combination of the text columns.
	formatSeries = "timeseries"
)

type queryModel struct {
	QueryType  string         `json:"queryType"`
	Branch     string         `json:"branch"`
	Domain     string         `json:"domain"`
	InstanceID string         `json:"instanceId"`
	EntityType string         `json:"entityType"`
	Depth      int            `json:"depth"`
	Limit      int            `json:"limit"`
	Cypher     string         `json:"cypher"`
	Parameters map[string]any `json:"parameters"`
	Format     string         `json:"format"`

	IncludeQuality bool `json:"includeQuality"`
	IncludeTime    bool `json:"includeTime"`

	// Values and History: what to read (instance, children, type); Values: how to lay it out.
	Scope  string `json:"scope"`
	Layout string `json:"layout"`

	// History: the series picked in the browse modal. Older queries use
	// Scope/InstanceID/EntityType with Properties instead.
	Selection []historyPick `json:"selection"`

	// History: live properties to read, and raw points or buckets.
	Properties    []string `json:"properties"`
	Aggregation   string   `json:"aggregation"`
	IncludeMinMax bool     `json:"includeMinMax"`

	// Graph and Cypher graph output.
	ColorBy           string `json:"colorBy"`
	RelationshipNodes bool   `json:"relationshipNodes"`
	LineColorBy       string `json:"lineColorBy"`

	// The datasource running the query, for the node links. Not part of the JSON.
	dsUID, dsName string
	// The panel's max data points, for Auto history. Not part of the JSON.
	maxPoints int
}

// badRequest is an error caused by the query itself, not by Atlas.
type badRequest string

func (e badRequest) Error() string { return string(e) }

func (d *Datasource) QueryData(ctx context.Context, req *backend.QueryDataRequest) (*backend.QueryDataResponse, error) {
	response := backend.NewQueryDataResponse()
	var uid, name string
	if s := req.PluginContext.DataSourceInstanceSettings; s != nil {
		uid, name = s.UID, s.Name
	}
	for _, q := range req.Queries {
		response.Responses[q.RefID] = d.query(ctx, q, uid, name)
	}
	return response, nil
}

func (d *Datasource) query(ctx context.Context, query backend.DataQuery, dsUID, dsName string) backend.DataResponse {
	if d.configErr != nil {
		return backend.ErrDataResponseWithSource(backend.StatusBadRequest, backend.ErrorSourcePlugin, "datasource is not configured: "+d.configErr.Error())
	}
	var qm queryModel
	if err := json.Unmarshal(query.JSON, &qm); err != nil {
		return backend.ErrDataResponseWithSource(backend.StatusBadRequest, backend.ErrorSourcePlugin, fmt.Sprintf("invalid query: %v", err))
	}
	qm.dsUID, qm.dsName = dsUID, dsName
	branch, domain := d.world(qm.Branch, qm.Domain)
	if domain == "" {
		return backend.ErrDataResponseWithSource(backend.StatusBadRequest, backend.ErrorSourcePlugin, "pick a domain in the query, or set a default domain on the data source")
	}

	var frames data.Frames
	var err error
	switch qm.QueryType {
	case queryGraph, "":
		frames, err = d.graphQuery(ctx, branch, domain, qm)
	case queryInstances:
		// Saved before Instances and Values were merged.
		qm.Scope = scopeType
		if qm.Layout == "" {
			qm.Layout = layoutColumns
		}
		frames, err = d.valuesQuery(ctx, branch, domain, qm)
	case queryValues:
		frames, err = d.valuesQuery(ctx, branch, domain, qm)
	case queryCypher:
		frames, err = d.cypherQuery(ctx, branch, domain, qm, query.TimeRange)
	case queryHistory:
		qm.maxPoints = int(query.MaxDataPoints)
		frames, err = d.historyQuery(ctx, branch, domain, qm, query.TimeRange.From, query.TimeRange.To)
	default:
		err = badRequest(fmt.Sprintf("unsupported query type %q", qm.QueryType))
	}
	if err != nil {
		return errorResponse(err)
	}
	return backend.DataResponse{Frames: frames}
}

func (d *Datasource) world(branch, domain string) (string, string) {
	branch, domain = strings.TrimSpace(branch), strings.TrimSpace(domain)
	if branch == "" {
		branch = d.cfg.DefaultBranch
	}
	if domain == "" {
		domain = d.cfg.DefaultDomain
	}
	return branch, domain
}

func clampLimit(v, def int) int {
	if v <= 0 {
		return def
	}
	if v > maxGraphLimit {
		return maxGraphLimit
	}
	return v
}

// graphQuery returns Node Graph frames: the whole domain, or the neighbourhood
// of one instance. The neighbourhood is expanded one hop per Cypher call,
// because Atlas returns internal ids for paths and variable-length matches.
func (d *Datasource) graphQuery(ctx context.Context, branch, domain string, qm queryModel) (data.Frames, error) {
	limit := clampLimit(qm.Limit, defaultGraphLimit)
	g := newGraph()
	id := strings.TrimSpace(qm.InstanceID)

	if id == "" {
		res, err := d.client.Query(ctx, branch, domain, "MATCH (a)-[r]->(b) RETURN a, r, b LIMIT "+strconv.Itoa(limit), nil)
		if err != nil {
			return nil, err
		}
		rows, err := decodeRows(res)
		if err != nil {
			return nil, err
		}
		for _, row := range rows {
			for _, v := range row {
				g.add(v)
			}
		}
		frames, err := d.graphFrames(ctx, branch, domain, g, qm)
		if err != nil {
			return nil, err
		}
		if len(rows) >= limit {
			addNotice(frames[1], data.NoticeSeverityWarning, fmt.Sprintf("Showing the first %d relationships. Raise the limit or start from an instance to see the rest.", limit))
		}
		return frames, nil
	}

	if strings.HasPrefix(id, "rel:") {
		return nil, badRequest("that is a relationship, not an instance; pick one of the entities it connects")
	}
	// Several start instances (a multi-value variable arrives comma-separated)
	// give one graph around all of them.
	var starts []string
	for _, s := range strings.Split(id, ",") {
		if s = strings.TrimSpace(s); s != "" {
			starts = append(starts, s)
		}
	}
	start, err := d.client.Query(ctx, branch, domain, "MATCH (a) WHERE a.entityId IN $ids RETURN a", map[string]any{"ids": starts})
	if err != nil {
		return nil, err
	}
	rows, err := decodeRows(start)
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, badRequest(fmt.Sprintf("instance %q not found in %s/%s", id, branch, domain))
	}
	for _, r := range rows {
		g.add(r[0])
	}

	depth := qm.Depth
	if depth <= 0 {
		depth = 1
	}
	if depth > maxDepth {
		depth = maxDepth
	}
	frontier := starts
	truncated := false
	for hop := 0; hop < depth && len(frontier) > 0; hop++ {
		res, err := d.client.Query(ctx, branch, domain,
			"MATCH (a)-[r]-(b) WHERE a.entityId IN $ids RETURN r, b LIMIT "+strconv.Itoa(limit),
			map[string]any{"ids": frontier})
		if err != nil {
			return nil, err
		}
		rows, err := decodeRows(res)
		if err != nil {
			return nil, err
		}
		if len(rows) >= limit {
			truncated = true
		}
		before := len(g.nodes)
		for _, row := range rows {
			for _, v := range row {
				g.add(v)
			}
		}
		frontier = frontier[:0]
		for _, n := range g.nodes[before:] {
			frontier = append(frontier, str(n["entityId"]))
		}
		if len(g.edges) >= limit {
			truncated = true
			break
		}
	}
	frames, err := d.graphFrames(ctx, branch, domain, g, qm)
	if err != nil {
		return nil, err
	}
	if truncated {
		addNotice(frames[1], data.NoticeSeverityWarning, fmt.Sprintf("Stopped at %d relationships. Lower the depth or raise the limit.", limit))
	}
	return frames, nil
}

// Entity type names are put into the Cypher text as a label, so only plain
// identifiers are accepted.
var labelPattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// graphFrames styles the graph. Edge styling needs to know which
// relationship types are containment, so the ontology's relationship types
// are read once per query.
func (d *Datasource) graphFrames(ctx context.Context, branch, domain string, g *graph, qm queryModel) (data.Frames, error) {
	opts := graphOptions{ColorBy: qm.ColorBy, RelationshipNodes: qm.RelationshipNodes, Containment: map[string]bool{}}
	if len(g.edges) > 0 {
		types, err := d.client.RelationshipTypes(ctx, branch, domain)
		if err != nil {
			return nil, err
		}
		for _, t := range types {
			if t.IsContainment {
				opts.Containment[t.URI] = true
			}
		}
		if qm.LineColorBy == linesByType {
			opts.LineColors = map[string]string{}
			for _, rc := range relationshipColors(types) {
				opts.LineColors[rc.URI] = rc.Color
			}
		}
	}
	frames := g.frames(opts)
	addNodeLinks(frames[0], branch, domain, qm)
	return frames, nil
}

// addNodeLinks puts "explore from here" links on graph nodes. The Node Graph
// panel shows them when a node is clicked; each opens Explore with a new
// query for that node. Relationship-name nodes get no links: their ids are
// not instances.
func addNodeLinks(nodes *data.Frame, branch, domain string, qm queryModel) {
	if qm.dsUID == "" {
		return
	}
	idx := -1
	for i, f := range nodes.Fields {
		if f.Name == "id" {
			idx = i
		}
	}
	if idx < 0 {
		return
	}
	link := func(title string, q map[string]any) data.DataLink {
		q["branch"], q["domain"] = branch, domain
		q["instanceId"] = "${__value.raw}"
		return data.DataLink{Title: title, Internal: &data.InternalDataLink{Query: q, DatasourceUID: qm.dsUID, DatasourceName: qm.dsName}}
	}
	style := func(q map[string]any) map[string]any {
		if qm.ColorBy != "" {
			q["colorBy"] = qm.ColorBy
		}
		if qm.LineColorBy != "" {
			q["lineColorBy"] = qm.LineColorBy
		}
		q["relationshipNodes"] = qm.RelationshipNodes
		return q
	}
	links := []data.DataLink{
		link("Graph around this instance", style(map[string]any{"queryType": queryGraph, "depth": 1})),
		link("Values of this instance", map[string]any{"queryType": queryValues, "scope": scopeInstance, "layout": layoutRows}),
		link("Values of this instance and what it contains", map[string]any{"queryType": queryValues, "scope": scopeChildren, "layout": layoutRows}),
	}
	f := nodes.Fields[idx]
	if f.Config == nil {
		f.Config = &data.FieldConfig{}
	}
	f.Config.Links = links
}

func (d *Datasource) cypherQuery(ctx context.Context, branch, domain string, qm queryModel, tr backend.TimeRange) (data.Frames, error) {
	text := strings.TrimSpace(qm.Cypher)
	if text == "" {
		return nil, badRequest("enter a Cypher query")
	}
	res, err := d.client.Query(ctx, branch, domain, text, withTimeRange(text, qm.Parameters, tr))
	if err != nil {
		return nil, err
	}
	rows, err := decodeRows(res)
	if err != nil {
		return nil, err
	}
	truncated := false
	if len(rows) > maxCypherRows {
		rows, truncated = rows[:maxCypherRows], true
	}

	format := qm.Format
	if format == "" || format == formatAuto {
		format = formatTable
		if hasGraphCells(rows) {
			format = formatGraph
		}
	}

	var frames data.Frames
	if format == formatGraph {
		g := newGraph()
		for _, row := range rows {
			for _, v := range row {
				g.add(v)
			}
		}
		if frames, err = d.graphFrames(ctx, branch, domain, g, qm); err != nil {
			return nil, err
		}
	} else {
		frames = data.Frames{cypherTable("result", res.Columns, rows)}
		if format == formatSeries {
			if frames, err = splitSeries(frames[0]); err != nil {
				return nil, err
			}
		}
	}
	if truncated {
		addNotice(frames[0], data.NoticeSeverityWarning, fmt.Sprintf("Only the first %d rows are shown. Add a LIMIT to the query.", maxCypherRows))
	}
	return frames, nil
}

func addNotice(f *data.Frame, sev data.NoticeSeverity, text string) {
	if f.Meta == nil {
		f.Meta = &data.FrameMeta{}
	}
	f.Meta.Notices = append(f.Meta.Notices, data.Notice{Severity: sev, Text: text})
}

func errorResponse(err error) backend.DataResponse {
	var bad badRequest
	if errors.As(err, &bad) {
		return backend.ErrDataResponseWithSource(backend.StatusBadRequest, backend.ErrorSourcePlugin, err.Error())
	}
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
// credentials problem is reported as such, then lists the compiled worlds.
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
	worlds, err := d.client.Worlds(ctx)
	if err != nil {
		if d.tokens != nil {
			return fail("Got a token, but the Atlas API call failed: " + err.Error())
		}
		return fail("The Atlas API call failed: " + err.Error())
	}
	if len(worlds) == 0 {
		return fail("Connected, but Atlas has no compiled worlds yet. Compile a domain in Atlas first.")
	}
	var names []string
	found := d.cfg.DefaultDomain == ""
	for _, w := range worlds {
		if w.Branch == d.cfg.DefaultBranch {
			names = append(names, w.Domain)
			if w.Domain == d.cfg.DefaultDomain {
				found = true
			}
		}
	}
	if !found {
		return fail(fmt.Sprintf("Connected, but domain %q is not compiled on branch %q. Compiled there: %s", d.cfg.DefaultDomain, d.cfg.DefaultBranch, strings.Join(names, ", ")))
	}
	msg := fmt.Sprintf("Connected to Atlas: %d compiled worlds", len(worlds))
	if len(names) > 0 {
		msg += fmt.Sprintf(". Domains on %s: %s", d.cfg.DefaultBranch, strings.Join(names, ", "))
	}
	if d.cfg.AuthType == models.AuthBearerToken {
		msg += ". This token is not renewed; queries stop when it expires. Use client credentials to renew automatically"
	}
	return &backend.CheckHealthResult{Status: backend.HealthStatusOk, Message: msg}, nil
}

// TypeOption is one entity type that has instances, for the editor's picker.
type TypeOption struct {
	URI   string `json:"uri"`
	Name  string `json:"name"`
	Count int    `json:"count"`
}

// InstanceOption is one instance, for the editor's picker.
type InstanceOption struct {
	ID   string `json:"id"`
	Type string `json:"type"`
}

// CallResource serves the query editor's pickers:
//
//	GET worlds
//	GET types?branch=&domain=
//	GET instances?branch=&domain=&type=
//	GET tree?branch=&domain=[&refresh=1]
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
	fail := func(err error) error {
		status := http.StatusBadGateway
		var withStatus interface{ HTTPStatus() int }
		if errors.As(err, &withStatus) {
			status = withStatus.HTTPStatus()
		}
		return reply(status, map[string]string{"message": err.Error()})
	}

	if req.Method != http.MethodGet {
		return reply(http.StatusMethodNotAllowed, map[string]string{"message": "only GET is supported"})
	}
	if d.configErr != nil {
		return reply(http.StatusBadRequest, map[string]string{"message": d.configErr.Error()})
	}

	params := map[string]string{}
	if _, q, ok := strings.Cut(req.URL, "?"); ok {
		for _, kv := range strings.Split(q, "&") {
			k, v, _ := strings.Cut(kv, "=")
			params[k] = unescape(v)
		}
	}
	branch, domain := d.world(params["branch"], params["domain"])

	switch req.Path {
	case "worlds":
		worlds, err := d.client.Worlds(ctx)
		if err != nil {
			return fail(err)
		}
		return reply(http.StatusOK, worlds)

	case "types":
		if domain == "" {
			return reply(http.StatusOK, []TypeOption{})
		}
		items, _, err := d.client.Instances(ctx, branch, domain, "", resourceListLimit)
		if err != nil {
			return fail(err)
		}
		counts := map[string]int{}
		for _, it := range items {
			counts[it.TypeURI]++
		}
		out := make([]TypeOption, 0, len(counts))
		for uri, n := range counts {
			out = append(out, TypeOption{URI: uri, Name: shortName(uri), Count: n})
		}
		sort.Slice(out, func(i, j int) bool { return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name) })
		return reply(http.StatusOK, out)

	case "relationship-types":
		if domain == "" {
			return reply(http.StatusOK, []RelationshipColor{})
		}
		types, err := d.client.RelationshipTypes(ctx, branch, domain)
		if err != nil {
			return fail(err)
		}
		return reply(http.StatusOK, relationshipColors(types))

	case "tree":
		if domain == "" {
			return reply(http.StatusOK, []TreeNode{})
		}
		nodes, err := d.trees.get(branch+"/"+domain, params["refresh"] == "1", func() ([]TreeNode, error) {
			return d.instanceTree(ctx, branch, domain)
		})
		if err != nil {
			return fail(err)
		}
		return reply(http.StatusOK, nodes)

	case "instances":
		if domain == "" {
			return reply(http.StatusOK, []InstanceOption{})
		}
		items, _, err := d.client.Instances(ctx, branch, domain, params["type"], resourceListLimit)
		if err != nil {
			return fail(err)
		}
		out := make([]InstanceOption, len(items))
		for i, it := range items {
			out[i] = InstanceOption{ID: it.ElementID, Type: shortName(it.TypeURI)}
		}
		sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
		return reply(http.StatusOK, out)
	}
	return reply(http.StatusNotFound, map[string]string{"message": "not found"})
}

func unescape(s string) string {
	if v, err := url.QueryUnescape(s); err == nil {
		return v
	}
	return s
}

// withTimeRange fills $from and $to from the panel's time range when the
// Cypher uses them and no dashboard variable supplied them, so hand-written
// history queries follow the time picker.
func withTimeRange(cypher string, params map[string]any, tr backend.TimeRange) map[string]any {
	out := make(map[string]any, len(params)+2)
	for k, v := range params {
		out[k] = v
	}
	for name, t := range map[string]time.Time{"from": tr.From, "to": tr.To} {
		if _, set := out[name]; set || t.IsZero() {
			continue
		}
		if usesParam(cypher, name) {
			out[name] = t.UTC().Format(time.RFC3339Nano)
		}
	}
	// $bucket: the relative() unit the History builder would use for this
	// range, so a Cypher history query also buckets by the time picker.
	if _, set := out["bucket"]; !set && usesParam(cypher, "bucket") && !tr.From.IsZero() {
		agg := autoAggregation(tr.From, tr.To)
		if agg == aggRaw {
			agg = aggMinute
		}
		out["bucket"] = bucketUnit[agg]
	}
	return out
}

// usesParam reports whether $name appears as a whole parameter name.
func usesParam(cypher, name string) bool {
	for i := 0; ; {
		j := strings.Index(cypher[i:], "$"+name)
		if j < 0 {
			return false
		}
		end := i + j + 1 + len(name)
		if end >= len(cypher) || !isIdentChar(cypher[end]) {
			return true
		}
		i = end
	}
}

func isIdentChar(c byte) bool {
	return c == '_' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9')
}
