// Package github mints short-lived GitHub App installation tokens.
package github

import (
	"bytes"
	"context"
	"crypto/rsa"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/dlddu/homelab-k3s-mcp/internal/version"
	"github.com/golang-jwt/jwt/v5"
)

const (
	defaultAPIBase    = "https://api.github.com"
	githubAPIVersion  = "2022-11-28"
	jwtTTLSeconds     = 540
	jwtClockSkewSecs  = 60
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
		return "github app unavailable: " + e.msg
	default:
		return "github api error: " + e.msg
	}
}

func unavailable(msg string) *Error { return &Error{kind: kindUnavailable, msg: msg} }
func apiError(msg string) *Error    { return &Error{kind: kindAPI, msg: msg} }

// InstallationToken is the GitHub-shaped installation access token response.
type InstallationToken struct {
	Token               string         `json:"token"`
	ExpiresAt           string         `json:"expires_at"`
	Permissions         map[string]any `json:"permissions,omitempty"`
	RepositorySelection string         `json:"repository_selection,omitempty"`
}

// Service mints installation tokens for the configured GitHub App.
type Service interface {
	CreateInstallationToken(ctx context.Context, repositories []string, permissions map[string]any) (*InstallationToken, error)
}

// Unavailable is a Service that fails every call with the same reason.
type Unavailable struct {
	reason string
}

// NewUnavailable builds an Unavailable service with the given reason.
func NewUnavailable(reason string) *Unavailable {
	if reason == "" {
		reason = "github app credentials are not configured"
	}
	return &Unavailable{reason: reason}
}

func (u *Unavailable) CreateInstallationToken(context.Context, []string, map[string]any) (*InstallationToken, error) {
	return nil, unavailable(u.reason)
}

// Client is the live GitHub App implementation of Service.
type Client struct {
	clientID       string
	installationID int64
	privateKey     *rsa.PrivateKey
	apiBase        string
	userAgent      string
	http           *http.Client
}

// FromEnv builds a Client from the GITHUB_APP_* environment variables. It
// returns (nil, nil) when GITHUB_APP_CLIENT_ID is unset, signalling that the
// GitHub integration is simply not configured (as opposed to misconfigured).
func FromEnv() (*Client, error) {
	clientID := os.Getenv("GITHUB_APP_CLIENT_ID")
	if clientID == "" {
		return nil, nil
	}

	installationRaw := os.Getenv("GITHUB_APP_INSTALLATION_ID")
	if installationRaw == "" {
		return nil, fmt.Errorf("GITHUB_APP_INSTALLATION_ID is required when GITHUB_APP_CLIENT_ID is set")
	}
	installationID, err := strconv.ParseInt(installationRaw, 10, 64)
	if err != nil {
		return nil, fmt.Errorf("parse GITHUB_APP_INSTALLATION_ID: %w", err)
	}

	pem := os.Getenv("GITHUB_APP_PRIVATE_KEY")
	if pem == "" {
		return nil, fmt.Errorf("GITHUB_APP_PRIVATE_KEY is required when GITHUB_APP_CLIENT_ID is set")
	}
	privateKey, err := jwt.ParseRSAPrivateKeyFromPEM([]byte(pem))
	if err != nil {
		return nil, fmt.Errorf("parse github app private key: %w", err)
	}

	apiBase := os.Getenv("GITHUB_API_BASE_URL")
	if apiBase == "" {
		apiBase = defaultAPIBase
	}

	return &Client{
		clientID:       clientID,
		installationID: installationID,
		privateKey:     privateKey,
		apiBase:        strings.TrimRight(apiBase, "/"),
		userAgent:      version.Name + "/" + version.Version,
		http:           &http.Client{Timeout: httpClientTimeout},
	}, nil
}

func (c *Client) appJWT() (string, error) {
	now := time.Now().Unix()
	claims := jwt.MapClaims{
		"iat": now - jwtClockSkewSecs,
		"exp": now + jwtTTLSeconds,
		"iss": c.clientID,
	}
	token := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	signed, err := token.SignedString(c.privateKey)
	if err != nil {
		return "", apiError(fmt.Sprintf("sign app jwt: %v", err))
	}
	return signed, nil
}

func (c *Client) CreateInstallationToken(ctx context.Context, repositories []string, permissions map[string]any) (*InstallationToken, error) {
	jwtToken, err := c.appJWT()
	if err != nil {
		return nil, err
	}

	url := fmt.Sprintf("%s/app/installations/%d/access_tokens", c.apiBase, c.installationID)

	body := map[string]any{}
	if repositories != nil {
		body["repositories"] = repositories
	}
	if permissions != nil {
		body["permissions"] = permissions
	}
	bodyBytes, err := json.Marshal(body)
	if err != nil {
		return nil, apiError(fmt.Sprintf("encode request body: %v", err))
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, apiError(fmt.Sprintf("build request: %v", err))
	}
	req.Header.Set("Authorization", "Bearer "+jwtToken)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", githubAPIVersion)
	req.Header.Set("User-Agent", c.userAgent)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, apiError(fmt.Sprintf("post %s: %v", url, err))
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, apiError(fmt.Sprintf("%s returned %s: %s", url, resp.Status, string(respBody)))
	}

	var token InstallationToken
	if err := json.Unmarshal(respBody, &token); err != nil {
		return nil, apiError(fmt.Sprintf("parse installation token: %v", err))
	}
	return &token, nil
}
