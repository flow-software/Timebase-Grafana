package plugin

import (
	"encoding/json"
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
