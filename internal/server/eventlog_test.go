package server_test

import (
	"bytes"
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/dlddu/homelab-k3s-mcp/internal/auth"
)

// prd-event-log AC1 at the wire: the record a deployment writes to stdout
// names the tool, the key that called it (by position, not value) and the
// result, on one line per call. The two layers that fill it — auth and the
// dispatcher — are only joined here, so this is where "one record per call"
// is observed end to end rather than per package.
func TestToolCallIsRecordedOnStdoutWithThePrincipal(t *testing.T) {
	var buf bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, nil)))
	t.Cleanup(func() { slog.SetDefault(previous) })

	t.Setenv("MCP_AUTH_DISABLED", "")
	t.Setenv("MCP_API_KEYS", "first-key,second-key")
	t.Setenv("MCP_OAUTH_ISSUER", "")
	t.Setenv("MCP_OAUTH_AUDIENCE", "")
	t.Setenv("MCP_OAUTH_RESOURCE", "")
	cfg, err := auth.FromEnv(context.Background())
	if err != nil {
		t.Fatalf("FromEnv() = %v", err)
	}
	app := appWith(cfg)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"ping","arguments":{}}}`))
	req.Header.Set("Authorization", "Bearer second-key")
	app.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}

	var records []string
	for _, line := range strings.Split(buf.String(), "\n") {
		if strings.Contains(line, `msg="tool call"`) {
			records = append(records, line)
		}
	}
	if len(records) != 1 {
		t.Fatalf("stdout carries %d tool-call records, want 1:\n%s", len(records), buf.String())
	}
	for _, want := range []string{"tool=ping", "principal=api_key:2", "result=success", `target.kind=""`} {
		if !strings.Contains(records[0], want) {
			t.Errorf("record %q is missing %q", records[0], want)
		}
	}
	if strings.Contains(buf.String(), "second-key") {
		t.Errorf("stdout carries the API key:\n%s", buf.String())
	}
}
