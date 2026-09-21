package auth

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"github.com/dlddu/homelab-k3s-mcp/internal/eventlog"
)

// The AC1/AC2/AC7 request-path tests exercise only local logic (header parsing,
// key matching, challenge emission, metadata rendering) and never reach the
// network. The FromEnv OAuth-gating tests stand up a local httptest OIDC server
// so discovery can complete without a live provider.

// --- AC1: 인증 게이트 (gate decision) ---

func TestExtractBearerClassifiesHeader(t *testing.T) {
	cases := []struct {
		name      string
		header    string
		setHeader bool
		wantToken string
		wantErr   string
	}{
		{name: "missing header", setHeader: false, wantToken: "", wantErr: "missing_token"},
		{name: "valid bearer", setHeader: true, header: "Bearer abc123", wantToken: "abc123", wantErr: ""},
		{name: "lowercase scheme", setHeader: true, header: "bearer abc123", wantToken: "abc123", wantErr: ""},
		{name: "wrong scheme", setHeader: true, header: "Basic abc123", wantToken: "", wantErr: "invalid_request"},
		{name: "empty token", setHeader: true, header: "Bearer ", wantToken: "", wantErr: "invalid_request"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/mcp", nil)
			if tc.setHeader {
				req.Header.Set("Authorization", tc.header)
			}
			token, authErr := extractBearer(req)
			if token != tc.wantToken {
				t.Errorf("token = %q, want %q", token, tc.wantToken)
			}
			if authErr != tc.wantErr {
				t.Errorf("authErr = %q, want %q", authErr, tc.wantErr)
			}
		})
	}
}

func TestRequireBearerRejectsMissingTokenAndAdvertisesDiscovery(t *testing.T) {
	cfg := &Config{Issuer: "https://issuer.example.test", Resource: "https://mcp.example.test/mcp"}

	nextCalled := false
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		nextCalled = true
		w.WriteHeader(http.StatusOK)
	})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/mcp", nil) // no Authorization header
	cfg.RequireBearer(next).ServeHTTP(rec, req)

	if nextCalled {
		t.Fatal("next handler must not run for an unauthenticated request")
	}
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}

	challenge := rec.Header().Get("WWW-Authenticate")
	if !strings.HasPrefix(challenge, "Bearer ") {
		t.Fatalf("challenge = %q, want Bearer scheme", challenge)
	}
	if !strings.Contains(challenge, `error="missing_token"`) {
		t.Errorf("challenge = %q, want error=missing_token", challenge)
	}
	// AC2: the challenge must point clients at the protected-resource metadata.
	wantMeta := "https://mcp.example.test/mcp/.well-known/oauth-protected-resource"
	if !strings.Contains(challenge, "resource_metadata="+`"`+wantMeta+`"`) {
		t.Errorf("challenge = %q, want resource_metadata %q", challenge, wantMeta)
	}
}

func TestRequireBearerRejectsMalformedScheme(t *testing.T) {
	cfg := &Config{Issuer: "https://issuer.example.test", Resource: "https://mcp.example.test/mcp"}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/mcp", nil)
	req.Header.Set("Authorization", "Basic Zm9vOmJhcg==")
	cfg.RequireBearer(http.NotFoundHandler()).ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
	if !strings.Contains(rec.Header().Get("WWW-Authenticate"), `error="invalid_request"`) {
		t.Errorf("challenge = %q, want error=invalid_request", rec.Header().Get("WWW-Authenticate"))
	}
}

// --- AC2: 인증 디스커버리 (protected-resource metadata) ---

func TestMetadataHandlerServesProtectedResource(t *testing.T) {
	cfg := &Config{
		Issuer:   "https://issuer.example.test",
		Audience: "homelab-k3s-mcp",
		Resource: "https://mcp.example.test/mcp",
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/.well-known/oauth-protected-resource", nil)
	MetadataHandler(cfg).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", ct)
	}

	var got ProtectedResourceMetadata
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode body: %v (body=%s)", err, rec.Body.String())
	}
	if got.Resource != cfg.Resource {
		t.Errorf("resource = %q, want %q", got.Resource, cfg.Resource)
	}
	if len(got.AuthorizationServers) != 1 || got.AuthorizationServers[0] != cfg.Issuer {
		t.Errorf("authorization_servers = %v, want [%q]", got.AuthorizationServers, cfg.Issuer)
	}
	if len(got.BearerMethodsSupported) != 1 || got.BearerMethodsSupported[0] != "header" {
		t.Errorf("bearer_methods_supported = %v, want [header]", got.BearerMethodsSupported)
	}
}

