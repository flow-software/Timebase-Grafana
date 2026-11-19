package plugin

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/flow-software/Timebase-Grafana/Atlas/pkg/atlas"
	"github.com/grafana/grafana-plugin-sdk-go/data"
)

// --- wide table: one row per instance ---------------------------------------

type tableOptions struct {
	IncludeQuality bool
	IncludeTime    bool
}

// nodesTable makes one row per node: id, label, type, then a column per
// flattened property (see flatten). Live values can add a quality and a
// time column each.
func nodesTable(name string, nodes []map[string]any, opts tableOptions) *data.Frame {
	n := len(nodes)
	ids := make([]string, n)
	labels := make([]*string, n)
	types := make([]string, n)

	var order []string
	cols := map[string]*column{}
	qual := map[string][]*string{}
	times := map[string][]*time.Time{}
	get := func(key string) *column {
		c, ok := cols[key]
		if !ok {
			c = &column{name: key, cells: make([]any, n)}
			cols[key] = c
			qual[key] = make([]*string, n)
			times[key] = make([]*time.Time, n)
			order = append(order, key)
		}
		return c
	}

	for i, node := range nodes {
		ids[i] = str(node["entityId"])
		if l := str(node["entityLabel"]); l != "" {
			labels[i] = &l
		}
		types[i] = shortName(str(node["typeId"]))
		for _, e := range flatten(node) {
			get(e.Prop).cells[i] = e.Value
			if e.Quality != nil {
				q := qualityText(e.Quality, e.Value)
				qual[e.Prop][i] = &q
			}
			times[e.Prop][i] = e.Time
		}
	}
	sort.Strings(order)

	frame := data.NewFrame(name,
		data.NewField("Id", nil, ids),
		data.NewField("Label", nil, labels),
		data.NewField("Type", nil, types),
	)
	anyQuality := false
	for _, k := range order {
		frame.Fields = append(frame.Fields, cols[k].field())
		if opts.IncludeQuality && hasAny(qual[k]) {
			anyQuality = true
			frame.Fields = append(frame.Fields, data.NewField(k+" quality", nil, qual[k]))
		}
		if opts.IncludeTime && hasAny(times[k]) {
			frame.Fields = append(frame.Fields, timeField(k+" time", times[k]))
		}
	}
	if opts.IncludeQuality && !anyQuality && n > 0 {
		addNotice(frame, data.NoticeSeverityInfo, "No quality columns: none of these instances has a live value with a quality. Compound properties such as LiveAsset carry no quality.")
	}
	return frame
}

func hasAny[T any](s []*T) bool {
	for _, v := range s {
		if v != nil {
			return true
		}
	}
	return false
}

// --- graph -------------------------------------------------------------------

// graph accumulates nodes and edges, de-duplicated by id, in first-seen order.
type graph struct {
	nodes     []map[string]any
	nodeIndex map[string]int
	edges     []map[string]any
	edgeSeen  map[string]bool
}

func newGraph() *graph {
	return &graph{nodeIndex: map[string]int{}, edgeSeen: map[string]bool{}}
}

func (g *graph) addNode(v any) {
	m, ok := asMap(v)
	if !ok || !isNode(v) {
		return
	}
	id := str(m["entityId"])
	if _, seen := g.nodeIndex[id]; seen || id == "" {
		return
	}
	g.nodeIndex[id] = len(g.nodes)
	g.nodes = append(g.nodes, m)
}

func (g *graph) addEdge(v any) {
	m, ok := asMap(v)
	if !ok || !isRelationship(v) {
		return
	}
	g.addNode(m["startNode"])
	g.addNode(m["endNode"])
	id := str(m["entityId"])
	if g.edgeSeen[id] {
		return
	}
	g.edgeSeen[id] = true
	g.edges = append(g.edges, m)
}

// add walks any Cypher cell, including lists, for nodes and relationships.
func (g *graph) add(v any) {
	switch x := v.(type) {
	case []any:
		for _, e := range x {
			g.add(e)
		}
	default:
		if isRelationship(x) {
			g.addEdge(x)
		} else {
			g.addNode(x)
		}
	}
}

