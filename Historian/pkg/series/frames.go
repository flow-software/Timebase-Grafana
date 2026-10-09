// Package series turns historian samples into Grafana data frames. Both the
// i3X and the native Timebase clients produce Samples, so panels and alerts
// see the same frame shape whichever API a datasource uses.
package series

import (
	"bytes"
	"encoding/json"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/grafana/grafana-plugin-sdk-go/data"
)

// Sample is one value/quality/timestamp. Value is kept raw because both APIs
// return numbers, booleans, strings and nulls through the same field. The JSON
// names match i3X's VQT so the i3X client can decode straight into it.
type Sample struct {
	Value     json.RawMessage `json:"value"`
	Quality   string          `json:"quality"`
	Timestamp time.Time       `json:"timestamp"`
}

// Meta is optional tag metadata. The native Timebase API supplies it; i3X
// does not, so i3X series use the zero value.
type Meta struct {
	Unit        string
	Description string
	// ValueText maps raw values to labels, e.g. "2" -> "Running".
	ValueText map[string]string
	// Text marks a tag declared as text, so it stays a text series even
	// when every sample in the range happens to look numeric.
	Text bool
}

// Options controls how samples become a frame.
type Options struct {
	// IncludeQuality adds a string "Quality" field next to the value.
	IncludeQuality bool
	// BadQualityAsNull blanks the value of any sample whose quality is not
	// Good, so alerts and stats do not act on untrusted values.
	BadQualityAsNull bool
}

// Frame builds a time series frame for one tag: a Time field and a Value
// field, plus Quality when asked for. Numbers and booleans become a nullable
// float64 field (true=1, false=0) so the series can be graphed and alerted
// on; if any sample is a string, or the tag is declared as text, the whole
// series is returned as strings.
func Frame(elementID string, samples []Sample, meta Meta, opts Options) *data.Frame {
	n := len(samples)
	times := make([]time.Time, n)
	qualities := make([]string, n)

	numeric := !meta.Text && !HasText(samples)

	var value *data.Field
	if numeric {
		vals := make([]*float64, n)
		for i, s := range samples {
			times[i], qualities[i] = s.Timestamp, s.Quality
			if opts.BadQualityAsNull && !isGood(s.Quality) {
				continue
			}
			vals[i] = asFloat(s.Value)
		}
		value = data.NewField("Value", labelsFor(elementID), vals)
	} else {
		vals := make([]*string, n)
		for i, s := range samples {
			times[i], qualities[i] = s.Timestamp, s.Quality
			if opts.BadQualityAsNull && !isGood(s.Quality) {
				continue
			}
			vals[i] = asString(s.Value)
		}
		value = data.NewField("Value", labelsFor(elementID), vals)
	}
	value.Config = fieldConfig(elementID, meta)

	frame := data.NewFrame(elementID, data.NewField("Time", nil, times), value)
	if opts.IncludeQuality {
		frame.Fields = append(frame.Fields, data.NewField("Quality", labelsFor(elementID), qualities))
	} else if numeric {
		// A time field plus one numeric field is the multi-frame time series
		// shape that alerting and server-side expressions expect.
		frame.Meta = &data.FrameMeta{Type: data.FrameTypeTimeSeriesMulti, TypeVersion: data.FrameTypeVersion{0, 1}}
	}
	return frame
}

// HasText reports whether any sample holds text, which makes the series a
// text series. Live streams use it to fix a channel's field type up front,
// since every frame on a channel must keep the same schema.
func HasText(samples []Sample) bool {
	for _, s := range samples {
		if kind(s.Value) == kindString {
			return true
		}
	}
	return false
}

func fieldConfig(elementID string, meta Meta) *data.FieldConfig {
	cfg := &data.FieldConfig{DisplayNameFromDS: DisplayName(elementID), Description: meta.Description}
	if meta.Unit != "" {
		// Grafana's custom-unit syntax; the space keeps "72 °F" readable.
		cfg.Unit = "suffix: " + meta.Unit
	}
	if len(meta.ValueText) > 0 {
		keys := make([]string, 0, len(meta.ValueText))
		for k := range meta.ValueText {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		mapper := data.ValueMapper{}
		for i, k := range keys {
			mapper[k] = data.ValueMappingResult{Text: meta.ValueText[k], Index: i}
		}
		cfg.Mappings = data.ValueMappings{mapper}
	}
	return cfg
}

// DisplayName drops the "<dataset>:" prefix from a Timebase ElementId, so
// "default:Line1.Speed" shows as "Line1.Speed".
func DisplayName(elementID string) string {
	if i := strings.Index(elementID, ":"); i >= 0 && i < len(elementID)-1 {
		return elementID[i+1:]
	}
	return elementID
}

// QualityFromOPC converts an OPC quality code, as the native Timebase API
// returns it (192 = Good), to the words i3X uses. The top two bits carry the
// status: 11 Good, 01 Uncertain, 00 Bad.
func QualityFromOPC(code int) string {
	switch code & 0xC0 {
	case 0xC0:
		return "Good"
	case 0x40:
		return "Uncertain"
	default:
		return "Bad"
	}
}

func labelsFor(elementID string) data.Labels {
	return data.Labels{"elementId": elementID}
}

func isGood(quality string) bool {
	return quality == "" || strings.EqualFold(quality, "Good")
}

type valueKind int

const (
	kindNull valueKind = iota
	kindNumber
	kindBool
	kindString
)

func kind(raw json.RawMessage) valueKind {
	b := bytes.TrimSpace(raw)
	if len(b) == 0 || bytes.Equal(b, []byte("null")) {
		return kindNull
	}
	switch b[0] {
	case 't', 'f':
		return kindBool
	case '-', '0', '1', '2', '3', '4', '5', '6', '7', '8', '9':
		return kindNumber
	}
	// Strings, and objects or arrays shown as their JSON text.
	return kindString
}

func asFloat(raw json.RawMessage) *float64 {
	var f float64
	switch kind(raw) {
	case kindNumber:
		if json.Unmarshal(raw, &f) != nil {
			return nil
		}
	case kindBool:
		if bytes.Equal(bytes.TrimSpace(raw), []byte("true")) {
			f = 1
		}
	default:
		return nil
	}
	return &f
}

func asString(raw json.RawMessage) *string {
	var s string
	switch kind(raw) {
	case kindNull:
		return nil
	case kindNumber:
		if f := asFloat(raw); f != nil {
			s = strconv.FormatFloat(*f, 'f', -1, 64)
		}
	default:
		b := bytes.TrimSpace(raw)
		if b[0] != '"' || json.Unmarshal(b, &s) != nil {
			s = string(b)
		}
	}
	return &s
}
