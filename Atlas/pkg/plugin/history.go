package plugin

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/grafana/grafana-plugin-sdk-go/data"
)

// History reads the stored values of live (Timestamp/Value/Quality)
// properties through Cypher:
//
//	MATCH (n:Type) WHERE n.entityId IN $ids
//	UNWIND n.Prop AS v
//	WITH n, v WHERE v.Timestamp >= datetime($from) AND v.Timestamp < datetime($to)
//	RETURN ...
//
// Atlas pushes the time window down to the source. Two things learned live
// (Atlas 2.0.0) shape the queries:
//   - the MATCH must name the type. With a bare (n) the UNWIND silently
//     expands only the current value, so instances are grouped by type first;
//   - relative(ts, unit) truncates only for a bare unit ("m", "h", "d"); a
//     sized unit like "5m" is read as an offset. Buckets are therefore a
//     minute, an hour or a day.

// Aggregation choices.
const (
	aggAuto   = "auto"
	aggRaw    = "raw"
	aggMinute = "minute"
	aggHour   = "hour"
	aggDay    = "day"
)

// maxHistoryRows caps one (type, property) read. Past it the panel is told
// to use buckets.
const maxHistoryRows = 200000

// historyConcurrency bounds parallel Cypher reads per panel query.
const historyConcurrency = 4

var bucketUnit = map[string]string{aggMinute: "m", aggHour: "h", aggDay: "d"}

// autoAggregation picks raw points or a bucket size from the panel's range,
// so a long range does not pull millions of points.
func autoAggregation(from, to time.Time) string {
	r := to.Sub(from)
	switch {
	case r <= 3*time.Hour:
		return aggRaw
	case r <= 3*24*time.Hour:
		return aggMinute
	case r <= 180*24*time.Hour:
		return aggHour
	default:
		return aggDay
	}
}

// historyTarget is one Cypher read: one property on instances of one type.
type historyTarget struct {
	Type    string
	Prop    string
	IDs     []string // empty means every instance of the type
	Numeric bool     // false for text and boolean properties: never bucketed
}

func (d *Datasource) historyQuery(ctx context.Context, branch, domain string, qm queryModel, from, to time.Time) (data.Frames, error) {
	props := cleanProps(qm.Properties)
	if len(props) == 0 {
		return nil, badRequest("pick at least one property")
	}
	for _, p := range props {
		if !labelPattern.MatchString(p) {
			return nil, badRequest(fmt.Sprintf("property %q is not a plain name", p))
		}
	}

	nodes, scopeErr := d.scopeNodes(ctx, branch, domain, qm)
	if scopeErr != nil {
		return nil, scopeErr
	}
	targets, labels := historyTargets(nodes, props, qm.Scope == scopeType)
	if len(targets) == 0 {
		return nil, badRequest(fmt.Sprintf("none of the selected instances has a live value called %s", strings.Join(props, " or ")))
	}

	agg := qm.Aggregation
	if agg == "" || agg == aggAuto {
		agg = autoAggregation(from, to)
	}

	type result struct {
		frames data.Frames
		err    error
		notice string
	}
	results := make([]result, len(targets))
	sem := make(chan struct{}, historyConcurrency)
	var wg sync.WaitGroup
	for i, t := range targets {
		wg.Add(1)
		go func(i int, t historyTarget) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			a := agg
			if !t.Numeric {
				a = aggRaw
			}
			f, notice, err := d.readHistory(ctx, branch, domain, t, a, from, to, qm, labels)
			results[i] = result{f, err, notice}
		}(i, t)
	}
	wg.Wait()

	var frames data.Frames
	var notices []string
	for _, r := range results {
		if r.err != nil {
			return nil, r.err
		}
		frames = append(frames, r.frames...)
		if r.notice != "" {
			notices = append(notices, r.notice)
		}
	}
	sort.SliceStable(frames, func(i, j int) bool { return frames[i].Name < frames[j].Name })
	if len(frames) == 0 {
		frames = data.Frames{data.NewFrame("history")}
		addNotice(frames[0], data.NoticeSeverityInfo, "No values in this time range.")
	}
	for _, n := range notices {
		addNotice(frames[0], data.NoticeSeverityWarning, n)
	}
	return frames, nil
}

