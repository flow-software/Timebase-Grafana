package models

import (
	"testing"

	"github.com/grafana/grafana-plugin-sdk-go/backend"
)

func TestAPITypeDefaultsToNative(t *testing.T) {
	s, err := LoadPluginSettings(backend.DataSourceInstanceSettings{URL: "https://historian:4512", JSONData: []byte(`{}`)})
	if err != nil {
		t.Fatal(err)
	}
	if s.APIType != APITimebase {
		t.Fatalf("unset apiType should mean the native API, got %q", s.APIType)
	}
}
