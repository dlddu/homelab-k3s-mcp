// Package sessionplatform talks to the session-platform control plane's
// in-cluster REST API: it creates and deletes sessions, reads the session
// inventory, reads the accumulated workload output of a single session, and
// injects input into one. The control
// plane is only reachable from inside the cluster, so this package is what opens
// it to MCP clients without an ingress or a port-forward.
package sessionplatform

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

	"github.com/dlddu/homelab-k3s-mcp/internal/version"
)

const (
	// sessionsPath is the control plane's list endpoint, including the API
	// version prefix the OpenAPI document declares as its single server
	// (`servers: [{url: /api/v1}]`).
	sessionsPath = "/api/v1/sessions"

	httpClientTimeout = 10 * time.Second
	createTimeout     = 5 * time.Minute
)

func sessionPath(id string) string { return sessionsPath + "/" + url.PathEscape(id) }

func readPath(id string) string { return sessionsPath + "/" + url.PathEscape(id) + "/read" }

func writePath(id string) string { return sessionsPath + "/" + url.PathEscape(id) + "/write" }

type errKind int

const (
	kindUnavailable errKind = iota
	kindAPI
	kindNotFound
	// kindInvalidArgument is a cursor this package refuses before it reaches
	// the wire. Rejecting locally is what makes "the session is left
	// untouched" structural rather than a promise: no request is issued at all.
	kindInvalidArgument
	// kindBusy is the control plane's 429: the workload's prompt queue is full. It
	// is the one refusal in this package worth retrying unchanged.
	kindBusy
	// kindTooLarge is the control plane's 413: the payload exceeds the
	// per-write limit (1 MiB for claude-code prompts). The limit is
	// workload-type specific and this client would have to look the session up
	// to know the type, so it is enforced there rather than here.
	kindTooLarge
	// kindQuotaExhausted is the control plane's 507: the session's bounded
	// output history is full, so further writes are refused for good while
	// the output already accumulated stays readable.
	kindQuotaExhausted
	kindConflict
)

// Error is the error type returned by Service.
type Error struct {
	kind errKind
	msg  string
}

func (e *Error) Error() string {
	switch e.kind {
	case kindUnavailable:
		return "session platform unavailable: " + e.msg
	case kindNotFound:
		return "session platform not found: " + e.msg
	case kindInvalidArgument:
		return "session platform invalid argument: " + e.msg
	case kindBusy:
		return "session platform busy, retry after a queued prompt finishes: " + e.msg
	case kindTooLarge:
		return "session platform payload too large, retrying will not help: " + e.msg
	case kindQuotaExhausted:
		return "session platform output quota exhausted, retrying will not help (existing output is still readable): " + e.msg
	case kindConflict:
		return "session platform conflict, retry after the in-progress lifecycle operation finishes: " + e.msg
	default:
		return "session platform api error: " + e.msg
	}
}

func unavailable(msg string) *Error     { return &Error{kind: kindUnavailable, msg: msg} }
func apiError(msg string) *Error        { return &Error{kind: kindAPI, msg: msg} }
func notFound(msg string) *Error        { return &Error{kind: kindNotFound, msg: msg} }
func invalidArgument(msg string) *Error { return &Error{kind: kindInvalidArgument, msg: msg} }
func busy(msg string) *Error            { return &Error{kind: kindBusy, msg: msg} }
func tooLarge(msg string) *Error        { return &Error{kind: kindTooLarge, msg: msg} }
func quotaExhausted(msg string) *Error  { return &Error{kind: kindQuotaExhausted, msg: msg} }
func conflict(msg string) *Error        { return &Error{kind: kindConflict, msg: msg} }

// Unavailable reports whether this is the "not configured" kind.
func (e *Error) Unavailable() bool { return e.kind == kindUnavailable }

// Session is one session as the control plane reports it. The fields mirror the
// control plane's `Session` schema; only those the session_list PRD exposes are
// carried, so a field the control plane adds later stays invisible until a PRD
// asks for it. `pod` is absent exactly while a session is snapshotted and its
// pods are reclaimed, hence omitempty.
type Session struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	WorkloadType string `json:"workloadType"`
	State        string `json:"state"`
	Pod          string `json:"pod,omitempty"`
	CreatedAt    string `json:"createdAt"`
	LastAccess   string `json:"lastAccess"`
}

// ReadResult is one read of a session's accumulated output. Path names the
// state branch the control plane took to serve the read ("active",
// "idle->active->read" or "snapshot->restore->read"), and Session is the
// session as it stands *after* the read.
type ReadResult struct {
	Session    Session `json:"session"`
	Path       string  `json:"path"`
	Payload    string  `json:"payload"`
	NextOffset int64   `json:"nextOffset"`
}

// WriteResult is one accepted write. Unlike ReadResult it carries no output:
// the control plane returns as soon as the payload is accepted — a shell write
// has only been pushed to the PTY, a claude-code write has only been queued —
// so anything the workload produces in response is recovered later through
// ReadSession. That absence is the contract, not an omission. Path names the
// state branch that served the write ("active", "idle->active->write" or
// "snapshot->restore->write") and Session is the session as it stands
// afterwards.
type WriteResult struct {
	Session Session `json:"session"`
	Path    string  `json:"path"`
}

