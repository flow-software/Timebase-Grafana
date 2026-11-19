package plugin

import "testing"

func TestInstanceUnits(t *testing.T) {
	tvq := map[string]any{"Timestamp": "2026-11-18T05:00:00Z", "Value": 3.0, "Quality": 192}
	kpi := map[string]any{"entityId": "ih-6873", "typeId": "urn:x:InfoHubKpi", "UOM": "%", "Data": tvq}
	if got := instanceUnit(kpi, "Data"); got != "percent" {
		t.Fatalf("Data with UOM %%: %q", got)
	}
	two := map[string]any{"entityId": "m", "typeId": "urn:x:M", "UOM": "min", "A": tvq, "B": tvq}
	if instanceUnit(two, "A") != "" {
		t.Fatal("a UOM on an instance with two live values must not be guessed onto one of them")
	}
	one := map[string]any{"entityId": "c", "typeId": "urn:x:C", "UOM": "cycles", "Count": tvq}
	if got := instanceUnit(one, "Count"); got != "suffix: cycles" {
		t.Fatalf("unknown unit: %q", got)
	}
	if instanceUnit(map[string]any{"UOM": " ", "Data": tvq}, "Data") != "" {
		t.Fatal("blank UOM")
	}
}
