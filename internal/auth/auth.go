// Package auth implements OAuth 2.0 bearer-token protection for the MCP
// endpoint, validating RS256 JWTs against a provider's JWKS.
package auth

import (
	"context"
	"crypto/rsa"
	"crypto/subtle"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"log/slog"
	"math/big"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

const httpClientTimeout = 10 * time.Second

// Config holds the credential configuration for the /mcp gate: the optional
// OAuth verified-issuer settings with a cached JWKS key set, and the optional
// set of static API keys for non-interactive clients.
type Config struct {
	Issuer   string
	Audience string
	Resource string

	jwksURI string
	http    *http.Client

	// apiKeys are static bearer credentials for non-interactive automation.
	// A request authorizes if it matches any key here or verifies as a JWT.
	apiKeys []string

	mu   sync.RWMutex
	keys map[string]*rsa.PublicKey
}

type providerMetadata struct {
	JWKSURI string `json:"jwks_uri"`
}

type jwks struct {
	Keys []jwk `json:"keys"`
}

type jwk struct {
	Kid string `json:"kid"`
	Kty string `json:"kty"`
	N   string `json:"n"`
	E   string `json:"e"`
}

// FromEnv builds auth configuration from the environment. It returns
// (nil, nil) when auth is disabled via MCP_AUTH_DISABLED.
//
// Two independent, composable credential paths gate /mcp:
//
//   - Static API keys, from MCP_API_KEYS (comma-separated), for non-interactive
//     automation clients that cannot run the interactive OAuth flow.
//   - OAuth 2.0 bearer JWTs, from MCP_OAUTH_ISSUER / MCP_OAUTH_AUDIENCE /
//     MCP_OAUTH_RESOURCE. Setting any MCP_OAUTH_* variable enables OAuth and
//     performs OIDC discovery plus an initial JWKS fetch.
//
// At least one path must be configured. With neither set (and auth not
// explicitly disabled) /mcp would be undefended, so FromEnv returns an error
// rather than silently serving without authentication.
func FromEnv(ctx context.Context) (*Config, error) {
	if disabled := os.Getenv("MCP_AUTH_DISABLED"); disabled == "1" || disabled == "true" {
		return nil, nil
	}

	apiKeys := parseAPIKeys(os.Getenv("MCP_API_KEYS"))

	issuer := os.Getenv("MCP_OAUTH_ISSUER")
	audience := os.Getenv("MCP_OAUTH_AUDIENCE")
	resource := os.Getenv("MCP_OAUTH_RESOURCE")
	oauthRequested := issuer != "" || audience != "" || resource != ""

	if len(apiKeys) == 0 && !oauthRequested {
		return nil, fmt.Errorf("no authentication configured: set MCP_API_KEYS and/or MCP_OAUTH_ISSUER+MCP_OAUTH_AUDIENCE, or MCP_AUTH_DISABLED=1 to serve /mcp without auth")
	}

	cfg := &Config{
		apiKeys: apiKeys,
		keys:    map[string]*rsa.PublicKey{},
	}

	if oauthRequested {
		if err := cfg.configureOAuth(ctx, issuer, audience, resource); err != nil {
			return nil, err
		}
	}

	return cfg, nil
}

// configureOAuth validates the OAuth settings, performs OIDC discovery and the
// initial JWKS fetch, and populates the OAuth fields on c. Partial
// configuration (issuer or audience missing) is an error.
func (c *Config) configureOAuth(ctx context.Context, issuer, audience, resource string) error {
	if issuer == "" {
		return fmt.Errorf("MCP_OAUTH_ISSUER is required when OAuth is enabled")
	}
	if audience == "" {
		return fmt.Errorf("MCP_OAUTH_AUDIENCE is required when OAuth is enabled")
	}
	if resource == "" {
		resource = audience
	}

	httpClient := &http.Client{Timeout: httpClientTimeout}
	metadataURL := strings.TrimRight(issuer, "/") + "/.well-known/openid-configuration"
	var metadata providerMetadata
	if err := getJSON(ctx, httpClient, metadataURL, &metadata); err != nil {
		return fmt.Errorf("fetch %s: %w", metadataURL, err)
	}
	if metadata.JWKSURI == "" {
		return fmt.Errorf("openid-configuration has no jwks_uri")
	}

	c.Issuer = issuer
	c.Audience = audience
	c.Resource = resource
	c.jwksURI = metadata.JWKSURI
	c.http = httpClient
	return c.refreshKeys(ctx)
}

// parseAPIKeys splits a comma-separated key list, trimming whitespace and
// dropping empty entries. It returns nil when no non-empty keys remain.
func parseAPIKeys(raw string) []string {
	var keys []string
	for _, part := range strings.Split(raw, ",") {
		if k := strings.TrimSpace(part); k != "" {
			keys = append(keys, k)
		}
	}
	return keys
}

func getJSON(ctx context.Context, client *http.Client, url string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("status %s", resp.Status)
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

func (c *Config) refreshKeys(ctx context.Context) error {
	var set jwks
	if err := getJSON(ctx, c.http, c.jwksURI, &set); err != nil {
		return fmt.Errorf("fetch %s: %w", c.jwksURI, err)
	}

	newKeys := map[string]*rsa.PublicKey{}
	for _, k := range set.Keys {
		if k.Kty != "RSA" || k.N == "" || k.E == "" {
			continue
		}
		key, err := rsaPublicKey(k.N, k.E)
		if err != nil {
			continue
		}
		newKeys[k.Kid] = key
	}

	if len(newKeys) == 0 {
		return fmt.Errorf("jwks contains no usable RSA keys")
	}

	c.mu.Lock()
	c.keys = newKeys
	c.mu.Unlock()
	return nil
}

func rsaPublicKey(nStr, eStr string) (*rsa.PublicKey, error) {
	nBytes, err := base64.RawURLEncoding.DecodeString(nStr)
	if err != nil {
		return nil, err
	}
	eBytes, err := base64.RawURLEncoding.DecodeString(eStr)
	if err != nil {
		return nil, err
	}

	// Left-pad the exponent to 8 bytes so it can be read as a big-endian uint64.
	padded := make([]byte, 8)
	copy(padded[8-len(eBytes):], eBytes)
	e := binary.BigEndian.Uint64(padded)

	return &rsa.PublicKey{
		N: new(big.Int).SetBytes(nBytes),
		E: int(e),
	}, nil
}

func (c *Config) keyForKid(ctx context.Context, kid string) *rsa.PublicKey {
	c.mu.RLock()
	key := c.keys[kid]
	c.mu.RUnlock()
	if key != nil {
		return key
	}
	if err := c.refreshKeys(ctx); err != nil {
		return nil
	}
	c.mu.RLock()
	key = c.keys[kid]
	c.mu.RUnlock()
	return key
}

func (c *Config) verify(ctx context.Context, raw string) error {
	// In API-key-only mode there is no issuer, JWKS URI, or HTTP client, so no
	// JWT can verify. Refuse before keyForKid would try to fetch a nil JWKS.
	if !c.OAuthConfigured() {
		return fmt.Errorf("oauth verification not configured")
	}

	keyFunc := func(token *jwt.Token) (any, error) {
		kid, _ := token.Header["kid"].(string)
		if kid == "" {
			return nil, fmt.Errorf("missing kid")
		}
		key := c.keyForKid(ctx, kid)
		if key == nil {
			return nil, fmt.Errorf("unknown kid")
		}
		return key, nil
	}

	_, err := jwt.Parse(raw, keyFunc,
		jwt.WithValidMethods([]string{"RS256"}),
		jwt.WithAudience(c.Audience),
		jwt.WithIssuer(c.Issuer),
		jwt.WithExpirationRequired(),
	)
	return err
}

// ProtectedResourceMetadata is the OAuth 2.0 protected-resource document.
type ProtectedResourceMetadata struct {
	Resource               string   `json:"resource"`
	AuthorizationServers   []string `json:"authorization_servers"`
	BearerMethodsSupported []string `json:"bearer_methods_supported"`
}

// MetadataHandler serves /.well-known/oauth-protected-resource.
func MetadataHandler(c *Config) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, ProtectedResourceMetadata{
			Resource:               c.Resource,
			AuthorizationServers:   []string{c.Issuer},
			BearerMethodsSupported: []string{"header"},
		})
	})
}

