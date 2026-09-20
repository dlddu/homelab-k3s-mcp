package github

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

const testSHA = "0123456789abcdef0123456789abcdef01234567"

func validInput() CommitStatusInput {
	return CommitStatusInput{
		Repository:  "test",
		SHA:         testSHA,
		State:       "success",
		Context:     "homelab-k3s-mcp/e2e",
		Description: "green",
		TargetURL:   "https://example.invalid/run/1",
	}
}

func statusClient(t *testing.T, fake *fakeGitHub, prefixes ...string) *Client {
	t.Helper()
	fake.account = "dlddu"
	c := newTestClient(t, fake)
	c.statusContextPrefixes = prefixes
	return c
}

// TestCommitStatusCreatesStatus covers AC1's success half: the status lands on
// the installation account's repo and comes back as GitHub reported it.
func TestCommitStatusCreatesStatus(t *testing.T) {
	fake := &fakeGitHub{installation: map[string]any{"statuses": "write"}}
	c := statusClient(t, fake, "homelab-k3s-mcp/")

	status, err := c.CreateCommitStatus(context.Background(), validInput())
	if err != nil {
		t.Fatalf("CreateCommitStatus: %v", err)
	}
	if status.State != "success" || status.Context != "homelab-k3s-mcp/e2e" || status.SHA != testSHA {
		t.Errorf("status = %+v, want the requested state/context/sha", status)
	}
	if status.ID == 0 || status.CreatedAt == "" {
		t.Errorf("status = %+v, want the upstream id and created_at", status)
	}

	path := "/repos/dlddu/test/statuses/" + testSHA
	if got := fake.countOf("POST", path); got != 1 {
		t.Fatalf("upstream saw %d POSTs to %s, want 1 (%+v)", got, path, fake.requests)
	}
	body := fake.bodyOf("POST", path)
	for field, want := range map[string]string{
		"state":       "success",
		"context":     "homelab-k3s-mcp/e2e",
		"description": "green",
		"target_url":  "https://example.invalid/run/1",
	} {
		if got, _ := body[field].(string); got != want {
			t.Errorf("request body %s = %q, want %q", field, got, want)
		}
	}
}

// TestCommitStatusSurfacesGitHubError covers AC1's failure half.
func TestCommitStatusSurfacesGitHubError(t *testing.T) {
	fake := &fakeGitHub{installation: map[string]any{"statuses": "write"}, statusStatus: 422}
	c := statusClient(t, fake, "homelab-k3s-mcp/")

	status, err := c.CreateCommitStatus(context.Background(), validInput())
	if err == nil {
		t.Fatalf("CreateCommitStatus = %+v, want an error", status)
	}
	if !strings.Contains(err.Error(), "No commit found for SHA") {
		t.Errorf("error = %q, want GitHub's own wording in it", err)
	}
}

// TestCommitStatusMintsNarrowToken covers AC2's first clause: the minted token
// is scoped to this repository and to statuses: write, and nothing else.
func TestCommitStatusMintsNarrowToken(t *testing.T) {
	fake := &fakeGitHub{installation: map[string]any{"statuses": "write"}}
	c := statusClient(t, fake, "homelab-k3s-mcp/")

	if _, err := c.CreateCommitStatus(context.Background(), validInput()); err != nil {
		t.Fatalf("CreateCommitStatus: %v", err)
	}
	if got := fake.mintCount(); got != 1 {
		t.Fatalf("mint requests = %d, want 1", got)
	}

	body := fake.bodyOf("POST", "/app/installations/42/access_tokens")
	got, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal mint body: %v", err)
	}
	const want = `{"permissions":{"statuses":"write"},"repositories":["test"]}`
	if string(got) != want {
		t.Errorf("mint body = %s, want %s", got, want)
	}
}

// TestCommitStatusNeverReturnsToken covers AC2's second clause. The success and
// the failure path are both checked because an error string is the easier place
// for a credential to escape.
func TestCommitStatusNeverReturnsToken(t *testing.T) {
	for _, tc := range []struct {
		name         string
		statusStatus int
	}{
		{name: "success", statusStatus: 201},
		{name: "upstream error", statusStatus: 422},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fake := &fakeGitHub{
				installation: map[string]any{"statuses": "write"},
				statusStatus: tc.statusStatus,
			}
			c := statusClient(t, fake, "homelab-k3s-mcp/")

			status, err := c.CreateCommitStatus(context.Background(), validInput())
			serialised, mErr := json.Marshal(map[string]any{
				"status": status,
				"error":  errString(err),
			})
			if mErr != nil {
				t.Fatalf("marshal result: %v", mErr)
			}
			for _, secret := range []string{"ghs_mock_42", "-----BEGIN", "eyJ"} {
				if strings.Contains(string(serialised), secret) {
					t.Errorf("serialised result contains %q: %s", secret, serialised)
				}
			}
		})
	}
}

