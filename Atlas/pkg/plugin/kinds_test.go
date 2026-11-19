package plugin

import (
	"encoding/json"
	"testing"

	"github.com/flow-software/Timebase-Grafana/Atlas/pkg/atlas"
)

// Shapes as Atlas 2.0 returned them for cesmii-workshop (2026-11-18), trimmed.
const entityTypesJSON = `[
 {"uri":"urn:atlas:ontology:cesmii-workshop:CNCProduction","properties":[
  {"uri":"urn:atlas:ontology:cesmii-workshop:JobNumber","compoundRange":{"baseCompoundUri":"urn:atlas:ontology:atlas-common:TVQ","fieldTypeOverrides":{"Value":"http://www.w3.org/2001/XMLSchema#string"}}},
  {"uri":"urn:atlas:ontology:cesmii-workshop:AlarmActive","compoundRange":{"baseCompoundUri":"urn:atlas:ontology:atlas-common:TVQ","fieldTypeOverrides":{"Value":"http://www.w3.org/2001/XMLSchema#double"}}}],
  "propertyPins":[]},
 {"uri":"urn:atlas:ontology:atlas-common:Tag","properties":[
  {"uri":"urn:atlas:ontology:atlas-common:Data","range":"urn:atlas:ontology:atlas-common:TVQ","compoundRange":{"baseCompoundUri":"urn:atlas:ontology:atlas-common:TVQ","fieldTypeOverrides":{}}}]},
 {"uri":"urn:atlas:ontology:cesmii-workshop:SqlTag","parentType":"urn:atlas:ontology:atlas-common:Tag","properties":[],
  "propertyPins":[{"propertyUri":"urn:atlas:ontology:atlas-common:Data","declaredOnUri":"urn:atlas:ontology:cesmii-workshop:SqlTag","fieldTypeOverrides":{"Value":"http://www.w3.org/2001/XMLSchema#double"}}]},
 {"uri":"urn:atlas:ontology:cesmii-workshop:OpenTag","parentType":"urn:atlas:ontology:atlas-common:Tag","properties":[]},
 {"uri":"urn:atlas:ontology:cesmii-workshop:CNCMachine","properties":[
  {"uri":"urn:atlas:ontology:cesmii-workshop:LiveAsset","range":"urn:atlas:ontology:maintainx-core:MaintainXAsset","compoundRange":{"baseCompoundUri":"urn:atlas:ontology:maintainx-core:MaintainXAsset","fieldTypeOverrides":{}}}]}
]`

func TestDeclaredKinds(t *testing.T) {
	var types []atlas.EntityType
	if err := json.Unmarshal([]byte(entityTypesJSON), &types); err != nil {
		t.Fatal(err)
	}
	r := newKindResolver(types)
	for _, c := range []struct{ typ, prop, want string }{
		{"urn:atlas:ontology:cesmii-workshop:CNCProduction", "JobNumber", "text"},
		{"urn:atlas:ontology:cesmii-workshop:CNCProduction", "AlarmActive", "number"},
		{"urn:atlas:ontology:cesmii-workshop:SqlTag", "Data", "number"}, // pinned
		{"urn:atlas:ontology:cesmii-workshop:OpenTag", "Data", ""},      // inherited, left open
		{"urn:atlas:ontology:cesmii-workshop:CNCMachine", "LiveAsset", "record"},
		{"urn:atlas:ontology:cesmii-workshop:Nope", "X", ""},
	} {
		if got := r.kind(c.typ, c.prop); got != c.want {
			t.Errorf("%s %s = %q, want %q", c.typ, c.prop, got, c.want)
		}
	}
	var none *kindResolver
	if none.kind("x", "y") != "" {
		t.Fatal("nil resolver must fall back")
	}
}

func TestHistoryTargetsUseDeclaredKind(t *testing.T) {
	node := mustNode(`{"entityId":"CNC02-Production","typeId":"urn:x:CNCProduction","JobNumber":{"Timestamp":"2026-11-18T05:00:00Z","Value":null,"Quality":64}}`)
	text := func(string, string) string { return "text" }
	none := func(string, string) string { return "" }
	got, _ := historyTargets([]map[string]any{node}, func(map[string]any) []string { return []string{"JobNumber"} }, false, text)
	if len(got) != 1 || got[0].Numeric {
		t.Fatalf("declared text must not be bucketed: %+v", got)
	}
	got, _ = historyTargets([]map[string]any{node}, func(map[string]any) []string { return []string{"JobNumber"} }, false, none)
	if !got[0].Numeric {
		t.Fatal("without a declared kind an empty value is tried as a number")
	}
}

func TestRecordParts(t *testing.T) {
	v := map[string]any{"Timestamp": "2026-11-06T10:38:08Z", "MxLocationId": 6380808.0, "MxAssetName": "CNC01", "MxBarcode": nil, "Nested": map[string]any{"a": 1.0}}
	got := recordParts(v)
	want := []LiveProperty{{Name: "MxAssetName", Kind: "text"}, {Name: "MxBarcode", Kind: "unknown"}, {Name: "MxLocationId", Kind: "number"}}
	if len(got) != len(want) {
		t.Fatalf("parts %v", got)
	}
	for i := range want {
		if got[i].Name != want[i].Name || got[i].Kind != want[i].Kind {
			t.Fatalf("parts %v, want %v", got, want)
		}
	}
}
