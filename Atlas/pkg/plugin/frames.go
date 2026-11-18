package plugin

import (
	"encoding/json"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/flow-software/Timebase-Grafana/Atlas/pkg/atlas"
	"github.com/grafana/grafana-plugin-sdk-go/data"
)

// Keys every Cypher node object carries. Anything else is a property.
var nodeMetaKeys = map[string]bool{"entityId": true, "typeId": true, "row": true, "entityLabel": true}

// shortName turns "urn:atlas:ontology:cnc-base:SpindleType" into "SpindleType".
func shortName(uri string) string {
	if i := strings.LastIndexAny(uri, ":#/"); i >= 0 && i < len(uri)-1 {
		return uri[i+1:]
	}
	return uri
}

func asMap(v any) (map[string]any, bool) {
	m, ok := v.(map[string]any)
	return m, ok
}

// isRelationship: Cypher relationship objects carry Source and Target.
func isRelationship(v any) bool {
	m, ok := asMap(v)
	if !ok {
		return false
	}
	_, s := m["Source"]
	_, t := m["Target"]
	_, id := m["entityId"]
	return s && t && id
}

// isNode: Cypher node objects carry entityId and typeId, and are not relationships.
func isNode(v any) bool {
	m, ok := asMap(v)
	if !ok {
		return false
	}
	_, id := m["entityId"]
	_, typ := m["typeId"]
	return id && typ && !isRelationship(v)
}

// isTVQ: a Timestamp/Value/Quality compound, as Cypher returns live values.
func isTVQ(v any) bool {
	m, ok := asMap(v)
	if !ok {
		return false
	}
	_, val := m["Value"]
	_, ts := m["Timestamp"]
	return val && ts && len(m) <= 3
}

// isRecord reports a compound value with its own Timestamp that is not a
// TVQ, such as MaintainX LiveAsset. Atlas stores its history like a TVQ's.
func isRecord(v any) bool {
	m, ok := asMap(v)
	if !ok || isTVQ(m) {
		return false
	}
	_, ts := m["Timestamp"]
	return ts
}

// hasHistory reports a property whose values Atlas keeps over time.
func hasHistory(v any) bool { return isTVQ(v) || isRecord(v) }

func str(v any) string {
	s, _ := v.(string)
	return s
}

func parseTime(s string) *time.Time {
	if s == "" {
		return nil
	}
	t, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		return nil
	}
	return &t
}

// --- typed columns ---------------------------------------------------------

// column collects cells and picks the narrowest Grafana field type that fits
// every non-null cell: number, boolean, otherwise text (maps and lists as JSON).
type column struct {
	name  string
	cells []any
}

func (c *column) field() *data.Field {
	allNum, allBool, seen := true, true, false
	for _, v := range c.cells {
		if v == nil {
			continue
		}
		seen = true
		if _, ok := v.(float64); !ok {
			allNum = false
		}
		if _, ok := v.(bool); !ok {
			allBool = false
		}
	}
	switch {
	case seen && allNum:
		vals := make([]*float64, len(c.cells))
		for i, v := range c.cells {
			if f, ok := v.(float64); ok {
				vals[i] = &f
			}
		}
		return data.NewField(c.name, nil, vals)
	case seen && allTimes(c.cells):
		vals := make([]*time.Time, len(c.cells))
		for i, v := range c.cells {
			if v != nil {
				vals[i] = parseTime(v.(string))
			}
		}
		return data.NewField(c.name, nil, vals)
	case seen && allBool:
		vals := make([]*bool, len(c.cells))
		for i, v := range c.cells {
			if b, ok := v.(bool); ok {
				vals[i] = &b
			}
		}
		return data.NewField(c.name, nil, vals)
	default:
		vals := make([]*string, len(c.cells))
		for i, v := range c.cells {
			if v == nil {
				continue
			}
			var s string
			switch x := v.(type) {
			case string:
				s = x
			default:
				b, _ := json.Marshal(x)
				s = string(b)
			}
			vals[i] = &s
		}
		return data.NewField(c.name, nil, vals)
	}
}

func timeField(name string, cells []*time.Time) *data.Field {
	return data.NewField(name, nil, cells)
}

func nonNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

func plural(n int, word string) string {
	if n == 1 {
		return "1 " + word
	}
	return strconv.Itoa(n) + " " + word + "s"
}

// --- Cypher result as a table ------------------------------------------------

// cypherTable converts a raw Cypher result. Nodes and relationships become
// their entityId, live values become their Value, other objects become JSON.
func cypherTable(name string, cols []string, rows [][]any) *data.Frame {
	frame := data.NewFrame(name)
	for ci, cname := range cols {
		c := column{name: cname, cells: make([]any, len(rows))}
		for ri, row := range rows {
			if ci >= len(row) {
				continue
			}
			v := row[ci]
			switch {
			case isNode(v) || isRelationship(v):
				v = str(v.(map[string]any)["entityId"])
			case isTVQ(v):
				v = v.(map[string]any)["Value"]
			}
			c.cells[ri] = v
		}
		frame.Fields = append(frame.Fields, c.field())
	}
	return frame
}