func errString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

// TestCommitStatusValidatesBeforeGitHub covers AC3: the six malformed calls of
// test-github-commit-status.md scenario 3 are refused with the upstream request
// count still at zero — token mint included.
func TestCommitStatusValidatesBeforeGitHub(t *testing.T) {
	cases := []struct {
		name  string
		mutro func(*CommitStatusInput)
	}{
		{"repository missing", func(in *CommitStatusInput) { in.Repository = "" }},
		{"short sha", func(in *CommitStatusInput) { in.SHA = "abc1234" }},
		{"state outside the four", func(in *CommitStatusInput) { in.State = "ok" }},
		{"context missing", func(in *CommitStatusInput) { in.Context = "" }},
		{"description too long", func(in *CommitStatusInput) { in.Description = strings.Repeat("x", 141) }},
		{"target_url not http(s)", func(in *CommitStatusInput) { in.TargetURL = "ftp://x" }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fake := &fakeGitHub{installation: map[string]any{"statuses": "write"}}
			c := statusClient(t, fake, "homelab-k3s-mcp/")

			in := validInput()
			tc.mutro(&in)
			if status, err := c.CreateCommitStatus(context.Background(), in); err == nil {
				t.Fatalf("CreateCommitStatus = %+v, want an error", status)
			}
			if got := len(fake.requests); got != 0 {
				t.Errorf("upstream saw %d requests (%+v), want 0", got, fake.requests)
			}
		})
	}
}

// TestCommitStatusRejectsForeignContext covers AC4's allowed/refused pair.
func TestCommitStatusRejectsForeignContext(t *testing.T) {
	fake := &fakeGitHub{installation: map[string]any{"statuses": "write"}}
	c := statusClient(t, fake, "homelab-k3s-mcp/", "reconciler/")

	in := validInput()
	in.Context = "ci/build"
	if status, err := c.CreateCommitStatus(context.Background(), in); err == nil {
		t.Fatalf("CreateCommitStatus = %+v, want an error", status)
	} else if !strings.Contains(err.Error(), "homelab-k3s-mcp/, reconciler/") {
		t.Errorf("error = %q, want it to show the allowed prefixes", err)
	}
	if got := len(fake.requests); got != 0 {
		t.Fatalf("upstream saw %d requests (%+v), want 0", got, fake.requests)
	}

	if _, err := c.CreateCommitStatus(context.Background(), validInput()); err != nil {
		t.Fatalf("an allowed context was refused: %v", err)
	}
}

// TestCommitStatusRefusesWhenNoPrefixConfigured covers AC4's fail-closed half:
// no configuration means no writes, not unrestricted writes.
func TestCommitStatusRefusesWhenNoPrefixConfigured(t *testing.T) {
	fake := &fakeGitHub{installation: map[string]any{"statuses": "write"}}
	c := statusClient(t, fake)

	status, err := c.CreateCommitStatus(context.Background(), validInput())
	if err == nil {
		t.Fatalf("CreateCommitStatus = %+v, want an error", status)
	}
	if !strings.Contains(err.Error(), contextPrefixesEnv) {
		t.Errorf("error = %q, want it to name %s", err, contextPrefixesEnv)
	}
	if got := len(fake.requests); got != 0 {
		t.Errorf("upstream saw %d requests (%+v), want 0", got, fake.requests)
	}
}

// TestCommitStatusUnavailableReturnsToolError covers AC5.
func TestCommitStatusUnavailableReturnsToolError(t *testing.T) {
	svc := NewUnavailable("")
	status, err := svc.CreateCommitStatus(context.Background(), validInput())
	if err == nil {
		t.Fatalf("CreateCommitStatus = %+v, want an error", status)
	}
	if !strings.Contains(err.Error(), "github app unavailable") {
		t.Errorf("error = %q, want an unavailable-class error", err)
	}
}

// TestCommitStatusDiscardsItsToken covers the PRD's "mint, spend, discard": the
// token is revoked after the call rather than left alive for its full hour.
func TestCommitStatusDiscardsItsToken(t *testing.T) {
	fake := &fakeGitHub{installation: map[string]any{"statuses": "write"}}
	c := statusClient(t, fake, "homelab-k3s-mcp/")

	if _, err := c.CreateCommitStatus(context.Background(), validInput()); err != nil {
		t.Fatalf("CreateCommitStatus: %v", err)
	}
	if got := fake.countOf("DELETE", "/installation/token"); got != 1 {
		t.Errorf("revoke requests = %d, want 1 (%+v)", got, fake.requests)
	}
}
