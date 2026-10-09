package auth

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// TokenSource supplies the bearer token sent with every API call.
type TokenSource interface {
	// Token returns a token that is valid now, fetching a new one if needed.
	Token(ctx context.Context) (string, error)
	// Invalidate drops any cached token so the next Token call fetches a new
	// one. The client calls it when the API rejects a token with 401, which
	// covers a token revoked or re-keyed before its stated expiry.
	Invalidate()
}

// StaticToken is a fixed bearer token pasted by the user. It is never renewed.
type StaticToken string

func (t StaticToken) Token(context.Context) (string, error) { return string(t), nil }
func (t StaticToken) Invalidate()                           {}

// refreshMargin is how long before expiry a cached token is replaced, so a
// token never expires between being handed out and reaching the server.
const refreshMargin = 60 * time.Second

// ClientCredentials fetches tokens with the OAuth 2.0 client_credentials grant
// and caches each one until shortly before it expires. Timebase Pulse expects
// the credentials as form fields in the body rather than as basic auth.
type ClientCredentials struct {
	TokenURL     string
	ClientID     string
	ClientSecret string
	Scope        string
	HTTP         *http.Client

	// now is replaceable in tests.
	now func() time.Time

	mu      sync.Mutex
	token   string
	renewAt time.Time
}

type tokenResponse struct {
	AccessToken      string `json:"access_token"`
	TokenType        string `json:"token_type"`
	ExpiresIn        int64  `json:"expires_in"`
	Error            string `json:"error"`
	ErrorDescription string `json:"error_description"`
}

func (c *ClientCredentials) clock() time.Time {
	if c.now != nil {
		return c.now()
	}
	return time.Now()
}

// Token holds the lock while fetching, so concurrent queries that find the
// cache empty wait for one token request instead of each sending their own.
func (c *ClientCredentials) Token(ctx context.Context) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.token != "" && c.clock().Before(c.renewAt) {
		return c.token, nil
	}

	tr, err := c.fetch(ctx)
	if err != nil {
		return "", err
	}

	lifetime := time.Duration(tr.ExpiresIn) * time.Second
	switch {
	case lifetime <= 0:
		// No expiry given: renew on every request rather than guess.
		c.token, c.renewAt = "", time.Time{}
		return tr.AccessToken, nil
	case lifetime > 2*refreshMargin:
		c.renewAt = c.clock().Add(lifetime - refreshMargin)
	default:
		c.renewAt = c.clock().Add(lifetime / 2)
	}
	c.token = tr.AccessToken
	return c.token, nil
}

func (c *ClientCredentials) Invalidate() {
	c.mu.Lock()
	c.token, c.renewAt = "", time.Time{}
	c.mu.Unlock()
}

func (c *ClientCredentials) fetch(ctx context.Context) (*tokenResponse, error) {
	form := url.Values{
		"grant_type":    {"client_credentials"},
		"client_id":     {c.ClientID},
		"client_secret": {c.ClientSecret},
	}
	if c.Scope != "" {
		form.Set("scope", c.Scope)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.TokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, fmt.Errorf("invalid token URL: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")

	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, fmt.Errorf("token request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))

	var tr tokenResponse
	if err := json.Unmarshal(body, &tr); err != nil {
		return nil, fmt.Errorf("token endpoint returned %d with a non-JSON body; check the token URL (for Timebase Pulse it is https://<pulse-host>:4542/auth/token)", resp.StatusCode)
	}
	if resp.StatusCode != http.StatusOK || tr.AccessToken == "" {
		if tr.Error != "" {
			return nil, fmt.Errorf("token endpoint returned %d: %s: %s", resp.StatusCode, tr.Error, tr.ErrorDescription)
		}
		return nil, fmt.Errorf("token endpoint returned %d without an access token", resp.StatusCode)
	}
	return &tr, nil
}