// scopeNodes returns the nodes a Values or History query reads (instance,
// nested instances, or every instance of a type), in tree order.
func (d *Datasource) scopeNodes(ctx context.Context, branch, domain string, qm queryModel) ([]map[string]any, error) {
	limit := clampLimit(qm.Limit, defaultTableLimit)
	switch qm.Scope {
	case scopeType:
		typ := shortName(strings.TrimSpace(qm.EntityType))
		if typ == "" {
			return nil, badRequest("pick an entity type")
		}
		if !labelPattern.MatchString(typ) {
			return nil, badRequest(fmt.Sprintf("entity type %q is not a plain name", typ))
		}
		return d.nodes(ctx, branch, domain, "MATCH (n:"+typ+") RETURN n LIMIT "+strconv.Itoa(limit), nil)
	case scopeInstance, scopeChildren, "":
		id := strings.TrimSpace(qm.InstanceID)
		if id == "" {
			return nil, badRequest("pick an instance")
		}
		ids := []string{id}
		if qm.Scope == scopeChildren {
			tree, err := d.trees.get(branch+"/"+domain, false, func() ([]TreeNode, error) { return d.instanceTree(ctx, branch, domain) })
			if err != nil {
				return nil, err
			}
			ids = descendants(tree, id)
			if len(ids) > limit {
				ids = ids[:limit]
			}
		}
		found, err := d.nodes(ctx, branch, domain, "MATCH (n) WHERE n.entityId IN $ids RETURN n", map[string]any{"ids": ids})
		if err != nil {
			return nil, err
		}
		if len(found) == 0 {
			return nil, badRequest(fmt.Sprintf("instance %q not found in %s/%s", id, branch, domain))
		}
		byID := make(map[string]map[string]any, len(found))
		for _, n := range found {
			byID[str(n["entityId"])] = n
		}
		var out []map[string]any
		for _, i := range ids {
			if n, ok := byID[i]; ok {
				out = append(out, n)
			}
		}
		return out, nil
	}
	return nil, badRequest(fmt.Sprintf("unknown scope %q", qm.Scope))
}

// historyTargets groups instances by type for each property they hold as a
// live value. labels maps entityId to a display name.
func historyTargets(nodes []map[string]any, props []string, wholeType bool) ([]historyTarget, map[string]string) {
	labels := map[string]string{}
	byKey := map[string]*historyTarget{}
	var order []string
	for _, n := range nodes {
		id := str(n["entityId"])
		labels[id] = id
		typ := shortName(str(n["typeId"]))
		if !labelPattern.MatchString(typ) {
			continue
		}
		for _, p := range props {
			v, ok := n[p]
			if !ok || !isTVQ(v) {
				continue
			}
			key := typ + "\x00" + p
			t, seen := byKey[key]
			if !seen {
				t = &historyTarget{Type: typ, Prop: p}
				byKey[key] = t
				order = append(order, key)
			}
			if !wholeType {
				t.IDs = append(t.IDs, id)
			}
			switch v.(map[string]any)["Value"].(type) {
			case float64:
				t.Numeric = true
			case nil:
				// unknown from the current value; numeric unless proven otherwise
				if !seen {
					t.Numeric = true
				}
			default:
				t.Numeric = false
			}
		}
	}
	out := make([]historyTarget, 0, len(order))
	for _, k := range order {
		out = append(out, *byKey[k])
	}
	return out, labels
}

// historyCypher builds the read for one target. Times and ids are
// parameters; type, property and bucket unit are checked identifiers.
func historyCypher(t historyTarget, agg string) (string, map[string]any) {
	var b strings.Builder
	params := map[string]any{}
	b.WriteString("MATCH (n:" + t.Type + ")")
	if len(t.IDs) > 0 {
		b.WriteString(" WHERE n.entityId IN $ids")
		params["ids"] = t.IDs
	}
	b.WriteString(" UNWIND n." + t.Prop + " AS v")
	b.WriteString(" WITH n, v WHERE v.Timestamp >= datetime($from) AND v.Timestamp < datetime($to)")
	if unit, ok := bucketUnit[agg]; ok {
		b.WriteString(` RETURN n.entityId AS id, relative(v.Timestamp, "` + unit + `") AS t, avg(v.Value) AS avg, min(v.Value) AS min, max(v.Value) AS max, count(v) AS n ORDER BY t`)
	} else {
		b.WriteString(" RETURN n.entityId AS id, v.Timestamp AS t, v.Value AS value, v.Quality AS quality ORDER BY t LIMIT " + strconv.Itoa(maxHistoryRows+1))
	}
	return b.String(), params
}

// readHistory runs one target and returns a frame per instance.
func (d *Datasource) readHistory(ctx context.Context, branch, domain string, t historyTarget, agg string, from, to time.Time, qm queryModel, labels map[string]string) (data.Frames, string, error) {
	cypher, params := historyCypher(t, agg)
	params["from"] = from.UTC().Format(time.RFC3339Nano)
	params["to"] = to.UTC().Format(time.RFC3339Nano)
	res, err := d.client.Query(ctx, branch, domain, cypher, params)
	if err != nil {
		return nil, "", err
	}
	rows, err := decodeRows(res)
	if err != nil {
		return nil, "", err
	}
	notice := ""
	if _, bucketed := bucketUnit[agg]; !bucketed && len(rows) > maxHistoryRows {
		rows = rows[:maxHistoryRows]
		notice = fmt.Sprintf("%s: only the first %d values are shown; use minute, hour or day buckets for this range.", t.Prop, maxHistoryRows)
	}

	// Split the rows by instance, keeping time order.
	byID := map[string][][]any{}
	var ids []string
	for _, r := range rows {
		if len(r) < 2 {
			continue
		}
		id := str(r[0])
		if _, ok := byID[id]; !ok {
			ids = append(ids, id)
		}
		byID[id] = append(byID[id], r)
	}

	var frames data.Frames
	for _, id := range ids {
		frames = append(frames, historyFrame(id, labels[id], t.Prop, byID[id], agg, qm))
	}
	return frames, notice, nil
}