// hasGraphCells reports whether any cell holds a relationship, which is what
// "auto" format uses to choose the Node Graph output.
func hasGraphCells(rows [][]any) bool {
	var walk func(v any) bool
	walk = func(v any) bool {
		if l, ok := v.([]any); ok {
			for _, e := range l {
				if walk(e) {
					return true
				}
			}
			return false
		}
		return isRelationship(v)
	}
	for _, row := range rows {
		for _, v := range row {
			if walk(v) {
				return true
			}
		}
	}
	return false
}

// decodeRows turns the raw JSON cells into Go values.
func decodeRows(res *atlas.QueryResult) ([][]any, error) {
	rows := make([][]any, len(res.Data))
	for i, raw := range res.Data {
		row := make([]any, len(raw))
		for j, cell := range raw {
			if len(cell) == 0 {
				continue
			}
			if err := json.Unmarshal(cell, &row[j]); err != nil {
				return nil, err
			}
		}
		rows[i] = row
	}
	return rows, nil
}

// allTimes reports whether every non-null cell is an ISO 8601 timestamp, as
// Atlas returns them (for example from v.Timestamp in a history query), so
// the column becomes a time field and the result can be drawn over time.
func allTimes(cells []any) bool {
	n := 0
	for _, v := range cells {
		if v == nil {
			continue
		}
		s, ok := v.(string)
		if !ok || len(s) < 20 || s[10] != 'T' || parseTime(s) == nil {
			return false
		}
		n++
	}
	return n > 0
}

// splitSeries turns a long Cypher result into time series: the first time
// column is the time, text columns are labels, and the other columns are
// values. Rows are grouped by their labels (first-seen order) and sorted by
// time; rows without a time are dropped. A series is named by its label
// values, so "entity, property, time, value" rows give series named
// "CNC01-Production FeedRate" like the History builder's.
func splitSeries(f *data.Frame) (data.Frames, error) {
	timeIdx := -1
	var labelIdx, valueIdx []int
	for i, fl := range f.Fields {
		switch {
		case timeIdx < 0 && (fl.Type() == data.FieldTypeNullableTime || fl.Type() == data.FieldTypeTime):
			timeIdx = i
		case fl.Type() == data.FieldTypeNullableString || fl.Type() == data.FieldTypeString:
			labelIdx = append(labelIdx, i)
		default:
			valueIdx = append(valueIdx, i)
		}
	}
	if timeIdx < 0 {
		return nil, badRequest("time series output needs a timestamp column, e.g. RETURN v.Timestamp AS time")
	}
	if len(valueIdx) == 0 {
		return nil, badRequest("time series output needs a number column, e.g. v.Value AS value")
	}

	type group struct {
		labels data.Labels
		name   string
		rows   []int
	}
	groups := map[string]*group{}
	var order []string
	for r := 0; r < f.Rows(); r++ {
		if t, ok := f.Fields[timeIdx].ConcreteAt(r); !ok || t == nil {
			continue
		}
		var parts []string
		labels := data.Labels{}
		for _, li := range labelIdx {
			v, _ := f.Fields[li].ConcreteAt(r)
			s, _ := v.(string)
			labels[f.Fields[li].Name] = s
			parts = append(parts, s)
		}
		key := strings.Join(parts, "\x00")
		g, ok := groups[key]
		if !ok {
			g = &group{labels: labels, name: strings.TrimSpace(strings.Join(parts, " "))}
			groups[key] = g
			order = append(order, key)
		}
		g.rows = append(g.rows, r)
	}

	out := make(data.Frames, 0, len(order))
	for _, key := range order {
		g := groups[key]
		rows := g.rows
		at := func(r int) time.Time {
			v, _ := f.Fields[timeIdx].ConcreteAt(r)
			return v.(time.Time)
		}
		sort.SliceStable(rows, func(i, j int) bool { return at(rows[i]).Before(at(rows[j])) })
		times := make([]time.Time, len(rows))
		for i, r := range rows {
			times[i] = at(r)
		}
		name := g.name
		if name == "" {
			name = "series"
		}
		fr := data.NewFrame(name, data.NewField("Time", nil, times))
		for _, vi := range valueIdx {
			src := f.Fields[vi]
			dst := data.NewFieldFromFieldType(src.Type(), len(rows))
			dst.Name = src.Name
			dst.Labels = g.labels.Copy()
			for i, r := range rows {
				dst.Set(i, src.At(r))
			}
			display := name
			if len(valueIdx) > 1 && src.Name != "value" {
				display = name + " " + src.Name
			}
			dst.SetConfig(&data.FieldConfig{DisplayNameFromDS: display})
			fr.Fields = append(fr.Fields, dst)
		}
		out = append(out, fr)
	}
	if len(out) == 0 {
		fr := data.NewFrame("result")
		addNotice(fr, data.NoticeSeverityInfo, "No values in this time range.")
		out = data.Frames{fr}
	}
	return out, nil
}
