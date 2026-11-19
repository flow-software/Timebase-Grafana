package plugin

import (
	"strings"

	"github.com/grafana/grafana-plugin-sdk-go/data"
)

// Units of measure.
//
// Atlas's type definitions carry no units. The atlas-common library puts a
// unit on the instance instead: a Tag holds Data plus a UOM text property
// (live in cesmii-workshop on the InfoHubKpi instances: "%", "min",
// "cycles", "stops"). The UOM describes the instance's value, so it is used
// for the Data property, or for any property of an instance that has only
// one live value.

// instanceUnit returns the Grafana unit for property prop of node n, or "".
func instanceUnit(n map[string]any, prop string) string {
	uom, _ := n["UOM"].(string)
	uom = strings.TrimSpace(uom)
	if uom == "" {
		return ""
	}
	if prop != "Data" {
		live := 0
		for k, v := range n {
			if !nodeMetaKeys[k] && hasHistory(v) {
				live++
			}
		}
		if live != 1 {
			return ""
		}
	}
	return grafanaUnit(uom)
}

// grafanaUnit maps common unit spellings to Grafana unit ids; anything else
// is shown as a suffix.
func grafanaUnit(uom string) string {
	switch strings.ToLower(uom) {
	case "%", "percent", "pct":
		return "percent"
	case "°c", "degc", "deg c", "celsius":
		return "celsius"
	case "°f", "degf", "deg f", "fahrenheit":
		return "fahrenheit"
	case "k", "kelvin":
		return "kelvin"
	case "ms":
		return "ms"
	case "s", "sec", "secs", "seconds":
		return "s"
	case "min", "mins", "minutes":
		return "m"
	case "h", "hr", "hrs", "hours":
		return "h"
	case "rpm":
		return "rotrpm"
	case "hz":
		return "hertz"
	case "w":
		return "watt"
	case "kw":
		return "kwatt"
	case "kwh":
		return "kwatth"
	case "v":
		return "volt"
	case "a":
		return "amp"
	case "bar":
		return "pressurebar"
	case "mbar":
		return "pressurembar"
	case "kpa":
		return "pressurekpa"
	case "psi":
		return "pressurepsi"
	case "kg":
		return "masskg"
	case "tonne", "tonnes":
		return "masst"
	case "mm":
		return "lengthmm"
	case "l", "litre", "liter", "litres", "liters":
		return "litre"
	case "m3", "m³":
		return "m3"
	}
	return "suffix: " + uom
}

// applyUnits sets the unit on every number field of the frames whose
// instance label has one.
func applyUnits(frames data.Frames, units map[string]string) {
	if len(units) == 0 {
		return
	}
	for _, f := range frames {
		for _, fl := range f.Fields {
			u := units[fl.Labels["instance"]]
			if u == "" || fl.Name == "Quality" || !fl.Type().Numeric() {
				continue
			}
			if fl.Config == nil {
				fl.Config = &data.FieldConfig{}
			}
			fl.Config.Unit = u
		}
	}
}
