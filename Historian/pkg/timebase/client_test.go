package timebase

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestNormalizeBaseURL(t *testing.T) {
	for in, want := range map[string]string{
		"https://historian:4512":         "https://historian:4512",
		" https://historian:4512/ ":      "https://historian:4512",
		"https://historian:4512/api":     "https://historian:4512",
		"https://historian:4512/api/":    "https://historian:4512",
		"https://historian:4512/i3x/v1":  "https://historian:4512",
		"https://proxy/timebase/i3x/v1/": "https://proxy/timebase",
	} {
		if got := NormalizeBaseURL(in); got != want {
			t.Errorf("NormalizeBaseURL(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestTagListAcceptsBothShapes(t *testing.T) {
	// What Historian 2.0.813 returns...
	var split TagList
	if err := json.Unmarshal([]byte(`{"User":[{"n":"A"}],"System":[{"n":"Dataset.Size","u":{"1":"GB"}}]}`), &split); err != nil {
		t.Fatal(err)
	}
	if len(split.User) != 1 || len(split.System) != 1 || split.System[0].Units["1"] != "GB" {
		t.Fatalf("got %+v", split)
	}
	// ...and the plain array the API spec documents.
	var plain TagList
	if err := json.Unmarshal([]byte(`[{"n":"A"},{"n":"B"}]`), &plain); err != nil {
		t.Fatal(err)
	}
	if len(plain.User) != 2 || len(plain.System) != 0 {
		t.Fatalf("got %+v", plain)
	}
}

func TestDataSendsRepeatedTagnamesAndParsesErrors(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		tags := r.URL.Query()["tagname"]
		if r.URL.Path != "/api/datasets/my set/data" || len(tags) != 2 || r.URL.Query().Get("start") == "" {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		if tags[1] == "missing" {
			w.WriteHeader(http.StatusNotFound)
			_, _ = fmt.Fprint(w, `{"key":"Error.TagNotFound","message":"Tag 'missing' could not be found.","errorCode":1}`)
			return
		}
		_, _ = fmt.Fprint(w, `{"tl":[{"t":{"n":"a","t":"System.Double"},"d":[{"t":"2026-10-05T18:32:09Z","v":1,"q":192}]}]}`)
	}))
	defer srv.Close()
	c := &Client{BaseURL: srv.URL + "/api", HTTP: srv.Client()}

	got, err := c.Data(context.Background(), "my set", []string{"a", "b"}, testTime, testTime)
	if err != nil || len(got) != 1 || got[0].Tag.Name != "a" || got[0].Data[0].Q != 192 {
		t.Fatalf("got %+v, %v", got, err)
	}

	_, err = c.Data(context.Background(), "my set", []string{"a", "missing"}, testTime, testTime)
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.Key != ErrKeyTagNotFound || apiErr.Status != 404 || apiErr.Message != "Tag 'missing' could not be found." {
		t.Fatalf("got %v", err)
	}
}

func TestHTMLMeansWrongURL(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = fmt.Fprint(w, "<!doctype html>")
	}))
	defer srv.Close()
	c := &Client{BaseURL: srv.URL, HTTP: srv.Client()}
	if _, err := c.Datasets(context.Background()); !errors.Is(err, ErrNotJSON) {
		t.Fatalf("got %v, want ErrNotJSON", err)
	}
}

var testTime = mustTime("2026-10-05T18:32:00Z")

func mustTime(s string) (t time.Time) {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		panic(err)
	}
	return t
}
