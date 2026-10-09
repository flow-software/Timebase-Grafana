package i3x

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

// rotatingAuth hands out tok-1, then tok-2 after Invalidate.
type rotatingAuth struct{ n atomic.Int32 }

func (a *rotatingAuth) Token(context.Context) (string, error) {
	return fmt.Sprintf("tok-%d", a.n.Load()+1), nil
}
func (a *rotatingAuth) Invalidate() { a.n.Add(1) }

func TestClientRetriesOnceWithFreshTokenAfter401(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.Header.Get("Authorization") != "Bearer tok-2" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_, _ = fmt.Fprint(w, `{"success":true,"result":{"serverName":"Timebase Historian","capabilities":{"query":{"history":true}}}}`)
	}))
	defer srv.Close()

	c := &Client{BaseURL: srv.URL, HTTP: srv.Client(), Auth: &rotatingAuth{}}
	info, err := c.Info(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if info.ServerName != "Timebase Historian" || calls.Load() != 2 {
		t.Fatalf("got %+v after %d calls; want success on the second call", info, calls.Load())
	}
}

func TestClientGivesUpAfterSecond401(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()

	c := &Client{BaseURL: srv.URL, HTTP: srv.Client(), Auth: &rotatingAuth{}}
	_, err := c.Info(context.Background())
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.Status != http.StatusUnauthorized || calls.Load() != 2 {
		t.Fatalf("got %v after %d calls; want a 401 APIError after exactly 2", err, calls.Load())
	}
}

func TestClientDetectsHTMLFallback(t *testing.T) {
	// The historian serves its web UI for unknown paths, e.g. /i3x/info
	// when the base URL is missing /v1.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = fmt.Fprint(w, "<!doctype html><html></html>")
	}))
	defer srv.Close()

	c := &Client{BaseURL: srv.URL + "/i3x", HTTP: srv.Client()}
	if _, err := c.Info(context.Background()); !errors.Is(err, ErrNotJSON) {
		t.Fatalf("got %v, want ErrNotJSON", err)
	}
}
