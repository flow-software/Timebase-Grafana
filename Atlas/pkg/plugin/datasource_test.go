package plugin

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/grafana/grafana-plugin-sdk-go/backend"
	"github.com/grafana/grafana-plugin-sdk-go/data"
)

func fixture(t *testing.T, name string) string {
	b, err := os.ReadFile("testdata/" + name)
	if err != nil {
		panic(err) // t may be nil when called from the fake server
	}
	return string(b)
}

// fakeAtlas answers the routes the plugin uses. Cypher requests are recorded
// and answered by the first handler whose key is contained in the query text.
type fakeAtlas struct {
	cypher  map[string]string
	queries []map[string]any
}

func (f *fakeAtlas) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	p := r.URL.Path
	switch {
	case p == "/api/v1/worlds":
		_, _ = io.WriteString(w, `{"items":[{"branch":"main","domain":"cesmii-workshop","instanceCount":103},{"branch":"main","domain":"chamdor"}]}`)
	case strings.HasSuffix(p, "/relationship-types"):
		_, _ = io.WriteString(w, `[{"uri":"urn:x:contains","label":"contains","isContainment":true},{"uri":"urn:x:targets","label":"targets","isContainment":false}]`)
	case strings.HasSuffix(p, "/instances"):
		_, _ = io.WriteString(w, `{"items":[{"elementId":"CNC02","typeUri":"urn:x:CNCMachine"},{"elementId":"CNC01","typeUri":"urn:x:CNCMachine"},{"elementId":"wo-1","typeUri":"urn:x:WorkOrder"}],"totalCount":3,"page":1,"pageSize":500}`)
	case strings.HasSuffix(p, "/queries"):
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		f.queries = append(f.queries, body)
		text, _ := body["cypher"].(string)
		for k, v := range f.cypher {
			if strings.Contains(text, k) {
				_, _ = io.WriteString(w, v)
				return
			}
		}
		w.Header().Set("Content-Type", "application/problem+json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = io.WriteString(w, `{"title":"Invalid Request","status":400,"detail":"Unknown function 'foo'"}`)
	default:
		w.WriteHeader(http.StatusNotFound)
	}
}

func newTestDS(t *testing.T, f *fakeAtlas, jsonData string) *Datasource {
	t.Helper()
	srv := httptest.NewServer(f)
	t.Cleanup(srv.Close)
	inst, err := NewDatasource(context.Background(), backend.DataSourceInstanceSettings{URL: srv.URL, JSONData: []byte(jsonData)})
	if err != nil {
		t.Fatal(err)
	}
	ds := inst.(*Datasource)
	if ds.configErr != nil {
		t.Fatal(ds.configErr)
	}
	return ds
}

func run(t *testing.T, ds *Datasource, q string) backend.DataResponse {
	t.Helper()
	resp, err := ds.QueryData(context.Background(), &backend.QueryDataRequest{Queries: []backend.DataQuery{{RefID: "A", JSON: []byte(q)}}})
	if err != nil {
		t.Fatal(err)
	}
	return resp.Responses["A"]
}

const noAuth = `{"authType":"none","defaultDomain":"cesmii-workshop"}`

func strings_(f *data.Field) []string {
	out := make([]string, f.Len())
	for i := range out {
		switch v := f.At(i).(type) {
		case string:
			out[i] = v
		case *string:
			if v != nil {
				out[i] = *v
			}
		}
	}
	return out
}

