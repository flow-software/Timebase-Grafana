package timebase

import "testing"

func TestStreamPattern(t *testing.T) {
	for tag, want := range map[string]string{
		"OpcPlc.Telemetry.Fast.FastUInt1":         "OpcPlc.Telemetry.Fast.FastUInt1",
		"Boilers.Boiler #1.BoilerStatus.Pressure": "Boilers.+.BoilerStatus.Pressure",
		"Mixer+2.Speed":                           "+.Speed",
		"131-TT-001.PV":                           "131-TT-001.PV",
	} {
		if got := streamPattern(tag); got != want {
			t.Errorf("streamPattern(%q) = %q, want %q", tag, got, want)
		}
	}
}
