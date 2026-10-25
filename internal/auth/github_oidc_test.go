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
		"iss":        ghIssuer,
		"aud":        ghAudience,
		"exp":        time.Now().Add(5 * time.Minute).Unix(),
		"sub":        "repo:dlddu/reconciler:ref:refs/heads/main",
		"repository": "dlddu/reconciler",
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
			repos: map[string][]string{
				"dlddu/reconciler": {"ping", "github_commit_status_create"},
			},
			keys: map[string]*rsa.PublicKey{ghKid: &key.PublicKey},
		},
	}
}

func TestParseGitHubRepositories(t *testing.T) {
	t.Run("valid", func(t *testing.T) {
		got, err := parseGitHubRepositories(`{"dlddu/Reconciler": ["ping"], "dlddu/other": ["ping", "opensearch_search"]}`)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(got) != 2 {
			t.Fatalf("got %d repositories, want 2: %v", len(got), got)
		}
		if tools := got["dlddu/reconciler"]; len(tools) != 1 || tools[0] != "ping" {
			t.Errorf("dlddu/reconciler = %v, want [ping] under a lowercased key", tools)
		}
	})

	bad := map[string]string{
		"empty":           ``,
		"not json":        `dlddu/reconciler=ping`,
		"not an object":   `["dlddu/reconciler"]`,
		"no repository":   `{}`,
		"no owner":        `{"reconciler": ["ping"]}`,
		"empty owner":     `{"/reconciler": ["ping"]}`,
		"empty name":      `{"dlddu/": ["ping"]}`,
		"nested path":     `{"dlddu/a/b": ["ping"]}`,
		"whitespace":      `{"dlddu/ reconciler": ["ping"]}`,
		"no tools":        `{"dlddu/reconciler": []}`,
		"empty tool":      `{"dlddu/reconciler": ["ping", " "]}`,
		"case duplicates": `{"dlddu/reconciler": ["ping"], "DLDDU/Reconciler": ["ping"]}`,
	}
	for name, raw := range bad {
		t.Run(name, func(t *testing.T) {
			if _, err := parseGitHubRepositories(raw); err == nil {
				t.Fatalf("parseGitHubRepositories(%q) succeeded, want an error", raw)
			}
		})
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
	t.Setenv("MCP_GITHUB_OIDC_REPOSITORIES", `{"dlddu/reconciler": ["ping"]}`)

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
	if got := cfg.GitHubOIDCRepositoryCount(); got != 1 {
		t.Errorf("GitHubOIDCRepositoryCount = %d, want 1", got)
	}
	if cfg.github.issuer != srv.URL {
		t.Errorf("issuer = %q, want the trailing slash trimmed to %q", cfg.github.issuer, srv.URL)
	}
	if cfg.github.keys[ghKid] == nil {
		t.Error("JWKS was not loaded at startup")
	}
}

func TestFromEnvGitHubOIDCRequiresAudienceAndRepositories(t *testing.T) {
	key := newRSAKey(t)
	srv := oidcServer(t, ghKid, &key.PublicKey)

	cases := map[string][2]string{
		"no audience":     {"", `{"dlddu/reconciler": ["ping"]}`},
		"no repositories": {ghAudience, ""},
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
			t.Setenv("MCP_GITHUB_OIDC_REPOSITORIES", c[1])
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
	t.Setenv("MCP_GITHUB_OIDC_REPOSITORIES", `{"dlddu/reconciler": ["ping"]}`)

	if _, err := FromEnv(context.Background()); err == nil {
		t.Fatal("FromEnv succeeded with one issuer behind both paths, want an error")
	}
}

func TestCheckToolScopes(t *testing.T) {
	cfg := githubConfig(newRSAKey(t))
	if err := cfg.CheckToolScopes([]string{"ping", "github_commit_status_create", "resource_get"}); err != nil {
		t.Fatalf("CheckToolScopes: %v", err)
	}
	err := cfg.CheckToolScopes([]string{"ping"})
	if err == nil {
		t.Fatal("CheckToolScopes accepted a tool the server does not have")
	}
	if !strings.Contains(err.Error(), "dlddu/reconciler: github_commit_status_create") {
		t.Errorf("error = %q, want it to name the repository and the unknown tool", err)
	}
	if err := (&Config{}).CheckToolScopes(nil); err != nil {
		t.Errorf("CheckToolScopes without GitHub OIDC = %v, want nil", err)
	}
}

func TestRequireBearerAcceptsGitHubOIDCAndScopesTools(t *testing.T) {
	key := newRSAKey(t)
	cfg := githubConfig(key)

	for name, repo := range map[string]string{"exact case": "dlddu/reconciler", "other case": "dlddu/Reconciler"} {
		t.Run(name, func(t *testing.T) {
			var got eventlog.Principal
			var seen map[string]bool
			next := http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
				got = eventlog.PrincipalFrom(r.Context())
				seen = map[string]bool{
					"restricted":                  toolscope.Restricted(r.Context()),
					"ping":                        toolscope.Allows(r.Context(), "ping"),
					"github_commit_status_create": toolscope.Allows(r.Context(), "github_commit_status_create"),
					"resource_delete":             toolscope.Allows(r.Context(), "resource_delete"),
				}
			})
			rec := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodPost, "/mcp", nil)
			req.Header.Set("Authorization", "Bearer "+signGitHubToken(t, key, jwt.MapClaims{"repository": repo}))
			cfg.RequireBearer(next).ServeHTTP(rec, req)

			if seen == nil {
				t.Fatalf("next did not run; status=%d", rec.Code)
			}
			if want := (eventlog.Principal{Method: "github_oidc", ID: repo}); got != want {
				t.Errorf("principal = %+v, want %+v", got, want)
			}
			want := map[string]bool{"restricted": true, "ping": true, "github_commit_status_create": true, "resource_delete": false}
			for k, v := range want {
				if seen[k] != v {
					t.Errorf("%s = %v, want %v", k, seen[k], v)
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
		"repository not allowed": signGitHubToken(t, key, jwt.MapClaims{"repository": "dlddu/elsewhere"}),
		"no repository claim":    signGitHubToken(t, key, jwt.MapClaims{"repository": nil}),
		"wrong audience":         signGitHubToken(t, key, jwt.MapClaims{"aud": "https://github.com/dlddu"}),
		"expired":                signGitHubToken(t, key, jwt.MapClaims{"exp": time.Now().Add(-time.Minute).Unix()}),
		"no expiry":              signGitHubToken(t, key, jwt.MapClaims{"exp": nil}),
		"signed by another key":  signGitHubToken(t, other, nil),
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