// Node colouring modes.
const (
	colorQuality = "quality" // ring split by the quality of the node's live values
	colorType    = "type"    // one colour per entity type
	colorNone    = "none"
)

type graphOptions struct {
	ColorBy string
	// RelationshipNodes draws each relationship as a small labelled node
	// between its ends, so relationship names are always visible. The Node
	// Graph panel otherwise shows them only on hover.
	RelationshipNodes bool
	// Containment is the set of relationship type URIs that are containment.
	Containment map[string]bool
	// LineColors, when set, colours each line by its relationship type
	// (see relationshipColors). Otherwise lines are coloured by kind.
	LineColors map[string]string
}

// Line colouring modes.
const (
	linesByKind = "kind" // grey containment, orange other relationships
	linesByType = "type" // one colour per relationship type
)

// RelationshipColor is one relationship type and its line colour.
type RelationshipColor struct {
	URI           string `json:"uri"`
	Name          string `json:"name"`
	IsContainment bool   `json:"isContainment"`
	Color         string `json:"color"`
}

// relationshipColors gives every relationship type in the domain's ontology a
// colour, in name order. Using the whole ontology rather than the types in one
// result keeps a type's colour the same in every panel and query.
func relationshipColors(types []atlas.RelationshipType) []RelationshipColor {
	out := make([]RelationshipColor, 0, len(types))
	for _, t := range types {
		name := t.Label
		if name == "" {
			name = shortName(t.URI)
		}
		out = append(out, RelationshipColor{URI: t.URI, Name: name, IsContainment: t.IsContainment})
	}
	sort.SliceStable(out, func(i, j int) bool {
		a, b := strings.ToLower(out[i].Name), strings.ToLower(out[j].Name)
		if a != b {
			return a < b
		}
		return out[i].URI < out[j].URI
	})
	// Two libraries can define types with the same label (atlas-common and
	// isa95-common both have "contains"); name the library so the key is clear.
	count := map[string]int{}
	for _, rc := range out {
		count[strings.ToLower(rc.Name)]++
	}
	for i := range out {
		out[i].Color = linePalette[i%len(linePalette)]
		if count[strings.ToLower(out[i].Name)] > 1 {
			out[i].Name += " (" + library(out[i].URI) + ")"
		}
	}
	return out
}

// library returns "isa95-common" from "urn:atlas:ontology:isa95-common:contains".
func library(uri string) string {
	parts := strings.Split(uri, ":")
	if len(parts) >= 2 {
		return parts[len(parts)-2]
	}
	return uri
}

// linePalette avoids the quality colours used on node rings.
var linePalette = []string{"#5794F2", "#FF9830", "#B877D9", "#8AB8FF", "#FFB357", "#CA95E5", "#1F60C4", "#FA6400", "#8F3BB8", "#C0D8FF", "#FFCB7D", "#DEB6F2", "#4FC3F7", "#F06292", "#A1887F", "#4DB6AC", "#BA68C8", "#90A4AE", "#FF8A65", "#7986CB", "#26A69A", "#D4E157", "#EC407A", "#8D6E63"}

// Colours. Quality uses Grafana's standard green/yellow/red; edges separate
// containment (structure) from other relationships (how things interact).
const (
	colGood      = "#73BF69"
	colUncertain = "#FADE2A"
	colBad       = "#F2495C"
	colNoData    = "#8E8E8E"
	colStatic    = "#5794F2"
	// colPlain is the ring with Colour "None". It is set explicitly, because
	// once the nodes frame has a color field (relationship-name nodes), an
	// empty colour draws no ring at all.
	colPlain       = "#9FA7B3"
	colContainment = "#6E6E6E"
	colRelation    = "#FF9830"
	colRelNode     = "#B877D9"
)

