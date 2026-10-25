package auth

import (
	"context"
	"crypto/rsa"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"sort"
	"strings"
	"sync"

	"github.com/golang-jwt/jwt/v5"
)

const (
	defaultGitHubOIDCIssuer = "https://token.actions.githubusercontent.com"

	githubOIDCMethod = "github_oidc"
)

type githubOIDC struct {
	issuer   string
	audience string
	jwksURI  string
	http     *http.Client
	rules    []githubRule

	mu   sync.RWMutex
	keys map[string]*rsa.PublicKey
}

type githubRule struct {
	// repository is the lowercased "owner/name"; GitHub treats repository
	// names case-insensitively, and the claim carries whatever case the
	// repository was created with.
	repository string
	workflow   string
	tools      []string
}

type githubClaims struct {
	Repository  string `json:"repository"`
	WorkflowRef string `json:"workflow_ref"`
	jwt.RegisteredClaims
}

func newGitHubOIDC(ctx context.Context, issuer, audience, rawRules string) (*githubOIDC, error) {
	if issuer == "" {
		issuer = defaultGitHubOIDCIssuer
	}
	issuer = strings.TrimRight(issuer, "/")
	if audience == "" {
		return nil, fmt.Errorf("MCP_GITHUB_OIDC_AUDIENCE is required when GitHub OIDC is enabled")
	}
	rules, err := parseGitHubRules(rawRules)
	if err != nil {
		return nil, err
	}

	httpClient := &http.Client{Timeout: httpClientTimeout}
	metadataURL := issuer + "/.well-known/openid-configuration"
	var metadata providerMetadata
	if err := getJSON(ctx, httpClient, metadataURL, &metadata); err != nil {
		return nil, fmt.Errorf("fetch %s: %w", metadataURL, err)
	}
	if metadata.JWKSURI == "" {
		return nil, fmt.Errorf("%s has no jwks_uri", metadataURL)
	}

	g := &githubOIDC{
		issuer:   issuer,
		audience: audience,
		jwksURI:  metadata.JWKSURI,
		http:     httpClient,
		rules:    rules,
	}
	if err := g.refreshKeys(ctx); err != nil {
		return nil, err
	}
	return g, nil
}

func parseGitHubRules(raw string) ([]githubRule, error) {
	if strings.TrimSpace(raw) == "" {
		return nil, fmt.Errorf("MCP_GITHUB_OIDC_RULES is required when GitHub OIDC is enabled")
	}
	var decoded []struct {
		Repository string   `json:"repository"`
		Workflow   string   `json:"workflow"`
		Tools      []string `json:"tools"`
	}
	dec := json.NewDecoder(strings.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&decoded); err != nil {
		return nil, fmt.Errorf(`MCP_GITHUB_OIDC_RULES must be a JSON array of {"repository", "workflow", "tools"}: %w`, err)
	}
	if len(decoded) == 0 {
		return nil, fmt.Errorf("MCP_GITHUB_OIDC_RULES lists no rule")
	}

	rules := make([]githubRule, 0, len(decoded))
	seen := map[string]bool{}
	for i, d := range decoded {
		where := fmt.Sprintf("MCP_GITHUB_OIDC_RULES[%d]", i)
		owner, name, ok := strings.Cut(d.Repository, "/")
		if !ok || owner == "" || name == "" || strings.Contains(name, "/") || strings.ContainsAny(d.Repository, " \t") {
			return nil, fmt.Errorf("%s: repository %q is not \"owner/name\"", where, d.Repository)
		}
		if d.Workflow != "" {
			if !strings.HasPrefix(d.Workflow, ".github/workflows/") ||
				!(strings.HasSuffix(d.Workflow, ".yml") || strings.HasSuffix(d.Workflow, ".yaml")) ||
				strings.ContainsAny(d.Workflow, "@ \t") {
				return nil, fmt.Errorf("%s: workflow %q is not a path like .github/workflows/<file>.yml", where, d.Workflow)
			}
		}
		if len(d.Tools) == 0 {
			return nil, fmt.Errorf("%s lists no tool", where)
		}
		for _, tool := range d.Tools {
			if strings.TrimSpace(tool) == "" {
				return nil, fmt.Errorf("%s lists an empty tool name", where)
			}
		}
		rule := githubRule{repository: strings.ToLower(d.Repository), workflow: d.Workflow, tools: d.Tools}
		if seen[rule.label()] {
			return nil, fmt.Errorf("%s repeats %s", where, rule.label())
		}
		seen[rule.label()] = true
		rules = append(rules, rule)
	}
	return rules, nil
}

