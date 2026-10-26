package mcp

import (
	"context"
	"encoding/json"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/dlddu/homelab-k3s-mcp/internal/github"
	"github.com/dlddu/homelab-k3s-mcp/internal/toolscope"
)

type recordingTokenGitHub struct {
	repositories []string
	permissions  map[string]any
	calls        int
}

func (g *recordingTokenGitHub) CreateInstallationToken(_ context.Context, repositories []string, permissions map[string]any) (*github.InstallationToken, error) {
	g.calls++
	g.repositories = repositories
	g.permissions = permissions
	return &github.InstallationToken{Token: "ghs_test"}, nil
}

func (g *recordingTokenGitHub) CreateCommitStatus(context.Context, github.CommitStatusInput) (*github.CommitStatus, error) {
	return nil, nil
}

func reconcilerCap() *toolscope.TokenCap {
	return &toolscope.TokenCap{
		Repositories: []string{"plugin-marketplace", "econ-monitor"},
		Permissions:  map[string]string{"contents": "write", "pull_requests": "write", "metadata": "read"},
	}
}

func TestInstallationTokenUnboundedWithoutCap(t *testing.T) {
	gh := &recordingTokenGitHub{}
	h := &Handler{github: gh}
	if _, rerr := h.githubAppInstallationToken(context.Background(), json.RawMessage(`{}`)); rerr != nil {
		t.Fatalf("unexpected error: %v", rerr)
	}
	if gh.repositories != nil || gh.permissions != nil {
		t.Errorf("an uncapped caller's omitted arguments must stay omitted, got %v %v", gh.repositories, gh.permissions)
	}
}

func TestInstallationTokenCapFillsOmittedArguments(t *testing.T) {
	gh := &recordingTokenGitHub{}
	h := &Handler{github: gh}
	ctx := toolscope.WithTokenCap(context.Background(), reconcilerCap())
	if _, rerr := h.githubAppInstallationToken(ctx, json.RawMessage(`{}`)); rerr != nil {
		t.Fatalf("unexpected error: %v", rerr)
	}
	got := append([]string(nil), gh.repositories...)
	sort.Strings(got)
	if want := []string{"econ-monitor", "plugin-marketplace"}; !reflect.DeepEqual(got, want) {
		t.Errorf("repositories = %v, want the cap %v", got, want)
	}
	if want := map[string]any{"contents": "write", "pull_requests": "write", "metadata": "read"}; !reflect.DeepEqual(gh.permissions, want) {
		t.Errorf("permissions = %v, want the cap %v", gh.permissions, want)
	}
}

func TestInstallationTokenCapAllowsNarrowerRequest(t *testing.T) {
	gh := &recordingTokenGitHub{}
	h := &Handler{github: gh}
	ctx := toolscope.WithTokenCap(context.Background(), reconcilerCap())
	args := `{"repositories":["Plugin-Marketplace"],"permissions":{"contents":"read"}}`
	if _, rerr := h.githubAppInstallationToken(ctx, json.RawMessage(args)); rerr != nil {
		t.Fatalf("unexpected error: %v", rerr)
	}
	if !reflect.DeepEqual(gh.repositories, []string{"Plugin-Marketplace"}) || !reflect.DeepEqual(gh.permissions, map[string]any{"contents": "read"}) {
		t.Errorf("a request inside the cap must pass unchanged, got %v %v", gh.repositories, gh.permissions)
	}
}

func TestInstallationTokenCapRefusesWiderRequest(t *testing.T) {
	cases := map[string]string{
		"repository outside the cap": `{"repositories":["reconciler"]}`,
		"empty repository list":      `{"repositories":[]}`,
		"permission outside the cap": `{"permissions":{"workflows":"write"}}`,
		"level above the cap":        `{"permissions":{"metadata":"write"}}`,
		"unknown level":              `{"permissions":{"contents":"owner"}}`,
		"non-string level":           `{"permissions":{"contents":true}}`,
		"empty permission map":       `{"permissions":{}}`,
	}
	for name, args := range cases {
		t.Run(name, func(t *testing.T) {
			gh := &recordingTokenGitHub{}
			h := &Handler{github: gh}
			ctx := toolscope.WithTokenCap(context.Background(), reconcilerCap())
			_, rerr := h.githubAppInstallationToken(ctx, json.RawMessage(args))
			if rerr == nil {
				t.Fatal("want a refusal")
			}
			if rerr.code != -32602 || !strings.Contains(rerr.message, "this caller") && !strings.Contains(rerr.message, "must be") {
				t.Errorf("refusal = %+v, want an invalid-params error naming the caller's limit", rerr)
			}
			if gh.calls != 0 {
				t.Errorf("GitHub was asked for a token %d times, want 0", gh.calls)
			}
		})
	}
}

