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

// APIs a datasource can talk to. Both are served by the same historian and
// share its Pulse authentication.
const (
	APITimebase = "timebase" // native REST API under /api
	APII3X      = "i3x"      // i3X standard API under /i3x/v1
)

// PluginSettings is the datasource configuration. TLS options (skip verify,
// CA certificate) use Grafana's standard jsonData/secureJsonData keys and are
// read by the SDK's HTTP client options, so they are not repeated here.
type PluginSettings struct {
	URL      string `json:"-"`
	APIType  string `json:"apiType"`
	AuthType string `json:"authType"`
	TokenURL string `json:"tokenUrl"`
	ClientID string `json:"clientId"`
	Scope    string `json:"scope"`

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
	if settings.AuthType == "" {
		settings.AuthType = AuthClientCredentials
	}
	// Datasources created before the API choice existed all used i3X.
	if settings.APIType == "" {
		settings.APIType = APII3X
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
	switch s.APIType {
	case APITimebase:
		if s.URL == "" {
			return fmt.Errorf("URL is required, for example https://historian:4512")
		}
	case APII3X:
		if s.URL == "" {
			return fmt.Errorf("URL is required, for example https://historian:4512/i3x/v1")
		}
	default:
		return fmt.Errorf("unknown API %q", s.APIType)
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
