// Package grafana mints short-lived, read-only Grafana Cloud tokens and pairs
// them with the static query endpoints/instance IDs needed to use them.
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

// Credentials bundles a freshly minted short-lived token with the static
// Grafana Cloud query endpoints and their numeric instance IDs. Token is the
// shared HTTP Basic password for both the metrics and logs users.
type Credentials struct {
	Token       string
	ExpiresAt   string
	MetricsURL  string
	MetricsUser string
	LogsURL     string
	LogsUser    string
}

// Service mints short-lived read-only Grafana Cloud credentials. The access
// policy (and therefore the scope) and the one-hour TTL are fixed on the
// server, so the call takes no arguments.
type Service interface {
	CreateToken(ctx context.Context) (*Credentials, error)
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
func (u *Unavailable) CreateToken(context.Context) (*Credentials, error) {
	return nil, unavailable(u.reason)
}

// Client is the live Grafana Cloud implementation of Service.
type Client struct {
	issuerToken  string
	readPolicyID string
	region       string
	apiBase      string
	metricsURL   string
	metricsUser  string
	logsURL      string
	logsUser     string
	userAgent    string
	http         *http.Client
}

// FromEnv builds a Client from the GRAFANA_* environment variables. It returns
// (nil, nil) when GRAFANA_ISSUER_TOKEN is unset, signalling that the Grafana
// integration is simply not configured. When the issuer token is set but a
// required companion variable is missing, it returns an error (a
// misconfiguration) rather than silently dropping it.
func FromEnv() (*Client, error) {
	issuerToken := os.Getenv("GRAFANA_ISSUER_TOKEN")
	if issuerToken == "" {
		return nil, nil
	}

	readPolicyID := os.Getenv("GRAFANA_READ_POLICY_ID")
	if readPolicyID == "" {
		return nil, fmt.Errorf("GRAFANA_READ_POLICY_ID is required when GRAFANA_ISSUER_TOKEN is set")
	}

	metricsURL := os.Getenv("GRAFANA_METRICS_URL")
	metricsUser := os.Getenv("GRAFANA_METRICS_USER")
	logsURL := os.Getenv("GRAFANA_LOGS_URL")
	logsUser := os.Getenv("GRAFANA_LOGS_USER")
	var missing []string
	for _, kv := range []struct{ name, val string }{
		{"GRAFANA_METRICS_URL", metricsURL},
		{"GRAFANA_METRICS_USER", metricsUser},
		{"GRAFANA_LOGS_URL", logsURL},
		{"GRAFANA_LOGS_USER", logsUser},
	} {
		if kv.val == "" {
			missing = append(missing, kv.name)
		}
	}
	if len(missing) > 0 {
		return nil, fmt.Errorf("%s required when GRAFANA_ISSUER_TOKEN is set", strings.Join(missing, ", "))
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
		metricsURL:   metricsURL,
		metricsUser:  metricsUser,
		logsURL:      logsURL,
		logsUser:     logsUser,
		userAgent:    version.Name + "/" + version.Version,
		http:         &http.Client{Timeout: httpClientTimeout},
	}, nil
}

// CreateToken mints a fresh token under the configured access policy that
// expires one hour from now and returns it alongside the static query config.
func (c *Client) CreateToken(ctx context.Context) (*Credentials, error) {
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

	var parsed struct {
		Token     string `json:"token"`
		ExpiresAt string `json:"expiresAt"`
	}
	if err := json.Unmarshal(respBody, &parsed); err != nil {
		return nil, apiError(fmt.Sprintf("parse token: %v", err))
	}
	if parsed.Token == "" {
		return nil, apiError("grafana response did not include a token")
	}
	return &Credentials{
		Token:       parsed.Token,
		ExpiresAt:   parsed.ExpiresAt,
		MetricsURL:  c.metricsURL,
		MetricsUser: c.metricsUser,
		LogsURL:     c.logsURL,
		LogsUser:    c.logsUser,
	}, nil
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