func TestInstallationTokenRefusesWorkflowsOnSelf(t *testing.T) {
	refused := map[string]string{
		"all repositories, all permissions": `{}`,
		"all repositories, workflows named": `{"permissions":{"workflows":"write","contents":"write"}}`,
		"self named, all permissions":       `{"repositories":["Reconciler","dear-baby"]}`,
		"self named, workflows named":       `{"repositories":["reconciler"],"permissions":{"workflows":"write"}}`,
	}
	for name, args := range refused {
		t.Run("refuses "+name, func(t *testing.T) {
			gh := &recordingTokenGitHub{}
			h := &Handler{github: gh}
			ctx := toolscope.WithSelfRepository(context.Background(), "reconciler")
			_, rerr := h.githubAppInstallationToken(ctx, json.RawMessage(args))
			if rerr == nil || rerr.code != -32602 || !strings.Contains(rerr.message, "own repository") {
				t.Fatalf("refusal = %+v, want -32602 naming the caller's own repository", rerr)
			}
			if gh.calls != 0 {
				t.Errorf("GitHub was asked for a token %d times, want 0", gh.calls)
			}
		})
	}

	allowed := map[string]string{
		"workflows on other repositories": `{"repositories":["dear-baby","feature-doc"],"permissions":{"workflows":"write","contents":"write"}}`,
		"all permissions on other repos":  `{"repositories":["dear-baby"]}`,
		"self without workflows":          `{"repositories":["reconciler"],"permissions":{"contents":"write"}}`,
		"all repositories, read only":     `{"permissions":{"contents":"read","metadata":"read"}}`,
	}
	for name, args := range allowed {
		t.Run("allows "+name, func(t *testing.T) {
			gh := &recordingTokenGitHub{}
			h := &Handler{github: gh}
			ctx := toolscope.WithSelfRepository(context.Background(), "reconciler")
			if _, rerr := h.githubAppInstallationToken(ctx, json.RawMessage(args)); rerr != nil {
				t.Fatalf("unexpected refusal: %v", rerr)
			}
			if gh.calls != 1 {
				t.Errorf("GitHub calls = %d, want 1", gh.calls)
			}
		})
	}
}

func TestInstallationTokenSelfGuardAppliesAfterCap(t *testing.T) {
	gh := &recordingTokenGitHub{}
	h := &Handler{github: gh}
	ctx := toolscope.WithTokenCap(context.Background(), &toolscope.TokenCap{
		Repositories: []string{"reconciler", "dear-baby"},
		Permissions:  map[string]string{"contents": "write", "workflows": "write"},
	})
	ctx = toolscope.WithSelfRepository(ctx, "reconciler")
	if _, rerr := h.githubAppInstallationToken(ctx, json.RawMessage(`{}`)); rerr == nil {
		t.Fatal("a cap that unions self with workflows must not mint by default")
	}
	if _, rerr := h.githubAppInstallationToken(ctx, json.RawMessage(`{"repositories":["dear-baby"]}`)); rerr != nil {
		t.Fatalf("narrowing to other repositories must pass: %v", rerr)
	}
	if gh.calls != 1 {
		t.Errorf("GitHub calls = %d, want 1", gh.calls)
	}
}

func TestInstallationTokenWithoutSelfIsUnguarded(t *testing.T) {
	gh := &recordingTokenGitHub{}
	h := &Handler{github: gh}
	if _, rerr := h.githubAppInstallationToken(context.Background(), json.RawMessage(`{}`)); rerr != nil {
		t.Fatalf("API-key and OAuth callers keep the old behaviour: %v", rerr)
	}
}

func plannerCaps() []*toolscope.TokenCap {
	return []*toolscope.TokenCap{
		{Repositories: []string{"dear-baby", "feature-doc"}, Permissions: map[string]string{"contents": "write", "pull_requests": "write", "metadata": "read"}},
		{Repositories: []string{"plugin-marketplace"}, Permissions: map[string]string{"contents": "read", "metadata": "read"}},
	}
}

