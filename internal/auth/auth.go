// Package auth implements OAuth 2.0 bearer-token protection for the MCP
// endpoint, validating RS256 JWTs against a provider's JWKS.
package auth

import (
	"context"
	"crypto/rsa"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"math/big"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

const httpClientTimeout = 10 * time.Second

// Config holds the verified-issuer configuration and a cached JWKS key set.
type Config struct {
	Issuer   string
	Audience string
	Resource string

	jwksURI string
	http    *http.Client

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
// (nil, nil) when auth is disabled via MCP_AUTH_DISABLED. When auth is enabled
// it performs OIDC discovery and an initial JWKS fetch.
func FromEnv(ctx context.Context) (*Config, error) {
	if disabled := os.Getenv("MCP_AUTH_DISABLED"); disabled == "1" || disabled == "true" {
		return nil, nil
	}

	issuer := os.Getenv("MCP_OAUTH_ISSUER")
	if issuer == "" {
		return nil, fmt.Errorf("MCP_OAUTH_ISSUER is required when auth is enabled")
	}
	audience := os.Getenv("MCP_OAUTH_AUDIENCE")
	if audience == "" {
		return nil, fmt.Errorf("MCP_OAUTH_AUDIENCE is required when auth is enabled")
	}
	resource := os.Getenv("MCP_OAUTH_RESOURCE")
	if resource == "" {
		resource = audience
	}

	httpClient := &http.Client{Timeout: httpClientTimeout}

	metadataURL := strings.TrimRight(issuer, "/") + "/.well-known/openid-configuration"
	var metadata providerMetadata
	if err := getJSON(ctx, httpClient, metadataURL, &metadata); err != nil {
		return nil, fmt.Errorf("fetch %s: %w", metadataURL, err)
	}
	if metadata.JWKSURI == "" {
		return nil, fmt.Errorf("openid-configuration has no jwks_uri")
	}

	cfg := &Config{
		Issuer:   issuer,
		Audience: audience,
		Resource: resource,
		jwksURI:  metadata.JWKSURI,
		http:     httpClient,
		keys:     map[string]*rsa.PublicKey{},
	}
	if err := cfg.refreshKeys(ctx); err != nil {
		return nil, err
	}
	return cfg, nil
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

// RequireBearer wraps next, rejecting requests without a valid bearer token.
func (c *Config) RequireBearer(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token, authErr := extractBearer(r)
		if authErr != "" {
			c.unauthorized(w, authErr)
			return
		}
		if err := c.verify(r.Context(), token); err != nil {
			c.unauthorized(w, "invalid_token")
			return
		}
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
	challenge := fmt.Sprintf(
		`Bearer realm=%q, error=%q, resource_metadata=%q`,
		c.Resource, authErr, c.Resource+"/.well-known/oauth-protected-resource",
	)
	w.Header().Set("WWW-Authenticate", challenge)
	w.WriteHeader(http.StatusUnauthorized)
	_, _ = w.Write([]byte(authErr))
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
