package plugin

import (
	"math"
	"testing"
	"time"

	"github.com/grafana/grafana-plugin-sdk-go/data"
)

// cypherSeries is one series as splitSeries gives it.
func cypherSeries(name string, times []time.Time, values []*float64) *data.Frame {
	lbl := data.Labels{"entity": "CNC01-SpindleMotor", "property": "RPM"}
	return data.NewFrame(name,
		data.NewField("Time", nil, times),
		data.NewField("value", lbl, values).SetConfig(&data.FieldConfig{DisplayNameFromDS: name}),
	)
}

func TestWeighSeriesHourMatchesDataView(t *testing.T) {
	from := time.Date(2026, 11, 19, 10, 0, 0, 0, time.UTC)
	to := from.Add(time.Hour)
	f := func(v float64) *float64 { return &v }
	// 100 for 50 minutes, then 0 for 10: the hour averages 83.3, not 50.
	fr := cypherSeries("CNC01-SpindleMotor RPM",
		[]time.Time{from, from.Add(50 * time.Minute)},
		[]*float64{f(100), f(0)})
	out := weighSeries(data.Frames{fr}, weightHour, from, to, 1000, true)
	if len(out) != 1 || len(out[0].Fields) != 4 {
		t.Fatalf("want one frame with time, value, min, max; got %v", out)
	}
	v, _ := out[0].Fields[1].ConcreteAt(0)
	if got := v.(float64); math.Abs(got-250.0/3) > 1e-9 {
		t.Fatalf("hour average %v, want 83.33", got)
	}
	if out[0].Name != "CNC01-SpindleMotor RPM" || out[0].Fields[1].Labels["entity"] != "CNC01-SpindleMotor" {
		t.Fatalf("name and labels not kept: %q %v", out[0].Name, out[0].Fields[1].Labels)
	}
}

func TestWeighSeriesAutoHoldsAStop(t *testing.T) {
	from := time.Date(2026, 11, 19, 10, 0, 0, 0, time.UTC)
	to := from.Add(2 * time.Hour)
	f := func(v float64) *float64 { return &v }
	// Running at 12,000 every second for 30 minutes, then one 0 that holds
	// for an hour (no values stored), then running again.
	var times []time.Time
	var values []*float64
	for s := 0; s < 1800; s++ {
		times, values = append(times, from.Add(time.Duration(s)*time.Second)), append(values, f(12000))
	}
	times, values = append(times, from.Add(30*time.Minute)), append(values, f(0))
	for s := 0; s < 1800; s++ {
		times, values = append(times, from.Add(90*time.Minute+time.Duration(s)*time.Second)), append(values, f(12000))
	}
	out := weighSeries(data.Frames{cypherSeries("s", times, values)}, weightAuto, from, to, 300, false)
	vf := out[0].Fields[1]
	if vf.Len() > 300 {
		t.Fatalf("%d points, want at most 300", vf.Len())
	}
	// Every point during the stop is 0, as the Data view draws it.
	tf := out[0].Fields[0]
	stopPoints := 0
	for i := 0; i < vf.Len(); i++ {
		ts, _ := tf.ConcreteAt(i)
		at := ts.(time.Time)
		if at.After(from.Add(31*time.Minute)) && at.Before(from.Add(89*time.Minute)) {
			v, _ := vf.ConcreteAt(i)
			if v.(float64) != 0 {
				t.Fatalf("point at %v is %v during the stop, want 0", at, v)
			}
			stopPoints++
		}
	}
	if stopPoints == 0 {
		t.Fatal("no points drawn during the stop")
	}
}

func TestWeighSeriesOffLeavesRows(t *testing.T) {
	fr := cypherSeries("s", []time.Time{time.Now()}, []*float64{nil})
	if out := weighSeries(data.Frames{fr}, "", time.Now(), time.Now(), 0, false); out[0] != fr {
		t.Fatal("weighting off should leave the series as returned")
	}
	if !validWeighting("none") || validWeighting("week") {
		t.Fatal("validWeighting")
	}
}