func TestInstallationTokenCapListMintsWithinOneCap(t *testing.T) {
	cases := map[string]struct {
		args      string
		wantRepos []string
		wantPerms map[string]any
	}{
		"repositories of one cap fill its permissions": {
			args:      `{"repositories":["plugin-marketplace"]}`,
			wantRepos: []string{"plugin-marketplace"},
			wantPerms: map[string]any{"contents": "read", "metadata": "read"},
		},
		"permissions of one cap fill its repositories": {
			args:      `{"permissions":{"pull_requests":"write"}}`,
			wantRepos: []string{"dear-baby", "feature-doc"},
			wantPerms: map[string]any{"pull_requests": "write"},
		},
		"both named inside one cap pass unchanged": {
			args:      `{"repositories":["Feature-Doc"],"permissions":{"contents":"write"}}`,
			wantRepos: []string{"Feature-Doc"},
			wantPerms: map[string]any{"contents": "write"},
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			gh := &recordingTokenGitHub{}
			h := &Handler{github: gh}
			ctx := toolscope.WithTokenCaps(context.Background(), plannerCaps())
			if _, rerr := h.githubAppInstallationToken(ctx, json.RawMessage(tc.args)); rerr != nil {
				t.Fatalf("unexpected refusal: %v", rerr)
			}
			if !reflect.DeepEqual(gh.repositories, tc.wantRepos) || !reflect.DeepEqual(gh.permissions, tc.wantPerms) {
				t.Errorf("minted %v %v, want %v %v", gh.repositories, gh.permissions, tc.wantRepos, tc.wantPerms)
			}
		})
	}
}

func TestInstallationTokenCapListRefusesAcrossOrAmbiguous(t *testing.T) {
	cases := map[string]struct {
		args string
		want string
	}{
		"repositories spanning two caps": {`{"repositories":["dear-baby","plugin-marketplace"],"permissions":{"contents":"read"}}`, "fits none"},
		"write outside the write cap":    {`{"repositories":["plugin-marketplace"],"permissions":{"contents":"write"}}`, "fits none"},
		"repository in no cap":           {`{"repositories":["reconciler"]}`, "fits none"},
		"empty repository list":          {`{"repositories":[]}`, "fits none"},
		"nothing named":                  {`{}`, "ambiguous"},
		"permissions fitting both caps":  {`{"permissions":{"contents":"read"}}`, "ambiguous"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			gh := &recordingTokenGitHub{}
			h := &Handler{github: gh}
			ctx := toolscope.WithTokenCaps(context.Background(), plannerCaps())
			_, rerr := h.githubAppInstallationToken(ctx, json.RawMessage(tc.args))
			if rerr == nil || rerr.code != -32602 || !strings.Contains(rerr.message, tc.want) {
				t.Fatalf("refusal = %+v, want -32602 saying %q", rerr, tc.want)
			}
			if !strings.Contains(rerr.message, "plugin-marketplace") && tc.want == "fits none" {
				t.Errorf("refusal must list the caller's scopes, got %q", rerr.message)
			}
			if gh.calls != 0 {
				t.Errorf("GitHub was asked for a token %d times, want 0", gh.calls)
			}
		})
	}
}

func TestInstallationTokenCapListKeepsSelfGuard(t *testing.T) {
	gh := &recordingTokenGitHub{}
	h := &Handler{github: gh}
	ctx := toolscope.WithTokenCaps(context.Background(), []*toolscope.TokenCap{
		{Repositories: []string{"dear-baby"}, Permissions: map[string]string{"workflows": "write"}},
		{Repositories: []string{"reconciler"}, Permissions: map[string]string{"contents": "read"}},
	})
	ctx = toolscope.WithSelfRepository(ctx, "reconciler")
	if _, rerr := h.githubAppInstallationToken(ctx, json.RawMessage(`{"repositories":["dear-baby"]}`)); rerr != nil {
		t.Fatalf("workflows on another repository must pass: %v", rerr)
	}
	if _, rerr := h.githubAppInstallationToken(ctx, json.RawMessage(`{"repositories":["reconciler"],"permissions":{"workflows":"write"}}`)); rerr == nil {
		t.Fatal("workflows on the caller's own repository must stay refused")
	}
	if gh.calls != 1 {
		t.Errorf("GitHub calls = %d, want 1", gh.calls)
	}
}

func excludeCaps() []*toolscope.TokenCap {
	return []*toolscope.TokenCap{
		{ExcludeRepositories: []string{"reconciler", "plugin-marketplace", "infrastructure"}},
		{Repositories: []string{"plugin-marketplace"}, Permissions: map[string]string{"contents": "read", "metadata": "read"}},
	}
}