// typePalette is Grafana's classic palette; types get colours in name order,
// so the same model keeps the same colours.
var typePalette = []string{"#7EB26D", "#EAB839", "#6ED0E0", "#EF843C", "#E24D42", "#1F78C1", "#BA43A9", "#705DA0", "#508642", "#CCA300", "#447EBC", "#C15C17", "#890F02", "#0A437C", "#6D1F62", "#584477"}

// frames returns the two frames Grafana's Node Graph panel reads. Edges whose
// ends were not returned as nodes are dropped, because the panel cannot draw
// them; the count is reported as a notice.
func (g *graph) frames(opts graphOptions) data.Frames {
	nb := newNodeBuilder(g.nodes, opts.ColorBy)
	for _, node := range g.nodes {
		nb.addEntity(node)
	}

	eb := &edgeBuilder{colors: opts.LineColors}
	dropped := 0
	for _, e := range g.edges {
		s, t := str(e["Source"]), str(e["Target"])
		_, okS := g.nodeIndex[s]
		_, okT := g.nodeIndex[t]
		if !okS || !okT {
			dropped++
			continue
		}
		typeURI := str(e["typeId"])
		rel := shortName(typeURI)
		containment := opts.Containment[typeURI]
		kind := "Relationship"
		if containment {
			kind = "Containment"
		}
		id := str(e["entityId"])
		if opts.RelationshipNodes {
			mid := "rel:" + id
			nb.addRelationship(mid, rel, kind, eb.colorFor(typeURI, containment))
			eb.add(id+":in", s, mid, "", kind, typeURI, containment)
			eb.add(id+":out", mid, t, "", kind, typeURI, containment)
			continue
		}
		eb.add(id, s, t, rel, kind, typeURI, containment)
	}

	nodes := nb.frame()
	edges := eb.frame()
	if dropped > 0 {
		addNotice(edges, data.NoticeSeverityInfo, plural(dropped, "relationship")+" not drawn because an end node was not in the result")
	}
	return data.Frames{nodes, edges}
}

// nodeBuilder fills the nodes frame column by column.
type nodeBuilder struct {
	colorBy   string
	typeColor map[string]string

	ids, titles, subtitles, mainstat, colors, types []string
	radius                                          []float64
	arcs                                            [5][]float64 // good, uncertain, bad, no data, no live values
	details                                         map[string][]*string
	hasRelNodes                                     bool
	detailOrder                                     []string
}

func newNodeBuilder(nodes []map[string]any, colorBy string) *nodeBuilder {
	nb := &nodeBuilder{colorBy: colorBy, typeColor: map[string]string{}, details: map[string][]*string{}}
	var typeNames []string
	for _, n := range nodes {
		t := shortName(str(n["typeId"]))
		if _, ok := nb.typeColor[t]; !ok {
			nb.typeColor[t] = ""
			typeNames = append(typeNames, t)
		}
	}
	sort.Strings(typeNames)
	for i, t := range typeNames {
		nb.typeColor[t] = typePalette[i%len(typePalette)]
	}
	return nb
}

// addEntity adds an Atlas instance. Its flattened values go into detail__
// fields, which the panel lists when the node is clicked.
func (nb *nodeBuilder) addEntity(node map[string]any) {
	row := len(nb.ids)
	id := str(node["entityId"])
	title := str(node["entityLabel"])
	if title == "" {
		title = id
	}
	typ := shortName(str(node["typeId"]))

	var counts [5]float64
	live := 0
	for _, e := range flatten(node) {
		if e.Quality != nil {
			live++
			switch qualityText(e.Quality, e.Value) {
			case "Good":
				counts[0]++
			case "Uncertain":
				counts[1]++
			case "Bad":
				counts[2]++
			default:
				counts[3]++
			}
		}
		text := formatDetail(e)
		nb.setDetail(e.Prop, row, &text)
	}
	if live == 0 {
		counts[4] = 1 // configuration only: no live values
	} else {
		for k := range counts {
			counts[k] /= float64(live)
		}
	}

	ms := ""
	if live > 0 {
		ms = fmt.Sprintf("%d live", live)
	}
	color := ""
	switch nb.colorBy {
	case colorType:
		color = nb.typeColor[typ]
	case colorNone:
		color = colPlain
	}
	nb.append(id, title, typ, typ, ms, color, 40, counts)
}

