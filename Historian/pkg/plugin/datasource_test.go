package plugin

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/grafana/grafana-plugin-sdk-go/backend"
	"github.com/grafana/grafana-plugin-sdk-go/data"
)

// fakeTimebase serves Pulse's /auth/token and the historian's /i3x/v1 API
// from one server, with responses shaped like Historian 2.0.813.
func fakeTimebase(t *testing.T, tokensIssued *atomic.Int32) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("POST /auth/token", func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		w.Header().Set("Content-Type", "application/json")
		if r.Form.Get("client_secret") != "s3cret" {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = fmt.Fprint(w, `{"error":"invalid_client","error_description":"bad secret"}`)
			return
		}
		tokensIssued.Add(1)
		_, _ = fmt.Fprint(w, `{"access_token":"good.jwt.token","token_type":"Bearer","expires_in":3600}`)
	})
	authed := func(h http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			if r.Header.Get("Authorization") != "Bearer good.jwt.token" {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			h(w, r)
		}
	}
	mux.HandleFunc("GET /i3x/v1/info", authed(func(w http.ResponseWriter, r *http.Request) {
		_, _ = fmt.Fprint(w, `{"success":true,"result":{"specVersion":"1.0","serverVersion":"2.0.813","serverName":"Timebase Historian","capabilities":{"query":{"history":true},"subscribe":{"stream":true}}}}`)
	}))
	mux.HandleFunc("GET /i3x/v1/objects", authed(func(w http.ResponseWriter, r *http.Request) {
		_, _ = fmt.Fprint(w, `{"success":true,"result":[
			{"elementId":"default","displayName":"default","typeElementId":"Dataset","isComposition":true},
			{"elementId":"default:b.Tag","displayName":"b.Tag","typeElementId":"Tag","parentId":"default","isComposition":false},
			{"elementId":"default:a.Tag","displayName":"a.Tag","typeElementId":"Tag","parentId":"default","isComposition":false}]}`)
	}))
	mux.HandleFunc("POST /i3x/v1/objects/history", authed(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			ElementIDs []string `json:"elementIds"`
			StartTime  string   `json:"startTime"`
			EndTime    string   `json:"endTime"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.StartTime == "" || req.EndTime == "" {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		var items []string
		for _, id := range req.ElementIDs {
			switch {
			case strings.HasSuffix(id, "missing"):
				items = append(items, fmt.Sprintf(`{"success":false,"elementId":%q,"responseDetail":{"title":"Not Found","status":404,"detail":"Tag or folder 'missing' does not exist in dataset 'default'"}}`, id))
			case id == "default":
				items = append(items, `{"success":true,"elementId":"default","result":{"isComposition":true,"values":[]}}`)
			default:
				items = append(items, fmt.Sprintf(`{"success":true,"elementId":%q,"result":{"isComposition":false,"values":[
					{"value":1.5,"quality":"Good","timestamp":"2026-10-02T10:00:00Z"},
					{"value":2.5,"quality":"Good","timestamp":"2026-10-02T10:00:05Z"}]}}`, id))
			}
		}
		_, _ = fmt.Fprintf(w, `{"success":false,"results":[%s]}`, strings.Join(items, ","))
	}))
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func newTestDatasource(t *testing.T, srv *httptest.Server, secret string) *Datasource {
	t.Helper()
	inst, err := NewDatasource(context.Background(), backend.DataSourceInstanceSettings{
		URL:                     srv.URL + "/i3x/v1/",
		JSONData:                []byte(fmt.Sprintf(`{"apiType":"i3x","authType":"clientCredentials","tokenUrl":%q,"clientId":"Historian"}`, srv.URL+"/auth/token")),
		DecryptedSecureJSONData: map[string]string{"clientSecret": secret},
	})
	if err != nil {
		t.Fatal(err)
	}
	return inst.(*Datasource)
}

func historyQuery(refID string, ids ...string) backend.DataQuery {
	b, _ := json.Marshal(map[string]any{"queryType": "history", "elementIds": ids})
	return backend.DataQuery{
		RefID:     refID,
		JSON:      b,
		TimeRange: backend.TimeRange{From: time.Date(2026, 10, 2, 9, 0, 0, 0, time.UTC), To: time.Date(2026, 10, 2, 11, 0, 0, 0, time.UTC)},
	}
}

func TestQueryDataReturnsOneFramePerTagAndReusesToken(t *testing.T) {
	var issued atomic.Int32
	srv := fakeTimebase(t, &issued)
	ds := newTestDatasource(t, srv, "s3cret")

	resp, err := ds.QueryData(context.Background(), &backend.QueryDataRequest{Queries: []backend.DataQuery{
		historyQuery("A", "default:a.Tag", "default:b.Tag", " default:a.Tag "),
		historyQuery("B", "default:a.Tag"),
	}})
	if err != nil {
		t.Fatal(err)
	}

	a := resp.Responses["A"]
	if a.Error != nil {
		t.Fatal(a.Error)
	}
	if len(a.Frames) != 2 {
		t.Fatalf("got %d frames, want 2 (duplicates removed)", len(a.Frames))
	}
	if a.Frames[0].Meta == nil || a.Frames[0].Meta.Type != data.FrameTypeTimeSeriesMulti || a.Frames[0].Rows() != 2 {
		t.Fatal("want a 2-row timeseries-multi frame")
	}
	if resp.Responses["B"].Error != nil {
		t.Fatal(resp.Responses["B"].Error)
	}
	if issued.Load() != 1 {
		t.Fatalf("issued %d tokens across queries, want 1", issued.Load())
	}
}

func TestQueryDataPartialFailureIsAWarning(t *testing.T) {
	var issued atomic.Int32
	ds := newTestDatasource(t, fakeTimebase(t, &issued), "s3cret")

	resp, _ := ds.QueryData(context.Background(), &backend.QueryDataRequest{Queries: []backend.DataQuery{
		historyQuery("A", "default:a.Tag", "default:missing", "default"),
	}})
	a := resp.Responses["A"]
	if a.Error != nil || len(a.Frames) != 1 {
		t.Fatalf("want 1 frame and no error, got %d frames, %v", len(a.Frames), a.Error)
	}
	if n := len(a.Frames[0].Meta.Notices); n != 2 {
		t.Fatalf("want 2 warnings (missing tag, folder), got %d", n)
	}
}

func TestQueryDataAllFailedIsAnError(t *testing.T) {
	var issued atomic.Int32
	ds := newTestDatasource(t, fakeTimebase(t, &issued), "s3cret")

	resp, _ := ds.QueryData(context.Background(), &backend.QueryDataRequest{Queries: []backend.DataQuery{historyQuery("A", "default:missing")}})
	a := resp.Responses["A"]
	if a.Error == nil || !strings.Contains(a.Error.Error(), "does not exist") || a.ErrorSource != backend.ErrorSourceDownstream {
		t.Fatalf("want a downstream not-found error, got %v (%v)", a.Error, a.ErrorSource)
	}
}

func TestQueryDataEmptyQueryReturnsNothing(t *testing.T) {
	var issued atomic.Int32
	ds := newTestDatasource(t, fakeTimebase(t, &issued), "s3cret")

	resp, _ := ds.QueryData(context.Background(), &backend.QueryDataRequest{Queries: []backend.DataQuery{historyQuery("A")}})
	if a := resp.Responses["A"]; a.Error != nil || len(a.Frames) != 0 || issued.Load() != 0 {
		t.Fatal("an empty query should not call the server")
	}
}

func TestCheckHealth(t *testing.T) {
	var issued atomic.Int32
	srv := fakeTimebase(t, &issued)

	ok, _ := newTestDatasource(t, srv, "s3cret").CheckHealth(context.Background(), &backend.CheckHealthRequest{})
	if ok.Status != backend.HealthStatusOk || !strings.Contains(ok.Message, "Timebase Historian 2.0.813") {
		t.Fatalf("got %v %q", ok.Status, ok.Message)
	}

	bad, _ := newTestDatasource(t, srv, "wrong").CheckHealth(context.Background(), &backend.CheckHealthRequest{})
	if bad.Status != backend.HealthStatusError || !strings.Contains(bad.Message, "Could not get a token") || !strings.Contains(bad.Message, "invalid_client") {
		t.Fatalf("got %v %q", bad.Status, bad.Message)
	}
}

func TestCheckHealthReportsMissingSettings(t *testing.T) {
	inst, _ := NewDatasource(context.Background(), backend.DataSourceInstanceSettings{URL: "https://historian:4512/i3x/v1", JSONData: []byte(`{}`)})
	res, _ := inst.(*Datasource).CheckHealth(context.Background(), &backend.CheckHealthRequest{})
	if res.Status != backend.HealthStatusError || !strings.Contains(res.Message, "token URL is required") {
		t.Fatalf("got %v %q", res.Status, res.Message)
	}
}

type captureSender struct{ resp *backend.CallResourceResponse }

func (c *captureSender) Send(r *backend.CallResourceResponse) error { c.resp = r; return nil }

func TestCallResourceObjectsListsSortedObjectsWithParents(t *testing.T) {
	var issued atomic.Int32
	ds := newTestDatasource(t, fakeTimebase(t, &issued), "s3cret")

	var s captureSender
	if err := ds.CallResource(context.Background(), &backend.CallResourceRequest{Path: "objects", Method: http.MethodGet}, &s); err != nil {
		t.Fatal(err)
	}
	var got []objectOption
	if err := json.Unmarshal(s.resp.Body, &got); err != nil {
		t.Fatal(err)
	}
	if s.resp.Status != 200 || len(got) != 3 {
		t.Fatalf("got %d %+v; want the dataset and both tags", s.resp.Status, got)
	}
	if got[0].ElementID != "default" || !got[0].IsFolder {
		t.Fatalf("first entry %+v; want the dataset, marked as a folder", got[0])
	}
	if got[1].ElementID != "default:a.Tag" || got[1].DisplayName != "a.Tag" || got[1].ParentID != "default" || got[1].IsFolder {
		t.Fatalf("second entry %+v; want tag a with its parent", got[1])
	}
}
