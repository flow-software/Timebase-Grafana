package plugin

import (
	"context"
	"sort"
	"strconv"
	"strings"

	"github.com/grafana/grafana-plugin-sdk-go/data"

	"github.com/flow-software/Timebase-Grafana/Atlas/pkg/atlas"
)

// Enumerations (Atlas ADR 030): a property can be bound to an enumeration
// type, a closed set of keys with names and labels (PackML MachineState:
// 2 = Stopped, 6 = Execute). The stored value is the key. The plugin
// - never averages an enumeration (Stopped and Execute do not average to
//   a state in between): it is read raw and holds until the next value;
// - attaches Grafana value mappings, so tables, stats and state timelines
//   show the labels without set-up;
// - lists the members in the browse modal.

// EnumInfo is an enumeration as the browse tree and the frames use it.
type EnumInfo struct {
	Name  string `json:"name"`  // local name of the enumeration type
	Label string `json:"label"` // its display label, or the name
	// KeyKind: "number", "boolean" or "text", from the enumeration's key type.
	KeyKind string       `json:"keyKind,omitempty"`
	Members []EnumOption `json:"members"`
}

// EnumOption is one member: the stored key and what to show for it.
type EnumOption struct {
	Key   string `json:"key"`
	Label string `json:"label"`
}

func enumInfo(e *atlas.EnumType) *EnumInfo {
	if e == nil {
		return nil
	}
	out := &EnumInfo{Name: localName(e.URI), Label: e.Label, KeyKind: xsdKind(e.KeyType), Members: make([]EnumOption, 0, len(e.Members))}
	if out.Label == "" {
		out.Label = out.Name
	}
	for _, m := range e.Members {
		label := m.Label
		if label == "" {
			label = m.Name
		}
		if label == "" {
			label = m.Key
		}
		out.Members = append(out.Members, EnumOption{Key: m.Key, Label: label})
	}
	return out
}

// allEnums lists the domain's enumerations (its own and its libraries'), by label.
func (r *kindResolver) allEnums() []*EnumInfo {
	out := []*EnumInfo{}
	if r == nil {
		return out
	}
	for _, e := range r.enums {
		out = append(out, enumInfo(e))
	}
	sort.Slice(out, func(i, j int) bool { return strings.ToLower(out[i].Label) < strings.ToLower(out[j].Label) })
	return out
}

// enumInfoOf: type URI and property name to the enumeration, or nil.
func (r *kindResolver) enumInfoOf(typeURI, prop string) *EnumInfo {
	return enumInfo(r.enumOf(typeURI, prop))
}

// valueMappings maps each key to its label. Grafana matches a value by its
// text form, so number keys are normalised ("2.0" becomes "2").
func (e *EnumInfo) valueMappings() data.ValueMappings {
	if e == nil || len(e.Members) == 0 {
		return nil
	}
	vm := data.ValueMapper{}
	for i, m := range e.Members {
		key := m.Key
		if f, err := strconv.ParseFloat(key, 64); err == nil {
			key = strconv.FormatFloat(f, 'f', -1, 64)
		}
		vm[key] = data.ValueMappingResult{Text: m.Label, Color: enumColors[i%len(enumColors)], Index: i}
	}
	return data.ValueMappings{vm}
}

// enumColors: one Grafana colour per member, in member order, so a state
// has the same colour on every series (state timelines colour by it).
var enumColors = []string{"green", "blue", "yellow", "purple", "orange", "red", "light-blue", "dark-green", "light-purple", "semi-dark-orange"}

// applyEnum puts the enumeration's mappings on every value field of the
// frames (not Time, not Quality).
func applyEnum(frames data.Frames, e *EnumInfo) {
	if e == nil {
		return
	}
	for _, f := range frames {
		for i, fl := range f.Fields {
			if i == 0 && (fl.Type() == data.FieldTypeTime || fl.Type() == data.FieldTypeNullableTime) {
				continue
			}
			if fl.Name == "Quality" {
				continue
			}
			setMappings(fl, e)
		}
	}
}

func setMappings(fl *data.Field, e *EnumInfo) {
	if fl.Config == nil {
		fl.Config = &data.FieldConfig{}
	}
	fl.Config.Mappings = e.valueMappings()
}

// label is the member label for a stored value, or "".
func (e *EnumInfo) label(v any) string {
	if e == nil || v == nil {
		return ""
	}
	key := ""
	switch x := v.(type) {
	case string:
		key = x
	case bool:
		key = strconv.FormatBool(x)
	case float64:
		key = strconv.FormatFloat(x, 'f', -1, 64)
	case int64:
		key = strconv.FormatInt(x, 10)
	default:
		return ""
	}
	for _, m := range e.Members {
		if m.Key == key {
			return m.Label
		}
		if f, err := strconv.ParseFloat(m.Key, 64); err == nil && strconv.FormatFloat(f, 'f', -1, 64) == key {
			return m.Label
		}
	}
	return ""
}

// applyTableEnums puts mappings on the columns of a row-per-instance table
// that hold an enumeration (by property name; the first instance that binds
// one decides).
func applyTableEnums(frame *data.Frame, nodes []map[string]any, enumOf func(typeURI, prop string) *EnumInfo) {
	if frame == nil || enumOf == nil {
		return
	}
	for _, fl := range frame.Fields {
		for _, n := range nodes {
			if _, has := n[fl.Name]; !has {
				continue
			}
			if e := enumOf(str(n["typeId"]), fl.Name); e != nil {
				setMappings(fl, e)
				break
			}
		}
	}
}

// seriesEnums finds the enumeration of each Cypher time series from its
// labels: one label value is an instance id in the tree, another a live
// property of it ("entity, property, time, value" rows).
func (d *Datasource) seriesEnums(ctx context.Context, branch, domain string, frames data.Frames) map[*data.Frame]*EnumInfo {
	tree, err := d.trees.get(branch+"/"+domain, false, func() ([]TreeNode, error) { return d.instanceTree(ctx, branch, domain) })
	if err != nil {
		return nil
	}
	byID := make(map[string]TreeNode, len(tree))
	found := false
	for _, n := range tree {
		byID[n.ID] = n
		for _, l := range n.Live {
			if l.Enum != nil {
				found = true
			}
		}
	}
	if !found {
		return nil
	}
	out := map[*data.Frame]*EnumInfo{}
	for _, f := range frames {
		vf := seriesValueField(f)
		if vf == nil {
			continue
		}
		for _, v := range vf.Labels {
			n, ok := byID[v]
			if !ok {
				continue
			}
			for _, p := range vf.Labels {
				for _, l := range n.Live {
					if l.Name == p && l.Enum != nil {
						out[f] = l.Enum
					}
				}
			}
		}
	}
	return out
}
