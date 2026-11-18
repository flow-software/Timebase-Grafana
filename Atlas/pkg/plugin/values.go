package plugin

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/grafana/grafana-plugin-sdk-go/data"
)

// Values query scopes and layouts.
const (
	scopeInstance = "instance" // one instance
	scopeChildren = "children" // one instance and everything it contains
	scopeType     = "type"     // every instance of one entity type

	layoutRows    = "rows"    // one row per property (long)
	layoutColumns = "columns" // one row per instance, a column per property (wide)
)

// entry is one flattened property value of one instance.
type entry struct {
	Prop    string
	Value   any
	Quality *int64
	Time    *time.Time
}

// flatten turns a Cypher node's properties into named values. Live values
// (Timestamp/Value/Quality) keep their quality and time. Other compounds are
// expanded into dotted names (LiveAsset.MxAssetName), taking the compound's
// own Timestamp as the time of each part. Lists are numbered (Items[0].Name).
// Properties are sorted by name.
func flatten(node map[string]any) []entry {
	keys := make([]string, 0, len(node))
	for k := range node {
		if !nodeMetaKeys[k] {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	var out []entry
	for _, k := range keys {
		out = flattenValue(out, k, node[k], nil)
	}
	return out
}

func flattenValue(out []entry, name string, v any, t *time.Time) []entry {
	switch x := v.(type) {
	case map[string]any:
		if isTVQ(x) {
			e := entry{Prop: name, Value: x["Value"], Time: parseTime(str(x["Timestamp"]))}
			if q, ok := x["Quality"].(float64); ok {
				qi := int64(q)
				e.Quality = &qi
			}
			return append(out, e)
		}
		if ts := parseTime(str(x["Timestamp"])); ts != nil {
			t = ts
		}
		keys := make([]string, 0, len(x))
		for k := range x {
			if k != "Timestamp" {
				keys = append(keys, k)
			}
		}
		sort.Strings(keys)
		for _, k := range keys {
			out = flattenValue(out, name+"."+k, x[k], t)
		}
		return out
	case []any:
		if len(x) == 0 {
			return append(out, entry{Prop: name, Time: t})
		}
		for i, e := range x {
			out = flattenValue(out, name+"["+strconv.Itoa(i)+"]", e, t)
		}
		return out
	default:
		return append(out, entry{Prop: name, Value: v, Time: t})
	}
}

// qualityText names an OPC quality code by its top two bits (OPC DA):
// 11 Good, 01 Uncertain, 00 Bad. A missing value is "No data" whatever the
// code; Atlas itself reports that case as GoodNoData with code 64.
func qualityText(code *int64, value any) string {
	if value == nil {
		return "No data"
	}
	if code == nil {
		return ""
	}
	switch *code & 0xC0 {
	case 0xC0:
		return "Good"
	case 0x40:
		return "Uncertain"
	default:
		return "Bad"
	}
}

func (d *Datasource) valuesQuery(ctx context.Context, branch, domain string, qm queryModel) (data.Frames, error) {
	if len(qm.Selection) > 0 {
		return d.selectionValues(ctx, branch, domain, qm)
	}
	limit := clampLimit(qm.Limit, defaultTableLimit)
	scope := qm.Scope
	if scope == "" {
		scope = scopeInstance
	}

	var nodes []map[string]any
	var truncated bool
	switch scope {
	case scopeType:
		typ := shortName(strings.TrimSpace(qm.EntityType))
		if typ == "" {
			return nil, badRequest("pick an entity type")
		}
		if !labelPattern.MatchString(typ) {
			return nil, badRequest(fmt.Sprintf("entity type %q is not a plain name", typ))
		}
		var err error
		if nodes, err = d.nodes(ctx, branch, domain, "MATCH (n:"+typ+") RETURN n LIMIT "+strconv.Itoa(limit), nil); err != nil {
			return nil, err
		}
		truncated = len(nodes) >= limit

	case scopeInstance, scopeChildren:
		id := strings.TrimSpace(qm.InstanceID)
		if id == "" {
			return nil, badRequest("pick an instance")
		}
		ids := []string{id}
		if scope == scopeChildren {
			tree, err := d.trees.get(branch+"/"+domain, false, func() ([]TreeNode, error) { return d.instanceTree(ctx, branch, domain) })
			if err != nil {
				return nil, err
			}
			ids = descendants(tree, id)
			if len(ids) > limit {
				ids, truncated = ids[:limit], true
			}
		}
		found, err := d.nodes(ctx, branch, domain, "MATCH (n) WHERE n.entityId IN $ids RETURN n", map[string]any{"ids": ids})
		if err != nil {
			return nil, err
		}
		if len(found) == 0 {
			return nil, badRequest(fmt.Sprintf("instance %q not found in %s/%s", id, branch, domain))
		}
		// Keep the tree order (parent first, then its children).
		byID := make(map[string]map[string]any, len(found))
		for _, n := range found {
			byID[str(n["entityId"])] = n
		}
		for _, i := range ids {
			if n, ok := byID[i]; ok {
				nodes = append(nodes, n)
			}
		}

	default:
		return nil, badRequest(fmt.Sprintf("unknown scope %q", scope))
	}

	name := strings.TrimSpace(qm.InstanceID)
	if scope == scopeType {
		name = shortName(qm.EntityType)
	}
	var frame *data.Frame
	if qm.Layout == layoutColumns {
		frame = nodesTable(name, nodes, tableOptions{IncludeQuality: qm.IncludeQuality, IncludeTime: qm.IncludeTime})
	} else {
		frame = rowsTable(name, nodes, scope != scopeInstance)
	}
	if truncated {
		addNotice(frame, data.NoticeSeverityWarning, fmt.Sprintf("Only the first %d instances are shown. Raise the limit to see more.", limit))
	}
	return data.Frames{frame}, nil
}

// nodes runs a Cypher query whose first column is a node and returns the nodes.
func (d *Datasource) nodes(ctx context.Context, branch, domain, cypher string, params map[string]any) ([]map[string]any, error) {
	res, err := d.client.Query(ctx, branch, domain, cypher, params)
	if err != nil {
		return nil, err
	}
	rows, err := decodeRows(res)
	if err != nil {
		return nil, err
	}
	out := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		if len(row) > 0 && isNode(row[0]) {
			out = append(out, row[0].(map[string]any))
		}
	}
	return out, nil
}

// descendants returns id and everything below it in the containment tree,
// depth first in tree order.
func descendants(tree []TreeNode, id string) []string {
	kids := map[string][]string{}
	for _, n := range tree {
		if n.ParentID != "" {
			kids[n.ParentID] = append(kids[n.ParentID], n.ID)
		}
	}
	var out []string
	seen := map[string]bool{}
	var walk func(string)
	walk = func(i string) {
		if seen[i] {
			return
		}
		seen[i] = true
		out = append(out, i)
		for _, k := range kids[i] {
			walk(k)
		}
	}
	walk(id)
	return out
}

// rowsTable: one row per property of each instance. The instance columns are
// only added when there is more than one instance.
func rowsTable(name string, nodes []map[string]any, withInstance bool) *data.Frame {
	var inst, instID []string
	var props []string
	value := column{name: "Value"}
	var quality []string
	var codes []*int64
	var times []*time.Time
	for _, n := range nodes {
		label := str(n["entityLabel"])
		if label == "" {
			label = str(n["entityId"])
		}
		for _, e := range flatten(n) {
			inst = append(inst, label)
			instID = append(instID, str(n["entityId"]))
			props = append(props, e.Prop)
			value.cells = append(value.cells, e.Value)
			quality = append(quality, qualityText(e.Quality, e.Value))
			codes = append(codes, e.Quality)
			times = append(times, e.Time)
		}
	}
	frame := data.NewFrame(name)
	if withInstance {
		frame.Fields = append(frame.Fields,
			data.NewField("Instance", nil, nonNil(inst)),
			data.NewField("Instance id", nil, nonNil(instID)),
		)
	}
	frame.Fields = append(frame.Fields,
		data.NewField("Property", nil, nonNil(props)),
		// Without an explicit name Grafana shows the frame name here.
		value.field().SetConfig(&data.FieldConfig{DisplayNameFromDS: "Value"}),
		data.NewField("Quality", nil, nonNil(quality)),
		data.NewField("Quality code", nil, nonNilPtr(codes)),
		timeField("Time", nonNilPtr(times)),
	)
	return frame
}

func nonNilPtr[T any](s []*T) []*T {
	if s == nil {
		return []*T{}
	}
	return s
}

// jsonText is used where a value has to be shown as text.
func jsonText(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	b, _ := json.Marshal(v)
	return string(b)
}

// selectionValues: the current values of a selection picked in the browse
// modal (the same picks as History). A whole-instance pick shows every
// property; a property pick shows only that property (a record expands into
// its parts). Instances keep the order they were picked in.
func (d *Datasource) selectionValues(ctx context.Context, branch, domain string, qm queryModel) (data.Frames, error) {
	picks, err := d.expandPicks(ctx, branch, domain, qm.Selection)
	if err != nil {
		return nil, err
	}
	var ids []string
	props := map[string]map[string]bool{} // nil set: the whole instance
	for _, p := range picks {
		id := strings.TrimSpace(p.Instance)
		if id == "" {
			continue
		}
		set, seen := props[id]
		if !seen {
			ids = append(ids, id)
		}
		prop := strings.TrimSpace(p.Property)
		switch {
		case prop == "":
			props[id] = nil
		case !seen:
			props[id] = map[string]bool{prop: true}
		case set != nil:
			set[prop] = true
		}
	}
	if len(ids) == 0 {
		return nil, badRequest("nothing to read: no instance matches the selection")
	}
	if len(ids) > maxPickedInstances {
		return nil, badRequest(fmt.Sprintf("too many instances (%d); pick at most %d", len(ids), maxPickedInstances))
	}
	found, err := d.nodes(ctx, branch, domain, "MATCH (n) WHERE n.entityId IN $ids RETURN n", map[string]any{"ids": ids})
	if err != nil {
		return nil, err
	}
	byID := make(map[string]map[string]any, len(found))
	for _, n := range found {
		byID[str(n["entityId"])] = n
	}
	var nodes []map[string]any
	var missing []string
	for _, id := range ids {
		n, ok := byID[id]
		if !ok {
			missing = append(missing, id)
			continue
		}
		if set := props[id]; set != nil {
			kept := map[string]any{}
			for k, v := range n {
				if nodeMetaKeys[k] || set[k] {
					kept[k] = v
				}
			}
			n = kept
		}
		nodes = append(nodes, n)
	}

	var frame *data.Frame
	if qm.Layout == layoutColumns {
		frame = nodesTable("values", nodes, tableOptions{IncludeQuality: qm.IncludeQuality, IncludeTime: qm.IncludeTime})
	} else {
		frame = rowsTable("values", nodes, len(nodes) > 1)
	}
	if len(missing) > 0 {
		addNotice(frame, data.NoticeSeverityWarning, fmt.Sprintf("Not found in %s/%s: %s", branch, domain, strings.Join(missing, ", ")))
	}
	return data.Frames{frame}, nil
}