// OAuthConfigured reports whether OAuth bearer verification is active. When
// false, only static API keys authorize requests and no OAuth discovery
// metadata is advertised.
func (c *Config) OAuthConfigured() bool {
	return c.Issuer != ""
}

// APIKeyCount returns how many static API keys are configured. It exposes only
// the count, never the key values, for startup logging.
func (c *Config) APIKeyCount() int {
	return len(c.apiKeys)
}

// matchAPIKey reports whether raw equals any configured static API key. Each
// comparison uses subtle.ConstantTimeCompare so equal-length keys are checked
// without leaking content through timing, and the loop accumulates the result
// without an early return so the number of comparisons never reveals which key
// (if any) matched.
func (c *Config) matchAPIKey(raw string) bool {
	rawBytes := []byte(raw)
	var matched int
	for _, key := range c.apiKeys {
		matched |= subtle.ConstantTimeCompare(rawBytes, []byte(key))
	}
	return matched == 1
}

// RequireBearer wraps next, rejecting requests that present neither a valid
// static API key nor a valid OAuth bearer JWT. The static key is checked first
// (constant time); on no match it falls back to JWT verification.
func (c *Config) RequireBearer(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token, authErr := extractBearer(r)
		if authErr != "" {
			c.unauthorized(w, authErr)
			return
		}
		if c.matchAPIKey(token) {
			slog.Debug("mcp authenticated", "method", "api_key")
			next.ServeHTTP(w, r)
			return
		}
		if err := c.verify(r.Context(), token); err != nil {
			c.unauthorized(w, "invalid_token")
			return
		}
		slog.Debug("mcp authenticated", "method", "jwt")
		next.ServeHTTP(w, r)
	})
}

func extractBearer(r *http.Request) (token string, authErr string) {
	header := r.Header.Get("Authorization")
	if header == "" {
		return "", "missing_token"
	}
	rest, ok := strings.CutPrefix(header, "Bearer ")
	if !ok {
		rest, ok = strings.CutPrefix(header, "bearer ")
	}
	if !ok || rest == "" {
		return "", "invalid_request"
	}
	return rest, ""
}

func (c *Config) unauthorized(w http.ResponseWriter, authErr string) {
	var challenge string
	if c.OAuthConfigured() {
		// Point OAuth clients at the protected-resource metadata for discovery.
		challenge = fmt.Sprintf(
			`Bearer realm=%q, error=%q, resource_metadata=%q`,
			c.Resource, authErr, c.Resource+"/.well-known/oauth-protected-resource",
		)
	} else {
		// API-key-only mode: there is no OAuth discovery document to advertise.
		challenge = fmt.Sprintf(`Bearer error=%q`, authErr)
	}
	w.Header().Set("WWW-Authenticate", challenge)
	w.WriteHeader(http.StatusUnauthorized)
	_, _ = w.Write([]byte(authErr))
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