func TestGraphAroundInstance(t *testing.T) {
	f := &fakeAtlas{cypher: map[string]string{
		"a.entityId = $id": `{"columns":["a"],"data":[[{"entityId":"CNC02","typeId":"urn:x:CNCMachine","entityLabel":"CNC02"}]]}`,
		"IN $ids":          fixture(t, "graph_cnc02.json"),
	}}
	ds := newTestDS(t, f, noAuth)
	r := run(t, ds, `{"queryType":"graph","instanceId":"CNC02","depth":1}`)
	if r.Error != nil {
		t.Fatal(r.Error)
	}
	if len(r.Frames) != 2 || r.Frames[0].Name != "nodes" || r.Frames[1].Name != "edges" {
		t.Fatalf("want nodes+edges frames, got %v", r.Frames)
	}
	nodes, edges := r.Frames[0], r.Frames[1]
	ids := strings_(nodes.Fields[0])
	want := []string{"CNC02", "CNC02-MachineInformation", "CNC02-Channel"}
	if strings.Join(ids, ",") != strings.Join(want, ",") {
		t.Fatalf("nodes = %v, want %v", ids, want)
	}
	if edges.Rows() != 2 {
		t.Fatalf("edges = %d, want 2", edges.Rows())
	}
	if got := strings_(edges.Fields[3]); got[0] != "cncBaseContainsMachineInformation" {
		t.Fatalf("edge mainstat = %v", got)
	}
	// Title falls back to the id when Atlas has no label.
	if got := strings_(nodes.Fields[1]); got[1] != "Machine Information" {
		t.Fatalf("titles = %v", got)
	}
	// The hop query must pass ids as a parameter, never in the query text.
	last := f.queries[len(f.queries)-1]
	if strings.Contains(last["cypher"].(string), "CNC02") {
		t.Fatalf("instance id leaked into Cypher text: %v", last["cypher"])
	}
}

func TestGraphUnknownInstance(t *testing.T) {
	f := &fakeAtlas{cypher: map[string]string{"a.entityId = $id": `{"columns":["a"],"data":[]}`}}
	r := run(t, newTestDS(t, f, noAuth), `{"queryType":"graph","instanceId":"nope"}`)
	if r.Error == nil || !strings.Contains(r.Error.Error(), `"nope" not found`) {
		t.Fatalf("want not-found error, got %v", r.Error)
	}
}

func TestInstancesTable(t *testing.T) {
	f := &fakeAtlas{cypher: map[string]string{"MATCH (n:CNCProduction)": fixture(t, "nodes_cncproduction.json")}}
	r := run(t, newTestDS(t, f, noAuth), `{"queryType":"instances","entityType":"urn:atlas:ontology:cesmii-workshop:CNCProduction","includeQuality":true}`)
	if r.Error != nil {
		t.Fatal(r.Error)
	}
	fr := r.Frames[0]
	if fr.Rows() != 2 {
		t.Fatalf("rows = %d", fr.Rows())
	}
	_, idx := fr.FieldByName("FeedRate")
	if idx < 0 {
		t.Fatal("no FeedRate column")
	}
	if fr.Fields[idx].Type() != data.FieldTypeNullableFloat64 {
		t.Fatalf("FeedRate type = %v, want number", fr.Fields[idx].Type())
	}
	if _, q := fr.FieldByName("FeedRate quality"); q < 0 {
		t.Fatal("no quality column")
	}
	if _, j := fr.FieldByName("JobNumber"); j < 0 || fr.Fields[j].Type() != data.FieldTypeNullableString {
		t.Fatal("JobNumber should be text")
	}
}

func TestInstancesRejectsUnsafeType(t *testing.T) {
	r := run(t, newTestDS(t, &fakeAtlas{}, noAuth), `{"queryType":"instances","entityType":"X) DETACH DELETE n //"}`)
	if r.Error == nil || r.Status != backend.StatusBadRequest {
		t.Fatalf("want 400, got %v %v", r.Status, r.Error)
	}
}

// cnc01 is CNC01 as Cypher returns it: no live values, but MaintainX
// compounds that /values leaves out.
const cnc01 = `{"entityId":"CNC01","typeId":"urn:x:CNCMachine","entityLabel":"CNC01","LiveAsset":{"Timestamp":"2026-11-06T10:38:08.961Z","MxAssetName":"CNC01","MxLocationId":6380808},"LiveOpenWorkOrders":null}`
const cnc01Prod = `{"entityId":"CNC01-Production","typeId":"urn:x:CNCProduction","entityLabel":"Production","FeedRate":{"Timestamp":"2026-11-16T09:15:31Z","Value":12.5,"Quality":192},"AlarmCode":{"Timestamp":"2026-11-16T03:32:31Z","Value":null,"Quality":64}}`

