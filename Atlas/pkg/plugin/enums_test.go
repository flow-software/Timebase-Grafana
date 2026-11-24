package plugin

import (
	"testing"
	"time"

	"github.com/grafana/grafana-plugin-sdk-go/data"

	"github.com/flow-software/Timebase-Grafana/Atlas/pkg/atlas"
)

const stateEnumURI = "urn:atlas:ontology:packml:MachineStateEnum"

func stateEnum() atlas.EnumType {
	return atlas.EnumType{URI: stateEnumURI, Label: "PackML Machine State", KeyType: "http://www.w3.org/2001/XMLSchema#long",
		Members: []atlas.EnumMember{{Key: "2", Name: "Stopped", Label: "Stopped"}, {Key: "6", Name: "Execute"}, {Key: "11", Name: "Held", Label: ""}}}
}

func TestEnumOfFindsEveryBinding(t *testing.T) {
	tvq := &atlas.CompoundRange{BaseCompoundURI: tvqURI, FieldTypeOverrides: map[string]string{"Value": "xsd:long"}, FieldEnumOverrides: map[string]string{"Value": stateEnumURI}}
	types := []atlas.EntityType{
		{URI: "urn:x:Machine", Properties: []atlas.EntityProperty{
			{URI: "urn:x:State", CompoundRange: tvq},
			{URI: "urn:x:Mode", Range: "xsd:long", EnumType: stateEnumURI},
			{URI: "urn:x:Data", CompoundRange: &atlas.CompoundRange{BaseCompoundURI: tvqURI}},
			{URI: "urn:x:RPM", CompoundRange: &atlas.CompoundRange{BaseCompoundURI: tvqURI, FieldTypeOverrides: map[string]string{"Value": "xsd:double"}}},
		}},
		{URI: "urn:x:Packer", ParentType: "urn:x:Machine", Pins: []atlas.EntityPropertyPin{
			{PropertyURI: "urn:x:Data", FieldEnumOverrides: map[string]string{"Value": stateEnumURI}},
		}},
	}
	r := newKindResolver(types, []atlas.EnumType{stateEnum()})
	for _, c := range []struct {
		typ, prop string
		want      bool
	}{
		{"urn:x:Machine", "State", true},  // TVQ Value field
		{"urn:x:Machine", "Mode", true},   // plain property
		{"urn:x:Packer", "State", true},   // inherited
		{"urn:x:Packer", "Data", true},    // pinned on the subtype
		{"urn:x:Machine", "Data", false},  // open on the base type
		{"urn:x:Machine", "RPM", false},   // a plain number
		{"urn:x:Unknown", "State", false}, // type not in the ontology
	} {
		if got := r.enumOf(c.typ, c.prop) != nil; got != c.want {
			t.Errorf("%s %s: enum %v, want %v", c.typ, c.prop, got, c.want)
		}
	}
	var none *kindResolver
	if none.enumInfoOf("urn:x:Machine", "State") != nil {
		t.Fatal("a nil resolver knows no enumerations")
	}
}

func TestEnumInfoMappingsAndLabels(t *testing.T) {
	e := stateEnum()
	info := enumInfo(&e)
	if info.Name != "MachineStateEnum" || info.Label != "PackML Machine State" {
		t.Fatalf("info %+v", info)
	}
	// A member without a label shows its name.
	if info.Members[1].Label != "Execute" || info.Members[2].Label != "Held" {
		t.Fatalf("members %+v", info.Members)
	}
	if got := info.label(2.0); got != "Stopped" {
		t.Fatalf("label(2.0) = %q", got)
	}
	if got := info.label(int64(6)); got != "Execute" {
		t.Fatalf("label(6) = %q", got)
	}
	if got := info.label(99.0); got != "" {
		t.Fatalf("label(99) = %q, want empty", got)
	}
	vm := info.valueMappings()
	if len(vm) != 1 {
		t.Fatalf("mappings %v", vm)
	}
	if m := vm[0].(data.ValueMapper); m["2"].Text != "Stopped" || m["11"].Text != "Held" {
		t.Fatalf("mapper %v", m)
	}

	f := data.NewFrame("s",
		data.NewField("Time", nil, []time.Time{time.Now()}),
		data.NewField("Value", nil, []float64{2}),
		data.NewField("Quality", nil, []int64{192}),
	)
	applyEnum(data.Frames{f}, info)
	if f.Fields[1].Config == nil || len(f.Fields[1].Config.Mappings) != 1 {
		t.Fatal("value field has no mappings")
	}
	if f.Fields[0].Config != nil || f.Fields[2].Config != nil {
		t.Fatal("time and quality must stay unmapped")
	}
}

func TestRowsTableShowsEnumLabels(t *testing.T) {
	ts := "2026-11-23T10:00:00Z"
	n := map[string]any{
		"entityId": "Packer1", "typeId": "urn:x:Machine",
		"State": map[string]any{"Timestamp": ts, "Value": 2.0, "Quality": 192.0},
		"RPM":   map[string]any{"Timestamp": ts, "Value": 1200.0, "Quality": 192.0},
	}
	e := stateEnum()
	info := enumInfo(&e)
	enumOf := func(_, prop string) *EnumInfo {
		if prop == "State" {
			return info
		}
		return nil
	}
	f := rowsTable("values", []map[string]any{n}, false, enumOf)
	lf, _ := f.FieldByName("Value label")
	if lf == nil {
		t.Fatal("no Value label column")
	}
	props, _ := f.FieldByName("Property")
	for i := 0; i < f.Rows(); i++ {
		p, _ := props.ConcreteAt(i)
		l, _ := lf.ConcreteAt(i)
		want := map[string]string{"State": "Stopped", "RPM": ""}[p.(string)]
		if l != want {
			t.Errorf("%v label %q, want %q", p, l, want)
		}
	}
	if f2 := rowsTable("values", []map[string]any{n}, false, nil); func() bool { x, _ := f2.FieldByName("Value label"); return x != nil }() {
		t.Fatal("no enumerations, no label column")
	}
}

func TestAllEnumsByLabel(t *testing.T) {
	b := atlas.EnumType{URI: "urn:x:AlarmEnum", Label: "Alarm", KeyType: "http://www.w3.org/2001/XMLSchema#string", Members: []atlas.EnumMember{{Key: "A", Name: "A"}}}
	r := newKindResolver(nil, []atlas.EnumType{stateEnum(), b})
	got := r.allEnums()
	if len(got) != 2 || got[0].Name != "AlarmEnum" || got[1].Name != "MachineStateEnum" {
		t.Fatalf("allEnums %+v", got)
	}
	if got[0].KeyKind != "text" || got[1].KeyKind != "number" {
		t.Fatalf("key kinds %q %q", got[0].KeyKind, got[1].KeyKind)
	}
	var none *kindResolver
	if len(none.allEnums()) != 0 {
		t.Fatal("nil resolver")
	}
}
