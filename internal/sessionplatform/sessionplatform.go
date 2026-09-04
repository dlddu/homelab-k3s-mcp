// Package sessionplatform talks to the session-platform control plane's
// in-cluster REST API: it reads the session inventory and the accumulated
// workload output of a single session. The control plane is only reachable from
// inside the cluster, so this package is what opens it to MCP clients without an
// ingress or a port-forward.
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
)

// readPath renders the control plane's per-session read endpoint,
// POST /api/v1/sessions/{id}/read.
func readPath(id string) string { return sessionsPath + "/" + url.PathEscape(id) + "/read" }

// errKind separates a "not configured" failure from a control plane error.
type errKind int

const (
	kindUnavailable errKind = iota
	kindAPI
	// kindNotFound is the control plane's 404: the session id does not exist.
	// It is split out because session_read/AC3 requires a caller to tell a
	// missing target apart from a malformed cursor and from a control plane
	// that is merely broken.
	kindNotFound
	// kindInvalidArgument is a cursor this package refuses before it reaches
	// the wire. Rejecting locally is what makes AC3's "the session is left
	// untouched" structural rather than a promise: no request is issued at all.
	kindInvalidArgument
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
	default:
		return "session platform api error: " + e.msg
	}
}

func unavailable(msg string) *Error     { return &Error{kind: kindUnavailable, msg: msg} }
func apiError(msg string) *Error        { return &Error{kind: kindAPI, msg: msg} }
func notFound(msg string) *Error        { return &Error{kind: kindNotFound, msg: msg} }
func invalidArgument(msg string) *Error { return &Error{kind: kindInvalidArgument, msg: msg} }

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
// session as it stands *after* the read. Both are carried so a caller can see
// that a single read woke a parked session and brought its pod back —
// session_read/AC2 is precisely the requirement that this not be silent.
type ReadResult struct {
	Session    Session `json:"session"`
	Path       string  `json:"path"`
	Payload    string  `json:"payload"`
	NextOffset int64   `json:"nextOffset"`
}

// Service talks to the control plane on behalf of the session tools.
//
// ListSessions is a passive read: it never promotes an idle session, restores a
// snapshot, or refreshes lastAccess. ReadSession is not — the control plane
// activates the target before reading it, which is why its result carries the
// branch it took.
type Service interface {
	ListSessions(ctx context.Context) ([]Session, error)
	ReadSession(ctx context.Context, id string, offset int64) (*ReadResult, error)
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

// ListSessions always fails with the configured reason.
func (u *Unavailable) ListSessions(context.Context) ([]Session, error) {
	return nil, unavailable(u.reason)
}

// ReadSession always fails with the configured reason.
func (u *Unavailable) ReadSession(context.Context, string, int64) (*ReadResult, error) {
	return nil, unavailable(u.reason)
}

// Client is the live control plane implementation of Service.
type Client struct {
	endpoint  string
	userAgent string
	http      *http.Client
}

// FromEnv builds a Client from SESSION_PLATFORM_ENDPOINT, the control plane's
// base URL (in production the cluster-internal
// http://control-plane.session-platform.svc.cluster.local). It returns
// (nil, nil) when the variable is unset, signalling that the session platform
// integration is simply not configured; a value that is not an absolute http(s)
// URL is a misconfiguration and returns an error.
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
	}, nil
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
//
// The offset is validated here rather than at the control plane: a negative
// cursor must be reported as a bad argument *and* must leave the session alone,
// and the only way to guarantee the second half is to issue no request at all.
// A control plane 404 is surfaced as its own error kind so "no such session"
// stays distinguishable from "the control plane is broken" (AC3).
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

// controlPlaneMessage pulls the `{"error": "..."}` string out of a control plane
// error body, falling back to the HTTP status when the body is not that shape.
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
