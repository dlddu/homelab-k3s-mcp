package auth

import (
	"context"
	"crypto/rsa"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"slices"
	"sort"
	"strings"
	"sync"

	"github.com/golang-jwt/jwt/v5"

	"github.com/dlddu/homelab-k3s-mcp/internal/toolscope"
)

const (
	defaultGitHubOIDCIssuer = "https://token.actions.githubusercontent.com"

	githubOIDCMethod = "github_oidc"

	installationTokenTool = "github_app_installation_token"
)

var permissionRank = map[string]int{"read": 1, "write": 2, "admin": 3}

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
	token      *toolscope.TokenCap
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
		Repository  string   `json:"repository"`
		Workflow    string   `json:"workflow"`
		Tools       []string `json:"tools"`
		GitHubToken *struct {
			Repositories []string          `json:"repositories"`
			Permissions  map[string]string `json:"permissions"`
		} `json:"githubToken"`
	}
	dec := json.NewDecoder(strings.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&decoded); err != nil {
		return nil, fmt.Errorf(`MCP_GITHUB_OIDC_RULES must be a JSON array of {"repository", "workflow", "tools", "githubToken"}: %w`, err)
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
		if d.GitHubToken != nil {
			tokenCap, err := parseTokenCap(where, d.Tools, d.GitHubToken.Repositories, d.GitHubToken.Permissions)
			if err != nil {
				return nil, err
			}
			own := strings.ToLower(name)
			if _, workflows := tokenCap.Permissions["workflows"]; workflows && slices.Contains(tokenCap.Repositories, own) {
				return nil, fmt.Errorf("%s: githubToken grants workflows on %s, the rule's own repository — a workflow that can rewrite its own workflow files can read every secret there", where, own)
			}
			rule.token = tokenCap
		}
		if seen[rule.label()] {
			return nil, fmt.Errorf("%s repeats %s", where, rule.label())
		}
		seen[rule.label()] = true
		rules = append(rules, rule)
	}
	return rules, nil
}

func parseTokenCap(where string, tools, repositories []string, permissions map[string]string) (*toolscope.TokenCap, error) {
	grants := false
	for _, tool := range tools {
		if tool == installationTokenTool {
			grants = true
		}
	}
	if !grants {
		return nil, fmt.Errorf("%s: githubToken is set but tools does not grant %s", where, installationTokenTool)
	}
	if len(repositories) == 0 {
		return nil, fmt.Errorf("%s: githubToken lists no repository", where)
	}
	if len(permissions) == 0 {
		return nil, fmt.Errorf("%s: githubToken lists no permission", where)
	}
	tokenCap := &toolscope.TokenCap{Permissions: map[string]string{}}
	for _, repo := range repositories {
		if repo == "" || strings.ContainsAny(repo, "/ \t") {
			return nil, fmt.Errorf("%s: githubToken repository %q is not a bare repository name", where, repo)
		}
		tokenCap.Repositories = append(tokenCap.Repositories, strings.ToLower(repo))
	}
	for name, level := range permissions {
		if strings.TrimSpace(name) == "" {
			return nil, fmt.Errorf("%s: githubToken names an empty permission", where)
		}
		if permissionRank[level] == 0 {
			return nil, fmt.Errorf("%s: githubToken permission %s level %q is not read, write or admin", where, name, level)
		}
		tokenCap.Permissions[name] = level
	}
	return tokenCap, nil
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

func (g *githubOIDC) grantsFor(repository, workflow string) ([]string, *toolscope.TokenCap) {
	repo := strings.ToLower(repository)
	var tools []string
	have := map[string]bool{}
	var tokenCap *toolscope.TokenCap
	unbounded := false
	for _, r := range g.rules {
		if r.repository != repo || (r.workflow != "" && r.workflow != workflow) {
			continue
		}
		for _, t := range r.tools {
			if !have[t] {
				have[t] = true
				tools = append(tools, t)
			}
			if t != installationTokenTool {
				continue
			}
			if r.token == nil {
				unbounded = true
				continue
			}
			tokenCap = mergeTokenCap(tokenCap, r.token)
		}
	}
	if unbounded {
		tokenCap = nil
	}
	return tools, tokenCap
}

func mergeTokenCap(into, from *toolscope.TokenCap) *toolscope.TokenCap {
	if into == nil {
		into = &toolscope.TokenCap{Permissions: map[string]string{}}
	}
	seen := map[string]bool{}
	for _, repo := range into.Repositories {
		seen[repo] = true
	}
	for _, repo := range from.Repositories {
		if !seen[repo] {
			seen[repo] = true
			into.Repositories = append(into.Repositories, repo)
		}
	}
	for name, level := range from.Permissions {
		if permissionRank[level] > permissionRank[into.Permissions[name]] {
			into.Permissions[name] = level
		}
	}
	return into
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

type githubGrant struct {
	caller   string
	tools    []string
	tokenCap *toolscope.TokenCap
	self     string
}

func (g *githubOIDC) verify(ctx context.Context, raw string) (githubGrant, error) {
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
		return githubGrant{}, err
	}
	if claims.Repository == "" {
		return githubGrant{}, fmt.Errorf("token has no repository claim")
	}
	workflow := workflowPath(claims.Repository, claims.WorkflowRef)
	tools, tokenCap := g.grantsFor(claims.Repository, workflow)
	if len(tools) == 0 {
		slog.Warn("github oidc token matches no MCP_GITHUB_OIDC_RULES entry", "repository", claims.Repository, "workflow", workflow)
		return githubGrant{}, fmt.Errorf("%s %s is not allowed", claims.Repository, workflow)
	}
	caller := claims.Repository
	if workflow != "" {
		caller += "/" + workflow
	}
	_, self, _ := strings.Cut(claims.Repository, "/")
	return githubGrant{caller: caller, tools: tools, tokenCap: tokenCap, self: strings.ToLower(self)}, nil
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
