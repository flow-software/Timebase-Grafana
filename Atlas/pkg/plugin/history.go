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
	aggAuto = "auto"
	// aggPeak is what Auto does for number properties: one line that keeps
	// each slice's lowest and highest value at their own times.
	aggPeak   = "peak"
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
// so a long range does not pull millions of points. Auto history of number
// properties uses peakSource instead; this stays for $bucket in Cypher.
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
	IDs     []string          // empty means every instance of the type
	Numeric bool              // false for text and boolean properties: never bucketed
	Record  bool              // a compound with its own Timestamp: read raw, one column per part
	Units   map[string]string // entityId -> Grafana unit, from the instance's UOM
}

// historyPick is one entry of a History selection: one property of an
// instance, or (Property empty) every live property of it.
type historyPick struct {
	Instance string `json:"instance"`
	Property string `json:"property,omitempty"`
	// Type: the instance's entity type (informational), or for scope "type"
	// the type whose instances are read.
	Type string `json:"type,omitempty"`
	// Scope: "" just this instance; "nested" this instance and everything
	// nested under it; "type" every instance of Type. Nested and type picks
	// are resolved when the query runs, so they follow changes in Atlas.
	Scope string `json:"scope,omitempty"`
}

const (
	pickNested = "nested"
	pickType   = "type"
)

// expandPicks resolves nested and type picks into plain instance picks, in
// tree order, using the cached instance tree. With a property, only
// instances that have it as a live value are kept, so a scope does not
// report every instance that lacks it.
func (d *Datasource) expandPicks(ctx context.Context, branch, domain string, picks []historyPick) ([]historyPick, error) {
	dynamic := false
	for _, p := range picks {
		if p.Scope == pickNested || p.Scope == pickType {
			dynamic = true
			break
		}
	}
	if !dynamic {
		return picks, nil
	}
	tree, err := d.trees.get(branch+"/"+domain, false, func() ([]TreeNode, error) { return d.instanceTree(ctx, branch, domain) })
	if err != nil {
		return nil, err
	}
	byID := make(map[string]TreeNode, len(tree))
	for _, n := range tree {
		byID[n.ID] = n
	}
	hasLive := func(id, prop string) bool {
		for _, l := range byID[id].Live {
			if l.Name == prop {
				return true
			}
		}
		return false
	}
	var out []historyPick
	for _, p := range picks {
		var ids []string
		switch p.Scope {
		case pickNested:
			id := strings.TrimSpace(p.Instance)
			if _, ok := byID[id]; !ok {
				out = append(out, historyPick{Instance: id, Property: p.Property}) // reported as not found
				continue
			}
			ids = descendants(tree, id)
		case pickType:
			typ := shortName(strings.TrimSpace(p.Type))
			for _, n := range tree {
				if n.Type == typ {
					ids = append(ids, n.ID)
				}
			}
		default:
			out = append(out, p)
			continue
		}
		for _, id := range ids {
			if p.Property != "" && !hasLive(id, p.Property) {
				continue
			}
			out = append(out, historyPick{Instance: id, Property: p.Property})
		}
	}
	return out, nil
}

// maxPickedInstances bounds one History selection.
const maxPickedInstances = 1000