// addRelationship adds the small node that carries a relationship's name.
func (nb *nodeBuilder) addRelationship(id, name, kind, color string) {
	// No arc values: the node is drawn in its colour field instead.
	nb.hasRelNodes = true
	nb.append(id, name, "", kind, "", color, 3, [5]float64{})
}

func (nb *nodeBuilder) append(id, title, subtitle, typ, mainstat, color string, radius float64, arcs [5]float64) {
	nb.ids = append(nb.ids, id)
	nb.titles = append(nb.titles, title)
	nb.subtitles = append(nb.subtitles, subtitle)
	nb.types = append(nb.types, typ)
	nb.mainstat = append(nb.mainstat, mainstat)
	nb.colors = append(nb.colors, color)
	nb.radius = append(nb.radius, radius)
	for k := range nb.arcs {
		nb.arcs[k] = append(nb.arcs[k], arcs[k])
	}
	// Keep every detail column as long as the node list.
	for k, col := range nb.details {
		for len(col) < len(nb.ids) {
			col = append(col, nil)
		}
		nb.details[k] = col
	}
}

func (nb *nodeBuilder) setDetail(prop string, row int, v *string) {
	col, ok := nb.details[prop]
	if !ok {
		nb.detailOrder = append(nb.detailOrder, prop)
	}
	for len(col) <= row {
		col = append(col, nil)
	}
	col[row] = v
	nb.details[prop] = col
}

func (nb *nodeBuilder) frame() *data.Frame {
	f := data.NewFrame("nodes",
		data.NewField("id", nil, nonNil(nb.ids)),
		data.NewField("title", nil, nonNil(nb.titles)),
		data.NewField("subtitle", nil, nonNil(nb.subtitles)),
		data.NewField("mainstat", nil, nonNil(nb.mainstat)).SetConfig(hiddenFromLegend("Live values")),
		data.NewField("noderadius", nil, nonNilF(nb.radius)).SetConfig(hiddenFromLegend("Size")),
	)
	// The colour field draws type colours, and relationship nodes in every
	// mode. Left out otherwise, because the panel lists it in the legend.
	if nb.colorBy == colorType || nb.hasRelNodes {
		f.Fields = append(f.Fields, data.NewField("color", nil, nonNil(nb.colors)).SetConfig(hiddenFromLegend("Colour")))
	}
	switch nb.colorBy {
	case colorType, colorNone:
	default:
		names := []string{"Good", "Uncertain", "Bad", "No data", "No live values"}
		colors := []string{colGood, colUncertain, colBad, colNoData, colStatic}
		keys := []string{"good", "uncertain", "bad", "nodata", "static"}
		for k := range nb.arcs {
			f.Fields = append(f.Fields, data.NewField("arc__"+keys[k], nil, nonNilF(nb.arcs[k])).SetConfig(&data.FieldConfig{
				DisplayName: names[k],
				Color:       map[string]any{"mode": "fixed", "fixedColor": colors[k]},
			}))
		}
	}
	f.Fields = append(f.Fields, data.NewField("detail__type", nil, nonNil(nb.types)).SetConfig(&data.FieldConfig{DisplayName: "Type"}))
	sort.Strings(nb.detailOrder)
	for _, k := range nb.detailOrder {
		col := nb.details[k]
		for len(col) < len(nb.ids) {
			col = append(col, nil)
		}
		f.Fields = append(f.Fields, data.NewField("detail__"+k, nil, col).SetConfig(&data.FieldConfig{DisplayName: k}))
	}
	f.Meta = &data.FrameMeta{PreferredVisualization: data.VisTypeNodeGraph}
	return f
}

