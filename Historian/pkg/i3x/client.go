// Package i3x is a minimal client for the i3X REST API served by the Timebase
// Historian under /i3x/v1.
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
)

// Client calls one i3X server. BaseURL includes the version segment, for
// example https://historian:4512/i3x/v1.
type Client struct {
	BaseURL string
	HTTP    *http.Client
	Auth    auth.TokenSource // nil means no Authorization header
	// StreamHTTP is used for long-lived streams; it must not have an overall
	// request timeout. Nil means HTTP.
	StreamHTTP *http.Client
}

// APIError is a non-2xx response from the i3X server.
type APIError struct {
	Status int
	Detail string
}

func (e *APIError) Error() string {
	if e.Detail == "" {
		return fmt.Sprintf("i3X server returned %d", e.Status)
	}
	return fmt.Sprintf("i3X server returned %d: %s", e.Status, e.Detail)
}

// HTTPStatus lets callers map the error to a response status.
func (e *APIError) HTTPStatus() int { return e.Status }

// ErrNotJSON means the server answered with something other than JSON, which
// for the Timebase Historian almost always means the URL is missing /i3x/v1
// and the request fell through to the web UI.
var ErrNotJSON = errors.New("response was not JSON; check that the URL ends in /i3x/v1")

// ResponseDetail is the problem-details object attached to failed items.
type ResponseDetail struct {
	Title  string `json:"title"`
	Status int    `json:"status"`
	Detail string `json:"detail"`
}

func (d *ResponseDetail) String() string {
	if d == nil {
		return "unknown error"
	}
	if d.Detail != "" {
		return d.Detail
	}
	return d.Title
}

type envelope[T any] struct {
	Success        bool            `json:"success"`
	Result         T               `json:"result"`
	ResponseDetail *ResponseDetail `json:"responseDetail"`
}

// BulkItem is one per-element result in a bulk response. Items fail
// independently: one unknown ElementId does not fail the others.
type BulkItem[T any] struct {
	Success        bool            `json:"success"`
	ElementID      string          `json:"elementId"`
	Result         T               `json:"result"`
	ResponseDetail *ResponseDetail `json:"responseDetail"`
}

type bulkResponse[T any] struct {
	// Success is false whenever any item failed, so it is not checked.
	Success bool          `json:"success"`
	Results []BulkItem[T] `json:"results"`
}

// VQT is one value/quality/timestamp sample.
type VQT = series.Sample

type HistoricalValueResult struct {
	IsComposition bool  `json:"isComposition"`
	Values        []VQT `json:"values"`
}

type ServerInfo struct {
	SpecVersion   string `json:"specVersion"`
	ServerVersion string `json:"serverVersion"`
	ServerName    string `json:"serverName"`
	Capabilities  struct {
		Query struct {
			History bool `json:"history"`
		} `json:"query"`
		Subscribe struct {
			Stream bool `json:"stream"`
		} `json:"subscribe"`
	} `json:"capabilities"`
}

type ObjectInstance struct {
	ElementID     string `json:"elementId"`
	DisplayName   string `json:"displayName"`
	TypeElementID string `json:"typeElementId"`
	ParentID      string `json:"parentId"`
	IsComposition bool   `json:"isComposition"`
}

func (c *Client) Info(ctx context.Context) (*ServerInfo, error) {
	var env envelope[ServerInfo]
	if err := c.do(ctx, http.MethodGet, "/info", nil, &env); err != nil {
		return nil, err
	}
	if !env.Success {
		return nil, errors.New(env.ResponseDetail.String())
	}
	return &env.Result, nil
}

func (c *Client) Objects(ctx context.Context) ([]ObjectInstance, error) {
	var env envelope[[]ObjectInstance]
	if err := c.do(ctx, http.MethodGet, "/objects", nil, &env); err != nil {
		return nil, err
	}
	if !env.Success {
		return nil, errors.New(env.ResponseDetail.String())
	}
	return env.Result, nil
}

func (c *Client) History(ctx context.Context, elementIDs []string, from, to time.Time) ([]BulkItem[HistoricalValueResult], error) {
	req := map[string]any{
		"elementIds": elementIDs,
		"startTime":  from.UTC().Format(time.RFC3339Nano),
		"endTime":    to.UTC().Format(time.RFC3339Nano),
	}
	var resp bulkResponse[HistoricalValueResult]
	if err := c.do(ctx, http.MethodPost, "/objects/history", req, &resp); err != nil {
		return nil, err
	}
	return resp.Results, nil
}

func (c *Client) do(ctx context.Context, method, path string, in, out any) error {
	var body []byte
	if in != nil {
		var err error
		if body, err = json.Marshal(in); err != nil {
			return err
		}
	}

	resp, err := auth.Do(ctx, c.HTTP, c.Auth, func() (*http.Request, error) {
		req, err := http.NewRequestWithContext(ctx, method, strings.TrimRight(c.BaseURL, "/")+path, bytes.NewReader(body))
		if err != nil {
			return nil, err
		}
		req.Header.Set("Accept", "application/json")
		if body != nil {
			req.Header.Set("Content-Type", "application/json")
		}
		return req, nil
	})
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()

	data, err := io.ReadAll(io.LimitReader(resp.Body, 256<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return &APIError{Status: resp.StatusCode, Detail: errorDetail(resp, data)}
	}
	if !auth.IsJSON(resp) {
		return ErrNotJSON
	}
	return json.Unmarshal(data, out)
}

func errorDetail(resp *http.Response, data []byte) string {
	if resp.StatusCode == http.StatusUnauthorized {
		if h := resp.Header.Get("WWW-Authenticate"); h != "" {
			return "unauthorized (" + h + ")"
		}
		return "unauthorized; check the client credentials or token"
	}
	if auth.IsJSON(resp) {
		var env envelope[json.RawMessage]
		if json.Unmarshal(data, &env) == nil && env.ResponseDetail != nil {
			return env.ResponseDetail.String()
		}
		var pd ResponseDetail
		if json.Unmarshal(data, &pd) == nil && (pd.Detail != "" || pd.Title != "") {
			return pd.String()
		}
	}
	return http.StatusText(resp.StatusCode)
}