type CreateRequest struct {
	Name         string `json:"name"`
	WorkloadType string `json:"workloadType,omitempty"`
	Model        string `json:"model,omitempty"`
}

// Service talks to the control plane on behalf of the session tools.
//
// ListSessions is a passive read: it never promotes an idle session, restores a
// snapshot, or refreshes lastAccess. ReadSession and WriteSession are not — the
// control plane activates the target before serving either, which is why their
// results carry the branch they took.
type Service interface {
	CreateSession(ctx context.Context, req CreateRequest) (*Session, error)
	DeleteSession(ctx context.Context, id string) error
	ListSessions(ctx context.Context) ([]Session, error)
	ReadSession(ctx context.Context, id string, offset int64) (*ReadResult, error)
	WriteSession(ctx context.Context, id, payload string) (*WriteResult, error)
}

// Unavailable is a Service that fails every call with the same reason.
type Unavailable struct {
	reason string
}

// NewUnavailable builds an Unavailable service with the given reason.
func NewUnavailable(reason string) *Unavailable {
	if reason == "" {
		reason = "session platform endpoint is not configured"
	}
	return &Unavailable{reason: reason}
}

// CreateSession always fails with the configured reason.
func (u *Unavailable) CreateSession(context.Context, CreateRequest) (*Session, error) {
	return nil, unavailable(u.reason)
}

// DeleteSession always fails with the configured reason.
func (u *Unavailable) DeleteSession(context.Context, string) error {
	return unavailable(u.reason)
}

// ListSessions always fails with the configured reason.
func (u *Unavailable) ListSessions(context.Context) ([]Session, error) {
	return nil, unavailable(u.reason)
}

// ReadSession always fails with the configured reason.
func (u *Unavailable) ReadSession(context.Context, string, int64) (*ReadResult, error) {
	return nil, unavailable(u.reason)
}

// WriteSession always fails with the configured reason.
func (u *Unavailable) WriteSession(context.Context, string, string) (*WriteResult, error) {
	return nil, unavailable(u.reason)
}

// Client is the live control plane implementation of Service.
type Client struct {
	endpoint  string
	userAgent string
	http      *http.Client
	create    *http.Client
}

// FromEnv builds a Client from SESSION_PLATFORM_ENDPOINT, the control plane's
// base URL (in production the cluster-internal
// http://control-plane.session-platform.svc.cluster.local).
func FromEnv() (*Client, error) {
	endpoint := strings.TrimSpace(os.Getenv("SESSION_PLATFORM_ENDPOINT"))
	if endpoint == "" {
		return nil, nil
	}
	if !strings.HasPrefix(endpoint, "http://") && !strings.HasPrefix(endpoint, "https://") {
		return nil, fmt.Errorf("SESSION_PLATFORM_ENDPOINT must be an absolute http(s) URL, got %q", endpoint)
	}

	return &Client{
		endpoint:  strings.TrimRight(endpoint, "/"),
		userAgent: version.Name + "/" + version.Version,
		http:      &http.Client{Timeout: httpClientTimeout},
		create:    &http.Client{Timeout: createTimeout},
	}, nil
}