// formatDetail shows a value with its quality and time, for example
// "2343.23 (Good, 2026-11-16 10:28:21Z)".
func formatDetail(e entry) string {
	v := "-"
	if e.Value != nil {
		switch x := e.Value.(type) {
		case float64:
			v = strconv.FormatFloat(x, 'f', -1, 64)
			if len(v) > 12 {
				v = strconv.FormatFloat(x, 'f', 2, 64)
			}
		default:
			v = jsonText(x)
		}
	}
	var extra []string
	if e.Quality != nil {
		extra = append(extra, qualityText(e.Quality, e.Value))
	}
	if e.Time != nil {
		extra = append(extra, e.Time.UTC().Format("2006-01-02 15:04:05Z"))
	}
	if len(extra) > 0 {
		v += " (" + strings.Join(extra, ", ") + ")"
	}
	return v
}

func nonNilF(s []float64) []float64 {
	if s == nil {
		return []float64{}
	}
	return s
}

// edgeBuilder fills the edges frame.
type edgeBuilder struct {
	colors map[string]string // by relationship type URI; nil means by kind

	ids, src, dst, main, kind, typeURI, color, dash []string
	thickness                                       []float64
}

func (eb *edgeBuilder) add(id, s, t, label, kind, typeURI string, containment bool) {
	eb.ids = append(eb.ids, id)
	eb.src = append(eb.src, s)
	eb.dst = append(eb.dst, t)
	eb.main = append(eb.main, label)
	eb.kind = append(eb.kind, kind)
	eb.typeURI = append(eb.typeURI, typeURI)
	// Containment stays solid and thin, other relationships dashed and
	// thicker, whichever way the lines are coloured.
	if c, ok := eb.colors[typeURI]; ok && eb.colors != nil {
		if containment {
			eb.color = append(eb.color, c)
			eb.thickness = append(eb.thickness, 1)
			eb.dash = append(eb.dash, "")
		} else {
			eb.color = append(eb.color, c)
			eb.thickness = append(eb.thickness, 2)
			eb.dash = append(eb.dash, "6 3")
		}
		return
	}
	if containment {
		eb.color = append(eb.color, colContainment)
		eb.thickness = append(eb.thickness, 1)
		eb.dash = append(eb.dash, "")
	} else {
		eb.color = append(eb.color, colRelation)
		eb.thickness = append(eb.thickness, 2)
		eb.dash = append(eb.dash, "6 3")
	}
}

func (eb *edgeBuilder) frame() *data.Frame {
	f := data.NewFrame("edges",
		data.NewField("id", nil, nonNil(eb.ids)),
		data.NewField("source", nil, nonNil(eb.src)),
		data.NewField("target", nil, nonNil(eb.dst)),
		data.NewField("mainstat", nil, nonNil(eb.main)).SetConfig(&data.FieldConfig{DisplayName: "Relationship"}),
		data.NewField("color", nil, nonNil(eb.color)),
		data.NewField("thickness", nil, nonNilF(eb.thickness)),
		data.NewField("strokedasharray", nil, nonNil(eb.dash)),
		data.NewField("detail__kind", nil, nonNil(eb.kind)).SetConfig(&data.FieldConfig{DisplayName: "Kind"}),
		data.NewField("detail__type", nil, nonNil(eb.typeURI)).SetConfig(&data.FieldConfig{DisplayName: "Relationship type"}),
	)
	f.Meta = &data.FrameMeta{PreferredVisualization: data.VisTypeNodeGraph}
	return f
}

// hiddenFromLegend keeps helper fields out of the Node Graph legend, which
// should list only the quality ring colours.
func hiddenFromLegend(name string) *data.FieldConfig {
	return &data.FieldConfig{DisplayName: name, Custom: map[string]any{"hideFrom": map[string]any{"legend": true, "tooltip": false, "viz": false}}}
}

// colorFor is the line colour for a relationship type.
func (eb *edgeBuilder) colorFor(typeURI string, containment bool) string {
	if c, ok := eb.colors[typeURI]; ok {
		return c
	}
	if containment {
		return colContainment
	}
	return colRelation
}