// --- AC1: 인증 게이트 enable/disable via environment ---

func TestFromEnvDisabledReturnsNilConfig(t *testing.T) {
	t.Setenv("MCP_AUTH_DISABLED", "1")

	cfg, err := FromEnv(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg != nil {
		t.Fatalf("config = %+v, want nil when auth is disabled", cfg)
	}
}

// FromEnv gates on four credential combinations: neither → error;
// API keys only → key-only config; OAuth only and OAuth+keys → OAuth config.

func TestFromEnvNoAuthConfiguredErrors(t *testing.T) {
	t.Setenv("MCP_AUTH_DISABLED", "0")
	t.Setenv("MCP_API_KEYS", "")
	t.Setenv("MCP_OAUTH_ISSUER", "")
	t.Setenv("MCP_OAUTH_AUDIENCE", "")
	t.Setenv("MCP_OAUTH_RESOURCE", "")

	if _, err := FromEnv(context.Background()); err == nil {
		t.Fatal("expected error when neither API keys nor OAuth are configured")
	}
}

func TestFromEnvAPIKeysOnly(t *testing.T) {
	t.Setenv("MCP_AUTH_DISABLED", "0")
	t.Setenv("MCP_API_KEYS", "k1, k2 ,k3")
	t.Setenv("MCP_OAUTH_ISSUER", "")
	t.Setenv("MCP_OAUTH_AUDIENCE", "")
	t.Setenv("MCP_OAUTH_RESOURCE", "")

	cfg, err := FromEnv(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg == nil {
		t.Fatal("config = nil, want non-nil for API-key-only auth")
	}
	if cfg.OAuthConfigured() {
		t.Error("OAuthConfigured() = true, want false when no OAuth env is set")
	}
	if cfg.APIKeyCount() != 3 {
		t.Errorf("APIKeyCount() = %d, want 3", cfg.APIKeyCount())
	}
}

func TestFromEnvOAuthGating(t *testing.T) {
	key := newRSAKey(t)
	srv := oidcServer(t, "kid-1", &key.PublicKey)

	cases := []struct {
		name     string
		apiKeys  string
		wantKeys int
	}{
		{name: "oauth only", apiKeys: "", wantKeys: 0},
		{name: "oauth and keys", apiKeys: "k1,k2", wantKeys: 2},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("MCP_AUTH_DISABLED", "0")
			t.Setenv("MCP_API_KEYS", tc.apiKeys)
			t.Setenv("MCP_OAUTH_ISSUER", srv.URL)
			t.Setenv("MCP_OAUTH_AUDIENCE", "homelab-k3s-mcp")
			t.Setenv("MCP_OAUTH_RESOURCE", "https://mcp.example.test/mcp")

			cfg, err := FromEnv(context.Background())
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !cfg.OAuthConfigured() {
				t.Error("OAuthConfigured() = false, want true")
			}
			if cfg.Issuer != srv.URL {
				t.Errorf("Issuer = %q, want %q", cfg.Issuer, srv.URL)
			}
			if cfg.APIKeyCount() != tc.wantKeys {
				t.Errorf("APIKeyCount() = %d, want %d", cfg.APIKeyCount(), tc.wantKeys)
			}
		})
	}
}

func TestFromEnvRequiresIssuerWhenOAuthRequested(t *testing.T) {
	t.Setenv("MCP_AUTH_DISABLED", "0")
	t.Setenv("MCP_API_KEYS", "")
	t.Setenv("MCP_OAUTH_ISSUER", "")
	t.Setenv("MCP_OAUTH_AUDIENCE", "homelab-k3s-mcp") // OAuth partially requested
	t.Setenv("MCP_OAUTH_RESOURCE", "")

	if _, err := FromEnv(context.Background()); err == nil {
		t.Fatal("expected error when OAuth is requested but MCP_OAUTH_ISSUER is unset")
	}
}

