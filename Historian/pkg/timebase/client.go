// Package timebase is a minimal client for the Timebase Historian's native
// REST API, served under /api.
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
)

// Client calls one Timebase Historian. BaseURL is the historian's address
// without a path, for example https://historian:4512.
type Client struct {
	BaseURL string
	HTTP    *http.Client
	Auth    auth.TokenSource // nil means no Authorization header
	// StreamHTTP is used for long-lived streams; it must not have an overall
	// request timeout. Nil means HTTP.
	StreamHTTP *http.Client
}

// NormalizeBaseURL strips a trailing slash and the API path segments people
// tend to paste, so "https://h:4512/api/" and "https://h:4512/i3x/v1" both
// become "https://h:4512".
func NormalizeBaseURL(raw string) string {
	u := strings.TrimRight(strings.TrimSpace(raw), "/")
	for _, suffix := range []string{"/i3x/v1", "/api"} {
		u = strings.TrimSuffix(u, suffix)
	}
	return u
}

// Error keys the historian returns in its error body.
const (
	ErrKeyTagNotFound     = "Error.TagNotFound"
	ErrKeyDatasetNotFound = "Error.DatasetNotFound"
)

// APIError is a non-2xx response from the historian.
type APIError struct {
	Status  int
	Key     string
	Message string
}

func (e *APIError) Error() string {
	if e.Message == "" {
		return fmt.Sprintf("Timebase returned %d", e.Status)
	}
	return fmt.Sprintf("Timebase returned %d: %s", e.Status, e.Message)
}

// HTTPStatus lets callers map the error to a response status.
func (e *APIError) HTTPStatus() int { return e.Status }

// ErrNotJSON means the server answered with something other than JSON,
// usually the historian's web UI because the URL points at the wrong path.
var ErrNotJSON = errors.New("response was not JSON; check that the URL is the historian's base address, e.g. https://historian:4512")

type Dataset struct {
	Name     string `json:"n"`
	TagCount int    `json:"c"`
}

// TagValue is one sample. Q is an OPC quality code (192 = Good).
type TagValue struct {
	T time.Time       `json:"t"`
	V json.RawMessage `json:"v"`
	Q int             `json:"q"`
}

// Tag is a tag's definition. Units maps values to labels: a single entry is
// the engineering unit ({"1":"°F"}); several entries label discrete values
// ({"0":"Off","1":"On"}).
type Tag struct {
	Name        string            `json:"n"`
	Description string            `json:"d"`
	Units       map[string]string `json:"u"`
	Type        string            `json:"t"`
	Last        *TagValue         `json:"l"`
}

// IsText reports whether the tag holds text rather than numbers.
func (t Tag) IsText() bool { return t.Type == "System.String" }

// TagList is a dataset's tags, split into the user's tags and the
// historian's own health tags.
type TagList struct {
	User   []Tag `json:"User"`
	System []Tag `json:"System"`
}

// UnmarshalJSON also accepts a plain array, which is the shape the API spec
// documents, and treats it as user tags.
func (l *TagList) UnmarshalJSON(b []byte) error {
	if trimmed := strings.TrimSpace(string(b)); strings.HasPrefix(trimmed, "[") {
		l.System = nil
		return json.Unmarshal(b, &l.User)
	}
	type plain TagList
	return json.Unmarshal(b, (*plain)(l))
}

// TagData is one tag's samples from a data read.
type TagData struct {
	Tag  Tag        `json:"t"`
	Data []TagValue `json:"d"`
}

func (c *Client) Datasets(ctx context.Context) ([]Dataset, error) {
	var out []Dataset
	return out, c.get(ctx, "/api/datasets", nil, &out)
}

func (c *Client) Tags(ctx context.Context, dataset string) (*TagList, error) {
	var out TagList
	return &out, c.get(ctx, "/api/datasets/"+url.PathEscape(dataset)+"/tags", nil, &out)
}

// Data reads every sample of the given tags between from and to. The
// historian also returns the last sample before from, so a graph does not
// start with a gap. One unknown tag fails the whole read with
// ErrKeyTagNotFound.
func (c *Client) Data(ctx context.Context, dataset string, tags []string, from, to time.Time) ([]TagData, error) {
	q := url.Values{
		"tagname": tags,
		"start":   {from.UTC().Format(time.RFC3339Nano)},
		"end":     {to.UTC().Format(time.RFC3339Nano)},
	}
	var out struct {
		TagList []TagData `json:"tl"`
	}
	if err := c.get(ctx, "/api/datasets/"+url.PathEscape(dataset)+"/data", q, &out); err != nil {
		return nil, err
	}
	return out.TagList, nil
}

func (c *Client) get(ctx context.Context, path string, query url.Values, out any) error {
	target := NormalizeBaseURL(c.BaseURL) + path
	if len(query) > 0 {
		target += "?" + query.Encode()
	}
	resp, err := auth.Do(ctx, c.HTTP, c.Auth, func() (*http.Request, error) {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("Accept", "application/json")
		return req, nil
	})
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 256<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return apiError(resp, body)
	}
	if !auth.IsJSON(resp) {
		return ErrNotJSON
	}
	return json.Unmarshal(body, out)
}

func apiError(resp *http.Response, body []byte) *APIError {
	e := &APIError{Status: resp.StatusCode}
	if resp.StatusCode == http.StatusUnauthorized {
		e.Message = "unauthorized; check the client credentials or token"
		return e
	}
	var detail struct {
		Key     string `json:"key"`
		Message string `json:"message"`
	}
	if auth.IsJSON(resp) && json.Unmarshal(body, &detail) == nil {
		e.Key, e.Message = detail.Key, detail.Message
	}
	if e.Message == "" {
		e.Message = http.StatusText(resp.StatusCode)
	}
	return e
}
