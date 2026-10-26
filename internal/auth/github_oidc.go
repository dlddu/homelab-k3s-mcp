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

const githubRulesEnv = "MCP_GITHUB_OIDC_RULES"

// GitHubRuleSources returns the rule variables set in environ.
func GitHubRuleSources(environ []string) []RuleSource {
	var base []RuleSource
	var extras []RuleSource
	for _, kv := range environ {
		name, value, ok := strings.Cut(kv, "=")
		if !ok || value == "" {
			continue
		}
		switch {
		case name == githubRulesEnv:
			base = append(base, RuleSource{Name: name, Raw: value})
		case strings.HasPrefix(name, githubRulesEnv+"_"):
			extras = append(extras, RuleSource{Name: name, Raw: value})
		}
	}
	sort.Slice(extras, func(i, j int) bool { return extras[i].Name < extras[j].Name })
	return append(base, extras...)
}

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
	tokens     []*toolscope.TokenCap
}

// RuleSource is one environment variable that holds rules.
type RuleSource struct {
	Name string
	Raw  string
}

type githubClaims struct {
	Repository  string `json:"repository"`
	WorkflowRef string `json:"workflow_ref"`
	jwt.RegisteredClaims
}

func newGitHubOIDC(ctx context.Context, issuer, audience string, sources []RuleSource) (*githubOIDC, error) {
	if issuer == "" {
		issuer = defaultGitHubOIDCIssuer
	}
	issuer = strings.TrimRight(issuer, "/")
	if audience == "" {
		return nil, fmt.Errorf("MCP_GITHUB_OIDC_AUDIENCE is required when GitHub OIDC is enabled")
	}
	rules, err := parseGitHubRuleSources(sources)
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
	return parseGitHubRuleSources([]RuleSource{{Name: githubRulesEnv, Raw: raw}})
}

func parseGitHubRuleSources(sources []RuleSource) ([]githubRule, error) {
	var rules []githubRule
	seen := map[string]string{}
	for _, src := range sources {
		parsed, err := parseRuleSource(src)
		if err != nil {
			return nil, err
		}
		for i, rule := range parsed {
			where := fmt.Sprintf("%s[%d]", src.Name, i)
			if first, dup := seen[rule.label()]; dup {
				return nil, fmt.Errorf("%s repeats %s (already in %s)", where, rule.label(), first)
			}
			seen[rule.label()] = where
		}
		rules = append(rules, parsed...)
	}
	if len(rules) == 0 {
		return nil, fmt.Errorf("%s is required when GitHub OIDC is enabled", githubRulesEnv)
	}
	return rules, nil
}

type tokenCapJSON struct {
	Repositories        []string          `json:"repositories"`
	ExcludeRepositories []string          `json:"excludeRepositories"`
	Permissions         map[string]string `json:"permissions"`
}