func TestFromEnvRequiresAudienceWhenOAuthRequested(t *testing.T) {
	t.Setenv("MCP_AUTH_DISABLED", "0")
	t.Setenv("MCP_API_KEYS", "")
	t.Setenv("MCP_OAUTH_ISSUER", "https://issuer.example.test")
	t.Setenv("MCP_OAUTH_AUDIENCE", "")
	t.Setenv("MCP_OAUTH_RESOURCE", "")

	if _, err := FromEnv(context.Background()); err == nil {
		t.Fatal("expected error when OAuth is requested but MCP_OAUTH_AUDIENCE is unset")
	}
}

// --- AC7: 비대화형 API 키 인증 (static key gate) ---

func TestParseAPIKeys(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		want []string
	}{
		{name: "empty", raw: "", want: nil},
		{name: "whitespace only", raw: "  ,  ,", want: nil},
		{name: "single", raw: "key1", want: []string{"key1"}},
		{name: "multiple trimmed", raw: " key1 , key2,key3 ", want: []string{"key1", "key2", "key3"}},
		{name: "drops empties", raw: "key1,,key2,", want: []string{"key1", "key2"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := parseAPIKeys(tc.raw)
			if len(got) != len(tc.want) {
				t.Fatalf("parseAPIKeys(%q) = %v, want %v", tc.raw, got, tc.want)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Errorf("key[%d] = %q, want %q", i, got[i], tc.want[i])
				}
			}
		})
	}
}

func TestMatchAPIKey(t *testing.T) {
	cfg := &Config{apiKeys: []string{"alpha-secret", "beta-secret"}}
	cases := []struct {
		name string
		raw  string
		want bool
	}{
		{name: "first key", raw: "alpha-secret", want: true},
		{name: "second key", raw: "beta-secret", want: true},
		{name: "unknown", raw: "nope", want: false},
		{name: "empty", raw: "", want: false},
		{name: "prefix only", raw: "alpha", want: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := cfg.matchAPIKey(tc.raw); got != tc.want {
				t.Errorf("matchAPIKey(%q) = %v, want %v", tc.raw, got, tc.want)
			}
		})
	}

	// A config with no keys never matches (OAuth-only mode).
	if (&Config{}).matchAPIKey("anything") {
		t.Error("matchAPIKey on a keyless config = true, want false")
	}
}

func TestRequireBearerAcceptsAPIKey(t *testing.T) {
	cfg := &Config{apiKeys: []string{"automation-key"}}

	nextCalled := false
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		nextCalled = true
		w.WriteHeader(http.StatusOK)
	})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/mcp", nil)
	req.Header.Set("Authorization", "Bearer automation-key")
	cfg.RequireBearer(next).ServeHTTP(rec, req)

	if !nextCalled {
		t.Fatal("next handler must run for a valid API key")
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
}

func TestRequireBearerRejectsUnknownKeyInKeyOnlyMode(t *testing.T) {
	const key = "automation-key"
	cfg := &Config{apiKeys: []string{key}}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/mcp", nil)
	req.Header.Set("Authorization", "Bearer wrong-key")
	cfg.RequireBearer(http.NotFoundHandler()).ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
	challenge := rec.Header().Get("WWW-Authenticate")
	if !strings.HasPrefix(challenge, "Bearer ") {
		t.Fatalf("challenge = %q, want Bearer scheme", challenge)
	}
	// AC8: API-key-only mode advertises no OAuth discovery metadata.
	if strings.Contains(challenge, "resource_metadata") {
		t.Errorf("challenge = %q, must not advertise resource_metadata without OAuth", challenge)
	}
	// The configured key must never leak into the challenge or the body.
	if strings.Contains(challenge, key) || strings.Contains(rec.Body.String(), key) {
		t.Errorf("response leaked the API key: challenge=%q body=%q", challenge, rec.Body.String())
	}
}

// A structurally valid JWT presented in API-key-only mode must be rejected
// (401), never triggering a JWKS fetch against the unconfigured OAuth client.
func TestRequireBearerRejectsJWTInKeyOnlyMode(t *testing.T) {
	cfg := &Config{apiKeys: []string{"automation-key"}} // no OAuth configured

	key := newRSAKey(t)
	token := signJWT(t, key, "some-kid", "https://issuer.example.test", "homelab-k3s-mcp")

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/mcp", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	cfg.RequireBearer(http.NotFoundHandler()).ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401 (a JWT must not verify in key-only mode)", rec.Code)
	}
}

