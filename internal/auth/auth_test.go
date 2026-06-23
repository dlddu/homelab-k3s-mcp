package auth

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// These tests cover platform-auth-safety acceptance criteria that previously
// had no automated coverage:
//
//	AC1 인증 게이트         - the bearer gate decides what to reject and how.
//	AC2 인증 디스커버리      - the protected-resource metadata and the
//	                          WWW-Authenticate challenge advertise discovery.
//
// They exercise only the local request path (header parsing, challenge
// emission, metadata rendering, env gating) and never reach OIDC discovery or
// JWKS, so no network or live provider is required.

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
	cfg := &Config{Resource: "https://mcp.example.test/mcp"}

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
	cfg := &Config{Resource: "https://mcp.example.test/mcp"}

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

func TestFromEnvRequiresIssuer(t *testing.T) {
	t.Setenv("MCP_AUTH_DISABLED", "0")
	t.Setenv("MCP_OAUTH_ISSUER", "")

	if _, err := FromEnv(context.Background()); err == nil {
		t.Fatal("expected error when MCP_OAUTH_ISSUER is unset")
	}
}

func TestFromEnvRequiresAudience(t *testing.T) {
	t.Setenv("MCP_AUTH_DISABLED", "0")
	t.Setenv("MCP_OAUTH_ISSUER", "https://issuer.example.test")
	t.Setenv("MCP_OAUTH_AUDIENCE", "")

	if _, err := FromEnv(context.Background()); err == nil {
		t.Fatal("expected error when MCP_OAUTH_AUDIENCE is unset")
	}
}