func TestValuesFlattensCompounds(t *testing.T) {
	f := &fakeAtlas{cypher: map[string]string{"IN $ids": `{"columns":["n"],"data":[[` + cnc01 + `]]}`}}
	r := run(t, newTestDS(t, f, noAuth), `{"queryType":"values","instanceId":"CNC01"}`)
	if r.Error != nil {
		t.Fatal(r.Error)
	}
	fr := r.Frames[0]
	props := strings_(fr.Fields[0])
	want := []string{"LiveAsset.MxAssetName", "LiveAsset.MxLocationId", "LiveOpenWorkOrders"}
	if strings.Join(props, ",") != strings.Join(want, ",") {
		t.Fatalf("properties = %v, want %v", props, want)
	}
	// The compound's Timestamp becomes the time of its parts.
	if tm := fr.Fields[4].At(0); tm == nil {
		t.Fatal("LiveAsset part has no time")
	}
}

func TestValuesChildrenScope(t *testing.T) {
	f := &fakeAtlas{cypher: map[string]string{
		"IN $ids":            `{"columns":["n"],"data":[[` + cnc01Prod + `],[` + cnc01 + `]]}`,
		"MATCH (n) RETURN n": `{"columns":["n"],"data":[]}`,
		"RETURN r.Source":    `{"columns":["r.Source","r.Target","type(r)"],"data":[["CNC01","CNC01-Production","urn:x:contains"]]}`,
	}}
	// The fake instance list has CNC01, CNC02 and wo-1; add the child.
	ds := newTestDS(t, f, noAuth)
	ds.trees.entries = map[string]treeEntry{"main/cesmii-workshop": {at: time.Now(), nodes: []TreeNode{
		{ID: "CNC01", Type: "CNCMachine"}, {ID: "CNC01-Production", Type: "CNCProduction", ParentID: "CNC01"}, {ID: "CNC02", Type: "CNCMachine"},
	}}}
	r := run(t, ds, `{"queryType":"values","scope":"children","instanceId":"CNC01"}`)
	if r.Error != nil {
		t.Fatal(r.Error)
	}
	fr := r.Frames[0]
	inst := strings_(fr.Fields[1])
	// Parent first, then its child, whatever order Atlas returned them in.
	if inst[0] != "CNC01" || inst[len(inst)-1] != "CNC01-Production" {
		t.Fatalf("instance order = %v", inst)
	}
	_, qi := fr.FieldByName("Quality")
	quality := strings_(fr.Fields[qi])
	props := strings_(fr.Fields[2])
	for k, p := range props {
		if p == "AlarmCode" && quality[k] != "No data" {
			t.Fatalf("AlarmCode quality = %q", quality[k])
		}
		if p == "FeedRate" && quality[k] != "Good" {
			t.Fatalf("FeedRate quality = %q", quality[k])
		}
	}
}

func TestLegacyInstancesQueryIsWideTable(t *testing.T) {
	f := &fakeAtlas{cypher: map[string]string{"MATCH (n:CNCMachine)": `{"columns":["n"],"data":[[` + cnc01 + `]]}`}}
	r := run(t, newTestDS(t, f, noAuth), `{"queryType":"instances","entityType":"CNCMachine"}`)
	if r.Error != nil {
		t.Fatal(r.Error)
	}
	if _, i := r.Frames[0].FieldByName("LiveAsset.MxAssetName"); i < 0 {
		t.Fatal("compound should be split into columns, not JSON")
	}
}

