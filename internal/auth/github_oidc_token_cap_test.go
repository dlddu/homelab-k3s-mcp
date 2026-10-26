package auth

import (
	"context"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/golang-jwt/jwt/v5"

	"github.com/dlddu/homelab-k3s-mcp/internal/toolscope"
)

func TestParseGitHubRulesTokenCap(t *testing.T) {
	got, err := parseGitHubRules(`[
		{"repository": "dlddu/reconciler", "workflow": ".github/workflows/detect.yml",
		 "tools": ["github_app_installation_token", "ping"],
		 "githubToken": {"repositories": ["Plugin-Marketplace", "econ"], "permissions": {"contents": "read", "metadata": "read"}}},
		{"repository": "dlddu/reconciler", "tools": ["ping"]}
	]`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got[0].token == nil {
		t.Fatal("rule 0 must carry its githubToken cap")
	}
	if want := []string{"plugin-marketplace", "econ"}; !reflect.DeepEqual(got[0].token.Repositories, want) {
		t.Errorf("repositories = %v, want lowercased %v", got[0].token.Repositories, want)
	}
	if got[1].token != nil {
		t.Errorf("rule 1 has no githubToken, got cap %+v", got[1].token)
	}

	bad := map[string]string{
		"cap without the token tool":  `[{"repository": "dlddu/r", "tools": ["ping"], "githubToken": {"repositories": ["a"], "permissions": {"contents": "read"}}}]`,
		"no repository":               `[{"repository": "dlddu/r", "tools": ["github_app_installation_token"], "githubToken": {"permissions": {"contents": "read"}}}]`,
		"no permission":               `[{"repository": "dlddu/r", "tools": ["github_app_installation_token"], "githubToken": {"repositories": ["a"]}}]`,
		"owner in repository":         `[{"repository": "dlddu/r", "tools": ["github_app_installation_token"], "githubToken": {"repositories": ["dlddu/a"], "permissions": {"contents": "read"}}}]`,
		"unknown level":               `[{"repository": "dlddu/r", "tools": ["github_app_installation_token"], "githubToken": {"repositories": ["a"], "permissions": {"contents": "owner"}}}]`,
		"unknown cap field":           `[{"repository": "dlddu/r", "tools": ["github_app_installation_token"], "githubToken": {"repositories": ["a"], "permissions": {"contents": "read"}, "ttl": 60}}]`,
		"workflows on own repository": `[{"repository": "dlddu/Reconciler", "tools": ["github_app_installation_token"], "githubToken": {"repositories": ["reconciler", "dear-baby"], "permissions": {"workflows": "write"}}}]`,
	}
	for name, raw := range bad {
		t.Run(name, func(t *testing.T) {
			if _, err := parseGitHubRules(raw); err == nil {
				t.Error("want an error")
			}
		})
	}
}

func TestGrantsForCombinesTokenCaps(t *testing.T) {
	capped := func(repos []string, perms map[string]string) *toolscope.TokenCap {
		return &toolscope.TokenCap{Repositories: repos, Permissions: perms}
	}
	g := &githubOIDC{rules: []githubRule{
		{repository: "dlddu/reconciler", workflow: ".github/workflows/detect.yml", tools: []string{"github_app_installation_token"},
			token: capped([]string{"econ"}, map[string]string{"contents": "read"})},
		{repository: "dlddu/reconciler", workflow: ".github/workflows/detect.yml", tools: []string{"ping", "github_app_installation_token"},
			token: capped([]string{"plugin-marketplace", "econ"}, map[string]string{"contents": "write", "metadata": "read"})},
		{repository: "dlddu/reconciler", workflow: ".github/workflows/plan.yml", tools: []string{"github_app_installation_token"}},
		{repository: "dlddu/reconciler", workflow: ".github/workflows/plan.yml", tools: []string{"github_app_installation_token"},
			token: capped([]string{"econ"}, map[string]string{"contents": "read"})},
		{repository: "dlddu/reconciler", tools: []string{"opensearch_search"}},
	}}

	_, tokenCap := g.grantsFor("dlddu/reconciler", ".github/workflows/detect.yml")
	if tokenCap == nil {
		t.Fatal("two capped rules must combine into a cap")
	}
	repos := append([]string(nil), tokenCap.Repositories...)
	sort.Strings(repos)
	if want := []string{"econ", "plugin-marketplace"}; !reflect.DeepEqual(repos, want) {
		t.Errorf("repositories = %v, want the union %v", repos, want)
	}
	if want := map[string]string{"contents": "write", "metadata": "read"}; !reflect.DeepEqual(tokenCap.Permissions, want) {
		t.Errorf("permissions = %v, want the highest level per permission %v", tokenCap.Permissions, want)
	}

	if _, tokenCap := g.grantsFor("dlddu/reconciler", ".github/workflows/plan.yml"); tokenCap != nil {
		t.Errorf("an uncapped rule granting the tool must leave the union unbounded, got %+v", tokenCap)
	}
	if _, tokenCap := g.grantsFor("dlddu/reconciler", ".github/workflows/other.yml"); tokenCap != nil {
		t.Errorf("a workflow without the token tool has no cap to carry, got %+v", tokenCap)
	}
}

func TestRequireBearerCarriesTokenCap(t *testing.T) {
	key := newRSAKey(t)
	cfg := githubConfig(key)
	cfg.github.rules = append(cfg.github.rules, githubRule{
		repository: "dlddu/reconciler", workflow: ".github/workflows/detect.yml",
		tools: []string{"github_app_installation_token"},
		token: &toolscope.TokenCap{Repositories: []string{"econ"}, Permissions: map[string]string{"contents": "read"}},
	})

	var ctx context.Context
	next := http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) { ctx = r.Context() })
	req := httptest.NewRequest(http.MethodPost, "/mcp", nil)
	req.Header.Set("Authorization", "Bearer "+signGitHubToken(t, key, jwt.MapClaims{
		"workflow_ref": "dlddu/reconciler/.github/workflows/detect.yml@refs/heads/main",
	}))
	cfg.RequireBearer(next).ServeHTTP(httptest.NewRecorder(), req)
	if ctx == nil {
		t.Fatal("next did not run")
	}
	got := toolscope.TokenCapFrom(ctx)
	if got == nil || strings.Join(got.Repositories, ",") != "econ" || got.Permissions["contents"] != "read" {
		t.Errorf("token cap = %+v, want the detect.yml rule's cap", got)
	}
	if !toolscope.Allows(ctx, "github_app_installation_token") {
		t.Error("the capped rule's tool must still be allowed")
	}
	if got := toolscope.SelfRepository(ctx); got != "reconciler" {
		t.Errorf("self repository = %q, want reconciler", got)
	}
}

func TestParseGitHubRulesAllowsWorkflowsOnOtherRepositories(t *testing.T) {
	got, err := parseGitHubRules(`[{"repository": "dlddu/reconciler", "tools": ["github_app_installation_token"],
		"githubToken": {"repositories": ["dear-baby", "feature-doc"], "permissions": {"workflows": "write", "contents": "write"}}}]`)
	if err != nil {
		t.Fatalf("workflows on other repositories must be allowed: %v", err)
	}
	if got[0].token.Permissions["workflows"] != "write" {
		t.Errorf("cap = %+v, want workflows write kept", got[0].token)
	}
}
