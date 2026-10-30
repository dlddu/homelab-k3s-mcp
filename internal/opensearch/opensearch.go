// Package opensearch backs the opensearch_* tools with any server that speaks
// the slice of the OpenSearch REST API used here (_search with
// simple_query_string, _doc put/post/delete) at OPENSEARCH_ENDPOINT: in
// production docstore (github.com/dlddu/docstore), but equally OpenSearch
// itself. How requests are authenticated is configuration (see auth.go).
package opensearch

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

const (
	// requestTimeout budgets one request end to end, credential fetch included.
	// Both production-grade backends cold-start: docstore scales to zero behind
	// Sablier (pod scheduling + EFS mount, about 10-20s) and an OpenSearch
	// Serverless collection at min 0 OCU takes 10-30s. 45s covers either
	// without hanging a tool call forever.
	requestTimeout = 45 * time.Second

	// DefaultSearchSize is used when a search request does not specify a size.
	DefaultSearchSize int64 = 10
	// MaxSearchSize is the hard cap on the search result size.
	MaxSearchSize int64 = 50
)

type errKind int

const (
	kindUnavailable errKind = iota
	kindRequest
)

// Error is the error type returned by Service.
type Error struct {
	kind errKind
	msg  string
}

func (e *Error) Error() string {
	switch e.kind {
	case kindUnavailable:
		return "opensearch unavailable: " + e.msg
	default:
		return "opensearch error: " + e.msg
	}
}

func unavailable(msg string) *Error { return &Error{kind: kindUnavailable, msg: msg} }

// Unavailable reports whether this is the "not configured" kind.
func (e *Error) Unavailable() bool { return e.kind == kindUnavailable }
func requestError(format string, args ...any) *Error {
	return &Error{kind: kindRequest, msg: fmt.Sprintf(format, args...)}
}

// Hit is a single search match.
type Hit struct {
	Index  string          `json:"index"`
	ID     string          `json:"id"`
	Score  *float64        `json:"score"`
	Source json.RawMessage `json:"source"`
}

// SearchResult is the outcome of a Search call.
type SearchResult struct {
	Total int64 `json:"total"`
	Hits  []Hit `json:"hits"`
}

// PutResult is the outcome of a PutDocument call.
type PutResult struct {
	Index  string `json:"index"`
	ID     string `json:"id"`
	Result string `json:"result"`
}

// DeleteResult is the outcome of a DeleteDocument call.
type DeleteResult struct {
	Index  string `json:"index"`
	ID     string `json:"id"`
	Result string `json:"result"`
}

// Service exposes the OpenSearch operations backing the opensearch_* tools.
type Service interface {
	// Search runs a full-text query. A nil index searches every index in the
	// collection; a nil size defaults to DefaultSearchSize. Sizes above
	// MaxSearchSize are rejected.
	Search(ctx context.Context, query string, index *string, size *int64) (*SearchResult, error)
	// PutDocument indexes (upserts) a JSON document. A nil id lets the backend
	// generate one; the target index is auto-created on first write.
	PutDocument(ctx context.Context, index string, id *string, document map[string]any) (*PutResult, error)
	// DeleteDocument deletes a single document by id. A missing document (or
	// index) yields Result "not_found" rather than an error.
	DeleteDocument(ctx context.Context, index, id string) (*DeleteResult, error)
}

// Unavailable is a Service that fails every call with the same reason.
type Unavailable struct {
	reason string
}

// NewUnavailable builds an Unavailable service with the given reason.
func NewUnavailable(reason string) *Unavailable {
	if reason == "" {
		reason = "opensearch integration is not configured"
	}
	return &Unavailable{reason: reason}
}

// Search always fails with the configured reason.
func (u *Unavailable) Search(context.Context, string, *string, *int64) (*SearchResult, error) {
	return nil, unavailable(u.reason)
}

// PutDocument always fails with the configured reason.
func (u *Unavailable) PutDocument(context.Context, string, *string, map[string]any) (*PutResult, error) {
	return nil, unavailable(u.reason)
}

// DeleteDocument always fails with the configured reason.
func (u *Unavailable) DeleteDocument(context.Context, string, string) (*DeleteResult, error) {
	return nil, unavailable(u.reason)
}

type doer interface {
	Do(req *http.Request) (*http.Response, error)
}

// Client is the live implementation of Service.
type Client struct {
	endpoint string
	http     doer
	auth     authenticator
}

// AuthMode reports how this client authenticates its requests.
func (c *Client) AuthMode() AuthMode {
	if c.auth == nil {
		return AuthNone
	}
	return c.auth.mode()
}

// FromEnv builds a Client from OPENSEARCH_ENDPOINT, the backend base URL
// (e.g. http://docstore-external.docstore.svc.cluster.local), and the
// authentication settings read by authFromEnv. An unset endpoint returns
// (nil, nil): the tools are then unavailable.
func FromEnv(ctx context.Context) (*Client, error) {
	endpoint := strings.TrimRight(os.Getenv("OPENSEARCH_ENDPOINT"), "/")
	if endpoint == "" {
		return nil, nil
	}
	u, err := url.Parse(endpoint)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return nil, fmt.Errorf("OPENSEARCH_ENDPOINT must be an absolute http(s) URL, got %q", endpoint)
	}
	auth, err := authFromEnv(ctx)
	if err != nil {
		return nil, err
	}
	return &Client{endpoint: endpoint, http: &http.Client{}, auth: auth}, nil
}