func TestGraphStyling(t *testing.T) {
	f := &fakeAtlas{cypher: map[string]string{"RETURN a, r, b": fixture(t, "graph_cnc02.json")}}
	ds := newTestDS(t, f, noAuth)

	r := run(t, ds, `{"queryType":"cypher","cypher":"MATCH (a)-[r]->(b) RETURN a, r, b"}`)
	if r.Error != nil {
		t.Fatal(r.Error)
	}
	nodes, edges := r.Frames[0], r.Frames[1]
	// Quality rings by default; CNC02 in the fixture has no live values.
	_, si := nodes.FieldByName("arc__static")
	if si < 0 || nodes.Fields[si].At(0).(float64) != 1 {
		t.Fatal("node without live values should have a full 'no live values' ring")
	}
	if _, di := nodes.FieldByName("detail__LiveAsset.MxAssetName"); di < 0 {
		t.Fatal("node values should be in detail fields")
	}
	// Neither fixture relationship type is containment in the fake ontology.
	_, ci := edges.FieldByName("color")
	if edges.Fields[ci].At(0).(string) != colRelation {
		t.Fatal("non-containment edge colour")
	}

	r = run(t, ds, `{"queryType":"cypher","cypher":"MATCH (a)-[r]->(b) RETURN a, r, b","relationshipNodes":true,"colorBy":"type"}`)
	nodes, edges = r.Frames[0], r.Frames[1]
	if nodes.Rows() != 5 || edges.Rows() != 4 {
		t.Fatalf("relationship nodes: %d nodes, %d edges; want 5 and 4", nodes.Rows(), edges.Rows())
	}
	if _, ci := nodes.FieldByName("color"); ci < 0 {
		t.Fatal("colour by type needs a color field")
	}
}

func TestQualityText(t *testing.T) {
	q := func(c int64) *int64 { return &c }
	cases := []struct {
		code  *int64
		value any
		want  string
	}{{q(192), 1.0, "Good"}, {q(64), 1.0, "Uncertain"}, {q(0), 1.0, "Bad"}, {q(64), nil, "No data"}, {nil, "x", ""}}
	for _, c := range cases {
		if got := qualityText(c.code, c.value); got != c.want {
			t.Errorf("qualityText(%v, %v) = %q, want %q", c.code, c.value, got, c.want)
		}
	}
}

func TestCypherAutoFormat(t *testing.T) {
	f := &fakeAtlas{cypher: map[string]string{
		"RETURN a, r, b":    fixture(t, "graph_cnc02.json"),
		"RETURN a.entityId": `{"columns":["a.entityId","a.FeedRate"],"data":[["CNC01-Production",{"Timestamp":"2026-11-16T09:15:31Z","Value":12.5,"Quality":192}]]}`,
	}}
	ds := newTestDS(t, f, noAuth)

	r := run(t, ds, `{"queryType":"cypher","cypher":"MATCH (a)-[r]->(b) RETURN a, r, b"}`)
	if r.Error != nil || len(r.Frames) != 2 || r.Frames[0].Name != "nodes" {
		t.Fatalf("relationships should give a graph: %v %v", r.Error, r.Frames)
	}

	r = run(t, ds, `{"queryType":"cypher","cypher":"MATCH (a) RETURN a.entityId, a.FeedRate","parameters":{"x":1}}`)
	if r.Error != nil || len(r.Frames) != 1 {
		t.Fatalf("want one table: %v", r.Error)
	}
	if r.Frames[0].Fields[1].Type() != data.FieldTypeNullableFloat64 {
		t.Fatal("live value should become its number")
	}
	if p := f.queries[len(f.queries)-1]["parameters"].(map[string]any); p["x"] != float64(1) {
		t.Fatalf("parameters not forwarded: %v", p)
	}
}

func TestCypherErrorPassesAtlasDetail(t *testing.T) {
	r := run(t, newTestDS(t, &fakeAtlas{}, noAuth), `{"queryType":"cypher","cypher":"RETURN foo()"}`)
	if r.Error == nil || !strings.Contains(r.Error.Error(), "Unknown function 'foo'") || r.Status != backend.StatusBadRequest {
		t.Fatalf("want Atlas detail with 400, got %v %v", r.Status, r.Error)
	}
}

func TestNoDomain(t *testing.T) {
	r := run(t, newTestDS(t, &fakeAtlas{}, `{"authType":"none"}`), `{"queryType":"graph"}`)
	if r.Error == nil || !strings.Contains(r.Error.Error(), "domain") {
		t.Fatalf("want domain error, got %v", r.Error)
	}
}