func TestInstallationTokenExcludeCapAllowsNamedRepositories(t *testing.T) {
	cases := map[string]struct {
		args      string
		wantRepos []string
		wantPerms map[string]any
	}{
		"any permission on a repository outside the exclusions": {
			args:      `{"repositories":["pocket-aide","tessera"],"permissions":{"contents":"write","workflows":"write","administration":"write"}}`,
			wantRepos: []string{"pocket-aide", "tessera"},
			wantPerms: map[string]any{"contents": "write", "workflows": "write", "administration": "write"},
		},
		"omitted permissions stay omitted": {
			args:      `{"repositories":["Dear-Baby"]}`,
			wantRepos: []string{"Dear-Baby"},
			wantPerms: nil,
		},
		"nothing named fits only the listed cap": {
			args:      `{}`,
			wantRepos: []string{"plugin-marketplace"},
			wantPerms: map[string]any{"contents": "read", "metadata": "read"},
		},
		"an excluded repository can still fit the read cap": {
			args:      `{"repositories":["plugin-marketplace"]}`,
			wantRepos: []string{"plugin-marketplace"},
			wantPerms: map[string]any{"contents": "read", "metadata": "read"},
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			gh := &recordingTokenGitHub{}
			h := &Handler{github: gh}
			ctx := toolscope.WithSelfRepository(toolscope.WithTokenCaps(context.Background(), excludeCaps()), "reconciler")
			if _, rerr := h.githubAppInstallationToken(ctx, json.RawMessage(tc.args)); rerr != nil {
				t.Fatalf("unexpected refusal: %v", rerr)
			}
			if !reflect.DeepEqual(gh.repositories, tc.wantRepos) || !reflect.DeepEqual(gh.permissions, tc.wantPerms) {
				t.Errorf("minted %v %v, want %v %v", gh.repositories, gh.permissions, tc.wantRepos, tc.wantPerms)
			}
		})
	}
}

func TestInstallationTokenExcludeCapRefuses(t *testing.T) {
	cases := map[string]string{
		"an excluded repository":               `{"repositories":["Reconciler"],"permissions":{"contents":"read"}}`,
		"an excluded repository among others":  `{"repositories":["pocket-aide","infrastructure"]}`,
		"write on the read-only excluded one":  `{"repositories":["plugin-marketplace"],"permissions":{"contents":"write"}}`,
		"an excluded and a read-only together": `{"repositories":["pocket-aide","plugin-marketplace"],"permissions":{"contents":"read"}}`,
		"empty repository list":                `{"repositories":[]}`,
	}
	for name, args := range cases {
		t.Run(name, func(t *testing.T) {
			gh := &recordingTokenGitHub{}
			h := &Handler{github: gh}
			ctx := toolscope.WithTokenCaps(context.Background(), excludeCaps())
			_, rerr := h.githubAppInstallationToken(ctx, json.RawMessage(args))
			if rerr == nil || rerr.code != -32602 || !strings.Contains(rerr.message, "every repository except") {
				t.Fatalf("refusal = %+v, want -32602 describing the exclusions", rerr)
			}
			if gh.calls != 0 {
				t.Errorf("GitHub was asked for a token %d times, want 0", gh.calls)
			}
		})
	}
}

func TestInstallationTokenSingleExcludeCap(t *testing.T) {
	ctx := toolscope.WithTokenCap(context.Background(), &toolscope.TokenCap{
		ExcludeRepositories: []string{"reconciler"},
		Permissions:         map[string]string{"contents": "read", "metadata": "read"},
	})
	gh := &recordingTokenGitHub{}
	h := &Handler{github: gh}
	if _, rerr := h.githubAppInstallationToken(ctx, json.RawMessage(`{"repositories":["feature-doc"]}`)); rerr != nil {
		t.Fatalf("unexpected refusal: %v", rerr)
	}
	if want := map[string]any{"contents": "read", "metadata": "read"}; !reflect.DeepEqual(gh.permissions, want) {
		t.Errorf("permissions = %v, want the cap's %v", gh.permissions, want)
	}
	for args, want := range map[string]string{
		`{}`: "must be named",
		`{"repositories":["feature-doc"],"permissions":{"contents":"write"}}`: "exceeds",
	} {
		if _, rerr := h.githubAppInstallationToken(ctx, json.RawMessage(args)); rerr == nil || !strings.Contains(rerr.message, want) {
			t.Errorf("%s: refusal = %+v, want %q", args, rerr, want)
		}
	}
	if gh.calls != 1 {
		t.Errorf("GitHub calls = %d, want 1", gh.calls)
	}
}
