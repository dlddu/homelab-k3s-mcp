package auth

import (
	"context"
	"net/http"
	"net/http/httptest"
	"reflect"
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
	if len(got[0].tokens) != 1 {
		t.Fatalf("rule 0 must carry its one githubToken cap, got %+v", got[0].tokens)
	}
	if want := []string{"plugin-marketplace", "econ"}; !reflect.DeepEqual(got[0].tokens[0].Repositories, want) {
		t.Errorf("repositories = %v, want lowercased %v", got[0].tokens[0].Repositories, want)
	}
	if got[1].tokens != nil {
		t.Errorf("rule 1 has no githubToken, got caps %+v", got[1].tokens)
	}

	bad := map[string]string{
		"cap without the token tool":  `[{"repository": "dlddu/r", "tools": ["ping"], "githubToken": {"repositories": ["a"], "permissions": {"contents": "read"}}}]`,
		"no repository":               `[{"repository": "dlddu/r", "tools": ["github_app_installation_token"], "githubToken": {"permissions": {"contents": "read"}}}]`,
		"no permission":               `[{"repository": "dlddu/r", "tools": ["github_app_installation_token"], "githubToken": {"repositories": ["a"]}}]`,
		"owner in repository":         `[{"repository": "dlddu/r", "tools": ["github_app_installation_token"], "githubToken": {"repositories": ["dlddu/a"], "permissions": {"contents": "read"}}}]`,
		"unknown level":               `[{"repository": "dlddu/r", "tools": ["github_app_installation_token"], "githubToken": {"repositories": ["a"], "permissions": {"contents": "owner"}}}]`,
		"unknown cap field":           `[{"repository": "dlddu/r", "tools": ["github_app_installation_token"], "githubToken": {"repositories": ["a"], "permissions": {"contents": "read"}, "ttl": 60}}]`,
		"workflows on own repository": `[{"repository": "dlddu/Reconciler", "tools": ["github_app_installation_token"], "githubToken": {"repositories": ["reconciler", "dear-baby"], "permissions": {"workflows": "write"}}}]`,
		"empty cap list":              `[{"repository": "dlddu/r", "tools": ["github_app_installation_token"], "githubToken": []}]`,
		"bad cap in list":             `[{"repository": "dlddu/r", "tools": ["github_app_installation_token"], "githubToken": [{"repositories": ["a"], "permissions": {"contents": "read"}}, {"repositories": ["b"]}]}]`,
		"unknown field in list cap":   `[{"repository": "dlddu/r", "tools": ["github_app_installation_token"], "githubToken": [{"repositories": ["a"], "permissions": {"contents": "read"}, "ttl": 60}]}]`,
		"own workflows in list cap":   `[{"repository": "dlddu/reconciler", "tools": ["github_app_installation_token"], "githubToken": [{"repositories": ["a"], "permissions": {"contents": "read"}}, {"repositories": ["reconciler"], "permissions": {"workflows": "write"}}]}]`,
		"githubToken not an object":   `[{"repository": "dlddu/r", "tools": ["github_app_installation_token"], "githubToken": "a"}]`,
	}
	for name, raw := range bad {
		t.Run(name, func(t *testing.T) {
			if _, err := parseGitHubRules(raw); err == nil {
				t.Error("want an error")
			}
		})
	}
}

func TestGrantsForCollectsTokenCapsAsAlternatives(t *testing.T) {
	capped := func(repos []string, perms map[string]string) *toolscope.TokenCap {
		return &toolscope.TokenCap{Repositories: repos, Permissions: perms}
	}
	econRead := capped([]string{"econ"}, map[string]string{"contents": "read"})
	pmWrite := capped([]string{"plugin-marketplace", "econ"}, map[string]string{"contents": "write", "metadata": "read"})
	g := &githubOIDC{rules: []githubRule{
		{repository: "dlddu/reconciler", workflow: ".github/workflows/detect.yml", tools: []string{"github_app_installation_token"},
			tokens: []*toolscope.TokenCap{econRead}},
		{repository: "dlddu/reconciler", workflow: ".github/workflows/detect.yml", tools: []string{"ping", "github_app_installation_token"},
			tokens: []*toolscope.TokenCap{pmWrite}},
		{repository: "dlddu/reconciler", workflow: ".github/workflows/plan.yml", tools: []string{"github_app_installation_token"}},
		{repository: "dlddu/reconciler", workflow: ".github/workflows/plan.yml", tools: []string{"github_app_installation_token"},
			tokens: []*toolscope.TokenCap{econRead}},
		{repository: "dlddu/reconciler", tools: []string{"opensearch_search"}},
	}}

	_, caps := g.grantsFor("dlddu/reconciler", ".github/workflows/detect.yml")
	if want := []*toolscope.TokenCap{econRead, pmWrite}; !reflect.DeepEqual(caps, want) {
		t.Errorf("caps = %+v, want each matching rule's cap kept whole, not merged: %+v", caps, want)
	}
	if _, caps := g.grantsFor("dlddu/reconciler", ".github/workflows/plan.yml"); caps != nil {
		t.Errorf("an uncapped rule granting the tool must leave the workflow unbounded, got %+v", caps)
	}
	if _, caps := g.grantsFor("dlddu/reconciler", ".github/workflows/other.yml"); caps != nil {
		t.Errorf("a workflow without the token tool has no cap to carry, got %+v", caps)
	}
}