func TestHealth(t *testing.T) {
	res, _ := newTestDS(t, &fakeAtlas{}, noAuth).CheckHealth(context.Background(), nil)
	if res.Status != backend.HealthStatusOk || !strings.Contains(res.Message, "cesmii-workshop, chamdor") {
		t.Fatalf("health = %v %q", res.Status, res.Message)
	}
	res, _ = newTestDS(t, &fakeAtlas{}, `{"authType":"none","defaultDomain":"nope"}`).CheckHealth(context.Background(), nil)
	if res.Status != backend.HealthStatusError || !strings.Contains(res.Message, `"nope" is not compiled`) {
		t.Fatalf("health = %v %q", res.Status, res.Message)
	}
}

type captureSender struct{ resp *backend.CallResourceResponse }

func (c *captureSender) Send(r *backend.CallResourceResponse) error { c.resp = r; return nil }

func TestTypesResource(t *testing.T) {
	ds := newTestDS(t, &fakeAtlas{}, noAuth)
	var s captureSender
	if err := ds.CallResource(context.Background(), &backend.CallResourceRequest{Path: "types", Method: "GET", URL: "types?domain=cesmii-workshop"}, &s); err != nil {
		t.Fatal(err)
	}
	var out []TypeOption
	_ = json.Unmarshal(s.resp.Body, &out)
	if len(out) != 2 || out[0].Name != "CNCMachine" || out[0].Count != 2 {
		t.Fatalf("types = %+v", out)
	}
}

func TestTreeResource(t *testing.T) {
	f := &fakeAtlas{cypher: map[string]string{
		"MATCH (n) RETURN n": `{"columns":["n"],"data":[[{"entityId":"CNC01","typeId":"urn:x:CNCMachine","entityLabel":"Machine 1"}],[{"entityId":"wo-1","typeId":"urn:x:WorkOrder","entityLabel":null}]]}`,
		"RETURN r.Source":    `{"columns":["r.Source","r.Target","type(r)"],"data":[["CNC02","wo-1","urn:x:targets"],["CNC01","wo-1","urn:x:contains"],["CNC01","gone","urn:x:contains"]]}`,
	}}
	ds := newTestDS(t, f, noAuth)
	var s captureSender
	if err := ds.CallResource(context.Background(), &backend.CallResourceRequest{Path: "tree", Method: "GET", URL: "tree?domain=cesmii-workshop"}, &s); err != nil {
		t.Fatal(err)
	}
	var nodes []TreeNode
	if err := json.Unmarshal(s.resp.Body, &nodes); err != nil {
		t.Fatal(err, string(s.resp.Body))
	}
	byID := map[string]TreeNode{}
	for _, n := range nodes {
		byID[n.ID] = n
	}
	if len(nodes) != 3 {
		t.Fatalf("nodes = %+v", nodes)
	}
	// Only the containment edge nests; the "targets" edge is ignored.
	if byID["wo-1"].ParentID != "CNC01" || byID["CNC01"].ParentID != "" || byID["CNC02"].ParentID != "" {
		t.Fatalf("parents wrong: %+v", nodes)
	}
	if byID["CNC01"].Label != "Machine 1" || byID["wo-1"].Label != "" {
		t.Fatalf("labels wrong: %+v", nodes)
	}
	// Sorted by label, falling back to id: CNC02, Machine 1, wo-1.
	if nodes[0].ID != "CNC02" || nodes[1].ID != "CNC01" {
		t.Fatalf("order = %+v", nodes)
	}
}

func TestBreakCycles(t *testing.T) {
	parent := map[string]string{"a": "b", "b": "a", "c": "a"}
	breakCycles(parent)
	for id := range parent {
		seen := map[string]bool{id: true}
		for p := parent[id]; p != ""; p = parent[p] {
			if seen[p] {
				t.Fatalf("cycle left at %s: %v", id, parent)
			}
			seen[p] = true
		}
	}
}