func TestRequireBearerAcceptsAPIKeyAndJWT(t *testing.T) {
	const (
		kid      = "test-kid"
		issuer   = "https://issuer.example.test"
		audience = "homelab-k3s-mcp"
		apiKey   = "automation-key"
	)
	key := newRSAKey(t)
	cfg := &Config{
		Issuer:   issuer,
		Audience: audience,
		Resource: "https://mcp.example.test/mcp",
		apiKeys:  []string{apiKey},
		keys:     map[string]*rsa.PublicKey{kid: &key.PublicKey},
	}

	pass := func(name, authz string) {
		t.Run(name, func(t *testing.T) {
			nextCalled := false
			next := http.HandlerFunc(func(http.ResponseWriter, *http.Request) { nextCalled = true })
			rec := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodPost, "/mcp", nil)
			req.Header.Set("Authorization", authz)
			cfg.RequireBearer(next).ServeHTTP(rec, req)
			if !nextCalled {
				t.Fatalf("next must run; status=%d challenge=%q", rec.Code, rec.Header().Get("WWW-Authenticate"))
			}
		})
	}

	pass("api key", "Bearer "+apiKey)
	pass("jwt", "Bearer "+signJWT(t, key, kid, issuer, audience))

	t.Run("invalid token", func(t *testing.T) {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/mcp", nil)
		req.Header.Set("Authorization", "Bearer not-a-key-or-jwt")
		cfg.RequireBearer(http.NotFoundHandler()).ServeHTTP(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("status = %d, want 401", rec.Code)
		}
		if !strings.Contains(rec.Header().Get("WWW-Authenticate"), `error="invalid_token"`) {
			t.Errorf("challenge = %q, want error=invalid_token", rec.Header().Get("WWW-Authenticate"))
		}
	})
}

// --- test helpers (RSA key, OIDC server, JWT signing) ---

func newRSAKey(t *testing.T) *rsa.PrivateKey {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate rsa key: %v", err)
	}
	return key
}

// oidcServer stands up an OIDC discovery + JWKS endpoint backed by pub so
// FromEnv's discovery completes without a live provider.
func oidcServer(t *testing.T, kid string, pub *rsa.PublicKey) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	var base string
	mux.HandleFunc("/.well-known/openid-configuration", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]string{"jwks_uri": base + "/jwks"})
	})
	mux.HandleFunc("/jwks", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"keys": []map[string]string{{
				"kid": kid,
				"kty": "RSA",
				"n":   base64.RawURLEncoding.EncodeToString(pub.N.Bytes()),
				"e":   base64.RawURLEncoding.EncodeToString(big.NewInt(int64(pub.E)).Bytes()),
			}},
		})
	})
	srv := httptest.NewServer(mux)
	base = srv.URL
	t.Cleanup(srv.Close)
	return srv
}

func signJWT(t *testing.T, key *rsa.PrivateKey, kid, issuer, audience string) string {
	t.Helper()
	tok := jwt.NewWithClaims(jwt.SigningMethodRS256, jwt.MapClaims{
		"iss": issuer,
		"aud": audience,
		"exp": time.Now().Add(time.Hour).Unix(),
	})
	tok.Header["kid"] = kid
	signed, err := tok.SignedString(key)
	if err != nil {
		t.Fatalf("sign jwt: %v", err)
	}
	return signed
}

