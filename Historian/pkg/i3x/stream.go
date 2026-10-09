package i3x

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/flow-software/Timebase-Grafana/Historian/pkg/auth"
	"github.com/flow-software/Timebase-Grafana/Historian/pkg/series"
	"github.com/flow-software/Timebase-Grafana/Historian/pkg/sse"
)

// streamClientID identifies this plugin's subscriptions on the server.
const streamClientID = "grafana-timebase-datasource"

// ErrStreamClosed means the server ended a stream; callers reconnect.
var ErrStreamClosed = errors.New("the i3X server closed the stream")

type update struct {
	ElementID string `json:"elementId"`
	series.Sample
}

// Stream calls fn with each new value of one element until ctx ends or the
// connection drops. It blocks; it never returns nil.
//
// i3X streams through a subscription: create one, register the element,
// then POST /subscriptions/stream, which answers with Server-Sent Events.
// The subscription is deleted on the way out so none are left behind.
func (c *Client) Stream(ctx context.Context, elementID string, fn func(series.Sample)) error {
	var created envelope[struct {
		SubscriptionID string `json:"subscriptionId"`
	}]
	err := c.do(ctx, http.MethodPost, "/subscriptions", map[string]any{
		"clientId":    streamClientID,
		"displayName": "Grafana live: " + elementID,
	}, &created)
	if err != nil {
		return fmt.Errorf("create subscription: %w", err)
	}
	id := created.Result.SubscriptionID
	if !created.Success || id == "" {
		return fmt.Errorf("create subscription: %s", created.ResponseDetail)
	}
	defer c.deleteSubscription(id)

	ids := map[string]any{"clientId": streamClientID, "subscriptionId": id}
	var registered bulkResponse[json.RawMessage]
	reg := map[string]any{"clientId": streamClientID, "subscriptionId": id, "elementIds": []string{elementID}}
	if err := c.do(ctx, http.MethodPost, "/subscriptions/register", reg, &registered); err != nil {
		return fmt.Errorf("register %s: %w", elementID, err)
	}
	for _, item := range registered.Results {
		if !item.Success {
			return fmt.Errorf("register %s: %s", elementID, item.ResponseDetail)
		}
	}

	body, err := json.Marshal(ids)
	if err != nil {
		return err
	}
	hc := c.StreamHTTP
	if hc == nil {
		hc = c.HTTP
	}
	resp, err := auth.Do(ctx, hc, c.Auth, func() (*http.Request, error) {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(c.BaseURL, "/")+"/subscriptions/stream", bytes.NewReader(body))
		if err != nil {
			return nil, err
		}
		req.Header.Set("Accept", "text/event-stream")
		req.Header.Set("Content-Type", "application/json")
		return req, nil
	})
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		data, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		return &APIError{Status: resp.StatusCode, Detail: errorDetail(resp, data)}
	}

	err = sse.Read(resp.Body, func(e sse.Event) error {
		var batch []update
		if json.Unmarshal([]byte(e.Data), &batch) != nil {
			return nil
		}
		for _, u := range batch {
			if u.ElementID == elementID {
				fn(u.Sample)
			}
		}
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

// deleteSubscription runs on its own short deadline: it is called when the
// stream's context has usually just been cancelled.
func (c *Client) deleteSubscription(id string) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var out json.RawMessage
	_ = c.do(ctx, http.MethodPost, "/subscriptions/delete", map[string]any{
		"clientId":        streamClientID,
		"subscriptionIds": []string{id},
	}, &out)
}