func TestLinesByRelationshipType(t *testing.T) {
	f := &fakeAtlas{cypher: map[string]string{"RETURN a, r, b": fixture(t, "graph_cnc02.json")}}
	ds := newTestDS(t, f, noAuth)
	r := run(t, ds, `{"queryType":"cypher","cypher":"MATCH (a)-[r]->(b) RETURN a, r, b","lineColorBy":"type"}`)
	if r.Error != nil {
		t.Fatal(r.Error)
	}
	// The fixture's relationship types are not in the fake ontology, so they
	// fall back to the kind colour; the ontology's own types get palette colours.
	_, ci := r.Frames[1].FieldByName("color")
	if r.Frames[1].Fields[ci].At(0).(string) != colRelation {
		t.Fatal("unknown relationship type should fall back to the kind colour")
	}
	var s captureSender
	if err := ds.CallResource(context.Background(), &backend.CallResourceRequest{Path: "relationship-types", Method: "GET", URL: "relationship-types?domain=x"}, &s); err != nil {
		t.Fatal(err)
	}
	var rc []RelationshipColor
	_ = json.Unmarshal(s.resp.Body, &rc)
	// Sorted by name: contains, targets.
	if len(rc) != 2 || rc[0].Name != "contains" || rc[0].Color != linePalette[0] || !rc[0].IsContainment || rc[1].Color != linePalette[1] {
		t.Fatalf("relationship colours = %+v", rc)
	}
}

func TestNodeLinks(t *testing.T) {
	f := &fakeAtlas{cypher: map[string]string{"RETURN a, r, b": fixture(t, "graph_cnc02.json")}}
	ds := newTestDS(t, f, noAuth)
	resp, err := ds.QueryData(context.Background(), &backend.QueryDataRequest{
		PluginContext: backend.PluginContext{DataSourceInstanceSettings: &backend.DataSourceInstanceSettings{UID: "atlas-uid", Name: "Atlas"}},
		Queries:       []backend.DataQuery{{RefID: "A", JSON: []byte(`{"queryType":"cypher","cypher":"MATCH (a)-[r]->(b) RETURN a, r, b","colorBy":"type"}`)}},
	})
	if err != nil {
		t.Fatal(err)
	}
	id, _ := resp.Responses["A"].Frames[0].FieldByName("id")
	if id == nil || id.Config == nil || len(id.Config.Links) != 3 {
		t.Fatal("node id field should carry three Explore links")
	}
	l := id.Config.Links[0]
	q := l.Internal.Query.(map[string]any)
	if l.Internal.DatasourceUID != "atlas-uid" || q["instanceId"] != "${__value.raw}" || q["colorBy"] != "type" || q["domain"] != "cesmii-workshop" {
		t.Fatalf("graph link = %+v %+v", l.Internal, q)
	}
	r := run(t, ds, `{"queryType":"graph","instanceId":"rel:x"}`)
	if r.Error == nil || !strings.Contains(r.Error.Error(), "relationship, not an instance") {
		t.Fatalf("relationship node id should give a clear error, got %v", r.Error)
	}
}

func runRange(t *testing.T, ds *Datasource, q string, from, to time.Time) backend.DataResponse {
	t.Helper()
	resp, err := ds.QueryData(context.Background(), &backend.QueryDataRequest{Queries: []backend.DataQuery{{RefID: "A", JSON: []byte(q), TimeRange: backend.TimeRange{From: from, To: to}}}})
	if err != nil {
		t.Fatal(err)
	}
	return resp.Responses["A"]
}

const prodNode = `{"entityId":"CNC02-Production","typeId":"urn:x:CNCProduction","entityLabel":"Production","FeedRate":{"Timestamp":"2026-11-18T05:00:00Z","Value":12.5,"Quality":192},"JobNumber":{"Timestamp":"2026-11-18T05:00:00Z","Value":"JOB-1","Quality":192}}`

