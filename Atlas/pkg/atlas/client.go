// Package atlas is a small client for the Timebase Atlas public REST API
// (/api/v1). It deliberately does not use the i3X API or the internal
// SignalR value hub.
package atlas

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/flow-software/Timebase-Grafana/Atlas/pkg/auth"
)

// maxBody caps how much of a response is read. A whole-domain Cypher graph
// on a large model is a few MB; this leaves plenty of headroom.
const maxBody = 64 << 20

type Client struct {
	BaseURL string
	HTTP    *http.Client
	Auth    auth.TokenSource
}

// APIError is a non-2xx answer from Atlas. Atlas returns RFC 7807 problem
// details; Detail carries the useful text (for example a Cypher parse error).
type APIError struct {
	Status int
	Title  string
	Detail string
	Path   string
}

func (e *APIError) Error() string {
	msg := e.Detail
	if msg == "" {
		msg = e.Title
	}
	if msg == "" {
		msg = http.StatusText(e.Status)
	}
	return fmt.Sprintf("Atlas returned %d for %s: %s", e.Status, e.Path, msg)
}

// HTTPStatus lets the datasource pass the status on to Grafana.
func (e *APIError) HTTPStatus() int { return e.Status }

// World is one compiled branch/domain pair.
type World struct {
	Branch        string `json:"branch"`
	Domain        string `json:"domain"`
	CompiledAt    string `json:"compiledAt"`
	EntityCount   int    `json:"entityCount"`
	EdgeCount     int    `json:"edgeCount"`
	InstanceCount int    `json:"instanceCount"`
	IsStale       bool   `json:"isStale"`
}

// Instance is one entry from the instance list. The list carries no label.
type Instance struct {
	ElementID string `json:"elementId"`
	TypeURI   string `json:"typeUri"`
}

type instancePage struct {
	Items      []Instance `json:"items"`
	TotalCount int        `json:"totalCount"`
	Page       int        `json:"page"`
	PageSize   int        `json:"pageSize"`
}

// QueryResult is the plain columnar Cypher result.
type QueryResult struct {
	Columns []string            `json:"columns"`
	Data    [][]json.RawMessage `json:"data"`
}

func worldPath(branch, domain string) string {
	return "/api/v1/branches/" + url.PathEscape(branch) + "/domains/" + url.PathEscape(domain)
}

func (c *Client) Worlds(ctx context.Context) ([]World, error) {
	var out struct {
		Items []World `json:"items"`
	}
	if err := c.do(ctx, http.MethodGet, "/api/v1/worlds", nil, &out); err != nil {
		return nil, err
	}
	return out.Items, nil
}

// Instances lists instances in a world, optionally of one type (full URI or
// short name), reading pages until limit items are collected.
func (c *Client) Instances(ctx context.Context, branch, domain, typ string, limit int) ([]Instance, int, error) {
	const pageSize = 500
	var all []Instance
	total := 0
	for page := 1; ; page++ {
		q := url.Values{"page": {strconv.Itoa(page)}, "pageSize": {strconv.Itoa(pageSize)}}
		if typ != "" {
			q.Set("type", typ)
		}
		var p instancePage
		if err := c.do(ctx, http.MethodGet, worldPath(branch, domain)+"/instances?"+q.Encode(), nil, &p); err != nil {
			return nil, 0, err
		}
		total = p.TotalCount
		all = append(all, p.Items...)
		if len(p.Items) < pageSize || len(all) >= total || (limit > 0 && len(all) >= limit) {
			break
		}
	}
	if limit > 0 && len(all) > limit {
		all = all[:limit]
	}
	return all, total, nil
}

// RelationshipType is one relationship type in a domain's ontology.
// IsContainment is what Atlas uses to nest instances in its own tree.
type RelationshipType struct {
	URI           string `json:"uri"`
	Label         string `json:"label"`
	IsContainment bool   `json:"isContainment"`
}

