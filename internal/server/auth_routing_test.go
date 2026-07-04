package server_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/dlddu/homelab-k3s-mcp/internal/auth"
	"github.com/dlddu/homelab-k3s-mcp/internal/server"
)

// These tests cover platform AC8 at the routing layer: the OAuth
// protected-resource discovery document is registered only when OAuth is
// configured. In API-key-only mode the route is absent.

func appWith(authCfg *auth.Config) http.Handler {
	return server.App(authCfg, unavailableK8s(), unavailableGitHub(), unavailableAWS(), unavailableGrafana(), unavailableOpenSearch())
}

func TestDiscoveryServedWhenOAuthConfigured(t *testing.T) {
	cfg := &auth.Config{
		Issuer:   "https://issuer.example.test",
		Audience: "homelab-k3s-mcp",
		Resource: "https://mcp.example.test/mcp",
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/.well-known/oauth-protected-resource", nil)
	appWith(cfg).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 when OAuth is configured", rec.Code)
	}
}

func TestDiscoveryAbsentWhenOAuthNotConfigured(t *testing.T) {
	// No Issuer => OAuthConfigured() == false (API-key-only mode).
	cfg := &auth.Config{}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/.well-known/oauth-protected-resource", nil)
	appWith(cfg).ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404 when OAuth is not configured", rec.Code)
	}
}
