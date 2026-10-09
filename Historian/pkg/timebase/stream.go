package timebase

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/flow-software/Timebase-Grafana/Historian/pkg/auth"
	"github.com/flow-software/Timebase-Grafana/Historian/pkg/sse"
)

// ErrStreamClosed means the historian ended a stream; callers reconnect.
var ErrStreamClosed = errors.New("the historian closed the stream")

type streamMessage struct {
	Name string          `json:"n"`
	T    time.Time       `json:"t"`
	V    json.RawMessage `json:"v"`
	Q    int             `json:"q"`
}

// Stream calls fn with each new sample of one tag until ctx ends or the
// connection drops. It blocks; it never returns nil.
//
// The endpoint subscribes by MQTT-style pattern, where "." separates levels
// and "#" and "+" are wildcards. Tag names may contain those characters
// ("Boiler #1"), so any such level is widened to "+" and messages are then
// filtered by exact name.
func (c *Client) Stream(ctx context.Context, dataset, tag string, fn func(TagValue)) error {
	q := url.Values{"pattern": {streamPattern(tag)}}
	target := NormalizeBaseURL(c.BaseURL) + "/api/datasets/" + url.PathEscape(dataset) + "/data/stream?" + q.Encode()

	hc := c.StreamHTTP
	if hc == nil {
		hc = c.HTTP
	}
	resp, err := auth.Do(ctx, hc, c.Auth, func() (*http.Request, error) {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("Accept", "text/event-stream")
		return req, nil
	})
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		return apiError(resp, body)
	}

	err = sse.Read(resp.Body, func(e sse.Event) error {
		if e.Event != "" && e.Event != "data" {
			return nil
		}
		var m streamMessage
		if json.Unmarshal([]byte(e.Data), &m) != nil || m.Name != tag {
			return nil
		}
		fn(TagValue{T: m.T, V: m.V, Q: m.Q})
		return nil
	})
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if errors.Is(err, io.EOF) {
		return ErrStreamClosed
	}
	return fmt.Errorf("stream: %w", err)
}

func streamPattern(tag string) string {
	levels := strings.Split(tag, ".")
	for i, l := range levels {
		if strings.ContainsAny(l, "#+") {
			levels[i] = "+"
		}
	}
	return strings.Join(levels, ".")
}
