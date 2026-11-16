package models

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/grafana/grafana-plugin-sdk-go/backend"
)

// Authentication modes offered in the config editor.
const (
	AuthClientCredentials = "clientCredentials"
	AuthBearerToken       = "bearerToken"
	AuthNone              = "none"
)

// DefaultBranch is used when neither the datasource nor the query names one.
const DefaultBranch = "main"

// PluginSettings is the datasource configuration. TLS options (skip verify,
// CA certificate) use Grafana's standard jsonData/secureJsonData keys and are
// read by the SDK's HTTP client options, so they are not repeated here.
type PluginSettings struct {
	URL      string `json:"-"`
	AuthType string `json:"authType"`
	TokenURL string `json:"tokenUrl"`
	ClientID string `json:"clientId"`
	Scope    string `json:"scope"`

	// Defaults for queries that do not pick their own world.
	DefaultBranch string `json:"defaultBranch"`
	DefaultDomain string `json:"defaultDomain"`

	Secrets *SecretPluginSettings `json:"-"`
}

type SecretPluginSettings struct {
	ClientSecret string
	BearerToken  string
}

func LoadPluginSettings(source backend.DataSourceInstanceSettings) (*PluginSettings, error) {
	settings := PluginSettings{}
	if len(source.JSONData) > 0 {
		if err := json.Unmarshal(source.JSONData, &settings); err != nil {
			return nil, fmt.Errorf("could not unmarshal PluginSettings json: %w", err)
		}
	}
	settings.URL = strings.TrimRight(strings.TrimSpace(source.URL), "/")
	settings.TokenURL = strings.TrimSpace(settings.TokenURL)
	settings.DefaultBranch = strings.TrimSpace(settings.DefaultBranch)
	settings.DefaultDomain = strings.TrimSpace(settings.DefaultDomain)
	if settings.DefaultBranch == "" {
		settings.DefaultBranch = DefaultBranch
	}
	if settings.AuthType == "" {
		settings.AuthType = AuthClientCredentials
	}

	// People paste the whole header value; the "Bearer " prefix is added on send.
	token := strings.TrimSpace(source.DecryptedSecureJSONData["bearerToken"])
	if len(token) > 7 && strings.EqualFold(token[:7], "bearer ") {
		token = strings.TrimSpace(token[7:])
	}
	settings.Secrets = &SecretPluginSettings{
		ClientSecret: source.DecryptedSecureJSONData["clientSecret"],
		BearerToken:  token,
	}
	return &settings, nil
}

// Validate reports the first missing setting, worded for the config page.
func (s *PluginSettings) Validate() error {
	if s.URL == "" {
		return fmt.Errorf("URL is required, for example https://atlas:4552")
	}
	if strings.Contains(strings.ToLower(s.URL), "/i3x") {
		return fmt.Errorf("use the Atlas address without a path; this data source does not use the i3X API")
	}
	switch s.AuthType {
	case AuthClientCredentials:
		switch {
		case s.TokenURL == "":
			return fmt.Errorf("token URL is required, for example https://pulse:4542/auth/token")
		case s.ClientID == "":
			return fmt.Errorf("client ID is required")
		case s.Secrets.ClientSecret == "":
			return fmt.Errorf("client secret is required")
		}
	case AuthBearerToken:
		if s.Secrets.BearerToken == "" {
			return fmt.Errorf("bearer token is required")
		}
	case AuthNone:
	default:
		return fmt.Errorf("unknown authentication type %q", s.AuthType)
	}
	return nil
}
