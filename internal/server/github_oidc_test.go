package server_test

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"log/slog"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"github.com/dlddu/homelab-k3s-mcp/internal/auth"
)

const (
	ghTestKid      = "gh-kid"
	ghTestAudience = "homelab-k3s-mcp-ci"
)

func githubOIDCApp(t *testing.T) (http.Handler, func(repo string) string) {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate rsa key: %v", err)
	}
	mux := http.NewServeMux()
	var base string
	mux.HandleFunc("/.well-known/openid-configuration", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]string{"jwks_uri": base + "/jwks"})
	})
	mux.HandleFunc("/jwks", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"keys": []map[string]string{{
			"kid": ghTestKid,
			"kty": "RSA",
			"n":   base64.RawURLEncoding.EncodeToString(key.N.Bytes()),
			"e":   base64.RawURLEncoding.EncodeToString(big.NewInt(int64(key.E)).Bytes()),
		}}})
	})
	issuer := httptest.NewServer(mux)
	base = issuer.URL
	t.Cleanup(issuer.Close)

	t.Setenv("MCP_AUTH_DISABLED", "")
	t.Setenv("MCP_API_KEYS", "")
	t.Setenv("MCP_OAUTH_ISSUER", "")
	t.Setenv("MCP_OAUTH_AUDIENCE", "")
	t.Setenv("MCP_OAUTH_RESOURCE", "")
	t.Setenv("MCP_GITHUB_OIDC_ISSUER", issuer.URL)
	t.Setenv("MCP_GITHUB_OIDC_AUDIENCE", ghTestAudience)
	t.Setenv("MCP_GITHUB_OIDC_REPOSITORIES", `{"dlddu/reconciler": ["github_commit_status_create", "ping"]}`)
	cfg, err := auth.FromEnv(context.Background())
	if err != nil {
		t.Fatalf("FromEnv() = %v", err)
	}

	sign := func(repo string) string {
		tok := jwt.NewWithClaims(jwt.SigningMethodRS256, jwt.MapClaims{
			"iss":        issuer.URL,
			"aud":        ghTestAudience,
			"exp":        time.Now().Add(5 * time.Minute).Unix(),
			"repository": repo,
		})
		tok.Header["kid"] = ghTestKid
		signed, err := tok.SignedString(key)
		if err != nil {
			t.Fatalf("sign jwt: %v", err)
		}
		return signed
	}
	return appWith(cfg), sign
}

func rpcAs(t *testing.T, app http.Handler, token string, payload any) (*httptest.ResponseRecorder, map[string]any) {
	t.Helper()
	req := jsonRequest("/mcp", payload)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := serve(app, req)
	if rec.Code != http.StatusOK {
		return rec, nil
	}
	return rec, bodyJSON(t, rec)
}

func TestGitHubOIDCCallerSeesOnlyItsTools(t *testing.T) {
	app, sign := githubOIDCApp(t)

	_, body := rpcAs(t, app, sign("dlddu/reconciler"), map[string]any{"jsonrpc": "2.0", "id": 1, "method": "tools/list"})
	if body == nil {
		t.Fatal("tools/list was refused")
	}
	tools, ok := at(t, body, "result", "tools").([]any)
	if !ok {
		t.Fatalf("tools is not an array: %v", body)
	}
	var names []string
	for _, x := range tools {
		names = append(names, x.(map[string]any)["name"].(string))
	}
	wantStrSlice(t, names, "ping", "github_commit_status_create")
	if findTool(t, tools, "ping")["inputSchema"] == nil {
		t.Error("a scoped entry lost its inputSchema")
	}
}

func TestGitHubOIDCCallerIsRefusedOutOfScopeAsIfTheToolDidNotExist(t *testing.T) {
	var buf bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, nil)))
	t.Cleanup(func() { slog.SetDefault(previous) })

	app, sign := githubOIDCApp(t)
	token := sign("dlddu/reconciler")

	call := func(name string) map[string]any {
		t.Helper()
		_, body := rpcAs(t, app, token, map[string]any{
			"jsonrpc": "2.0", "id": 1, "method": "tools/call",
			"params": map[string]any{"name": name, "arguments": map[string]any{}},
		})
		if body == nil {
			t.Fatalf("tools/call %s was refused at the auth layer", name)
		}
		return body
	}

	if got := at(t, call("ping"), "result", "content", 0, "text"); got != "pong" {
		t.Fatalf("ping = %v, want pong", got)
	}

	outOfScope := call("resource_delete")
	missing := call("no_such_tool")
	if at(t, outOfScope, "error", "code") != at(t, missing, "error", "code") {
		t.Errorf("codes differ: out of scope %v, missing %v", outOfScope["error"], missing["error"])
	}
	if msg := at(t, outOfScope, "error", "message"); msg != "unknown tool: resource_delete" {
		t.Errorf("message = %v, want the unknown-tool refusal", msg)
	}

	var record string
	for _, line := range strings.Split(buf.String(), "\n") {
		if strings.Contains(line, `msg="tool call"`) && strings.Contains(line, "tool=resource_delete") {
			record = line
		}
	}
	for _, want := range []string{"principal=github_oidc:dlddu/reconciler", "result=refused", "reason=invalid_input"} {
		if !strings.Contains(record, want) {
			t.Errorf("record %q is missing %q", record, want)
		}
	}
	if strings.Contains(buf.String(), token) {
		t.Error("stdout carries the token")
	}
}

func TestGitHubOIDCTokenFromAnotherRepositoryIsUnauthorized(t *testing.T) {
	app, sign := githubOIDCApp(t)
	rec, _ := rpcAs(t, app, sign("dlddu/elsewhere"), map[string]any{"jsonrpc": "2.0", "id": 1, "method": "tools/list"})
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
}
