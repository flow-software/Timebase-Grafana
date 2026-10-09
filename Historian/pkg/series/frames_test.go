package series

import (
	"encoding/json"
	"testing"

	"github.com/grafana/grafana-plugin-sdk-go/data"
)

// Samples copied from Timebase Historian 2.0.813 responses.
func parse(t *testing.T, raw string) []Sample {
	t.Helper()
	var s []Sample
	if err := json.Unmarshal([]byte(raw), &s); err != nil {
		t.Fatal(err)
	}
	return s
}

func TestFrameNumeric(t *testing.T) {
	s := parse(t, `[
		{"value":18.02,"quality":"Good","timestamp":"2026-10-02T10:02:30.358Z"},
		{"value":null,"quality":"Bad","timestamp":"2026-10-02T10:02:31.250Z"},
		{"value":18.15,"quality":"Uncertain","timestamp":"2026-10-02T10:02:32.607Z"}]`)

	f := Frame("default:131-TT-001.PV", s, Meta{}, Options{})
	if len(f.Fields) != 2 || f.Rows() != 3 {
		t.Fatalf("got %d fields x %d rows, want 2 x 3", len(f.Fields), f.Rows())
	}
	if f.Meta == nil || f.Meta.Type != data.FrameTypeTimeSeriesMulti {
		t.Fatal("numeric frame without quality should be typed as timeseries-multi for alerting")
	}
	v := f.Fields[1]
	if v.Type() != data.FieldTypeNullableFloat64 {
		t.Fatalf("value type %v, want nullable float64", v.Type())
	}
	if got := *v.At(0).(*float64); got != 18.02 {
		t.Fatalf("row 0 = %v", got)
	}
	if v.At(1).(*float64) != nil {
		t.Fatal("null value should stay null")
	}
	if v.Config.DisplayNameFromDS != "131-TT-001.PV" || v.Labels["elementId"] != "default:131-TT-001.PV" {
		t.Fatalf("display name %q labels %v", v.Config.DisplayNameFromDS, v.Labels)
	}
	if v.Config.Unit != "" || v.Config.Mappings != nil {
		t.Fatal("no metadata should mean no unit and no mappings")
	}
}

func TestFrameBadQualityAsNullAndQualityField(t *testing.T) {
	s := parse(t, `[
		{"value":1,"quality":"Good","timestamp":"2026-10-02T10:00:30Z"},
		{"value":2,"quality":"Uncertain","timestamp":"2026-10-02T10:00:31Z"}]`)

	f := Frame("default:x", s, Meta{}, Options{IncludeQuality: true, BadQualityAsNull: true})
	if len(f.Fields) != 3 || f.Fields[2].Name != "Quality" || f.Fields[2].At(1).(string) != "Uncertain" {
		t.Fatalf("want a Quality field holding the raw quality; got %d fields", len(f.Fields))
	}
	if f.Fields[1].At(1).(*float64) != nil {
		t.Fatal("non-Good sample should be null with BadQualityAsNull")
	}
	if *f.Fields[1].At(0).(*float64) != 1 {
		t.Fatal("Good sample should keep its value")
	}
}

func TestFrameBooleansAsNumbers(t *testing.T) {
	s := parse(t, `[
		{"value":true,"quality":"Good","timestamp":"2026-10-02T10:00:30Z"},
		{"value":false,"quality":"Good","timestamp":"2026-10-02T10:00:31Z"}]`)

	f := Frame("default:b", s, Meta{}, Options{})
	if *f.Fields[1].At(0).(*float64) != 1 || *f.Fields[1].At(1).(*float64) != 0 {
		t.Fatal("booleans should map to 1/0")
	}
}

func TestFrameStringsMakeStringSeries(t *testing.T) {
	s := parse(t, `[
		{"value":5,"quality":"Good","timestamp":"2026-10-02T10:00:30Z"},
		{"value":"Running","quality":"Good","timestamp":"2026-10-02T10:00:31Z"},
		{"value":null,"quality":"Bad","timestamp":"2026-10-02T10:00:32Z"}]`)

	f := Frame("default:s", s, Meta{}, Options{})
	v := f.Fields[1]
	if v.Type() != data.FieldTypeNullableString {
		t.Fatalf("value type %v, want nullable string", v.Type())
	}
	if *v.At(0).(*string) != "5" || *v.At(1).(*string) != "Running" || v.At(2).(*string) != nil {
		t.Fatal("mixed series should keep every sample as text, null as null")
	}
	if f.Meta != nil {
		t.Fatal("string series must not claim the numeric time series type")
	}
}

func TestFrameDeclaredTextStaysText(t *testing.T) {
	s := parse(t, `[{"value":42,"quality":"Good","timestamp":"2026-10-02T10:00:30Z"}]`)
	f := Frame("default:code", s, Meta{Text: true}, Options{})
	if f.Fields[1].Type() != data.FieldTypeNullableString || *f.Fields[1].At(0).(*string) != "42" {
		t.Fatal("a tag declared as text should be a text series even when values look numeric")
	}
}

func TestFrameMetadata(t *testing.T) {
	s := parse(t, `[{"value":2,"quality":"Good","timestamp":"2026-10-02T10:00:30Z"}]`)

	unit := Frame("opcplc:Dataset.Size", s, Meta{Unit: "GB", Description: "System storage size"}, Options{})
	if c := unit.Fields[1].Config; c.Unit != "suffix: GB" || c.Description != "System storage size" {
		t.Fatalf("unit %q description %q", c.Unit, c.Description)
	}

	state := Frame("opcplc:Dataset.State", s, Meta{ValueText: map[string]string{"1": "Starting", "2": "Running"}}, Options{})
	m := state.Fields[1].Config.Mappings
	if len(m) != 1 {
		t.Fatalf("want one value mapper, got %d", len(m))
	}
	if r := m[0].(data.ValueMapper)["2"]; r.Text != "Running" {
		t.Fatalf("value 2 maps to %q, want Running", r.Text)
	}
}

func TestQualityFromOPC(t *testing.T) {
	for code, want := range map[int]string{192: "Good", 216: "Good", 68: "Uncertain", 64: "Uncertain", 0: "Bad", 24: "Bad"} {
		if got := QualityFromOPC(code); got != want {
			t.Errorf("QualityFromOPC(%d) = %q, want %q", code, got, want)
		}
	}
}

func TestDisplayName(t *testing.T) {
	for in, want := range map[string]string{
		"default:Boilers.Boiler #1.BoilerStatus.Pressure": "Boilers.Boiler #1.BoilerStatus.Pressure",
		"plain":    "plain",
		"default:": "default:",
	} {
		if got := DisplayName(in); got != want {
			t.Errorf("DisplayName(%q) = %q, want %q", in, got, want)
		}
	}
}