// historyFrame builds one time series. The series is named
// "<instance> <property>" and labelled for alerting.
func historyFrame(id, label, prop string, rows [][]any, agg string, qm queryModel) *data.Frame {
	if label == "" {
		label = id
	}
	name := label + " " + prop
	lbl := data.Labels{"instance": id, "property": prop}
	times := make([]time.Time, 0, len(rows))

	if _, bucketed := bucketUnit[agg]; bucketed {
		avg := make([]*float64, 0, len(rows))
		mins := make([]*float64, 0, len(rows))
		maxs := make([]*float64, 0, len(rows))
		for _, r := range rows {
			ts := parseTime(str(cell(r, 1)))
			if ts == nil {
				continue
			}
			times = append(times, *ts)
			avg = append(avg, num(cell(r, 2)))
			mins = append(mins, num(cell(r, 3)))
			maxs = append(maxs, num(cell(r, 4)))
		}
		f := data.NewFrame(name,
			data.NewField("Time", nil, times),
			data.NewField("Value", lbl, avg).SetConfig(&data.FieldConfig{DisplayNameFromDS: name}),
		)
		if qm.IncludeMinMax {
			f.Fields = append(f.Fields,
				data.NewField("Min", lbl, mins).SetConfig(&data.FieldConfig{DisplayNameFromDS: name + " min"}),
				data.NewField("Max", lbl, maxs).SetConfig(&data.FieldConfig{DisplayNameFromDS: name + " max"}),
			)
		}
		return f
	}

	values := column{name: "Value"}
	var quality []*int64
	for _, r := range rows {
		ts := parseTime(str(cell(r, 1)))
		if ts == nil {
			continue
		}
		times = append(times, *ts)
		values.cells = append(values.cells, cell(r, 2))
		var q *int64
		if f, ok := cell(r, 3).(float64); ok {
			qi := int64(f)
			q = &qi
		}
		quality = append(quality, q)
	}
	vf := values.field()
	vf.Labels = lbl
	vf.SetConfig(&data.FieldConfig{DisplayNameFromDS: name})
	f := data.NewFrame(name, data.NewField("Time", nil, times), vf)
	if qm.IncludeQuality {
		f.Fields = append(f.Fields, data.NewField("Quality", lbl, nonNilPtr(quality)).SetConfig(&data.FieldConfig{DisplayNameFromDS: name + " quality"}))
	}
	return f
}

func cell(r []any, i int) any {
	if i < len(r) {
		return r[i]
	}
	return nil
}

func num(v any) *float64 {
	if f, ok := v.(float64); ok {
		return &f
	}
	return nil
}

func cleanProps(in []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, p := range in {
		p = strings.TrimSpace(p)
		if p != "" && !seen[p] {
			seen[p] = true
			out = append(out, p)
		}
	}
	return out
}

// PropertyOption is one live (time-series) property, for the editor's picker.
type PropertyOption struct {
	Name  string `json:"name"`
	Kind  string `json:"kind"` // number, text, boolean or unknown (no current value)
	Count int    `json:"count"`
}

// liveProperties lists the live properties of the nodes, most common first.
func liveProperties(nodes []map[string]any) []PropertyOption {
	byName := map[string]*PropertyOption{}
	for _, n := range nodes {
		for k, v := range n {
			if nodeMetaKeys[k] || !isTVQ(v) || !labelPattern.MatchString(k) {
				continue
			}
			p, ok := byName[k]
			if !ok {
				p = &PropertyOption{Name: k, Kind: "unknown"}
				byName[k] = p
			}
			p.Count++
			switch v.(map[string]any)["Value"].(type) {
			case float64:
				p.Kind = "number"
			case string:
				if p.Kind == "unknown" {
					p.Kind = "text"
				}
			case bool:
				if p.Kind == "unknown" {
					p.Kind = "boolean"
				}
			}
		}
	}
	out := make([]PropertyOption, 0, len(byName))
	for _, p := range byName {
		out = append(out, *p)
	}
	sort.Slice(out, func(i, j int) bool { return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name) })
	return out
}
