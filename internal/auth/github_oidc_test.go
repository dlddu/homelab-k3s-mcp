package auth

import (
	"context"
	"crypto/rsa"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"github.com/dlddu/homelab-k3s-mcp/internal/eventlog"
	"github.com/dlddu/homelab-k3s-mcp/internal/toolscope"
)

const (
	ghKid      = "gh-kid"
	ghIssuer   = "https://token.actions.example.test"
	ghAudience = "homelab-k3s-mcp-ci"
)

func signGitHubToken(t *testing.T, key *rsa.PrivateKey, claims jwt.MapClaims) string {
	t.Helper()
	base := jwt.MapClaims{
		"iss":          ghIssuer,
		"aud":          ghAudience,
		"exp":          time.Now().Add(5 * time.Minute).Unix(),
		"sub":          "repo:dlddu/reconciler:ref:refs/heads/main",
		"repository":   "dlddu/reconciler",
		"workflow_ref": "dlddu/reconciler/.github/workflows/reconcile.yml@refs/heads/main",
	}
	for k, v := range claims {
		if v == nil {
			delete(base, k)
			continue
		}
		base[k] = v
	}
	tok := jwt.NewWithClaims(jwt.SigningMethodRS256, base)
	tok.Header["kid"] = ghKid
	signed, err := tok.SignedString(key)
	if err != nil {
		t.Fatalf("sign jwt: %v", err)
	}
	return signed
}

func githubConfig(key *rsa.PrivateKey) *Config {
	return &Config{
		keys: map[string]*rsa.PublicKey{},
		github: &githubOIDC{
			issuer:   ghIssuer,
			audience: ghAudience,
			rules: []githubRule{
				{repository: "dlddu/reconciler", workflow: ".github/workflows/reconcile.yml", tools: []string{"ping", "github_commit_status_create"}},
				{repository: "dlddu/reconciler", tools: []string{"opensearch_search", "ping"}},
				{repository: "dlddu/notes", workflow: ".github/workflows/journal.yml", tools: []string{"opensearch_document_put"}},
			},
			keys: map[string]*rsa.PublicKey{ghKid: &key.PublicKey},
		},
	}
}

func TestParseGitHubRules(t *testing.T) {
	t.Run("valid", func(t *testing.T) {
		got, err := parseGitHubRules(`[
			{"repository": "dlddu/Reconciler", "workflow": ".github/workflows/reconcile.yml", "tools": ["ping"]},
			{"repository": "dlddu/reconciler", "tools": ["opensearch_search"]}
		]`)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(got) != 2 {
			t.Fatalf("got %d rules, want 2: %v", len(got), got)
		}
		if got[0].repository != "dlddu/reconciler" || got[0].workflow != ".github/workflows/reconcile.yml" {
			t.Errorf("rule 0 = %+v, want a lowercased repository and the workflow path kept", got[0])
		}
		if got[1].workflow != "" {
			t.Errorf("rule 1 workflow = %q, want empty (any workflow)", got[1].workflow)
		}
	})

	bad := map[string]string{
		"empty":               ``,
		"not json":            `dlddu/reconciler=ping`,
		"not an array":        `{"dlddu/reconciler": ["ping"]}`,
		"no rule":             `[]`,
		"unknown field":       `[{"repository": "dlddu/reconciler", "workflows": ".github/workflows/a.yml", "tools": ["ping"]}]`,
		"no owner":            `[{"repository": "reconciler", "tools": ["ping"]}]`,
		"empty name":          `[{"repository": "dlddu/", "tools": ["ping"]}]`,
		"nested path":         `[{"repository": "dlddu/a/b", "tools": ["ping"]}]`,
		"bare workflow file":  `[{"repository": "dlddu/reconciler", "workflow": "reconcile.yml", "tools": ["ping"]}]`,
		"workflow not yaml":   `[{"repository": "dlddu/reconciler", "workflow": ".github/workflows/reconcile", "tools": ["ping"]}]`,
		"workflow with ref":   `[{"repository": "dlddu/reconciler", "workflow": ".github/workflows/a.yml@refs/heads/main", "tools": ["ping"]}]`,
		"no tools":            `[{"repository": "dlddu/reconciler", "tools": []}]`,
		"empty tool":          `[{"repository": "dlddu/reconciler", "tools": ["ping", " "]}]`,
		"repeated repository": `[{"repository": "dlddu/reconciler", "tools": ["ping"]}, {"repository": "DLDDU/Reconciler", "tools": ["ping"]}]`,
		"repeated workflow":   `[{"repository": "dlddu/r", "workflow": ".github/workflows/a.yml", "tools": ["ping"]}, {"repository": "dlddu/r", "workflow": ".github/workflows/a.yml", "tools": ["ping"]}]`,
	}
	for name, raw := range bad {
		t.Run(name, func(t *testing.T) {
			if _, err := parseGitHubRules(raw); err == nil {
				t.Fatalf("parseGitHubRules(%q) succeeded, want an error", raw)
			}
		})
	}
}