// Search runs a simple_query_string full-text query.
func (c *Client) Search(ctx context.Context, query string, index *string, size *int64) (*SearchResult, error) {
	n := DefaultSearchSize
	if size != nil {
		if *size < 1 {
			return nil, requestError("size must be >= 1, got %d", *size)
		}
		if *size > MaxSearchSize {
			return nil, requestError("size must be <= %d, got %d", MaxSearchSize, *size)
		}
		n = *size
	}

	path := "/_search"
	if index != nil {
		path = "/" + url.PathEscape(*index) + "/_search"
	}
	body, err := json.Marshal(map[string]any{
		"size": n,
		"query": map[string]any{
			"simple_query_string": map[string]any{"query": query},
		},
	})
	if err != nil {
		return nil, requestError("encode search body: %v", err)
	}

	status, data, rerr := c.do(ctx, http.MethodPost, path, body)
	if rerr != nil {
		return nil, rerr
	}
	if status != http.StatusOK {
		return nil, requestError("search %s: status %d: %s", path, status, snippet(data))
	}

	var parsed struct {
		Hits struct {
			Total struct {
				Value int64 `json:"value"`
			} `json:"total"`
			Hits []struct {
				Index  string          `json:"_index"`
				ID     string          `json:"_id"`
				Score  *float64        `json:"_score"`
				Source json.RawMessage `json:"_source"`
			} `json:"hits"`
		} `json:"hits"`
	}
	if err := json.Unmarshal(data, &parsed); err != nil {
		return nil, requestError("decode search response: %v", err)
	}

	result := &SearchResult{Total: parsed.Hits.Total.Value, Hits: make([]Hit, 0, len(parsed.Hits.Hits))}
	for _, h := range parsed.Hits.Hits {
		result.Hits = append(result.Hits, Hit{Index: h.Index, ID: h.ID, Score: h.Score, Source: h.Source})
	}
	return result, nil
}

// PutDocument indexes (upserts) a document.
func (c *Client) PutDocument(ctx context.Context, index string, id *string, document map[string]any) (*PutResult, error) {
	body, err := json.Marshal(document)
	if err != nil {
		return nil, requestError("encode document: %v", err)
	}

	method := http.MethodPost
	path := "/" + url.PathEscape(index) + "/_doc"
	if id != nil {
		method = http.MethodPut
		path += "/" + url.PathEscape(*id)
	}

	status, data, rerr := c.do(ctx, method, path, body)
	if rerr != nil {
		return nil, rerr
	}
	if status != http.StatusOK && status != http.StatusCreated {
		return nil, requestError("put document %s: status %d: %s", path, status, snippet(data))
	}

	var parsed struct {
		Index  string `json:"_index"`
		ID     string `json:"_id"`
		Result string `json:"result"`
	}
	if err := json.Unmarshal(data, &parsed); err != nil {
		return nil, requestError("decode put response: %v", err)
	}
	return &PutResult{Index: parsed.Index, ID: parsed.ID, Result: parsed.Result}, nil
}

// DeleteDocument deletes a single document. A 404 (missing document or index)
// maps to Result "not_found" so repeated deletes converge instead of failing.
func (c *Client) DeleteDocument(ctx context.Context, index, id string) (*DeleteResult, error) {
	path := "/" + url.PathEscape(index) + "/_doc/" + url.PathEscape(id)

	status, data, rerr := c.do(ctx, http.MethodDelete, path, nil)
	if rerr != nil {
		return nil, rerr
	}
	switch status {
	case http.StatusOK:
		return &DeleteResult{Index: index, ID: id, Result: "deleted"}, nil
	case http.StatusNotFound:
		return &DeleteResult{Index: index, ID: id, Result: "not_found"}, nil
	default:
		return nil, requestError("delete document %s: status %d: %s", path, status, snippet(data))
	}
}

func (c *Client) do(ctx context.Context, method, path string, body []byte) (int, []byte, *Error) {
	ctx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, method, c.endpoint+path, bytes.NewReader(body))
	if err != nil {
		return 0, nil, requestError("build request %s %s: %v", method, path, err)
	}
	if len(body) > 0 {
		req.Header.Set("Content-Type", "application/json")
	}
	if c.auth != nil {
		if aerr := c.auth.apply(ctx, req, body); aerr != nil {
			return 0, nil, aerr
		}
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return 0, nil, requestError("%s %s: %v", method, path, err)
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return 0, nil, requestError("read response %s %s: %v", method, path, err)
	}
	return resp.StatusCode, data, nil
}

func snippet(data []byte) string {
	s := strings.TrimSpace(string(data))
	const limit = 300
	if len(s) > limit {
		return s[:limit] + "..."
	}
	return s
}
