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