func TestWorkflowPath(t *testing.T) {
	cases := []struct{ repo, ref, want string }{
		{"dlddu/reconciler", "dlddu/reconciler/.github/workflows/reconcile.yml@refs/heads/main", ".github/workflows/reconcile.yml"},
		{"dlddu/Reconciler", "dlddu/Reconciler/.github/workflows/a.yaml@refs/pull/7/merge", ".github/workflows/a.yaml"},
		{"dlddu/reconciler", "dlddu/other/.github/workflows/a.yml@refs/heads/main", ""},
		{"dlddu/reconciler", "dlddu/reconciler/.github/workflows/a.yml", ""},
		{"dlddu/reconciler", "", ""},
	}
	for _, c := range cases {
		if got := workflowPath(c.repo, c.ref); got != c.want {
			t.Errorf("workflowPath(%q, %q) = %q, want %q", c.repo, c.ref, got, c.want)
		}
	}
}

func TestFromEnvGitHubOIDCOnly(t *testing.T) {
	key := newRSAKey(t)
	srv := oidcServer(t, ghKid, &key.PublicKey)

	t.Setenv("MCP_AUTH_DISABLED", "")
	t.Setenv("MCP_API_KEYS", "")
	t.Setenv("MCP_OAUTH_ISSUER", "")
	t.Setenv("MCP_OAUTH_AUDIENCE", "")
	t.Setenv("MCP_OAUTH_RESOURCE", "")
	t.Setenv("MCP_GITHUB_OIDC_ISSUER", srv.URL+"/")
	t.Setenv("MCP_GITHUB_OIDC_AUDIENCE", ghAudience)
	t.Setenv("MCP_GITHUB_OIDC_RULES", `[{"repository": "dlddu/reconciler", "tools": ["ping"]}]`)

	cfg, err := FromEnv(context.Background())
	if err != nil {
		t.Fatalf("FromEnv: %v", err)
	}
	if cfg == nil {
		t.Fatal("FromEnv returned nil config; GitHub OIDC alone must enable auth")
	}
	if cfg.OAuthConfigured() {
		t.Error("OAuth must stay unconfigured: GitHub OIDC is not advertised through discovery")
	}
	if got := cfg.GitHubOIDCRuleCount(); got != 1 {
		t.Errorf("GitHubOIDCRuleCount = %d, want 1", got)
	}
	if cfg.github.issuer != srv.URL {
		t.Errorf("issuer = %q, want the trailing slash trimmed to %q", cfg.github.issuer, srv.URL)
	}
	if cfg.github.keys[ghKid] == nil {
		t.Error("JWKS was not loaded at startup")
	}
}

func TestFromEnvGitHubOIDCRequiresAudienceAndRules(t *testing.T) {
	key := newRSAKey(t)
	srv := oidcServer(t, ghKid, &key.PublicKey)

	cases := map[string][2]string{
		"no audience": {"", `[{"repository": "dlddu/reconciler", "tools": ["ping"]}]`},
		"no rules":    {ghAudience, ""},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			t.Setenv("MCP_AUTH_DISABLED", "")
			t.Setenv("MCP_API_KEYS", "")
			t.Setenv("MCP_OAUTH_ISSUER", "")
			t.Setenv("MCP_OAUTH_AUDIENCE", "")
			t.Setenv("MCP_OAUTH_RESOURCE", "")
			t.Setenv("MCP_GITHUB_OIDC_ISSUER", srv.URL)
			t.Setenv("MCP_GITHUB_OIDC_AUDIENCE", c[0])
			t.Setenv("MCP_GITHUB_OIDC_RULES", c[1])
			if _, err := FromEnv(context.Background()); err == nil {
				t.Fatal("FromEnv succeeded, want an error")
			}
		})
	}
}