func TestParseGitHubRulesTokenCapList(t *testing.T) {
	got, err := parseGitHubRules(`[{"repository": "dlddu/reconciler", "workflow": ".github/workflows/plan.yml",
		"tools": ["github_app_installation_token"],
		"githubToken": [
			{"repositories": ["Dear-Baby", "feature-doc"], "permissions": {"contents": "write", "workflows": "write"}},
			{"repositories": ["plugin-marketplace"], "permissions": {"contents": "read"}}
		]}]`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got[0].tokens) != 2 {
		t.Fatalf("want two caps, got %+v", got[0].tokens)
	}
	if want := []string{"dear-baby", "feature-doc"}; !reflect.DeepEqual(got[0].tokens[0].Repositories, want) {
		t.Errorf("cap 0 repositories = %v, want %v", got[0].tokens[0].Repositories, want)
	}
	if got[0].tokens[1].Permissions["contents"] != "read" {
		t.Errorf("cap 1 = %+v, want contents read kept apart from cap 0", got[0].tokens[1])
	}
}

func TestGitHubRuleSourcesReadsSuffixedVariables(t *testing.T) {
	got := GitHubRuleSources([]string{
		"MCP_GITHUB_OIDC_RULES_ZZ=[3]",
		"PATH=/usr/bin",
		"MCP_GITHUB_OIDC_RULES_CLAUDE=[2]",
		"MCP_GITHUB_OIDC_RULES=[1]",
		"MCP_GITHUB_OIDC_RULES_EMPTY=",
		"MCP_GITHUB_OIDC_RULESX=[9]",
		"MCP_GITHUB_OIDC_AUDIENCE=a",
	})
	want := []RuleSource{
		{Name: "MCP_GITHUB_OIDC_RULES", Raw: "[1]"},
		{Name: "MCP_GITHUB_OIDC_RULES_CLAUDE", Raw: "[2]"},
		{Name: "MCP_GITHUB_OIDC_RULES_ZZ", Raw: "[3]"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("sources = %+v, want %+v", got, want)
	}
}

func TestParseGitHubRuleSourcesCombinesVariables(t *testing.T) {
	base := RuleSource{Name: "MCP_GITHUB_OIDC_RULES", Raw: `[{"repository": "dlddu/reconciler", "workflow": ".github/workflows/a.yml", "tools": ["ping"]}]`}
	extra := RuleSource{Name: "MCP_GITHUB_OIDC_RULES_CLAUDE", Raw: `[{"repository": "dlddu/reconciler", "workflow": ".github/workflows/b.yml", "tools": ["ping"]}]`}

	got, err := parseGitHubRuleSources([]RuleSource{base, extra})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 2 || got[1].workflow != ".github/workflows/b.yml" {
		t.Errorf("rules = %+v, want both variables' rules in order", got)
	}

	if got, err := parseGitHubRuleSources([]RuleSource{extra}); err != nil || len(got) != 1 {
		t.Errorf("a suffixed variable alone must be enough, got %+v, %v", got, err)
	}

	dup := RuleSource{Name: "MCP_GITHUB_OIDC_RULES_CLAUDE", Raw: `[{"repository": "dlddu/Reconciler", "workflow": ".github/workflows/a.yml", "tools": ["opensearch_search"]}]`}
	if _, err := parseGitHubRuleSources([]RuleSource{base, dup}); err == nil || !strings.Contains(err.Error(), "MCP_GITHUB_OIDC_RULES[0]") {
		t.Errorf("a rule repeated across variables must stop startup naming the first, got %v", err)
	}

	bad := RuleSource{Name: "MCP_GITHUB_OIDC_RULES_CLAUDE", Raw: `[{"repository": "reconciler", "tools": ["ping"]}]`}
	if _, err := parseGitHubRuleSources([]RuleSource{base, bad}); err == nil || !strings.Contains(err.Error(), "MCP_GITHUB_OIDC_RULES_CLAUDE[0]") {
		t.Errorf("an error must name the variable it came from, got %v", err)
	}
	if _, err := parseGitHubRuleSources(nil); err == nil {
		t.Error("no rules at all must stop startup")
	}
	if _, err := parseGitHubRuleSources([]RuleSource{{Name: "MCP_GITHUB_OIDC_RULES_CLAUDE", Raw: "[]"}}); err == nil {
		t.Error("a variable listing no rule must stop startup")
	}
}

func TestRequireBearerCarriesTokenCap(t *testing.T) {
	key := newRSAKey(t)
	cfg := githubConfig(key)
	cfg.github.rules = append(cfg.github.rules, githubRule{
		repository: "dlddu/reconciler", workflow: ".github/workflows/detect.yml",
		tools:  []string{"github_app_installation_token"},
		tokens: []*toolscope.TokenCap{{Repositories: []string{"econ"}, Permissions: map[string]string{"contents": "read"}}},
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
	got := toolscope.TokenCapsFrom(ctx)
	if len(got) != 1 || strings.Join(got[0].Repositories, ",") != "econ" || got[0].Permissions["contents"] != "read" {
		t.Errorf("token caps = %+v, want the detect.yml rule's cap", got)
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
	if got[0].tokens[0].Permissions["workflows"] != "write" {
		t.Errorf("cap = %+v, want workflows write kept", got[0].tokens[0])
	}
}

func TestParseGitHubRulesExcludeRepositoriesCap(t *testing.T) {
	got, err := parseGitHubRules(`[{"repository": "dlddu/reconciler", "workflow": ".github/workflows/plan.yml",
		"tools": ["github_app_installation_token"],
		"githubToken": [
			{"excludeRepositories": ["Reconciler", "plugin-marketplace", "infrastructure"]},
			{"excludeRepositories": ["reconciler"], "permissions": {"contents": "read"}},
			{"repositories": ["plugin-marketplace"], "permissions": {"contents": "read"}}
		]}]`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	all := got[0].tokens[0]
	if want := []string{"reconciler", "plugin-marketplace", "infrastructure"}; !reflect.DeepEqual(all.ExcludeRepositories, want) {
		t.Errorf("excludeRepositories = %v, want lowercased %v", all.ExcludeRepositories, want)
	}
	if all.Repositories != nil || all.Permissions != nil {
		t.Errorf("an exclude cap without permissions leaves permissions unbounded, got %+v", all)
	}
	if got[0].tokens[1].Permissions["contents"] != "read" {
		t.Errorf("an exclude cap may still bound permissions, got %+v", got[0].tokens[1])
	}

	bad := map[string]string{
		"both repositories and exclude": `[{"repository": "dlddu/reconciler", "tools": ["github_app_installation_token"], "githubToken": {"repositories": ["a"], "excludeRepositories": ["reconciler"], "permissions": {"contents": "read"}}}]`,
		"empty exclude":                 `[{"repository": "dlddu/reconciler", "tools": ["github_app_installation_token"], "githubToken": {"excludeRepositories": []}}]`,
		"owner in exclude":              `[{"repository": "dlddu/reconciler", "tools": ["github_app_installation_token"], "githubToken": {"excludeRepositories": ["dlddu/reconciler"]}}]`,
		"empty permission map":          `[{"repository": "dlddu/reconciler", "tools": ["github_app_installation_token"], "githubToken": {"excludeRepositories": ["reconciler"], "permissions": {}}}]`,
		"own repository not excluded":   `[{"repository": "dlddu/reconciler", "tools": ["github_app_installation_token"], "githubToken": {"excludeRepositories": ["plugin-marketplace"]}}]`,
		"own not excluded, workflows":   `[{"repository": "dlddu/reconciler", "tools": ["github_app_installation_token"], "githubToken": {"excludeRepositories": ["a"], "permissions": {"workflows": "write"}}}]`,
	}
	for name, raw := range bad {
		t.Run(name, func(t *testing.T) {
			if _, err := parseGitHubRules(raw); err == nil {
				t.Error("want an error")
			}
		})
	}

	if _, err := parseGitHubRules(`[{"repository": "dlddu/reconciler", "tools": ["github_app_installation_token"],
		"githubToken": {"excludeRepositories": ["plugin-marketplace"], "permissions": {"contents": "write"}}}]`); err != nil {
		t.Errorf("an exclude cap without workflows need not exclude its own repository: %v", err)
	}
}