func (d *Datasource) historyQuery(ctx context.Context, branch, domain string, qm queryModel, from, to time.Time) (data.Frames, error) {
	var targets []historyTarget
	var labels map[string]string
	var pickNotice string
	if len(qm.Selection) > 0 {
		var err error
		targets, labels, pickNotice, err = d.selectionTargets(ctx, branch, domain, qm.Selection)
		if err != nil {
			return nil, err
		}
	} else {
		props := cleanProps(qm.Properties)
		if len(props) == 0 {
			return nil, badRequest("pick at least one instance or property")
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
		targets, labels = historyTargets(nodes, func(map[string]any) []string { return props }, qm.Scope == scopeType, d.kinds(ctx, branch, domain, false).kind)
		if len(targets) == 0 {
			return nil, badRequest(fmt.Sprintf("none of the selected instances has a live value called %s", strings.Join(props, " or ")))
		}
	}

	agg := qm.Aggregation
	if agg == "" || agg == aggAuto {
		agg = aggPeak
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
			if !t.Numeric || t.Record {
				a = aggRaw
			}
			f, notice, err := d.readHistory(ctx, branch, domain, t, a, from, to, qm, labels)
			if err != nil && a != aggRaw && ctx.Err() == nil {
				// A property whose current value is empty looks like a number
				// but may hold text; Atlas fails to average text (500). Read it raw.
				if rf, rn, rerr := d.readHistory(ctx, branch, domain, t, aggRaw, from, to, qm, labels); rerr == nil {
					f, notice, err = rf, rn, nil
				}
			}
			applyUnits(f, t.Units)
			results[i] = result{f, err, notice}
		}(i, t)
	}
	wg.Wait()

	var frames data.Frames
	var notices []string
	if pickNotice != "" {
		notices = append(notices, pickNotice)
	}
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

// selectionTargets reads the picked instances and turns the picks into
// targets. A pick without a property takes every live property of the
// instance. The notice names picks that give no series.
func (d *Datasource) selectionTargets(ctx context.Context, branch, domain string, picks []historyPick) ([]historyTarget, map[string]string, string, error) {
	picks, err := d.expandPicks(ctx, branch, domain, picks)
	if err != nil {
		return nil, nil, "", err
	}
	want := map[string][]string{} // instance -> named properties
	all := map[string]bool{}      // instance picked whole
	var ids []string
	for _, p := range picks {
		id := strings.TrimSpace(p.Instance)
		prop := strings.TrimSpace(p.Property)
		if id == "" {
			continue
		}
		if prop != "" && !labelPattern.MatchString(prop) {
			return nil, nil, "", badRequest(fmt.Sprintf("property %q is not a plain name", prop))
		}
		if _, seen := want[id]; !seen {
			ids = append(ids, id)
			want[id] = nil
		}
		if prop == "" {
			all[id] = true
		} else {
			want[id] = append(want[id], prop)
		}
	}
	if len(ids) == 0 {
		return nil, nil, "", badRequest("nothing to read: no instance matches the selection")
	}
	if len(ids) > maxPickedInstances {
		return nil, nil, "", badRequest(fmt.Sprintf("too many instances (%d); pick at most %d", len(ids), maxPickedInstances))
	}
	nodes, err := d.nodes(ctx, branch, domain, "MATCH (n) WHERE n.entityId IN $ids RETURN n", map[string]any{"ids": ids})
	if err != nil {
		return nil, nil, "", err
	}
	byID := make(map[string]map[string]any, len(nodes))
	for _, n := range nodes {
		byID[str(n["entityId"])] = n
	}
	var ordered []map[string]any
	var missing, skipped []string
	for _, id := range ids {
		n, ok := byID[id]
		if !ok {
			missing = append(missing, id)
			continue
		}
		ordered = append(ordered, n)
		if all[id] {
			continue
		}
		for _, p := range cleanProps(want[id]) {
			if v, has := n[p]; !has || !hasHistory(v) {
				skipped = append(skipped, id+" "+p)
			}
		}
	}
	propsOf := func(n map[string]any) []string {
		id := str(n["entityId"])
		if !all[id] {
			return cleanProps(want[id])
		}
		var out []string
		for _, p := range liveProperties([]map[string]any{n}) {
			out = append(out, p.Name)
		}
		return out
	}
	targets, labels := historyTargets(ordered, propsOf, false, d.kinds(ctx, branch, domain, false).kind)

	var parts []string
	if len(missing) > 0 {
		parts = append(parts, "Not found in "+branch+"/"+domain+": "+strings.Join(missing, ", ")+".")
	}
	if len(skipped) > 0 {
		parts = append(parts, "No live value: "+strings.Join(skipped, ", ")+".")
	}
	notice := strings.Join(parts, " ")
	if len(targets) == 0 {
		if notice == "" {
			notice = "The selected instances have no live values."
		}
		return nil, nil, "", badRequest(notice)
	}
	return targets, labels, notice, nil
}

// historyTargets groups instances by type for each property they hold as a
// live value. labels maps entityId to a display name.
// declared gives a property's kind from the ontology ("" when open or unknown).
func historyTargets(nodes []map[string]any, propsOf func(map[string]any) []string, wholeType bool, declared func(typeURI, prop string) string) ([]historyTarget, map[string]string) {
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
		for _, p := range propsOf(n) {
			v, ok := n[p]
			if !ok || !hasHistory(v) {
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
			if u := instanceUnit(n, p); u != "" {
				if t.Units == nil {
					t.Units = map[string]string{}
				}
				t.Units[id] = u
			}
			if isRecord(v) {
				t.Record = true
				continue
			}
			if k := declared(str(n["typeId"]), p); k != "" {
				t.Numeric = k == "number"
				continue
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
	if t.Record {
		b.WriteString(" RETURN n.entityId AS id, v.Timestamp AS t, v AS value ORDER BY t LIMIT " + strconv.Itoa(maxHistoryRows+1))
	} else if unit, ok := bucketUnit[agg]; ok {
		b.WriteString(` RETURN n.entityId AS id, relative(v.Timestamp, "` + unit + `") AS t, avg(v.Value) AS avg, min(v.Value) AS min, max(v.Value) AS max, count(v) AS n ORDER BY t`)
	} else {
		b.WriteString(" RETURN n.entityId AS id, v.Timestamp AS t, v.Value AS value, v.Quality AS quality ORDER BY t LIMIT " + strconv.Itoa(maxHistoryRows+1))
	}
	return b.String(), params
}

// readHistory runs one target and returns a frame per instance.
func (d *Datasource) readHistory(ctx context.Context, branch, domain string, t historyTarget, agg string, from, to time.Time, qm queryModel, labels map[string]string) (data.Frames, string, error) {
	if _, bucketed := bucketUnit[agg]; agg == aggPeak || (bucketed && !t.Record) {
		return d.readWeighted(ctx, branch, domain, t, agg, from, to, qm, labels)
	}
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
		if t.Record {
			frames = append(frames, recordFrame(id, labels[id], t.Prop, byID[id]))
			continue
		}
		frames = append(frames, historyFrame(id, labels[id], t.Prop, byID[id], qm))
	}
	return frames, notice, nil
}

// historyFrame builds one time series. The series is named
// "<instance> <property>" and labelled for alerting.
func historyFrame(id, label, prop string, rows [][]any, qm queryModel) *data.Frame {
	if label == "" {
		label = id
	}
	name := label + " " + prop
	lbl := data.Labels{"instance": id, "property": prop}
	times := make([]time.Time, 0, len(rows))
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

// recordFrame builds one frame for a record property: Time plus a column per
// part (MxStatus, MxPriority, ...), named "<instance> <property>.<part>".
func recordFrame(id, label, prop string, rows [][]any) *data.Frame {
	if label == "" {
		label = id
	}
	name := label + " " + prop
	var times []time.Time
	cols := map[string]*column{}
	var order []string
	for _, r := range rows {
		ts := parseTime(str(cell(r, 1)))
		if ts == nil {
			continue
		}
		m, _ := asMap(cell(r, 2))
		parts := map[string]any{}
		for k, v := range m {
			if k == "Timestamp" {
				continue
			}
			for _, e := range flattenValue(nil, k, v, nil) {
				parts[e.Prop] = e.Value
			}
		}
		for k := range parts {
			if _, ok := cols[k]; !ok {
				cols[k] = &column{name: k, cells: make([]any, len(times))}
				order = append(order, k)
			}
		}
		times = append(times, *ts)
		for _, k := range order {
			cols[k].cells = append(cols[k].cells, parts[k])
		}
	}
	sort.Strings(order)
	f := data.NewFrame(name, data.NewField("Time", nil, times))
	for _, k := range order {
		fl := cols[k].field()
		fl.Labels = data.Labels{"instance": id, "property": prop, "part": k}
		fl.SetConfig(&data.FieldConfig{DisplayNameFromDS: name + "." + k})
		f.Fields = append(f.Fields, fl)
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

// PropertyOption is one live (time-series) property: its value kind and how many nodes have it.
type PropertyOption struct {
	Name  string `json:"name"`
	Kind  string `json:"kind"` // number, text, boolean, record (compound with a Timestamp) or unknown (no current value)
	Count int    `json:"count"`
}

// liveProperties lists the live properties of the nodes, most common first.
func liveProperties(nodes []map[string]any) []PropertyOption {
	byName := map[string]*PropertyOption{}
	for _, n := range nodes {
		for k, v := range n {
			if nodeMetaKeys[k] || !hasHistory(v) || !labelPattern.MatchString(k) {
				continue
			}
			p, ok := byName[k]
			if !ok {
				p = &PropertyOption{Name: k, Kind: "unknown"}
				byName[k] = p
			}
			p.Count++
			if isRecord(v) {
				p.Kind = "record"
				continue
			}
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

// Averages are time-weighted.
//
// A stored value holds until the next one, so an average over a period
// weighs each value by how long it was in force, not by how often it was
// recorded: a setpoint that sat at 100 for 50 minutes and at 0 for 10
// averages 83, not 50. Atlas's avg() is a plain mean, so the plugin does the
// weighting, the same way Timebase Explorer does for its Historian and Atlas
// sources. A raw read from Atlas starts with the last value before the
// window (live-checked on Atlas 2.0), which gives the value in force at the
// start. A null value holds nothing: that time is left out of the average.
//
// Raw values give an exact result. When a range holds too many of them,
// Atlas's minute (or hour, day) buckets are weighted instead: exact between
// buckets, a plain mean inside each one.
//
// Auto draws one line per series that does not hide events: the panel range
// is cut into slices, each slice draws its time-weighted average, and a
// slice's lowest or highest value is added at its own time when it lies
// further from that average than the series' typical spread (the median
// min-to-max range of its slices). Noise stays smoothed; stops and spikes
// keep their true values. When the raw values already fit the panel they are
// returned unchanged.

// peakRowBudget bounds the bucket rows one read asks Atlas for (instances x
// buckets). 7 days of minutes for 5 instances (~50k rows) read in 3.5 s live.
const peakRowBudget = 60000

// rawWeightBudget: when the buckets show at most this many raw values, they
// are read raw for an exact weighting. Live, 31k raw values read in 0.4 s.
const rawWeightBudget = 100000

const (
	defaultMaxPoints = 1000
	maxMaxPoints     = 5000
)

// peakSource picks raw values or the finest bucket unit for a read of n
// instances: raw up to 3 hours, then the finest bucket within the row budget.
func peakSource(from, to time.Time, n int) string {
	r := to.Sub(from)
	if r <= 3*time.Hour {
		return aggRaw
	}
	if n < 1 {
		n = 1
	}
	for _, s := range []struct {
		agg  string
		size time.Duration
	}{{aggMinute, time.Minute}, {aggHour, time.Hour}} {
		if int64(r/s.size)*int64(n) <= peakRowBudget {
			return s.agg
		}
	}
	return aggDay
}

// weightSource is peakSource, never coarser than a fixed bucket unit ("" for Auto).
func weightSource(from, to time.Time, n int, unit string) string {
	src := peakSource(from, to, n)
	if unit != "" && unitSize[src] > unitSize[unit] {
		src = unit
	}
	return src
}

var unitSize = map[string]time.Duration{aggMinute: time.Minute, aggHour: time.Hour, aggDay: 24 * time.Hour}

// sample is one raw value (lo = hi = avg, n = 1) or one Atlas bucket.
type sample struct {
	t           time.Time // raw time, or bucket start
	avg, lo, hi float64
	loT, hiT    time.Time
	n           float64
	q           *int64 // raw only
	null        bool   // a raw value that is empty: nothing is known until the next one
}

// point is one value of the line; v nil breaks it.
type point struct {
	t time.Time
	v *float64
	q *int64
}

// readSamples runs one target at src (raw or a bucket unit) and returns the
// samples per instance, the instance order and how many raw values they hold.
func (d *Datasource) readSamples(ctx context.Context, branch, domain string, t historyTarget, src string, from, to time.Time) (map[string][]sample, []string, float64, string, error) {
	cypher, params := historyCypher(t, src)
	params["from"] = from.UTC().Format(time.RFC3339Nano)
	params["to"] = to.UTC().Format(time.RFC3339Nano)
	res, err := d.client.Query(ctx, branch, domain, cypher, params)
	if err != nil {
		return nil, nil, 0, "", err
	}
	rows, err := decodeRows(res)
	if err != nil {
		return nil, nil, 0, "", err
	}
	notice := ""
	if src == aggRaw && len(rows) > maxHistoryRows {
		rows = rows[:maxHistoryRows]
		notice = fmt.Sprintf("%s: only the first %d values are used.", t.Prop, maxHistoryRows)
	}

	byID := map[string][]sample{}
	var ids []string
	seen := map[string]bool{}
	total := 0.0
	for _, r := range rows {
		ts := parseTime(str(cell(r, 1)))
		if ts == nil {
			continue
		}
		id := str(cell(r, 0))
		if !seen[id] {
			seen[id] = true
			ids = append(ids, id)
		}
		if src == aggRaw {
			total++
			v := num(cell(r, 2))
			if v == nil {
				byID[id] = append(byID[id], sample{t: *ts, null: true})
				continue
			}
			var q *int64
			if f, ok := cell(r, 3).(float64); ok {
				qi := int64(f)
				q = &qi
			}
			byID[id] = append(byID[id], sample{t: *ts, avg: *v, lo: *v, hi: *v, loT: *ts, hiT: *ts, n: 1, q: q})
			continue
		}
		avg, lo, hi := num(cell(r, 2)), num(cell(r, 3)), num(cell(r, 4))
		if avg == nil || lo == nil || hi == nil {
			continue
		}
		cnt := 1.0
		if c := num(cell(r, 5)); c != nil && *c > 0 {
			cnt = *c
		}
		total += cnt
		// Atlas does not say when in the bucket the min and max fell.
		mid := ts.Add(unitSize[src] / 2)
		byID[id] = append(byID[id], sample{t: *ts, avg: *avg, lo: *lo, hi: *hi, loT: mid, hiT: mid, n: cnt})
	}
	for _, id := range ids {
		ss := byID[id]
		sort.SliceStable(ss, func(i, j int) bool { return ss[i].t.Before(ss[j].t) })
	}
	return byID, ids, total, notice, nil
}

// readWeighted reads a number property for Auto (agg peak) or fixed
// minute, hour or day buckets, time-weighted.
func (d *Datasource) readWeighted(ctx context.Context, branch, domain string, t historyTarget, agg string, from, to time.Time, qm queryModel, labels map[string]string) (data.Frames, string, error) {
	n := len(t.IDs)
	if n == 0 {
		n = len(labels)
	}
	unit := ""
	if agg != aggPeak {
		unit = agg
	}
	src := weightSource(from, to, n, unit)
	byID, ids, total, notice, err := d.readSamples(ctx, branch, domain, t, src, from, to)
	if err != nil {
		return nil, "", err
	}
	if src != aggRaw && total <= rawWeightBudget {
		// Few enough values to weigh exactly.
		if rb, rids, _, rn, rerr := d.readSamples(ctx, branch, domain, t, aggRaw, from, to); rerr == nil && len(rids) > 0 {
			byID, ids, notice, src = rb, rids, rn, aggRaw
		}
	}
	end := to
	if now := time.Now(); now.Before(end) {
		end = now // the last value holds until now, not into the future
	}

	var frames data.Frames
	if unit != "" {
		for _, id := range ids {
			frames = append(frames, bucketFrame(id, labels[id], t.Prop, fixedBuckets(byID[id], from, to, end, unitSize[unit]), qm.IncludeMinMax))
		}
		if src == unit && len(ids) > 0 {
			notice = joinNotice(notice, fmt.Sprintf("%s: too many values to time-weight in this range; the %s values are plain averages.", t.Prop, unit))
		}
		return frames, notice, nil
	}

	maxPoints := qm.maxPoints
	if maxPoints <= 0 {
		maxPoints = defaultMaxPoints
	}
	if maxPoints > maxMaxPoints {
		maxPoints = maxMaxPoints
	}
	for _, id := range ids {
		pts := peakLine(byID[id], from, to, end, maxPoints, src == aggRaw)
		frames = append(frames, peakFrame(id, labels[id], t.Prop, pts, qm.IncludeQuality && src == aggRaw))
	}
	return frames, notice, nil
}

func joinNotice(a, b string) string {
	if a == "" {
		return b
	}
	return a + " " + b
}

// weighed is one bucket after weighting.
type weighed struct {
	sum      float64       // value x seconds
	cov      time.Duration // time with a known value
	a, b     time.Time     // first and last covered instant
	lo, hi   float64
	loT, hiT time.Time
	loQ, hiQ *int64
	samples  int // values recorded inside the bucket
}

func (w weighed) avg() float64 { return w.sum / w.cov.Seconds() }

// timeWeigh cuts time into n buckets of size starting at start (the last
// one ends at stop) and weighs every value by how long it held in each.
// Values hold until the next sample, or until end.
func timeWeigh(ss []sample, start time.Time, size time.Duration, n int, stop, end time.Time) []weighed {
	out := make([]weighed, n)
	if n < 1 || size <= 0 {
		return out
	}
	edge := func(k int) time.Time { return start.Add(size * time.Duration(k)) }
	bucketEnd := func(k int) time.Time {
		if k == n-1 {
			return stop
		}
		return edge(k + 1)
	}
	idx := func(t time.Time) int { return max(0, min(int(t.Sub(start)/size), n-1)) }
	if stop.Before(end) {
		end = stop
	}
	for i, s := range ss {
		if s.null {
			continue
		}
		a, b := s.t, end
		if i+1 < len(ss) && ss[i+1].t.Before(b) {
			b = ss[i+1].t
		}
		if a.Before(start) {
			a = start
		}
		if !b.After(a) {
			continue
		}
		for k := idx(a); k < n; k++ {
			ba, bb := edge(k), bucketEnd(k)
			if !ba.Before(b) {
				break
			}
			x, y := a, b
			if x.Before(ba) {
				x = ba
			}
			if y.After(bb) {
				y = bb
			}
			if !y.After(x) {
				continue
			}
			w := &out[k]
			dur := y.Sub(x)
			// Extremes: where the value was recorded in this bucket, its own;
			// where it is only held over from before, the held value.
			lo, loT, hi, hiT, q := s.avg, x, s.avg, x, (*int64)(nil)
			if !s.t.Before(ba) && s.t.Before(bb) {
				lo, loT, hi, hiT, q = s.lo, s.loT, s.hi, s.hiT, s.q
				w.samples++
			}
			if w.cov == 0 {
				w.a = x
				w.lo, w.loT, w.loQ, w.hi, w.hiT, w.hiQ = lo, loT, q, hi, hiT, q
			} else {
				if lo < w.lo {
					w.lo, w.loT, w.loQ = lo, loT, q
				}
				if hi > w.hi {
					w.hi, w.hiT, w.hiQ = hi, hiT, q
				}
			}
			w.sum += s.avg * dur.Seconds()
			w.cov += dur
			w.b = y
		}
	}
	return out
}

// bucketed is one fixed bucket: its start, time-weighted average and extremes.
type bucketed struct {
	t           time.Time
	avg, lo, hi *float64
}

// fixedBuckets weighs the samples into minute, hour or day buckets aligned
// like Atlas's relative() (UTC). A bucket without a known value between
// covered ones is kept empty, so the line breaks there.
func fixedBuckets(ss []sample, from, to, end time.Time, size time.Duration) []bucketed {
	start := from.UTC().Truncate(size)
	n := int((to.Sub(start) + size - 1) / size)
	ws := timeWeigh(ss, start, size, n, start.Add(size*time.Duration(n)), end)
	first, last := -1, -1
	for k, w := range ws {
		if w.cov > 0 {
			if first < 0 {
				first = k
			}
			last = k
		}
	}
	if first < 0 {
		return nil
	}
	out := make([]bucketed, 0, last-first+1)
	for k := first; k <= last; k++ {
		b := bucketed{t: start.Add(size * time.Duration(k))}
		if w := ws[k]; w.cov > 0 {
			avg, lo, hi := w.avg(), w.lo, w.hi
			b.avg, b.lo, b.hi = &avg, &lo, &hi
		}
		out = append(out, b)
	}
	return out
}

// bucketFrame is one fixed-bucket series, named and labelled like historyFrame's.
func bucketFrame(id, label, prop string, bs []bucketed, withMinMax bool) *data.Frame {
	if label == "" {
		label = id
	}
	name := label + " " + prop
	lbl := data.Labels{"instance": id, "property": prop}
	times := make([]time.Time, len(bs))
	avg := make([]*float64, len(bs))
	mins := make([]*float64, len(bs))
	maxs := make([]*float64, len(bs))
	for i, b := range bs {
		times[i], avg[i], mins[i], maxs[i] = b.t, b.avg, b.lo, b.hi
	}
	f := data.NewFrame(name,
		data.NewField("Time", nil, times),
		data.NewField("Value", lbl, avg).SetConfig(&data.FieldConfig{DisplayNameFromDS: name}),
	)
	if withMinMax {
		f.Fields = append(f.Fields,
			data.NewField("Min", lbl, mins).SetConfig(&data.FieldConfig{DisplayNameFromDS: name + " min"}),
			data.NewField("Max", lbl, maxs).SetConfig(&data.FieldConfig{DisplayNameFromDS: name + " max"}),
		)
	}
	return f
}

// peakLine turns time-ordered samples into at most maxPoints line points:
// raw samples that fit are returned as they are; otherwise the range is cut
// into maxPoints/3 slices, each giving its time-weighted average plus any
// extreme beyond the series' typical spread. A slice with no known value
// after one with a value breaks the line.
func peakLine(ss []sample, from, to, end time.Time, maxPoints int, raw bool) []point {
	sort.SliceStable(ss, func(i, j int) bool { return ss[i].t.Before(ss[j].t) })
	if len(ss) == 0 {
		return nil
	}
	if raw && len(ss) <= maxPoints {
		out := make([]point, len(ss))
		for i, s := range ss {
			out[i] = point{t: s.t, q: s.q}
			if !s.null {
				v := s.avg
				out[i].v = &v
			}
		}
		return out
	}
	slices := maxPoints / 3
	if slices < 1 {
		slices = 1
	}
	span := to.Sub(from)
	if span <= 0 {
		from = ss[0].t
		to = ss[len(ss)-1].t.Add(time.Second)
		span = to.Sub(from)
	}
	size := span / time.Duration(slices)
	if size <= 0 {
		size, slices = span, 1
	}
	ws := timeWeigh(ss, from, size, slices, to, end)

	// The series' typical spread: the median min-to-max range of its slices.
	var spreads []float64
	for _, w := range ws {
		if w.cov > 0 {
			spreads = append(spreads, w.hi-w.lo)
		}
	}
	if len(spreads) == 0 {
		return nil
	}
	sort.Float64s(spreads)
	typical := spreads[len(spreads)/2]

	out := make([]point, 0, len(ws)*3)
	for k, w := range ws {
		if w.cov == 0 {
			if len(out) > 0 && out[len(out)-1].v != nil {
				out = append(out, point{t: from.Add(size*time.Duration(k) + size/2)})
			}
			continue
		}
		avg := w.avg()
		pts := []point{{t: w.a.Add(w.b.Sub(w.a) / 2), v: &avg}}
		if avg-w.lo > typical {
			lo := w.lo
			pts = append(pts, point{t: w.loT, v: &lo, q: w.loQ})
		}
		if w.hi-avg > typical {
			hi := w.hi
			pts = append(pts, point{t: w.hiT, v: &hi, q: w.hiQ})
		}
		sort.SliceStable(pts, func(i, j int) bool { return pts[i].t.Before(pts[j].t) })
		out = append(out, pts...)
	}
	return out
}

// peakFrame is one Auto series, named and labelled like historyFrame's.
func peakFrame(id, label, prop string, pts []point, withQuality bool) *data.Frame {
	if label == "" {
		label = id
	}
	name := label + " " + prop
	lbl := data.Labels{"instance": id, "property": prop}
	times := make([]time.Time, len(pts))
	values := make([]*float64, len(pts))
	quality := make([]*int64, len(pts))
	for i, p := range pts {
		times[i], values[i], quality[i] = p.t, p.v, p.q
	}
	f := data.NewFrame(name,
		data.NewField("Time", nil, times),
		data.NewField("Value", lbl, values).SetConfig(&data.FieldConfig{DisplayNameFromDS: name}),
	)
	if withQuality {
		f.Fields = append(f.Fields, data.NewField("Quality", lbl, quality).SetConfig(&data.FieldConfig{DisplayNameFromDS: name + " quality"}))
	}
	return f
}
