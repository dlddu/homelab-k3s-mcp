// Package sessionplatform reads the session inventory from the session-platform
// control plane's in-cluster REST API. The control plane is only reachable from
// inside the cluster, so this package is what opens it to MCP clients without an
// ingress or a port-forward.
package sessionplatform

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
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

// errKind separates a "not configured" failure from a control plane error.
type errKind int

const (
	kindUnavailable errKind = iota
	kindAPI
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
	default:
		return "session platform api error: " + e.msg
	}
}

func unavailable(msg string) *Error { return &Error{kind: kindUnavailable, msg: msg} }
func apiError(msg string) *Error    { return &Error{kind: kindAPI, msg: msg} }

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

// Service lists the sessions the control plane holds. Listing is a passive read:
// it never promotes an idle session, restores a snapshot, or refreshes
// lastAccess, because only the control plane's read/write endpoints carry that
// "access means active" effect.
type Service interface {
	ListSessions(ctx context.Context) ([]Session, error)
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
