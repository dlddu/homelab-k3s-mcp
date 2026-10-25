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

	// repos is keyed by the lowercased "owner/name"; GitHub treats repository
	// names case-insensitively, and the claim carries whatever case the
	// repository was created with.
	repos map[string][]string

	mu   sync.RWMutex
	keys map[string]*rsa.PublicKey
}

type githubClaims struct {
	Repository string `json:"repository"`
	jwt.RegisteredClaims
}

func newGitHubOIDC(ctx context.Context, issuer, audience, rawRepos string) (*githubOIDC, error) {
	if issuer == "" {
		issuer = defaultGitHubOIDCIssuer
	}
	issuer = strings.TrimRight(issuer, "/")
	if audience == "" {
		return nil, fmt.Errorf("MCP_GITHUB_OIDC_AUDIENCE is required when GitHub OIDC is enabled")
	}
	repos, err := parseGitHubRepositories(rawRepos)
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
		repos:    repos,
	}
	if err := g.refreshKeys(ctx); err != nil {
		return nil, err
	}
	return g, nil
}

func parseGitHubRepositories(raw string) (map[string][]string, error) {
	if strings.TrimSpace(raw) == "" {
		return nil, fmt.Errorf("MCP_GITHUB_OIDC_REPOSITORIES is required when GitHub OIDC is enabled")
	}
	var decoded map[string][]string
	if err := json.Unmarshal([]byte(raw), &decoded); err != nil {
		return nil, fmt.Errorf("MCP_GITHUB_OIDC_REPOSITORIES must be a JSON object of \"owner/name\" to a list of tool names: %w", err)
	}
	if len(decoded) == 0 {
		return nil, fmt.Errorf("MCP_GITHUB_OIDC_REPOSITORIES lists no repository")
	}

	repos := make(map[string][]string, len(decoded))
	for repo, tools := range decoded {
		owner, name, ok := strings.Cut(repo, "/")
		if !ok || owner == "" || name == "" || strings.Contains(name, "/") || strings.ContainsAny(repo, " \t") {
			return nil, fmt.Errorf("MCP_GITHUB_OIDC_REPOSITORIES: %q is not an \"owner/name\" repository", repo)
		}
		key := strings.ToLower(repo)
		if _, dup := repos[key]; dup {
			return nil, fmt.Errorf("MCP_GITHUB_OIDC_REPOSITORIES lists %q more than once", key)
		}
		if len(tools) == 0 {
			return nil, fmt.Errorf("MCP_GITHUB_OIDC_REPOSITORIES: %s lists no tool", repo)
		}
		for _, tool := range tools {
			if strings.TrimSpace(tool) == "" {
				return nil, fmt.Errorf("MCP_GITHUB_OIDC_REPOSITORIES: %s lists an empty tool name", repo)
			}
		}
		repos[key] = tools
	}
	return repos, nil
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
	tools, ok := g.repos[strings.ToLower(claims.Repository)]
	if !ok {
		slog.Warn("github oidc token from a repository not in MCP_GITHUB_OIDC_REPOSITORIES", "repository", claims.Repository)
		return "", nil, fmt.Errorf("repository %q is not allowed", claims.Repository)
	}
	return claims.Repository, tools, nil
}

// GitHubOIDCRepositoryCount returns how many repositories may authenticate
// with a GitHub Actions OIDC token.
func (c *Config) GitHubOIDCRepositoryCount() int {
	if c.github == nil {
		return 0
	}
	return len(c.github.repos)
}

// CheckToolScopes reports the tool names MCP_GITHUB_OIDC_REPOSITORIES grants
// that are not among known.
func (c *Config) CheckToolScopes(known []string) error {
	if c.github == nil {
		return nil
	}
	have := make(map[string]bool, len(known))
	for _, k := range known {
		have[k] = true
	}
	var problems []string
	for repo, tools := range c.github.repos {
		for _, tool := range tools {
			if !have[tool] {
				problems = append(problems, fmt.Sprintf("%s: %s", repo, tool))
			}
		}
	}
	if len(problems) == 0 {
		return nil
	}
	sort.Strings(problems)
	return fmt.Errorf("MCP_GITHUB_OIDC_REPOSITORIES grants tools this server does not have: %s", strings.Join(problems, ", "))
}
