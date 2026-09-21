package server_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/dlddu/homelab-k3s-mcp/internal/auth"
	"github.com/dlddu/homelab-k3s-mcp/internal/metrics"
	"github.com/dlddu/homelab-k3s-mcp/internal/server"
)

// These tests cover platform AC8 at the routing layer: the OAuth
// protected-resource discovery document is registered only when OAuth is
// configured. In API-key-only mode the route is absent.

func appWith(authCfg *auth.Config) http.Handler {
	return server.App(authCfg, unavailableK8s(), unavailableGitHub(), unavailableAWS(), unavailableGrafana(), unavailableOpenSearch(), unavailableSessionPlatform())
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

// prd-metrics AC5: the metrics exposition is not a route of the handler the
// ingress fronts — a GET /metrics there is a 404, not a scrape — and the
// metrics handler carries no /mcp: there is no path on the metrics port that
// reaches a tool, authenticated or not.
func TestMetricsAreNotServedOnTheMCPListenerAndMCPIsNotOnTheMetricsOne(t *testing.T) {
	t.Setenv("MCP_AUTH_DISABLED", "")
	t.Setenv("MCP_API_KEYS", "first-key")
	t.Setenv("MCP_OAUTH_ISSUER", "")
	t.Setenv("MCP_OAUTH_AUDIENCE", "")
	t.Setenv("MCP_OAUTH_RESOURCE", "")
	cfg, err := auth.FromEnv(context.Background())
	if err != nil {
		t.Fatalf("FromEnv() = %v", err)
	}
	app := appWith(cfg)

	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	if rec.Code != http.StatusNotFound {
		t.Errorf("GET /metrics on the mcp listener = %d, want 404", rec.Code)
	}

	surface, err := metrics.New([]string{"ping"})
	if err != nil {
		t.Fatal(err)
	}
	metricsApp := server.MetricsApp(surface.Handler())

	rec = httptest.NewRecorder()
	metricsApp.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"ping","arguments":{}}}`)))
	if rec.Code != http.StatusNotFound {
		t.Errorf("POST /mcp on the metrics listener = %d, want 404", rec.Code)
	}

	rec = httptest.NewRecorder()
	metricsApp.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `mcp_tool_calls_total{result="success",tool="ping"} 0`) {
		t.Errorf("GET /metrics on the metrics listener = %d %q, want the exposition with ping at 0", rec.Code, rec.Body.String())
	}

	// /mcp itself still asks for a credential — the metrics port changed
	// nothing about the boundary it sits beside.
	rec = httptest.NewRecorder()
	app.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"ping","arguments":{}}}`)))
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("POST /mcp without a credential = %d, want 401", rec.Code)
	}
}