func TestHistoryRawUsesTypedMatchAndParameters(t *testing.T) {
	f := &fakeAtlas{cypher: map[string]string{
		"IN $ids RETURN n": `{"columns":["n"],"data":[[` + prodNode + `]]}`,
		"AS value":         `{"columns":["id","t","value","quality"],"data":[["CNC02-Production","2026-11-18T04:59:50Z",11,192],["CNC02-Production","2026-11-18T05:00:00Z",12.5,192]]}`,
	}}
	ds := newTestDS(t, f, noAuth)
	to := time.Date(2026, 11, 18, 6, 0, 0, 0, time.UTC)
	r := runRange(t, ds, `{"queryType":"history","instanceId":"CNC02-Production","properties":["FeedRate"],"includeQuality":true}`, to.Add(-time.Hour), to)
	if r.Error != nil {
		t.Fatal(r.Error)
	}
	last := f.queries[len(f.queries)-1]
	cy := last["cypher"].(string)
	if !strings.Contains(cy, "MATCH (n:CNCProduction)") || !strings.Contains(cy, "UNWIND n.FeedRate AS v") {
		t.Fatalf("history must use a typed MATCH: %s", cy)
	}
	p := last["parameters"].(map[string]any)
	if p["from"] != "2026-11-18T05:00:00Z" || p["to"] != "2026-11-18T06:00:00Z" {
		t.Fatalf("time window parameters = %v", p)
	}
	fr := r.Frames[0]
	if fr.Name != "CNC02-Production FeedRate" || fr.Rows() != 2 || fr.Fields[0].Type() != data.FieldTypeTime {
		t.Fatalf("frame = %s rows %d", fr.Name, fr.Rows())
	}
	if fr.Fields[1].Labels["instance"] != "CNC02-Production" || fr.Fields[1].Labels["property"] != "FeedRate" {
		t.Fatalf("labels = %v", fr.Fields[1].Labels)
	}
	if _, qi := fr.FieldByName("Quality"); qi < 0 {
		t.Fatal("quality field missing")
	}
}

func TestHistoryAutoBucketsLongRanges(t *testing.T) {
	f := &fakeAtlas{cypher: map[string]string{
		"IN $ids RETURN n": `{"columns":["n"],"data":[[` + prodNode + `]]}`,
		"relative(":        `{"columns":["id","t","avg","min","max","n"],"data":[["CNC02-Production","2026-11-17T00:00:00Z",10,5,15,3600]]}`,
		"AS value":         `{"columns":["id","t","value","quality"],"data":[["CNC02-Production","2026-11-17T00:00:00Z","JOB-1",192]]}`,
	}}
	ds := newTestDS(t, f, noAuth)
	to := time.Date(2026, 11, 18, 0, 0, 0, 0, time.UTC)
	r := runRange(t, ds, `{"queryType":"history","instanceId":"CNC02-Production","properties":["FeedRate","JobNumber"],"includeMinMax":true}`, to.Add(-7*24*time.Hour), to)
	if r.Error != nil {
		t.Fatal(r.Error)
	}
	var sawHour, sawTextRaw bool
	for _, q := range f.queries {
		cy := q["cypher"].(string)
		if strings.Contains(cy, "UNWIND n.FeedRate") && strings.Contains(cy, `relative(v.Timestamp, "h")`) {
			sawHour = true
		}
		if strings.Contains(cy, "UNWIND n.JobNumber") && strings.Contains(cy, "AS value") {
			sawTextRaw = true
		}
	}
	if !sawHour || !sawTextRaw {
		t.Fatalf("7 days should bucket FeedRate by hour and read text JobNumber raw (hour %v, text raw %v)", sawHour, sawTextRaw)
	}
	for _, fr := range r.Frames {
		if fr.Name == "CNC02-Production FeedRate" {
			if _, i := fr.FieldByName("Max"); i < 0 {
				t.Fatal("min/max requested but missing")
			}
		}
	}
}