func (c *Client) RelationshipTypes(ctx context.Context, branch, domain string) ([]RelationshipType, error) {
	var out []RelationshipType
	if err := c.do(ctx, http.MethodGet, worldPath(branch, domain)+"/relationship-types", nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// EntityType is one entity type of the domain's ontology, as GET
// /api/v1/branches/{b}/domains/{d}/entity-types returns it (only the parts
// the plugin reads).
type EntityType struct {
	URI        string              `json:"uri"`
	ParentType string              `json:"parentType"`
	Properties []EntityProperty    `json:"properties"`
	Pins       []EntityPropertyPin `json:"propertyPins"`
}

// EntityProperty is a property attached to a type. A live (TVQ) property has
// a compound range whose Value field type is set in fieldTypeOverrides; a
// record has its own compound type as range.
type EntityProperty struct {
	URI           string         `json:"uri"`
	Range         string         `json:"range"`
	CompoundRange *CompoundRange `json:"compoundRange"`
	// EnumType: the enumeration type of a plain (non-compound) property.
	EnumType string `json:"enumType"`
}

type CompoundRange struct {
	BaseCompoundURI    string            `json:"baseCompoundUri"`
	FieldTypeOverrides map[string]string `json:"fieldTypeOverrides"`
	// FieldEnumOverrides: the enumeration type of a field ("Value" on a TVQ).
	FieldEnumOverrides map[string]string `json:"fieldEnumOverrides"`
}

// EntityPropertyPin fixes an open field type (e.g. TVQ Value) of a property
// on a type; propertyPins are effective, so inherited pins are included.
type EntityPropertyPin struct {
	PropertyURI        string            `json:"propertyUri"`
	FieldTypeOverrides map[string]string `json:"fieldTypeOverrides"`
	FieldEnumOverrides map[string]string `json:"fieldEnumOverrides"`
}

// EnumType is an enumeration type (Atlas ADR 030): a closed set of keys,
// each with a code-safe name and an optional display label. Keys are the
// lexical form of the stored value ("2", "true", "Running").
type EnumType struct {
	URI     string       `json:"uri"`
	Label   string       `json:"label"`
	KeyType string       `json:"keyType"`
	Members []EnumMember `json:"members"`
}

type EnumMember struct {
	Key   string `json:"key"`
	Name  string `json:"name"`
	Label string `json:"label"`
}

// EnumTypes lists the enumeration types of a domain. An Atlas without
// enumeration types answers 404.
func (c *Client) EnumTypes(ctx context.Context, branch, domain string) ([]EnumType, error) {
	var out []EnumType
	if err := c.do(ctx, http.MethodGet, worldPath(branch, domain)+"/enum-types", nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// EntityTypes lists the entity types of a domain, with their properties.
func (c *Client) EntityTypes(ctx context.Context, branch, domain string) ([]EntityType, error) {
	var out []EntityType
	if err := c.do(ctx, http.MethodGet, worldPath(branch, domain)+"/entity-types", nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// Query runs a read-only Cypher query. Atlas rejects CREATE, MERGE, DELETE
// and SET in its parser.
func (c *Client) Query(ctx context.Context, branch, domain, cypher string, params map[string]any) (*QueryResult, error) {
	body := map[string]any{"cypher": cypher}
	if len(params) > 0 {
		body["parameters"] = params
	}
	var out QueryResult
	if err := c.do(ctx, http.MethodPost, worldPath(branch, domain)+"/queries", body, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) do(ctx context.Context, method, path string, in, out any) error {
	var payload []byte
	if in != nil {
		var err error
		if payload, err = json.Marshal(in); err != nil {
			return err
		}
	}
	resp, err := auth.Do(ctx, c.HTTP, c.Auth, func() (*http.Request, error) {
		var rd io.Reader
		if payload != nil {
			rd = bytes.NewReader(payload)
		}
		req, err := http.NewRequestWithContext(ctx, method, c.BaseURL+path, rd)
		if err != nil {
			return nil, err
		}
		req.Header.Set("Accept", "application/json")
		if payload != nil {
			req.Header.Set("Content-Type", "application/json")
		}
		return req, nil
	})
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	b, err := io.ReadAll(io.LimitReader(resp.Body, maxBody))
	if err != nil {
		return fmt.Errorf("reading %s: %w", path, err)
	}

	shortPath, _, _ := strings.Cut(path, "?")
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		e := &APIError{Status: resp.StatusCode, Path: shortPath}
		var problem struct {
			Title  string `json:"title"`
			Detail string `json:"detail"`
		}
		if json.Unmarshal(b, &problem) == nil {
			e.Title, e.Detail = problem.Title, problem.Detail
		}
		return e
	}
	if !auth.IsJSON(resp) {
		return fmt.Errorf("%s returned %d with a non-JSON body; check the URL points at Atlas (for example https://atlas:4552)", shortPath, resp.StatusCode)
	}
	if err := json.Unmarshal(b, out); err != nil {
		return fmt.Errorf("decoding %s: %w", shortPath, err)
	}
	return nil
}
