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

	statusesPermission = "statuses"
)

// errKind separates a "not configured" failure from an apiserver error.
type errKind int

const (
	kindUnavailable errKind = iota
	kindAPI
	kindRejected
	kindInvalid
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
	case kindRejected:
		return "github token request rejected: " + e.msg
	case kindInvalid:
		return "github commit status refused: " + e.msg
	default:
		return "github api error: " + e.msg
	}
}

func unavailable(msg string) *Error { return &Error{kind: kindUnavailable, msg: msg} }
func apiError(msg string) *Error    { return &Error{kind: kindAPI, msg: msg} }
func rejected(msg string) *Error    { return &Error{kind: kindRejected, msg: msg} }

// Unavailable reports whether this is the "not configured" kind — the one
// the dispatcher records as a refusal, not an error (prd-event-log AC4).
func (e *Error) Unavailable() bool { return e.kind == kindUnavailable }

// InstallationToken is the GitHub-shaped installation access token response.
type InstallationToken struct {
	Token               string         `json:"token"`
	ExpiresAt           string         `json:"expires_at"`
	Permissions         map[string]any `json:"permissions,omitempty"`
	RepositorySelection string         `json:"repository_selection,omitempty"`
}

// Service mints installation tokens for the configured GitHub App and spends
// the one permission it will not hand out as a token.
type Service interface {
	CreateInstallationToken(ctx context.Context, repositories []string, permissions map[string]any) (*InstallationToken, error)
	CreateCommitStatus(ctx context.Context, in CommitStatusInput) (*CommitStatus, error)
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

	statusContextPrefixes []string
}

// FromEnv builds a Client from the GITHUB_APP_* environment variables.
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
		clientID:              clientID,
		installationID:        installationID,
		privateKey:            privateKey,
		apiBase:               strings.TrimRight(apiBase, "/"),
		userAgent:             version.Name + "/" + version.Version,
		http:                  &http.Client{Timeout: httpClientTimeout},
		statusContextPrefixes: contextPrefixesFromEnv(),
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

func permissionLevel(permissions map[string]any, name string) string {
	level, _ := permissions[name].(string)
	return strings.ToLower(strings.TrimSpace(level))
}

type installation struct {
	Permissions map[string]any `json:"permissions"`
	Account     struct {
		Login string `json:"login"`
	} `json:"account"`
}

func (c *Client) fetchInstallation(ctx context.Context, jwtToken string) (*installation, error) {
	url := fmt.Sprintf("%s/app/installations/%d", c.apiBase, c.installationID)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, apiError(fmt.Sprintf("build request: %v", err))
	}
	req.Header.Set("Authorization", "Bearer "+jwtToken)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", githubAPIVersion)
	req.Header.Set("User-Agent", c.userAgent)

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, apiError(fmt.Sprintf("get %s: %v", url, err))
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, apiError(fmt.Sprintf("%s returned %s: %s", url, resp.Status, string(respBody)))
	}

	var parsed installation
	if err := json.Unmarshal(respBody, &parsed); err != nil {
		return nil, apiError(fmt.Sprintf("parse installation: %v", err))
	}
	return &parsed, nil
}

func (c *Client) installationPermissions(ctx context.Context, jwtToken string) (map[string]any, error) {
	parsed, err := c.fetchInstallation(ctx, jwtToken)
	if err != nil {
		return nil, err
	}
	if parsed.Permissions == nil {
		return nil, apiError(fmt.Sprintf("%s/app/installations/%d returned no permissions", c.apiBase, c.installationID))
	}
	return parsed.Permissions, nil
}

// installationOwner is the owner half of every repository path this server
// builds — CreateCommitStatus says why a caller never supplies that half.
func (c *Client) installationOwner(ctx context.Context, jwtToken string) (string, error) {
	parsed, err := c.fetchInstallation(ctx, jwtToken)
	if err != nil {
		return "", err
	}
	if parsed.Account.Login == "" {
		return "", apiError(fmt.Sprintf("%s/app/installations/%d returned no account login",
			c.apiBase, c.installationID))
	}
	return parsed.Account.Login, nil
}

func (c *Client) effectivePermissions(ctx context.Context, jwtToken string, requested map[string]any) (map[string]any, error) {
	if requested != nil {
		if permissionLevel(requested, statusesPermission) == "write" {
			return nil, rejected("this tool does not issue statuses: write — use github_commit_status_create, which exercises commit status writes as a narrow action. statuses: read is issued normally")
		}
		return requested, nil
	}

	installed, err := c.installationPermissions(ctx, jwtToken)
	if err != nil {
		return nil, err
	}

	effective := make(map[string]any, len(installed))
	for name, level := range installed {
		effective[name] = level
	}
	if permissionLevel(effective, statusesPermission) == "write" {
		effective[statusesPermission] = "read"
	}
	return effective, nil
}

// revokeToken authenticates with the installation token being discarded, not
// with the app JWT the other two calls use.
func (c *Client) revokeToken(ctx context.Context, token string) error {
	url := c.apiBase + "/installation/token"

	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, url, nil)
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", githubAPIVersion)
	req.Header.Set("User-Agent", c.userAgent)

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("delete %s: %w", url, err)
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("%s returned %s", url, resp.Status)
	}
	return nil
}

// mintInstallationToken is the raw exchange, shared by the token tool and by
// CreateCommitStatus. AC5's guard is deliberately *not* here: it belongs to the
// path that hands the token to a caller, and this one is also reached by the
// narrow action that spends statuses: write without returning it.
func (c *Client) mintInstallationToken(ctx context.Context, jwtToken string, body map[string]any) (*InstallationToken, error) {
	url := fmt.Sprintf("%s/app/installations/%d/access_tokens", c.apiBase, c.installationID)

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

func (c *Client) CreateInstallationToken(ctx context.Context, repositories []string, permissions map[string]any) (*InstallationToken, error) {
	jwtToken, err := c.appJWT()
	if err != nil {
		return nil, err
	}

	effective, err := c.effectivePermissions(ctx, jwtToken, permissions)
	if err != nil {
		return nil, err
	}

	body := map[string]any{}
	if repositories != nil {
		body["repositories"] = repositories
	}
	if effective != nil {
		body["permissions"] = effective
	}

	token, err := c.mintInstallationToken(ctx, jwtToken, body)
	if err != nil {
		return nil, err
	}

	if permissionLevel(token.Permissions, statusesPermission) == "write" {
		msg := "installation token came back carrying statuses: write; it was revoked and is not returned"
		if revokeErr := c.revokeToken(ctx, token.Token); revokeErr != nil {
			msg += fmt.Sprintf(" (revoke failed: %v)", revokeErr)
		}
		return nil, apiError(msg)
	}
	return token, nil
}
