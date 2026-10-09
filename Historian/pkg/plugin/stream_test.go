package plugin

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/grafana/grafana-plugin-sdk-go/backend"
	"github.com/grafana/grafana-plugin-sdk-go/data"
)

func TestParseLivePath(t *testing.T) {
	// The same vectors are pinned in src/live.test.ts, so both sides agree.
	for path, want := range map[string]liveChannel{
		"tag/-/ZGVmYXVsdDpCb2lsZXJzLkJvaWxlciAjMS5Cb2lsZXJTdGF0dXMuUHJlc3N1cmU": {ElementID: "default:Boilers.Boiler #1.BoilerStatus.Pressure"},
		"tag/n/cGxhbnQ6VGVtcMOpcmF0dXJlLsKwRg":                                  {ElementID: "plant:Température.°F", BadQualityAsNull: true},
	} {
		got, err := parseLivePath(path)
		if err != nil || got != want {
			t.Errorf("parseLivePath(%q) = %+v, %v; want %+v", path, got, err, want)
		}
	}
	for _, bad := range []string{"", "tag/", "tag/x/YQ", "tag/-/", "tag/-/!!", "other/-/YQ"} {
		if _, err := parseLivePath(bad); err == nil {
			t.Errorf("parseLivePath(%q) should fail", bad)
		}
	}
}

// fakeLiveNative serves Pulse and the native API with one tag whose stream
// sends a sample every 20 ms, and counts the open streams.
func fakeLiveNative(t *testing.T, open *atomic.Int32) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("POST /auth/token", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `{"access_token":"tok","expires_in":3600}`)
	})
	mux.HandleFunc("GET /api/datasets/plant/data", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `{"tl":[{"t":{"n":"Line #1.Speed","u":{"1":"m/s"},"t":"System.Double"},"d":[{"t":"2026-10-05T18:00:00Z","v":1,"q":192},{"t":"2026-10-05T18:00:01Z","v":2,"q":192}]}]}`)
	})
	mux.HandleFunc("GET /api/datasets/plant/data/stream", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("pattern") != "+.Speed" {
			t.Errorf("pattern %q: a level containing # must be widened to +", r.URL.Query().Get("pattern"))
		}
		open.Add(1)
		defer open.Add(-1)
		w.Header().Set("Content-Type", "text/event-stream")
		flusher := w.(http.Flusher)
		for i := 3; ; i++ {
			// A same-pattern tag the plugin must filter out, then ours.
			_, _ = fmt.Fprintf(w, "event: data\ndata: {\"n\":\"Line 2.Speed\",\"t\":\"2026-10-05T18:00:0%dZ\",\"v\":99,\"q\":192}\n\n", i%10)
			_, _ = fmt.Fprintf(w, "event: data\ndata: {\"n\":\"Line #1.Speed\",\"t\":\"2026-10-05T18:00:0%dZ\",\"v\":%d,\"q\":192}\n\n", i%10, i)
			flusher.Flush()
			select {
			case <-r.Context().Done():
				return
			case <-time.After(20 * time.Millisecond):
			}
		}
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func newLiveDatasource(t *testing.T, srv *httptest.Server) *Datasource {
	t.Helper()
	inst, err := NewDatasource(context.Background(), backend.DataSourceInstanceSettings{
		URL:                     srv.URL,
		JSONData:                []byte(fmt.Sprintf(`{"apiType":"timebase","tokenUrl":%q,"clientId":"c"}`, srv.URL+"/auth/token")),
		DecryptedSecureJSONData: map[string]string{"clientSecret": "s"},
	})
	if err != nil {
		t.Fatal(err)
	}
	return inst.(*Datasource)
}

// "plant:Line #1.Speed" with flags "-".
const testLivePath = "tag/-/cGxhbnQ6TGluZSAjMS5TcGVlZA"

func TestSubscribeStreamSendsHistory(t *testing.T) {
	var open atomic.Int32
	ds := newLiveDatasource(t, fakeLiveNative(t, &open))

	body, _ := json.Marshal(subscribeData{From: time.Now().Add(-time.Hour).UnixMilli(), To: time.Now().UnixMilli()})
	resp, err := ds.SubscribeStream(context.Background(), &backend.SubscribeStreamRequest{Path: testLivePath, Data: body})
	if err != nil || resp.Status != backend.SubscribeStreamStatusOK || resp.InitialData == nil {
		t.Fatalf("got %+v, %v", resp, err)
	}
	var frame data.Frame
	if err := json.Unmarshal(resp.InitialData.Data(), &frame); err != nil {
		t.Fatal(err)
	}
	if frame.Rows() != 2 || frame.Fields[1].Config.Unit != "suffix: m/s" {
		t.Fatalf("initial frame has %d rows, unit %q", frame.Rows(), frame.Fields[1].Config.Unit)
	}

	bad, _ := ds.SubscribeStream(context.Background(), &backend.SubscribeStreamRequest{Path: "tag/-/!!"})
	if bad.Status != backend.SubscribeStreamStatusNotFound {
		t.Fatalf("malformed path gave %v", bad.Status)
	}
}

type packetRecorder struct {
	mu      sync.Mutex
	packets []*backend.StreamPacket
}

func (p *packetRecorder) Send(pkt *backend.StreamPacket) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.packets = append(p.packets, pkt)
	return nil
}

func (p *packetRecorder) count() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.packets)
}

func TestRunStreamForwardsOnlyItsTagAndStops(t *testing.T) {
	var open atomic.Int32
	ds := newLiveDatasource(t, fakeLiveNative(t, &open))

	rec := &packetRecorder{}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- ds.RunStream(ctx, &backend.RunStreamRequest{Path: testLivePath}, backend.NewStreamSender(rec))
	}()

	deadline := time.Now().Add(3 * time.Second)
	for rec.count() < 3 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("RunStream returned %v after cancel, want nil", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("RunStream did not stop after cancel")
	}

	if rec.count() < 3 {
		t.Fatalf("got %d packets, want at least 3", rec.count())
	}
	for _, pkt := range rec.packets {
		// Live packets are data-only (the schema went out with the initial
		// frame): {"data":{"values":[[time],[value]]}}.
		var p struct {
			Schema json.RawMessage `json:"schema"`
			Data   struct {
				Values [][]any `json:"values"`
			} `json:"data"`
		}
		if err := json.Unmarshal(pkt.Data, &p); err != nil {
			t.Fatal(err)
		}
		if p.Schema != nil {
			t.Fatal("live packets should not repeat the schema")
		}
		if len(p.Data.Values) != 2 || len(p.Data.Values[1]) != 1 || p.Data.Values[1][0] == float64(99) {
			t.Fatalf("unexpected packet %s", pkt.Data)
		}
	}

	deadline = time.Now().Add(2 * time.Second)
	for open.Load() != 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if open.Load() != 0 {
		t.Fatal("the stream to the historian was left open after the channel ended")
	}
}

func TestPublishStreamIsRefused(t *testing.T) {
	resp, _ := (&Datasource{}).PublishStream(context.Background(), &backend.PublishStreamRequest{})
	if resp.Status != backend.PublishStreamStatusPermissionDenied {
		t.Fatalf("got %v", resp.Status)
	}
}
