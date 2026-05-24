// Package grafana mints short-lived Grafana Cloud access tokens from a
// pre-created, read-only access policy. The server holds the management
// credentials and the policy id; callers receive only the resulting
// time-boxed token.
package grafana

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
	defaultAPIBase    = "https://www.grafana.com/api"
	tokenTTL          = time.Hour
	httpClientTimeout = 10 * time.Second
)

// errKind separates a "not configured" failure from an api error.
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

// Token is the Grafana Cloud access-token response (the subset we surface).
// The token secret is only returned by the API at creation time.
type Token struct {
	Token          string `json:"token"`
	Name           string `json:"name,omitempty"`
	DisplayName    string `json:"displayName,omitempty"`
	ExpiresAt      string `json:"expiresAt"`
	AccessPolicyID string `json:"accessPolicyId,omitempty"`
}

// Service mints short-lived tokens from the configured access policy.
type Service interface {
	CreateShortLivedToken(ctx context.Context) (*Token, error)
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

// CreateShortLivedToken always fails with the configured reason.
func (u *Unavailable) CreateShortLivedToken(context.Context) (*Token, error) {
	return nil, unavailable(u.reason)
}

// Client is the live Grafana Cloud implementation of Service.
type Client struct {
	managementToken string
	accessPolicyID  string
	region          string
	apiBase         string
	userAgent       string
	http            *http.Client
}

// FromEnv builds a Client from the GRAFANA_CLOUD_* environment variables. It
// returns (nil, nil) when GRAFANA_CLOUD_ACCESS_POLICY_TOKEN is unset,
// signalling that the integration is simply not configured (as opposed to
// misconfigured). GRAFANA_API_BASE_URL overrides the management API base and
// is intended for tests pointing at a mock; production leaves it unset.
func FromEnv() (*Client, error) {
	token := os.Getenv("GRAFANA_CLOUD_ACCESS_POLICY_TOKEN")
	if token == "" {
		return nil, nil
	}

	accessPolicyID := os.Getenv("GRAFANA_CLOUD_ACCESS_POLICY_ID")
	if accessPolicyID == "" {
		return nil, fmt.Errorf("GRAFANA_CLOUD_ACCESS_POLICY_ID is required when GRAFANA_CLOUD_ACCESS_POLICY_TOKEN is set")
	}

	region := os.Getenv("GRAFANA_CLOUD_REGION")
	if region == "" {
		return nil, fmt.Errorf("GRAFANA_CLOUD_REGION is required when GRAFANA_CLOUD_ACCESS_POLICY_TOKEN is set")
	}

	apiBase := os.Getenv("GRAFANA_API_BASE_URL")
	if apiBase == "" {
		apiBase = defaultAPIBase
	}

	return &Client{
		managementToken: token,
		accessPolicyID:  accessPolicyID,
		region:          region,
		apiBase:         strings.TrimRight(apiBase, "/"),
		userAgent:       version.Name + "/" + version.Version,
		http:            &http.Client{Timeout: httpClientTimeout},
	}, nil
}

// CreateShortLivedToken issues a token from the configured access policy with
// an expiry one hour from now. The token name embeds a unix timestamp to
// satisfy Grafana Cloud's per-policy name uniqueness constraint.
func (c *Client) CreateShortLivedToken(ctx context.Context) (*Token, error) {
	now := time.Now().UTC()
	name := fmt.Sprintf("%s-%d", version.Name, now.Unix())

	body := map[string]any{
		"accessPolicyId": c.accessPolicyID,
		"name":           name,
		"displayName":    name,
		"expiresAt":      now.Add(tokenTTL).Format(time.RFC3339),
	}
	bodyBytes, err := json.Marshal(body)
	if err != nil {
		return nil, apiError(fmt.Sprintf("encode request body: %v", err))
	}

	endpoint := fmt.Sprintf("%s/v1/tokens?region=%s", c.apiBase, url.QueryEscape(c.region))
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, apiError(fmt.Sprintf("build request: %v", err))
	}
	req.Header.Set("Authorization", "Bearer "+c.managementToken)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
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
		return nil, apiError("response did not include a token")
	}
	return &token, nil
}