func TestHistorySelectionMixesWholeInstancesAndProperties(t *testing.T) {
	other := `{"entityId":"CNC03-Production","typeId":"urn:x:CNCProduction","FeedRate":{"Timestamp":"2026-11-18T05:00:00Z","Value":3,"Quality":192},"SpindleTemp":{"Timestamp":"2026-11-18T05:00:00Z","Value":40,"Quality":192}}`
	f := &fakeAtlas{cypher: map[string]string{
		"IN $ids RETURN n": `{"columns":["n"],"data":[[` + prodNode + `],[` + other + `]]}`,
		"AS value":         `{"columns":["id","t","value","quality"],"data":[]}`,
	}}
	ds := newTestDS(t, f, noAuth)
	to := time.Date(2026, 11, 18, 6, 0, 0, 0, time.UTC)
	q := `{"queryType":"history","selection":[{"instance":"CNC02-Production"},{"instance":"CNC03-Production","property":"SpindleTemp"},{"instance":"CNC03-Production","property":"Nope"},{"instance":"Gone"}]}`
	r := runRange(t, ds, q, to.Add(-time.Hour), to)
	if r.Error != nil {
		t.Fatal(r.Error)
	}
	// CNC02 whole: FeedRate and JobNumber; CNC03: SpindleTemp only.
	reads := map[string][]any{}
	for _, q := range f.queries {
		cy := q["cypher"].(string)
		if i := strings.Index(cy, "UNWIND n."); i >= 0 {
			prop := strings.Fields(cy[i+len("UNWIND n."):])[0]
			reads[prop] = q["parameters"].(map[string]any)["ids"].([]any)
		}
	}
	if len(reads) != 3 || len(reads["FeedRate"]) != 1 || reads["FeedRate"][0] != "CNC02-Production" || reads["SpindleTemp"][0] != "CNC03-Production" || reads["JobNumber"] == nil {
		t.Fatalf("reads = %v", reads)
	}
	var notices []string
	for _, n := range r.Frames[0].Meta.Notices {
		notices = append(notices, n.Text)
	}
	joined := strings.Join(notices, " | ")
	if !strings.Contains(joined, "Not found in main/") || !strings.Contains(joined, "Gone") || !strings.Contains(joined, "CNC03-Production Nope") {
		t.Fatalf("notices = %s", joined)
	}
}

func TestTreeCarriesLiveProperties(t *testing.T) {
	nodes := liveProperties([]map[string]any{mustNode(prodNode)})
	if len(nodes) != 2 || nodes[0].Name != "FeedRate" || nodes[0].Kind != "number" || nodes[1].Kind != "text" {
		t.Fatalf("live = %+v", nodes)
	}
}

func mustNode(s string) map[string]any {
	var m map[string]any
	if err := json.Unmarshal([]byte(s), &m); err != nil {
		panic(err)
	}
	return m
}

func TestHistoryNoMatchingProperty(t *testing.T) {
	f := &fakeAtlas{cypher: map[string]string{"IN $ids RETURN n": `{"columns":["n"],"data":[[` + prodNode + `]]}`}}
	r := run(t, newTestDS(t, f, noAuth), `{"queryType":"history","instanceId":"CNC02-Production","properties":["Nope"]}`)
	if r.Error == nil || !strings.Contains(r.Error.Error(), "no live value called Nope") && !strings.Contains(r.Error.Error(), "has a live value called Nope") {
		t.Fatalf("want a clear error, got %v", r.Error)
	}
}

func TestCypherGetsPanelTimeRange(t *testing.T) {
	tr := backend.TimeRange{From: time.Date(2026, 11, 18, 5, 0, 0, 0, time.UTC), To: time.Date(2026, 11, 18, 6, 0, 0, 0, time.UTC)}
	p := withTimeRange("WHERE v.Timestamp >= datetime($from) AND v.Timestamp < datetime($to) AND x = $fromage", map[string]any{"to": "user"}, tr)
	if p["from"] != "2026-11-18T05:00:00Z" || p["to"] != "user" {
		t.Fatalf("params = %v", p)
	}
	if _, ok := withTimeRange("RETURN $fromage", nil, tr)["from"]; ok {
		t.Fatal("$fromage is not $from")
	}
}

func TestCypherTimestampColumnsBecomeTime(t *testing.T) {
	f := &fakeAtlas{cypher: map[string]string{"UNWIND": `{"columns":["v.Timestamp","v.Value"],"data":[["2026-11-18T05:47:22.788Z",1.5],["2026-11-18T05:47:23Z",2]]}`}}
	r := run(t, newTestDS(t, f, noAuth), `{"queryType":"cypher","cypher":"MATCH (n:T) UNWIND n.P AS v RETURN v.Timestamp, v.Value","format":"table"}`)
	if r.Error != nil {
		t.Fatal(r.Error)
	}
	if r.Frames[0].Fields[0].Type() != data.FieldTypeNullableTime {
		t.Fatalf("timestamp column type = %v", r.Frames[0].Fields[0].Type())
	}
}
