package plugin

import (
	"time"

	"github.com/grafana/grafana-plugin-sdk-go/data"
)

// Cypher time series can be time-weighted like the Data view: the query
// returns stored values (time, value), and the plugin draws them with the
// same code the Data view uses. "Edit as Cypher" turns this on, so the
// panel looks the same before and after the switch.
const (
	weightAuto   = "auto"
	weightMinute = "minute"
	weightHour   = "hour"
	weightDay    = "day"
)

var weightUnit = map[string]time.Duration{weightMinute: time.Minute, weightHour: time.Hour, weightDay: 24 * time.Hour}

// validWeighting reports whether w is a known Cypher weighting ("" is off).
func validWeighting(w string) bool {
	_, fixed := weightUnit[w]
	return w == "" || w == "none" || w == weightAuto || fixed
}

// weighSeries time-weights each series from splitSeries. The value is the
// field named "value", or else the first number field; the others are
// dropped. Every row is one stored value that holds until the next.
// keep, when set, names series to leave as returned (enumerations).
func weighSeries(frames data.Frames, weighting string, from, to time.Time, maxPoints int, withMinMax bool, keep func(*data.Frame) bool) data.Frames {
	if weighting == "" || weighting == "none" {
		return frames
	}
	end := to
	if now := time.Now(); now.Before(end) {
		end = now
	}
	if maxPoints <= 0 {
		maxPoints = defaultMaxPoints
	}
	if maxPoints > maxMaxPoints {
		maxPoints = maxMaxPoints
	}
	out := make(data.Frames, 0, len(frames))
	for _, f := range frames {
		vf := seriesValueField(f)
		if vf == nil || len(f.Fields) == 0 || (keep != nil && keep(f)) {
			out = append(out, f)
			continue
		}
		tf := f.Fields[0]
		ss := make([]sample, 0, f.Rows())
		for i := 0; i < f.Rows(); i++ {
			tv, ok := tf.ConcreteAt(i)
			ts, isTime := tv.(time.Time)
			if !ok || !isTime {
				continue
			}
			v, isNum := numberAt(vf, i)
			if !isNum {
				ss = append(ss, sample{t: ts, null: true})
				continue
			}
			ss = append(ss, sample{t: ts, avg: v, lo: v, hi: v, loT: ts, hiT: ts, n: 1})
		}
		name := f.Name
		lbl := vf.Labels
		var nf *data.Frame
		if size, fixed := weightUnit[weighting]; fixed {
			bs := fixedBuckets(ss, from, to, end, size)
			times := make([]time.Time, len(bs))
			avg := make([]*float64, len(bs))
			mins := make([]*float64, len(bs))
			maxs := make([]*float64, len(bs))
			for i, b := range bs {
				times[i], avg[i], mins[i], maxs[i] = b.t, b.avg, b.lo, b.hi
			}
			nf = data.NewFrame(name,
				data.NewField("Time", nil, times),
				data.NewField("value", lbl.Copy(), avg).SetConfig(&data.FieldConfig{DisplayNameFromDS: name}),
			)
			if withMinMax {
				nf.Fields = append(nf.Fields,
					data.NewField("min", lbl.Copy(), mins).SetConfig(&data.FieldConfig{DisplayNameFromDS: name + " min"}),
					data.NewField("max", lbl.Copy(), maxs).SetConfig(&data.FieldConfig{DisplayNameFromDS: name + " max"}),
				)
			}
		} else {
			pts := peakLine(ss, from, to, end, maxPoints, true)
			times := make([]time.Time, len(pts))
			values := make([]*float64, len(pts))
			for i, p := range pts {
				times[i], values[i] = p.t, p.v
			}
			nf = data.NewFrame(name,
				data.NewField("Time", nil, times),
				data.NewField("value", lbl.Copy(), values).SetConfig(&data.FieldConfig{DisplayNameFromDS: name}),
			)
		}
		nf.Meta = f.Meta
		out = append(out, nf)
	}
	return out
}

// seriesValueField is the field named "value", or the first number field after the time.
func seriesValueField(f *data.Frame) *data.Field {
	var first *data.Field
	for i, fl := range f.Fields {
		if i == 0 || !isNumberField(fl) {
			continue
		}
		if fl.Name == "value" {
			return fl
		}
		if first == nil {
			first = fl
		}
	}
	return first
}

func isNumberField(f *data.Field) bool {
	switch f.Type() {
	case data.FieldTypeFloat64, data.FieldTypeNullableFloat64,
		data.FieldTypeInt64, data.FieldTypeNullableInt64:
		return true
	}
	return false
}

func numberAt(f *data.Field, i int) (float64, bool) {
	v, ok := f.ConcreteAt(i)
	if !ok {
		return 0, false
	}
	switch x := v.(type) {
	case float64:
		return x, true
	case int64:
		return float64(x), true
	}
	return 0, false
}
