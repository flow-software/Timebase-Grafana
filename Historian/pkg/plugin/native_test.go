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

	"github.com/grafana/grafana-plugin-sdk-go/backend"
	"github.com/grafana/grafana-plugin-sdk-go/data"
)

// fakeNative serves Pulse's /auth/token and the historian's native /api with
// responses shaped like Historian 2.0.813, including its habit of failing a
// whole multi-tag read when one tag is unknown.
func fakeNative(t *testing.T, dataCalls *atomic.Int32) *httptest.Server {
	t.Helper()
	tags := map[string]string{
		"Line1.Speed":   `{"n":"Line1.Speed","d":"Conveyor speed","u":{"1":"m/s"},"t":"System.Double"}`,
		"Dataset.State": `{"n":"Dataset.State","d":"System state","u":{"0":"Recovering","1":"Starting","2":"Running"},"t":"System.Double"}`,
		"Batch.Code":    `{"n":"Batch.Code","t":"System.String"}`,
		"Quiet.Tag":     `{"n":"Quiet.Tag","t":"System.Double"}`,
	}
	samples := map[string]string{
		"Line1.Speed":   `[{"t":"2026-10-05T18:31:59Z","v":1.5,"q":192},{"t":"2026-10-05T18:32:05Z","v":1.75,"q":68}]`,
		"Dataset.State": `[{"t":"2026-10-05T18:32:01Z","v":2,"q":192}]`,
		"Batch.Code":    `[{"t":"2026-10-05T18:32:01Z","v":"0042","q":192}]`,
		// A range with no data: one placeholder dated 0001-01-01, no value.
		"Quiet.Tag": `[{"t":"0001-01-01T00:00:00Z","q":0}]`,
	}

	mux := http.NewServeMux()
	mux.HandleFunc("POST /auth/token", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
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
	mux.HandleFunc("GET /api/datasets", authed(func(w http.ResponseWriter, r *http.Request) {
		_, _ = fmt.Fprint(w, `[{"n":"plant","c":4},{"n":"empty","c":0}]`)
	}))
	mux.HandleFunc("GET /api/datasets/{ds}/tags", authed(func(w http.ResponseWriter, r *http.Request) {
		if r.PathValue("ds") == "empty" {
			_, _ = fmt.Fprint(w, `{"User":[],"System":[]}`)
			return
		}
		_, _ = fmt.Fprintf(w, `{"User":[%s,%s,%s],"System":[%s]}`, tags["Line1.Speed"], tags["Batch.Code"], tags["Quiet.Tag"], tags["Dataset.State"])
	}))
	mux.HandleFunc("GET /api/datasets/{ds}/data", authed(func(w http.ResponseWriter, r *http.Request) {
		dataCalls.Add(1)
		if r.PathValue("ds") != "plant" {
			w.WriteHeader(http.StatusNotFound)
			_, _ = fmt.Fprintf(w, `{"key":"Error.DatasetNotFound","message":"The dataset '%s' could not be found.","errorCode":1}`, r.PathValue("ds"))
			return
		}
		var items []string
		for _, name := range r.URL.Query()["tagname"] {
			def, ok := tags[name]
			if !ok {
				w.WriteHeader(http.StatusNotFound)
				_, _ = fmt.Fprintf(w, `{"key":"Error.TagNotFound","message":"Tag '%s' could not be found.","errorCode":1}`, name)
				return
			}
			items = append(items, fmt.Sprintf(`{"t":%s,"d":%s}`, def, samples[name]))
		}
		_, _ = fmt.Fprintf(w, `{"s":"x","e":"y","tl":[%s]}`, strings.Join(items, ","))
	}))
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func newNativeDatasource(t *testing.T, srv *httptest.Server) *Datasource {
	t.Helper()
	inst, err := NewDatasource(context.Background(), backend.DataSourceInstanceSettings{
		// The /api suffix is tolerated and stripped.
		URL:                     srv.URL + "/api",
		JSONData:                []byte(fmt.Sprintf(`{"apiType":"timebase","authType":"clientCredentials","tokenUrl":%q,"clientId":"Historian"}`, srv.URL+"/auth/token")),
		DecryptedSecureJSONData: map[string]string{"clientSecret": "s3cret"},
	})
	if err != nil {
		t.Fatal(err)
	}
	return inst.(*Datasource)
}

func TestNativeQueryMetadataAndPartialFailure(t *testing.T) {
	var calls atomic.Int32
	ds := newNativeDatasource(t, fakeNative(t, &calls))

	resp, _ := ds.QueryData(context.Background(), &backend.QueryDataRequest{Queries: []backend.DataQuery{
		historyQuery("A", "plant:Line1.Speed", "plant:does.not.exist", "plant:Dataset.State", "plant:Batch.Code", "nowhere:X", "no-colon"),
	}})
	a := resp.Responses["A"]
	if a.Error != nil {
		t.Fatal(a.Error)
	}
	if len(a.Frames) != 3 {
		t.Fatalf("got %d frames, want 3 (speed, state, code)", len(a.Frames))
	}

	speed := a.Frames[0].Fields[1]
	if speed.Config.Unit != "suffix: m/s" || speed.Config.Description != "Conveyor speed" || a.Frames[0].Rows() != 2 {
		t.Fatalf("speed unit %q desc %q rows %d", speed.Config.Unit, speed.Config.Description, a.Frames[0].Rows())
	}
	state := a.Frames[1].Fields[1]
	if len(state.Config.Mappings) != 1 || state.Config.Mappings[0].(data.ValueMapper)["2"].Text != "Running" {
		t.Fatalf("state mappings %+v", state.Config.Mappings)
	}
	code := a.Frames[2].Fields[1]
	if code.Type() != data.FieldTypeNullableString {
		t.Fatalf("declared text tag should be a text series, got %v", code.Type())
	}

	notices := a.Frames[0].Meta.Notices
	if len(notices) != 3 {
		t.Fatalf("want 3 warnings (missing tag, missing dataset, bad id), got %+v", notices)
	}
	joined := fmt.Sprint(notices)
	for _, want := range []string{"'does.not.exist' could not be found", "dataset 'nowhere' could not be found", "no-colon: is not in dataset:tag form"} {
		if !strings.Contains(joined, want) {
			t.Errorf("warnings %q lack %q", joined, want)
		}
	}
}

func TestNativeQueryReadsAllTagsInOneCallWhenTheyExist(t *testing.T) {
	var calls atomic.Int32
	ds := newNativeDatasource(t, fakeNative(t, &calls))

	resp, _ := ds.QueryData(context.Background(), &backend.QueryDataRequest{Queries: []backend.DataQuery{
		historyQuery("A", "plant:Line1.Speed", "plant:Dataset.State"),
	}})
	if a := resp.Responses["A"]; a.Error != nil || len(a.Frames) != 2 {
		t.Fatalf("got %d frames, %v", len(a.Frames), a.Error)
	}
	if calls.Load() != 1 {
		t.Fatalf("made %d data calls, want 1", calls.Load())
	}
}

func TestNativeQueryDropsNoDataPlaceholder(t *testing.T) {
	var calls atomic.Int32
	ds := newNativeDatasource(t, fakeNative(t, &calls))

	resp, _ := ds.QueryData(context.Background(), &backend.QueryDataRequest{Queries: []backend.DataQuery{historyQuery("A", "plant:Quiet.Tag")}})
	a := resp.Responses["A"]
	if a.Error != nil || len(a.Frames) != 1 || a.Frames[0].Rows() != 0 {
		t.Fatalf("want one empty frame, got %d frames, err %v", len(a.Frames), a.Error)
	}
}

func TestNativeObjectsFlagSystemTags(t *testing.T) {
	var calls atomic.Int32
	ds := newNativeDatasource(t, fakeNative(t, &calls))

	var s captureSender
	if err := ds.CallResource(context.Background(), &backend.CallResourceRequest{Path: "objects", Method: http.MethodGet}, &s); err != nil {
		t.Fatal(err)
	}
	var got []objectOption
	if err := json.Unmarshal(s.resp.Body, &got); err != nil {
		t.Fatal(err)
	}
	byID := map[string]objectOption{}
	for _, o := range got {
		byID[o.ElementID] = o
	}
	if len(got) != 6 {
		t.Fatalf("got %d objects, want 2 datasets + 4 tags: %+v", len(got), got)
	}
	if o := byID["plant"]; !o.IsFolder || o.Type != "Dataset" {
		t.Fatalf("dataset entry %+v", o)
	}
	if o := byID["plant:Dataset.State"]; !o.IsSystem || o.ParentID != "plant" {
		t.Fatalf("system tag entry %+v", o)
	}
	if o := byID["plant:Line1.Speed"]; o.IsSystem || o.Unit != "m/s" || o.Description != "Conveyor speed" || o.DisplayName != "Line1.Speed" {
		t.Fatalf("user tag entry %+v", o)
	}
}

func TestNativeCheckHealth(t *testing.T) {
	var calls atomic.Int32
	res, _ := newNativeDatasource(t, fakeNative(t, &calls)).CheckHealth(context.Background(), &backend.CheckHealthRequest{})
	if res.Status != backend.HealthStatusOk || !strings.Contains(res.Message, "native API") || !strings.Contains(res.Message, "2 datasets: empty, plant") {
		t.Fatalf("got %v %q", res.Status, res.Message)
	}
}
