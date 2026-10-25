package server_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/dlddu/homelab-k3s-mcp/internal/approvalgate"
	"github.com/dlddu/homelab-k3s-mcp/internal/auth"
	"github.com/dlddu/homelab-k3s-mcp/internal/mcp"
	"github.com/dlddu/homelab-k3s-mcp/internal/metrics"
	"github.com/dlddu/homelab-k3s-mcp/internal/server"
)

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
	cfg := &auth.Config{}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/.well-known/oauth-protected-resource", nil)
	appWith(cfg).ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404 when OAuth is not configured", rec.Code)
	}
}

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

	rec = httptest.NewRecorder()
	app.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"ping","arguments":{}}}`)))
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("POST /mcp without a credential = %d, want 401", rec.Code)
	}
}

type recordingGate struct{ calls []approvalgate.Call }

func (g *recordingGate) Authorize(_ context.Context, call approvalgate.Call) (*approvalgate.Decision, error) {
	g.calls = append(g.calls, call)
	return nil, errors.New("refused by the test")
}

func TestSessionHeaderPassesAuthIntoTheApprovalRequest(t *testing.T) {
	t.Setenv("MCP_AUTH_DISABLED", "")
	t.Setenv("MCP_API_KEYS", "first-key")
	t.Setenv("MCP_OAUTH_ISSUER", "")
	t.Setenv("MCP_OAUTH_AUDIENCE", "")
	t.Setenv("MCP_OAUTH_RESOURCE", "")
	cfg, err := auth.FromEnv(context.Background())
	if err != nil {
		t.Fatalf("FromEnv() = %v", err)
	}
	gate := &recordingGate{}
	app := server.App(cfg, unavailableK8s(), unavailableGitHub(), unavailableAWS(), unavailableGrafana(),
		unavailableOpenSearch(), unavailableSessionPlatform(), mcp.WithGate(gate, nil))

	body := `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"resource_patch","arguments":` +
		`{"apiVersion":"v1","kind":"ConfigMap","namespace":"ops","name":"cm","patchType":"merge","patch":{},"reason":"설정을 고친다."}}}`
	req := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer first-key")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(mcp.SessionIDHeader, "sess-7")
	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if len(gate.calls) != 1 {
		t.Fatalf("gate calls = %d, want 1", len(gate.calls))
	}
	if c := gate.calls[0]; c.SessionID != "sess-7" || c.Reason != "설정을 고친다." {
		t.Errorf("Call = {SessionID:%q Reason:%q}, want the header and the argument", c.SessionID, c.Reason)
	}
}
