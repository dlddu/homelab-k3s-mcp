// Package opensearch talks to an OpenSearch Serverless collection over raw
// HTTP with SigV4 request signing (service "aoss"). The base credentials come
// from the default AWS credential chain (the instance profile in production);
// they are used only to assume the configured role via STS, and the resulting
// role credentials sign every data-plane request. No static AWS keys and no
// OpenSearch client dependency are involved.
package opensearch

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	v4 "github.com/aws/aws-sdk-go-v2/aws/signer/v4"
	sdkconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials/stscreds"
	"github.com/aws/aws-sdk-go-v2/service/sts"
)

const (
	defaultRoleSessionName = "homelab-k3s-mcp"
	requestTimeout         = 15 * time.Second
	signingService         = "aoss"

	// DefaultSearchSize is used when a search request does not specify a size.
	DefaultSearchSize int64 = 10
	// MaxSearchSize is the hard cap on the search result size. Requests above
	// it are rejected, not clamped.
	MaxSearchSize int64 = 50
)

// errKind separates a "not configured" failure from a runtime request error.
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

// PutResult is the outcome of a PutDocument call. Result is the OpenSearch
// index result: "created" or "updated".
type PutResult struct {
	Index  string `json:"index"`
	ID     string `json:"id"`
	Result string `json:"result"`
}

// DeleteResult is the outcome of a DeleteDocument call. Result is "deleted"
// or "not_found".
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
	// PutDocument indexes (upserts) a JSON document. A nil id lets OpenSearch
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

// doer is the subset of http.Client the client depends on.
type doer interface {
	Do(req *http.Request) (*http.Response, error)
}

// Client is the live SigV4-signing implementation of Service.
type Client struct {
	endpoint string
	region   string
	creds    aws.CredentialsProvider
	signer   *v4.Signer
	http     doer
}

// FromEnv builds a Client from the OPENSEARCH_* environment variables. It
// returns (nil, nil) when OPENSEARCH_ENDPOINT is unset, signalling that the
// integration is simply not configured (as opposed to misconfigured).
//
// OPENSEARCH_REGION sets the AWS region when the default chain does not
// provide one (it also determines the SigV4 signing region). When
// OPENSEARCH_STS_ENDPOINT is set, the AssumeRole call is routed to that
// endpoint. This targets STS-compatible servers such as MinIO and is intended
// for smoke testing; production leaves it unset to use real AWS.
func FromEnv(ctx context.Context) (*Client, error) {
	endpoint := os.Getenv("OPENSEARCH_ENDPOINT")
	if endpoint == "" {
		return nil, nil
	}
	roleARN := os.Getenv("OPENSEARCH_ROLE_ARN")
	if roleARN == "" {
		return nil, fmt.Errorf("OPENSEARCH_ROLE_ARN is required when OPENSEARCH_ENDPOINT is set")
	}

	var loadOpts []func(*sdkconfig.LoadOptions) error
	if region := os.Getenv("OPENSEARCH_REGION"); region != "" {
		loadOpts = append(loadOpts, sdkconfig.WithRegion(region))
	}

	// Base credentials: the default chain (instance profile in production).
	baseCfg, err := sdkconfig.LoadDefaultConfig(ctx, loadOpts...)
	if err != nil {
		return nil, fmt.Errorf("load aws config: %w", err)
	}
	if baseCfg.Region == "" {
		return nil, fmt.Errorf("no AWS region resolved: set OPENSEARCH_REGION when OPENSEARCH_ENDPOINT is set")
	}

	sessionName := os.Getenv("OPENSEARCH_ROLE_SESSION_NAME")
	if sessionName == "" {
		sessionName = defaultRoleSessionName
	}
	stsEndpoint := os.Getenv("OPENSEARCH_STS_ENDPOINT")

	stsClient := sts.NewFromConfig(baseCfg, func(o *sts.Options) {
		if stsEndpoint != "" {
			o.BaseEndpoint = aws.String(stsEndpoint)
		}
	})
	provider := stscreds.NewAssumeRoleProvider(stsClient, roleARN, func(o *stscreds.AssumeRoleOptions) {
		o.RoleSessionName = sessionName
	})

	return &Client{
		endpoint: strings.TrimRight(endpoint, "/"),
		region:   baseCfg.Region,
		creds:    aws.NewCredentialsCache(provider),
		signer:   v4.NewSigner(),
		http:     &http.Client{},
	}, nil
}

// Search runs a simple_query_string full-text query against one index or the
// whole collection.
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

// PutDocument indexes (upserts) a document, letting OpenSearch generate the
// id when none is given. The target index is auto-created on first write.
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

// do signs the request with assumed-role credentials (SigV4, service "aoss")
// and returns the response status and body.
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

	sum := sha256.Sum256(body)
	payloadHash := hex.EncodeToString(sum[:])
	// OpenSearch Serverless requires the payload hash as an explicit header.
	req.Header.Set("X-Amz-Content-Sha256", payloadHash)

	creds, err := c.creds.Retrieve(ctx)
	if err != nil {
		return 0, nil, requestError("assume role: %v", err)
	}
	if err := c.signer.SignHTTP(ctx, creds, req, payloadHash, signingService, c.region, time.Now()); err != nil {
		return 0, nil, requestError("sign request: %v", err)
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

// snippet trims a response body for inclusion in error messages.
func snippet(data []byte) string {
	s := strings.TrimSpace(string(data))
	const limit = 300
	if len(s) > limit {
		return s[:limit] + "..."
	}
	return s
}
