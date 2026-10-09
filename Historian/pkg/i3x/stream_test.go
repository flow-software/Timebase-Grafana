package i3x

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/flow-software/Timebase-Grafana/Historian/pkg/series"
)

// fakeSubscriptions follows the Historian 2.0.813 flow: create, register,
// stream (SSE), delete.
func fakeSubscriptions(t *testing.T, deleted *atomic.Bool) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	reply := func(w http.ResponseWriter, body string) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, body)
	}
	mux.HandleFunc("POST /subscriptions", func(w http.ResponseWriter, r *http.Request) {
		reply(w, `{"success":true,"result":{"clientId":"grafana-timebase-datasource","subscriptionId":"sub-1"}}`)
	})
	mux.HandleFunc("POST /subscriptions/register", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			SubscriptionID string   `json:"subscriptionId"`
			ElementIDs     []string `json:"elementIds"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		if req.SubscriptionID != "sub-1" || len(req.ElementIDs) != 1 {
			t.Errorf("register got %+v", req)
		}
		reply(w, fmt.Sprintf(`{"success":true,"results":[{"success":true,"elementId":%q}]}`, req.ElementIDs[0]))
	})
	mux.HandleFunc("POST /subscriptions/stream", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = fmt.Fprint(w, ": stream open\n\n")
		for i := 0; ; i++ {
			_, _ = fmt.Fprintf(w, "data: [{\"elementId\":\"plant:Other\",\"value\":99,\"quality\":\"Good\",\"timestamp\":\"2026-10-05T18:49:30Z\"},{\"elementId\":\"plant:Fast\",\"value\":%d,\"quality\":\"Good\",\"timestamp\":\"2026-10-05T18:49:30Z\"}]\n\n", i)
			w.(http.Flusher).Flush()
			select {
			case <-r.Context().Done():
				return
			case <-time.After(10 * time.Millisecond):
			}
		}
	})
	mux.HandleFunc("POST /subscriptions/delete", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			SubscriptionIDs []string `json:"subscriptionIds"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		if len(req.SubscriptionIDs) == 1 && req.SubscriptionIDs[0] == "sub-1" {
			deleted.Store(true)
		}
		reply(w, `{"success":true,"results":[{"success":true}]}`)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func TestStreamForwardsItsElementAndDeletesSubscription(t *testing.T) {
	var deleted atomic.Bool
	srv := fakeSubscriptions(t, &deleted)
	c := &Client{BaseURL: srv.URL, HTTP: srv.Client()}

	ctx, cancel := context.WithCancel(context.Background())
	var got []series.Sample
	err := c.Stream(ctx, "plant:Fast", func(s series.Sample) {
		got = append(got, s)
		if len(got) == 3 {
			cancel()
		}
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("got %v, want context.Canceled", err)
	}
	for i, s := range got {
		if string(s.Value) == "99" || s.Quality != "Good" || s.Timestamp.IsZero() {
			t.Fatalf("sample %d = %+v", i, s)
		}
	}
	if !deleted.Load() {
		t.Fatal("subscription was not deleted after the stream ended")
	}
}