// prd-event-log AC1/AC3: the identity handed to the dispatcher is the key's
// position or the JWT's subject, and never the credential itself.
func TestRequireBearerHandsDownAPrincipalWithoutTheCredential(t *testing.T) {
	const (
		kid      = "test-kid"
		issuer   = "https://issuer.example.test"
		audience = "homelab-k3s-mcp"
		subject  = "operator@example.test"
	)
	key := newRSAKey(t)
	cfg := &Config{
		Issuer:   issuer,
		Audience: audience,
		Resource: "https://mcp.example.test/mcp",
		apiKeys:  []string{"first-key", "second-key"},
		keys:     map[string]*rsa.PublicKey{kid: &key.PublicKey},
	}
	tok := jwt.NewWithClaims(jwt.SigningMethodRS256, jwt.MapClaims{
		"iss": issuer,
		"aud": audience,
		"sub": subject,
		"exp": time.Now().Add(time.Hour).Unix(),
	})
	tok.Header["kid"] = kid
	signed, err := tok.SignedString(key)
	if err != nil {
		t.Fatalf("sign jwt: %v", err)
	}

	cases := []struct {
		name, credential, want string
	}{
		{"second api key", "second-key", "api_key:2"},
		{"first api key", "first-key", "api_key:1"},
		{"jwt", signed, "jwt:" + subject},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var got eventlog.Principal
			next := http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
				got = eventlog.PrincipalFrom(r.Context())
			})
			rec := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodPost, "/mcp", nil)
			req.Header.Set("Authorization", "Bearer "+tc.credential)
			cfg.RequireBearer(next).ServeHTTP(rec, req)
			if got.String() != tc.want {
				t.Fatalf("principal = %q, want %q (status %d)", got, tc.want, rec.Code)
			}
			if strings.Contains(got.String(), tc.credential) {
				t.Fatalf("principal %q carries the credential", got)
			}
		})
	}

	t.Run("no auth layer", func(t *testing.T) {
		if got := eventlog.PrincipalFrom(context.Background()); got != eventlog.Anonymous {
			t.Fatalf("principal without RequireBearer = %+v, want Anonymous", got)
		}
	})
}

// recordingSink keeps the records this layer emits, as values.
type recordingSink struct{ records []eventlog.Record }

func (s *recordingSink) Emit(_ context.Context, r eventlog.Record) { s.records = append(s.records, r) }

// prd-event-log AC4: a tool call that fails authentication is recorded as a
// refused call with its reason, whichever way it failed — no header, a key
// nobody configured, a JWT that does not verify. AC3 crosses here: the record
// names the tool and says "unauthenticated", never the credential presented.
func TestRequireBearerRecordsTheCallItRefuses(t *testing.T) {
	const badKey = "not-a-configured-key"
	const badJWT = "eyJhbGciOiJSUzI1NiJ9.NOT-A-VALID-JWT.sig"
	body := `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"resource_get","arguments":{"apiVersion":"v1","kind":"Secret","namespace":"default","name":"s-1"}}}`
	cases := []struct {
		name, header string
	}{
		{"no header", ""},
		{"unknown api key", "Bearer " + badKey},
		{"unverifiable jwt", "Bearer " + badJWT},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sink := &recordingSink{}
			cfg := &Config{apiKeys: []string{"first-key"}, events: sink}
			rec := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(body))
			if tc.header != "" {
				req.Header.Set("Authorization", tc.header)
			}
			cfg.RequireBearer(http.NotFoundHandler()).ServeHTTP(rec, req)
			if rec.Code != http.StatusUnauthorized {
				t.Fatalf("status = %d, want 401", rec.Code)
			}
			if len(sink.records) != 1 {
				t.Fatalf("records = %d, want 1: %+v", len(sink.records), sink.records)
			}
			got := sink.records[0]
			want := eventlog.Record{Tool: "resource_get", Principal: eventlog.Unauthenticated, Result: eventlog.ResultRefused, Reason: eventlog.ReasonAuthFailed}
			if got != want {
				t.Errorf("record = %+v, want %+v", got, want)
			}
			for _, forbidden := range []string{badKey, badJWT, "first-key"} {
				if strings.Contains(got.Principal.String(), forbidden) {
					t.Errorf("principal %q carries %q", got.Principal, forbidden)
				}
			}
		})
	}
}

// AC4 counts refused tool calls, not refused requests: a body that is not a
// tools/call — initialize, tools/list, nothing at all — leaves no record,
// the same as it would authenticated.
func TestRequireBearerRecordsOnlyToolCalls(t *testing.T) {
	cases := map[string]string{
		"initialize": `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`,
		"tools/list": `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`,
		"not json":   `{"method":"tools/call",`,
		"empty":      ``,
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			sink := &recordingSink{}
			cfg := &Config{apiKeys: []string{"first-key"}, events: sink}
			rec := httptest.NewRecorder()
			cfg.RequireBearer(http.NotFoundHandler()).ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(body)))
			if rec.Code != http.StatusUnauthorized {
				t.Fatalf("status = %d, want 401", rec.Code)
			}
			if len(sink.records) != 0 {
				t.Errorf("records = %+v, want none for a request that named no tool", sink.records)
			}
		})
	}
}
