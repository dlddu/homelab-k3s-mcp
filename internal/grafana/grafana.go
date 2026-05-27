// Package grafana mints short-lived, read-only Grafana Cloud access tokens.
package grafana

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
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
	// defaultAPIBase is the Grafana Cloud API base, including the /api segment.
	// The token path (/v1/tokens) is appended to it, so GRAFANA_API_URL must
	// likewise point at the .../api base (not the bare host).
	defaultAPIBase    = "https://www.grafana.com/api"
	tokenTTL          = time.Hour
	httpClientTimeout = 10 * time.Second
)

// errKind separates a "not configured" failure from an apiserver error.
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
		return "grafana cloud unavailable: " + e.msg
	default:
		return "grafana cloud api error: " + e.msg
	}
}

func unavailable(msg string) *Error { return &Error{kind: kindUnavailable, msg: msg} }
func apiError(msg string) *Error    { return &Error{kind: kindAPI, msg: msg} }

// Token is the Grafana Cloud access-policy token response.
type Token struct {
	Token       string `json:"token"`
	Name        string `json:"name,omitempty"`
	DisplayName string `json:"displayName,omitempty"`
	ExpiresAt   string `json:"expiresAt,omitempty"`
}

// Service mints short-lived read-only Grafana Cloud tokens. The access policy
// (and therefore the scope) and the one-hour TTL are fixed on the server, so
// the call takes no arguments.
type Service interface {
	CreateToken(ctx context.Context) (*Token, error)
}

// Unavailable is a Service that fails every call with the same reason.
type Unavailable struct {
	reason string
}

// NewUnavailable builds an Unavailable service with the given reason.
func NewUnavailable(reason string) *Unavailable {
	if reason == "" {
		reason = "grafana cloud credentials are not configured"
	}
	return &Unavailable{reason: reason}
}

// CreateToken always fails with the configured reason.
func (u *Unavailable) CreateToken(context.Context) (*Token, error) {
	return nil, unavailable(u.reason)
}

// Client is the live Grafana Cloud implementation of Service.
type Client struct {
	issuerToken  string
	readPolicyID string
	region       string
	apiBase      string
	userAgent    string
	http         *http.Client
}

// FromEnv builds a Client from the GRAFANA_* environment variables. It returns
// (nil, nil) when GRAFANA_ISSUER_TOKEN is unset, signalling that the Grafana
// integration is simply not configured (as opposed to misconfigured).
func FromEnv() (*Client, error) {
	issuerToken := os.Getenv("GRAFANA_ISSUER_TOKEN")
	if issuerToken == "" {
		return nil, nil
	}

	readPolicyID := os.Getenv("GRAFANA_READ_POLICY_ID")
	if readPolicyID == "" {
		return nil, fmt.Errorf("GRAFANA_READ_POLICY_ID is required when GRAFANA_ISSUER_TOKEN is set")
	}

	apiBase := os.Getenv("GRAFANA_API_URL")
	if apiBase == "" {
		apiBase = defaultAPIBase
	}

	return &Client{
		issuerToken:  issuerToken,
		readPolicyID: readPolicyID,
		region:       os.Getenv("GRAFANA_REGION"),
		apiBase:      strings.TrimRight(apiBase, "/"),
		userAgent:    version.Name + "/" + version.Version,
		http:         &http.Client{Timeout: httpClientTimeout},
	}, nil
}

// CreateToken mints a fresh token under the configured access policy that
// expires one hour from now.
func (c *Client) CreateToken(ctx context.Context) (*Token, error) {
	name := tokenName()
	expiresAt := time.Now().Add(tokenTTL).UTC().Format(time.RFC3339)

	body := map[string]any{
		"accessPolicyId": c.readPolicyID,
		"name":           name,
		"displayName":    name,
		"expiresAt":      expiresAt,
	}
	bodyBytes, err := json.Marshal(body)
	if err != nil {
		return nil, apiError(fmt.Sprintf("encode request body: %v", err))
	}

	endpoint := c.apiBase + "/v1/tokens"
	if c.region != "" {
		endpoint += "?region=" + url.QueryEscape(c.region)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, apiError(fmt.Sprintf("build request: %v", err))
	}
	req.Header.Set("Authorization", "Bearer "+c.issuerToken)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", c.userAgent)

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, apiError(fmt.Sprintf("post %s: %v", endpoint, err))
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, apiError(fmt.Sprintf("%s returned %s: %s", endpoint, resp.Status, string(respBody)))
	}

	var token Token
	if err := json.Unmarshal(respBody, &token); err != nil {
		return nil, apiError(fmt.Sprintf("parse token: %v", err))
	}
	if token.Token == "" {
		return nil, apiError("grafana response did not include a token")
	}
	return &token, nil
}

// tokenName builds a per-request unique token name. Grafana Cloud requires the
// name to be unique within an access policy, so a timestamp plus a random
// suffix avoids collisions across rapid or concurrent calls.
func tokenName() string {
	suffix := make([]byte, 4)
	if _, err := rand.Read(suffix); err != nil {
		return fmt.Sprintf("%s-%d", version.Name, time.Now().UnixNano())
	}
	return fmt.Sprintf("%s-%d-%s", version.Name, time.Now().Unix(), hex.EncodeToString(suffix))
}