func TestFromEnvRefusesOneIssuerForOAuthAndGitHub(t *testing.T) {
	key := newRSAKey(t)
	srv := oidcServer(t, ghKid, &key.PublicKey)

	t.Setenv("MCP_AUTH_DISABLED", "")
	t.Setenv("MCP_API_KEYS", "")
	t.Setenv("MCP_OAUTH_ISSUER", srv.URL)
	t.Setenv("MCP_OAUTH_AUDIENCE", "homelab-k3s-mcp")
	t.Setenv("MCP_OAUTH_RESOURCE", "")
	t.Setenv("MCP_GITHUB_OIDC_ISSUER", srv.URL)
	t.Setenv("MCP_GITHUB_OIDC_AUDIENCE", ghAudience)
	t.Setenv("MCP_GITHUB_OIDC_RULES", `[{"repository": "dlddu/reconciler", "tools": ["ping"]}]`)

	if _, err := FromEnv(context.Background()); err == nil {
		t.Fatal("FromEnv succeeded with one issuer behind both paths, want an error")
	}
}

func TestCheckToolScopes(t *testing.T) {
	cfg := githubConfig(newRSAKey(t))
	if err := cfg.CheckToolScopes([]string{"ping", "github_commit_status_create", "opensearch_search", "opensearch_document_put"}); err != nil {
		t.Fatalf("CheckToolScopes: %v", err)
	}
	err := cfg.CheckToolScopes([]string{"ping", "opensearch_search", "opensearch_document_put"})
	if err == nil {
		t.Fatal("CheckToolScopes accepted a tool the server does not have")
	}
	if !strings.Contains(err.Error(), "dlddu/reconciler/.github/workflows/reconcile.yml: github_commit_status_create") {
		t.Errorf("error = %q, want it to name the rule and the unknown tool", err)
	}
	if err := (&Config{}).CheckToolScopes(nil); err != nil {
		t.Errorf("CheckToolScopes without GitHub OIDC = %v, want nil", err)
	}
}

func TestRequireBearerAcceptsGitHubOIDCAndScopesTools(t *testing.T) {
	key := newRSAKey(t)
	cfg := githubConfig(key)

	cases := []struct {
		name        string
		repo        string
		workflowRef string
		principal   string
		allowed     []string
		denied      []string
	}{
		{
			name:        "workflow rule unions with the repository rule",
			repo:        "dlddu/reconciler",
			workflowRef: "dlddu/reconciler/.github/workflows/reconcile.yml@refs/heads/main",
			principal:   "dlddu/reconciler/.github/workflows/reconcile.yml",
			allowed:     []string{"ping", "github_commit_status_create", "opensearch_search"},
			denied:      []string{"resource_delete", "opensearch_document_put"},
		},
		{
			name:        "repository in another case",
			repo:        "dlddu/Reconciler",
			workflowRef: "dlddu/Reconciler/.github/workflows/reconcile.yml@refs/heads/feature",
			principal:   "dlddu/Reconciler/.github/workflows/reconcile.yml",
			allowed:     []string{"ping", "github_commit_status_create", "opensearch_search"},
		},
		{
			name:        "another workflow gets only the repository rule",
			repo:        "dlddu/reconciler",
			workflowRef: "dlddu/reconciler/.github/workflows/other.yml@refs/heads/main",
			principal:   "dlddu/reconciler/.github/workflows/other.yml",
			allowed:     []string{"opensearch_search", "ping"},
			denied:      []string{"github_commit_status_create"},
		},
		{
			name:        "workflow-only repository",
			repo:        "dlddu/notes",
			workflowRef: "dlddu/notes/.github/workflows/journal.yml@refs/heads/main",
			principal:   "dlddu/notes/.github/workflows/journal.yml",
			allowed:     []string{"opensearch_document_put"},
			denied:      []string{"ping"},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var got eventlog.Principal
			var ctx context.Context
			next := http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
				got = eventlog.PrincipalFrom(r.Context())
				ctx = r.Context()
			})
			rec := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodPost, "/mcp", nil)
			req.Header.Set("Authorization", "Bearer "+signGitHubToken(t, key, jwt.MapClaims{"repository": c.repo, "workflow_ref": c.workflowRef}))
			cfg.RequireBearer(next).ServeHTTP(rec, req)

			if ctx == nil {
				t.Fatalf("next did not run; status=%d", rec.Code)
			}
			if want := (eventlog.Principal{Method: "github_oidc", ID: c.principal}); got != want {
				t.Errorf("principal = %+v, want %+v", got, want)
			}
			if !toolscope.Restricted(ctx) {
				t.Error("a GitHub OIDC caller must be tool-scoped")
			}
			for _, tool := range c.allowed {
				if !toolscope.Allows(ctx, tool) {
					t.Errorf("%s denied, want allowed", tool)
				}
			}
			for _, tool := range c.denied {
				if toolscope.Allows(ctx, tool) {
					t.Errorf("%s allowed, want denied", tool)
				}
			}
		})
	}
}