func (c *Client) CreateSession(ctx context.Context, req CreateRequest) (*Session, error) {
	if strings.TrimSpace(req.Name) == "" {
		return nil, invalidArgument("name must contain a non-whitespace character")
	}
	if err := c.checkWorkloadType(ctx, req.WorkloadType); err != nil {
		return nil, err
	}

	endpoint := c.endpoint + sessionsPath
	body, err := json.Marshal(req)
	if err != nil {
		return nil, apiError(fmt.Sprintf("build request: %v", err))
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, apiError(fmt.Sprintf("build request: %v", err))
	}
	httpReq.Header.Set("Accept", "application/json")
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("User-Agent", c.userAgent)

	resp, err := c.create.Do(httpReq)
	if err != nil {
		return nil, apiError(fmt.Sprintf("post %s: %v", endpoint, err))
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	message := controlPlaneMessage(respBody, resp.Status)
	switch {
	case resp.StatusCode == http.StatusBadRequest:
		return nil, invalidArgument(message)
	case resp.StatusCode == http.StatusRequestEntityTooLarge:
		return nil, tooLarge(message)
	case resp.StatusCode < 200 || resp.StatusCode >= 300:
		return nil, apiError(fmt.Sprintf("%s returned %s: %s", endpoint, resp.Status, string(respBody)))
	}

	var created Session
	if err := json.Unmarshal(respBody, &created); err != nil {
		return nil, apiError(fmt.Sprintf("parse created session: %v", err))
	}
	return &created, nil
}

func (c *Client) DeleteSession(ctx context.Context, id string) error {
	if id == "" {
		return invalidArgument("session id is required")
	}

	endpoint := c.endpoint + sessionPath(id)
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, endpoint, nil)
	if err != nil {
		return apiError(fmt.Sprintf("build request: %v", err))
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", c.userAgent)

	resp, err := c.http.Do(req)
	if err != nil {
		return apiError(fmt.Sprintf("delete %s: %v", endpoint, err))
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	message := controlPlaneMessage(respBody, resp.Status)
	switch {
	case resp.StatusCode == http.StatusNotFound:
		return notFound(fmt.Sprintf("session %q: %s", id, message))
	case resp.StatusCode == http.StatusConflict:
		return conflict(fmt.Sprintf("session %q: %s", id, message))
	case resp.StatusCode < 200 || resp.StatusCode >= 300:
		return apiError(fmt.Sprintf("%s returned %s: %s", endpoint, resp.Status, string(respBody)))
	}
	return nil
}

// ListSessions returns every session the control plane holds. It issues exactly
// one GET and no other request, so a listing cannot change session state; an
// empty inventory is an empty slice, not an error.
func (c *Client) ListSessions(ctx context.Context) ([]Session, error) {
	endpoint := c.endpoint + sessionsPath
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, apiError(fmt.Sprintf("build request: %v", err))
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", c.userAgent)

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, apiError(fmt.Sprintf("get %s: %v", endpoint, err))
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, apiError(fmt.Sprintf("%s returned %s: %s", endpoint, resp.Status, string(body)))
	}

	var parsed struct {
		Sessions []Session `json:"sessions"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, apiError(fmt.Sprintf("parse sessions: %v", err))
	}
	if parsed.Sessions == nil {
		parsed.Sessions = []Session{}
	}
	return parsed.Sessions, nil
}

// ReadSession returns the output this session accumulated after offset, plus
// the cursor to pass on the next call.
func (c *Client) ReadSession(ctx context.Context, id string, offset int64) (*ReadResult, error) {
	if id == "" {
		return nil, invalidArgument("session id is required")
	}
	if offset < 0 {
		return nil, invalidArgument(fmt.Sprintf("offset must be >= 0, got %d", offset))
	}

	endpoint := c.endpoint + readPath(id)
	body, err := json.Marshal(struct {
		Offset int64 `json:"offset"`
	}{Offset: offset})
	if err != nil {
		return nil, apiError(fmt.Sprintf("build request: %v", err))
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, apiError(fmt.Sprintf("build request: %v", err))
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", c.userAgent)

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, apiError(fmt.Sprintf("post %s: %v", endpoint, err))
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	switch {
	case resp.StatusCode == http.StatusNotFound:
		return nil, notFound(fmt.Sprintf("session %q: %s", id, controlPlaneMessage(respBody, resp.Status)))
	case resp.StatusCode == http.StatusBadRequest:
		return nil, invalidArgument(controlPlaneMessage(respBody, resp.Status))
	case resp.StatusCode < 200 || resp.StatusCode >= 300:
		return nil, apiError(fmt.Sprintf("%s returned %s: %s", endpoint, resp.Status, string(respBody)))
	}

	var parsed ReadResult
	if err := json.Unmarshal(respBody, &parsed); err != nil {
		return nil, apiError(fmt.Sprintf("parse read result: %v", err))
	}
	return &parsed, nil
}

// WriteSession injects payload into the session's workload and reports the
// branch that served it.
func (c *Client) WriteSession(ctx context.Context, id, payload string) (*WriteResult, error) {
	if id == "" {
		return nil, invalidArgument("session id is required")
	}

	endpoint := c.endpoint + writePath(id)
	body, err := json.Marshal(struct {
		Payload string `json:"payload"`
	}{Payload: payload})
	if err != nil {
		return nil, apiError(fmt.Sprintf("build request: %v", err))
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, apiError(fmt.Sprintf("build request: %v", err))
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", c.userAgent)

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, apiError(fmt.Sprintf("post %s: %v", endpoint, err))
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	message := controlPlaneMessage(respBody, resp.Status)
	switch {
	case resp.StatusCode == http.StatusNotFound:
		return nil, notFound(fmt.Sprintf("session %q: %s", id, message))
	case resp.StatusCode == http.StatusBadRequest:
		return nil, invalidArgument(message)
	case resp.StatusCode == http.StatusRequestEntityTooLarge:
		return nil, tooLarge(message)
	case resp.StatusCode == http.StatusTooManyRequests:
		return nil, busy(message)
	case resp.StatusCode == http.StatusInsufficientStorage:
		return nil, quotaExhausted(message)
	case resp.StatusCode < 200 || resp.StatusCode >= 300:
		return nil, apiError(fmt.Sprintf("%s returned %s: %s", endpoint, resp.Status, string(respBody)))
	}

	var parsed WriteResult
	if err := json.Unmarshal(respBody, &parsed); err != nil {
		return nil, apiError(fmt.Sprintf("parse write result: %v", err))
	}
	return &parsed, nil
}

func controlPlaneMessage(body []byte, status string) string {
	var parsed struct {
		Error string `json:"error"`
	}
	if err := json.Unmarshal(body, &parsed); err == nil && parsed.Error != "" {
		return parsed.Error
	}
	if len(body) > 0 {
		return status + ": " + string(body)
	}
	return status
}