func parseRuleSource(src RuleSource) ([]githubRule, error) {
	if strings.TrimSpace(src.Raw) == "" {
		return nil, fmt.Errorf("%s is empty", src.Name)
	}
	var decoded []struct {
		Repository  string          `json:"repository"`
		Workflow    string          `json:"workflow"`
		Tools       []string        `json:"tools"`
		GitHubToken json.RawMessage `json:"githubToken"`
	}
	dec := json.NewDecoder(strings.NewReader(src.Raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&decoded); err != nil {
		return nil, fmt.Errorf(`%s must be a JSON array of {"repository", "workflow", "tools", "githubToken"}: %w`, src.Name, err)
	}
	if len(decoded) == 0 {
		return nil, fmt.Errorf("%s lists no rule", src.Name)
	}

	rules := make([]githubRule, 0, len(decoded))
	for i, d := range decoded {
		where := fmt.Sprintf("%s[%d]", src.Name, i)
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
		caps, err := decodeTokenCaps(where, d.GitHubToken)
		if err != nil {
			return nil, err
		}
		own := strings.ToLower(name)
		for j, c := range caps {
			capWhere := where
			if len(caps) > 1 || isJSONArray(d.GitHubToken) {
				capWhere = fmt.Sprintf("%s.githubToken[%d]", where, j)
			}
			tokenCap, err := parseTokenCap(capWhere, d.Tools, c)
			if err != nil {
				return nil, err
			}
			if capGrantsWorkflowsOn(tokenCap, own) {
				return nil, fmt.Errorf("%s: githubToken grants workflows on %s, the rule's own repository — a workflow that can rewrite its own workflow files can read every secret there", capWhere, own)
			}
			rule.tokens = append(rule.tokens, tokenCap)
		}
		rules = append(rules, rule)
	}
	return rules, nil
}

func isJSONArray(raw json.RawMessage) bool {
	trimmed := strings.TrimSpace(string(raw))
	return strings.HasPrefix(trimmed, "[")
}

func decodeTokenCaps(where string, raw json.RawMessage) ([]tokenCapJSON, error) {
	if trimmed := strings.TrimSpace(string(raw)); trimmed == "" || trimmed == "null" {
		return nil, nil
	}
	dec := json.NewDecoder(strings.NewReader(string(raw)))
	dec.DisallowUnknownFields()
	if !isJSONArray(raw) {
		var one tokenCapJSON
		if err := dec.Decode(&one); err != nil {
			return nil, fmt.Errorf(`%s: githubToken must be {"repositories", "permissions"} or an array of them: %w`, where, err)
		}
		return []tokenCapJSON{one}, nil
	}
	var many []tokenCapJSON
	if err := dec.Decode(&many); err != nil {
		return nil, fmt.Errorf(`%s: githubToken must be {"repositories", "permissions"} or an array of them: %w`, where, err)
	}
	if len(many) == 0 {
		return nil, fmt.Errorf("%s: githubToken lists no cap", where)
	}
	return many, nil
}

func capGrantsWorkflowsOn(tokenCap *toolscope.TokenCap, own string) bool {
	_, named := tokenCap.Permissions["workflows"]
	workflows := named || tokenCap.Permissions == nil
	if !workflows {
		return false
	}
	if tokenCap.ExcludeRepositories != nil {
		return !slices.Contains(tokenCap.ExcludeRepositories, own)
	}
	return slices.Contains(tokenCap.Repositories, own)
}

func parseTokenCap(where string, tools []string, c tokenCapJSON) (*toolscope.TokenCap, error) {
	grants := false
	for _, tool := range tools {
		if tool == installationTokenTool {
			grants = true
		}
	}
	if !grants {
		return nil, fmt.Errorf("%s: githubToken is set but tools does not grant %s", where, installationTokenTool)
	}
	tokenCap := &toolscope.TokenCap{}
	switch {
	case c.Repositories != nil && c.ExcludeRepositories != nil:
		return nil, fmt.Errorf("%s: githubToken sets both repositories and excludeRepositories", where)
	case c.ExcludeRepositories != nil:
		if len(c.ExcludeRepositories) == 0 {
			return nil, fmt.Errorf("%s: githubToken excludeRepositories is empty", where)
		}
		names, err := bareRepositoryNames(where, c.ExcludeRepositories)
		if err != nil {
			return nil, err
		}
		tokenCap.ExcludeRepositories = names
	default:
		if len(c.Repositories) == 0 {
			return nil, fmt.Errorf("%s: githubToken lists no repository", where)
		}
		if len(c.Permissions) == 0 {
			return nil, fmt.Errorf("%s: githubToken lists no permission", where)
		}
		names, err := bareRepositoryNames(where, c.Repositories)
		if err != nil {
			return nil, err
		}
		tokenCap.Repositories = names
	}
	if c.Permissions != nil {
		if len(c.Permissions) == 0 {
			return nil, fmt.Errorf("%s: githubToken lists no permission", where)
		}
		tokenCap.Permissions = map[string]string{}
	}
	for name, level := range c.Permissions {
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

func bareRepositoryNames(where string, repositories []string) ([]string, error) {
	names := make([]string, 0, len(repositories))
	for _, repo := range repositories {
		if repo == "" || strings.ContainsAny(repo, "/ \t") {
			return nil, fmt.Errorf("%s: githubToken repository %q is not a bare repository name", where, repo)
		}
		names = append(names, strings.ToLower(repo))
	}
	return names, nil
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

func (g *githubOIDC) grantsFor(repository, workflow string) ([]string, []*toolscope.TokenCap) {
	repo := strings.ToLower(repository)
	var tools []string
	have := map[string]bool{}
	var tokenCaps []*toolscope.TokenCap
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
			if len(r.tokens) == 0 {
				unbounded = true
				continue
			}
			tokenCaps = append(tokenCaps, r.tokens...)
		}
	}
	if unbounded {
		tokenCaps = nil
	}
	return tools, tokenCaps
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
	caller    string
	tools     []string
	tokenCaps []*toolscope.TokenCap
	self      string
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
	tools, tokenCaps := g.grantsFor(claims.Repository, workflow)
	if len(tools) == 0 {
		slog.Warn("github oidc token matches no MCP_GITHUB_OIDC_RULES entry", "repository", claims.Repository, "workflow", workflow)
		return githubGrant{}, fmt.Errorf("%s %s is not allowed", claims.Repository, workflow)
	}
	caller := claims.Repository
	if workflow != "" {
		caller += "/" + workflow
	}
	_, self, _ := strings.Cut(claims.Repository, "/")
	return githubGrant{caller: caller, tools: tools, tokenCaps: tokenCaps, self: strings.ToLower(self)}, nil
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