func TestRequireBearerRefusesGitHubOIDC(t *testing.T) {
	key := newRSAKey(t)
	other := newRSAKey(t)
	cfg := githubConfig(key)

	cases := map[string]string{
		"repository not allowed":             signGitHubToken(t, key, jwt.MapClaims{"repository": "dlddu/elsewhere", "workflow_ref": "dlddu/elsewhere/.github/workflows/reconcile.yml@refs/heads/main"}),
		"workflow not allowed":               signGitHubToken(t, key, jwt.MapClaims{"repository": "dlddu/notes", "workflow_ref": "dlddu/notes/.github/workflows/other.yml@refs/heads/main"}),
		"no workflow_ref":                    signGitHubToken(t, key, jwt.MapClaims{"repository": "dlddu/notes", "workflow_ref": nil}),
		"workflow_ref of another repository": signGitHubToken(t, key, jwt.MapClaims{"repository": "dlddu/notes", "workflow_ref": "dlddu/elsewhere/.github/workflows/journal.yml@refs/heads/main"}),
		"no repository claim":                signGitHubToken(t, key, jwt.MapClaims{"repository": nil}),
		"wrong audience":                     signGitHubToken(t, key, jwt.MapClaims{"aud": "https://github.com/dlddu"}),
		"expired":                            signGitHubToken(t, key, jwt.MapClaims{"exp": time.Now().Add(-time.Minute).Unix()}),
		"no expiry":                          signGitHubToken(t, key, jwt.MapClaims{"exp": nil}),
		"signed by another key":              signGitHubToken(t, other, nil),
	}
	for name, token := range cases {
		t.Run(name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodPost, "/mcp", nil)
			req.Header.Set("Authorization", "Bearer "+token)
			nextCalled := false
			cfg.RequireBearer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { nextCalled = true })).ServeHTTP(rec, req)
			if nextCalled {
				t.Fatal("next must not run")
			}
			if rec.Code != http.StatusUnauthorized {
				t.Fatalf("status = %d, want 401", rec.Code)
			}
			if !strings.Contains(rec.Header().Get("WWW-Authenticate"), `error="invalid_token"`) {
				t.Errorf("challenge = %q, want error=invalid_token", rec.Header().Get("WWW-Authenticate"))
			}
			if strings.Contains(rec.Body.String(), token) {
				t.Error("response body echoes the token")
			}
		})
	}
}

func TestRequireBearerKeepsOAuthUnscopedNextToGitHubOIDC(t *testing.T) {
	const (
		kid      = "oauth-kid"
		issuer   = "https://issuer.example.test"
		audience = "homelab-k3s-mcp"
	)
	ghKey := newRSAKey(t)
	oauthKey := newRSAKey(t)
	cfg := githubConfig(ghKey)
	cfg.Issuer = issuer
	cfg.Audience = audience
	cfg.Resource = "https://mcp.example.test/mcp"
	cfg.keys = map[string]*rsa.PublicKey{kid: &oauthKey.PublicKey}

	var got eventlog.Principal
	restricted := true
	next := http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		got = eventlog.PrincipalFrom(r.Context())
		restricted = toolscope.Restricted(r.Context())
	})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/mcp", nil)
	req.Header.Set("Authorization", "Bearer "+signJWT(t, oauthKey, kid, issuer, audience))
	cfg.RequireBearer(next).ServeHTTP(rec, req)

	if got.Method != "jwt" {
		t.Fatalf("principal = %+v, want the OAuth path (status=%d)", got, rec.Code)
	}
	if restricted {
		t.Error("an OAuth caller must not be tool-scoped")
	}

	t.Run("token claiming the OAuth issuer, signed with the GitHub key", func(t *testing.T) {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/mcp", nil)
		req.Header.Set("Authorization", "Bearer "+signJWT(t, ghKey, kid, issuer, audience))
		cfg.RequireBearer(http.NotFoundHandler()).ServeHTTP(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("status = %d, want 401: the OAuth verifier must not accept a key it does not hold", rec.Code)
		}
	})
}
