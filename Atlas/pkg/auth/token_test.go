package auth

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func tokenServer(t *testing.T, expiresIn int, issued *atomic.Int32) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Fatal(err)
		}
		if r.Form.Get("grant_type") != "client_credentials" || r.Form.Get("client_id") != "Historian" || r.Form.Get("client_secret") != "s3cret" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			_, _ = fmt.Fprint(w, `{"error":"invalid_request","error_description":"Client 'x' could not be found"}`)
			return
		}
		n := issued.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprintf(w, `{"access_token":"tok-%d","token_type":"Bearer","expires_in":%d}`, n, expiresIn)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestClientCredentialsCachesUntilNearExpiry(t *testing.T) {
	var issued atomic.Int32
	srv := tokenServer(t, 3600, &issued)

	now := time.Date(2026, 10, 2, 10, 0, 0, 0, time.UTC)
	cc := &ClientCredentials{TokenURL: srv.URL, ClientID: "Historian", ClientSecret: "s3cret", HTTP: srv.Client(), now: func() time.Time { return now }}

	for i := 0; i < 3; i++ {
		tok, err := cc.Token(context.Background())
		if err != nil || tok != "tok-1" {
			t.Fatalf("call %d: got %q, %v; want cached tok-1", i, tok, err)
		}
	}

	// One second inside the refresh margin: renew.
	now = now.Add(time.Hour - refreshMargin + time.Second)
	if tok, _ := cc.Token(context.Background()); tok != "tok-2" {
		t.Fatalf("near expiry got %q, want renewed tok-2", tok)
	}
	if issued.Load() != 2 {
		t.Fatalf("issued %d tokens, want 2", issued.Load())
	}
}

func TestClientCredentialsInvalidate(t *testing.T) {
	var issued atomic.Int32
	srv := tokenServer(t, 3600, &issued)
	cc := &ClientCredentials{TokenURL: srv.URL, ClientID: "Historian", ClientSecret: "s3cret", HTTP: srv.Client()}

	_, _ = cc.Token(context.Background())
	cc.Invalidate()
	if tok, _ := cc.Token(context.Background()); tok != "tok-2" {
		t.Fatalf("after Invalidate got %q, want tok-2", tok)
	}
}

func TestClientCredentialsConcurrentCallersShareOneFetch(t *testing.T) {
	var issued atomic.Int32
	srv := tokenServer(t, 3600, &issued)
	cc := &ClientCredentials{TokenURL: srv.URL, ClientID: "Historian", ClientSecret: "s3cret", HTTP: srv.Client()}

	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := cc.Token(context.Background()); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	if issued.Load() != 1 {
		t.Fatalf("issued %d tokens, want 1", issued.Load())
	}
}

func TestClientCredentialsShortLifetimeRenewsAtHalf(t *testing.T) {
	var issued atomic.Int32
	srv := tokenServer(t, 60, &issued)
	now := time.Unix(0, 0)
	cc := &ClientCredentials{TokenURL: srv.URL, ClientID: "Historian", ClientSecret: "s3cret", HTTP: srv.Client(), now: func() time.Time { return now }}

	_, _ = cc.Token(context.Background())
	now = now.Add(29 * time.Second)
	_, _ = cc.Token(context.Background())
	now = now.Add(2 * time.Second)
	_, _ = cc.Token(context.Background())
	if issued.Load() != 2 {
		t.Fatalf("issued %d tokens, want 2 (cached until half of a 60s lifetime)", issued.Load())
	}
}

func TestClientCredentialsReportsOAuthError(t *testing.T) {
	var issued atomic.Int32
	srv := tokenServer(t, 3600, &issued)
	cc := &ClientCredentials{TokenURL: srv.URL, ClientID: "wrong", ClientSecret: "s3cret", HTTP: srv.Client()}

	_, err := cc.Token(context.Background())
	if err == nil || !strings.Contains(err.Error(), "invalid_request") || !strings.Contains(err.Error(), "could not be found") {
		t.Fatalf("got %v, want the OAuth error passed through", err)
	}
}

func TestClientCredentialsNonJSONHintsAtURL(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.WriteHeader(http.StatusMethodNotAllowed)
		_, _ = fmt.Fprint(w, "<!doctype html>")
	}))
	defer srv.Close()
	cc := &ClientCredentials{TokenURL: srv.URL + "/connect/token", ClientID: "Historian", ClientSecret: "s3cret", HTTP: srv.Client()}

	_, err := cc.Token(context.Background())
	if err == nil || !strings.Contains(err.Error(), "/auth/token") {
		t.Fatalf("got %v, want a hint about the token URL", err)
	}
}