func (r githubRule) label() string {
	if r.workflow == "" {
		return r.repository
	}
	return r.repository + "/" + r.workflow
}

func workflowPath(repository, workflowRef string) string {
	at := strings.LastIndex(workflowRef, "@")
	if at < 0 {
		return ""
	}
	prefix := repository + "/"
	p := workflowRef[:at]
	if len(p) <= len(prefix) || !strings.EqualFold(p[:len(prefix)], prefix) {
		return ""
	}
	return p[len(prefix):]
}

func (g *githubOIDC) toolsFor(repository, workflow string) []string {
	repo := strings.ToLower(repository)
	var tools []string
	have := map[string]bool{}
	for _, r := range g.rules {
		if r.repository != repo || (r.workflow != "" && r.workflow != workflow) {
			continue
		}
		for _, t := range r.tools {
			if !have[t] {
				have[t] = true
				tools = append(tools, t)
			}
		}
	}
	return tools
}

func (g *githubOIDC) refreshKeys(ctx context.Context) error {
	keys, err := fetchRSAKeys(ctx, g.http, g.jwksURI)
	if err != nil {
		return err
	}
	g.mu.Lock()
	g.keys = keys
	g.mu.Unlock()
	return nil
}

func (g *githubOIDC) keyForKid(ctx context.Context, kid string) *rsa.PublicKey {
	g.mu.RLock()
	key := g.keys[kid]
	g.mu.RUnlock()
	if key != nil {
		return key
	}
	if err := g.refreshKeys(ctx); err != nil {
		return nil
	}
	g.mu.RLock()
	key = g.keys[kid]
	g.mu.RUnlock()
	return key
}

func (g *githubOIDC) issues(raw string) bool {
	var claims jwt.RegisteredClaims
	if _, _, err := jwt.NewParser().ParseUnverified(raw, &claims); err != nil {
		return false
	}
	return strings.TrimRight(claims.Issuer, "/") == g.issuer
}

func (g *githubOIDC) verify(ctx context.Context, raw string) (string, []string, error) {
	keyFunc := func(token *jwt.Token) (any, error) {
		kid, _ := token.Header["kid"].(string)
		if kid == "" {
			return nil, fmt.Errorf("missing kid")
		}
		key := g.keyForKid(ctx, kid)
		if key == nil {
			return nil, fmt.Errorf("unknown kid")
		}
		return key, nil
	}

	var claims githubClaims
	_, err := jwt.ParseWithClaims(raw, &claims, keyFunc,
		jwt.WithValidMethods([]string{"RS256"}),
		jwt.WithAudience(g.audience),
		jwt.WithIssuer(g.issuer),
		jwt.WithExpirationRequired(),
	)
	if err != nil {
		return "", nil, err
	}
	if claims.Repository == "" {
		return "", nil, fmt.Errorf("token has no repository claim")
	}
	workflow := workflowPath(claims.Repository, claims.WorkflowRef)
	tools := g.toolsFor(claims.Repository, workflow)
	if len(tools) == 0 {
		slog.Warn("github oidc token matches no MCP_GITHUB_OIDC_RULES entry", "repository", claims.Repository, "workflow", workflow)
		return "", nil, fmt.Errorf("%s %s is not allowed", claims.Repository, workflow)
	}
	caller := claims.Repository
	if workflow != "" {
		caller += "/" + workflow
	}
	return caller, tools, nil
}

// GitHubOIDCRuleCount returns how many MCP_GITHUB_OIDC_RULES entries admit
// GitHub Actions OIDC tokens.
func (c *Config) GitHubOIDCRuleCount() int {
	if c.github == nil {
		return 0
	}
	return len(c.github.rules)
}

// CheckToolScopes reports the tool names MCP_GITHUB_OIDC_RULES grants that are
// not among known.
func (c *Config) CheckToolScopes(known []string) error {
	if c.github == nil {
		return nil
	}
	have := make(map[string]bool, len(known))
	for _, k := range known {
		have[k] = true
	}
	var problems []string
	for _, r := range c.github.rules {
		for _, tool := range r.tools {
			if !have[tool] {
				problems = append(problems, fmt.Sprintf("%s: %s", r.label(), tool))
			}
		}
	}
	if len(problems) == 0 {
		return nil
	}
	sort.Strings(problems)
	return fmt.Errorf("MCP_GITHUB_OIDC_RULES grants tools this server does not have: %s", strings.Join(problems, ", "))
}
