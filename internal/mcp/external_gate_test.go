package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/dlddu/homelab-k3s-mcp/internal/approvalgate"
	"github.com/dlddu/homelab-k3s-mcp/internal/github"
)

type countingGitHub struct {
	mu       sync.Mutex
	checkErr error
	statuses []github.CommitStatusInput
}

func (g *countingGitHub) CreateInstallationToken(context.Context, []string, map[string]any) (*github.InstallationToken, error) {
	return nil, errors.New("unused")
}

func (g *countingGitHub) CheckCommitStatus(github.CommitStatusInput) error { return g.checkErr }

func (g *countingGitHub) CreateCommitStatus(_ context.Context, in github.CommitStatusInput) (*github.CommitStatus, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.statuses = append(g.statuses, in)
	return &github.CommitStatus{ID: 7, State: in.State, Context: in.Context, SHA: in.SHA}, nil
}

func (g *countingGitHub) writes() int {
	g.mu.Lock()
	defer g.mu.Unlock()
	return len(g.statuses)
}

const commitStatusArgs = `{"repository":"homelab-k3s-mcp","sha":"0123456789abcdef0123456789abcdef01234567",` +
	`"state":"success","context":"review/manual-approval","description":"approved by operator"}`

func commitStatusHandler(t *testing.T, gate approvalgate.Gate) (*Handler, *countingGitHub) {
	t.Helper()
	h, _ := testHandler(t, gate, toolRegistry)
	gh := &countingGitHub{}
	h.github = gh
	return h, gh
}

func TestCommitStatusIsRefusedBeforeGitHubWithoutApproval(t *testing.T) {
	h, gh := commitStatusHandler(t, approvalgate.NewUnavailable(nil))

	_, rerr := callTool(t, h, "github_commit_status_create", commitStatusArgs)
	if rerr == nil {
		t.Fatal("tools/call = nil error, want the gate's refusal")
	}
	if gh.writes() != 0 {
		t.Errorf("commit status writes = %d, want 0", gh.writes())
	}
}

func TestCommitStatusRejectedByTheOperatorIsNotWritten(t *testing.T) {
	gate := &scriptedGate{err: &approvalgate.Refusal{
		Verdict: approvalgate.VerdictRejected, RequestID: "req-no", Err: errors.New("refusing: approval rejected"),
	}}
	h, gh := commitStatusHandler(t, gate)

	_, rerr := callTool(t, h, "github_commit_status_create", commitStatusArgs)
	if rerr == nil {
		t.Fatal("tools/call = nil error, want the rejection")
	}
	if len(gate.calls) != 1 {
		t.Errorf("approval requests = %d, want 1", len(gate.calls))
	}
	if gh.writes() != 0 {
		t.Errorf("commit status writes = %d, want 0", gh.writes())
	}
}

func TestApprovedCommitStatusIsWrittenWithAJudgeableContext(t *testing.T) {
	gate := &scriptedGate{decision: &approvalgate.Decision{RequestID: "req-ok"}}
	h, gh := commitStatusHandler(t, gate)

	result, rerr := callTool(t, h, "github_commit_status_create", commitStatusArgs)
	if rerr != nil {
		t.Fatalf("tools/call = %v", rerr)
	}
	if isErr, _ := result.(map[string]any)["isError"].(bool); isErr {
		t.Fatalf("result = %v, want success", result)
	}
	if gh.writes() != 1 {
		t.Fatalf("commit status writes = %d, want 1", gh.writes())
	}

	shown := gate.context(0)
	for _, want := range []string{
		"tool: github_commit_status_create",
		"action: write a GitHub commit status",
		"requested at: ",
		`"repository":"homelab-k3s-mcp"`,
		`"sha":"0123456789abcdef0123456789abcdef01234567"`,
		`"state":"success"`,
		`"context":"review/manual-approval"`,
		`"description":"approved by operator"`,
	} {
		if !strings.Contains(shown, want) {
			t.Errorf("approval context lacks %q:\n%s", want, shown)
		}
	}
}

func TestCommitStatusPrecheckRefusesWithoutAskingTheGate(t *testing.T) {
	gate := &scriptedGate{decision: &approvalgate.Decision{RequestID: "req-unused"}}
	h, gh := commitStatusHandler(t, gate)
	gh.checkErr = errors.New(`context "ci/build" is outside the allowed prefixes: review/`)

	result, rerr := callTool(t, h, "github_commit_status_create", commitStatusArgs)
	if rerr != nil {
		t.Fatalf("tools/call = %v, want the handler's own tool error", rerr)
	}
	encoded, _ := json.Marshal(result)
	if isErr, _ := result.(map[string]any)["isError"].(bool); !isErr || !strings.Contains(string(encoded), "outside the allowed prefixes") {
		t.Errorf("result = %s, want the precheck's tool error", encoded)
	}
	if len(gate.calls) != 0 {
		t.Errorf("approval requests = %d, want 0", len(gate.calls))
	}
	if gh.writes() != 0 {
		t.Errorf("commit status writes = %d, want 0", gh.writes())
	}
}

func TestCommitStatusWithANonStringArgumentNeverReachesTheGate(t *testing.T) {
	gate := &scriptedGate{decision: &approvalgate.Decision{RequestID: "req-unused"}}
	h, gh := commitStatusHandler(t, gate)

	_, rerr := callTool(t, h, "github_commit_status_create", `{"repository":"homelab-k3s-mcp","sha":123}`)
	if rerr == nil || rerr.code != -32602 {
		t.Fatalf("tools/call = %v, want -32602", rerr)
	}
	if len(gate.calls) != 0 || gh.writes() != 0 {
		t.Errorf("approval requests = %d, writes = %d, want 0 and 0", len(gate.calls), gh.writes())
	}
}

func TestCommitStatusApprovalCannotBeSpentTwice(t *testing.T) {
	gate := &scriptedGate{decision: &approvalgate.Decision{RequestID: "req-once"}}
	h, gh := commitStatusHandler(t, gate)

	if _, rerr := callTool(t, h, "github_commit_status_create", commitStatusArgs); rerr != nil {
		t.Fatalf("first call = %v", rerr)
	}
	if _, rerr := callTool(t, h, "github_commit_status_create", commitStatusArgs); rerr == nil {
		t.Fatal("second call on the same approval = nil error, want a refusal")
	}
	if gh.writes() != 1 {
		t.Errorf("commit status writes = %d, want 1", gh.writes())
	}
}

func TestAutoApprovedCommitStatusSaysSo(t *testing.T) {
	gate := &scriptedGate{decision: &approvalgate.Decision{RequestID: "req-auto", AutoApproved: true}}
	h, _ := commitStatusHandler(t, gate)

	result, rerr := callTool(t, h, "github_commit_status_create", commitStatusArgs)
	if rerr != nil {
		t.Fatalf("tools/call = %v", rerr)
	}
	encoded, _ := json.Marshal(result)
	if !strings.Contains(string(encoded), "auto-approved") {
		t.Errorf("result %s does not disclose that no human reviewed it", encoded)
	}
}

func TestShippedExternalWritesAreExactlyTheDocumentedOnes(t *testing.T) {
	var gated []string
	for name, entry := range toolRegistry {
		if entry.decl.externalWrite != nil {
			gated = append(gated, name)
		}
	}
	if len(gated) != 1 || gated[0] != "github_commit_status_create" {
		t.Errorf("external writes behind the gate = %v, want [github_commit_status_create]", gated)
	}
}

func TestValidateDeclarationsRejectsMalformedExternalWrites(t *testing.T) {
	precheck := func(*Handler, context.Context, json.RawMessage) (any, *rpcErr) { return nil, nil }
	nothing := &noResourcePermission{kind: touchesNothing, reason: "reaches an outside system"}
	cases := map[string]toolDeclaration{
		"on a tool with pairs": {
			pairs:         []approvalgate.Pair{{Verb: "get", Resource: "pods"}},
			externalWrite: &externalWrite{action: "a", reason: "r", precheck: precheck},
		},
		"on discovery": {
			noResourcePermission: &noResourcePermission{kind: discoveryOnly, reason: "discovery"},
			externalWrite:        &externalWrite{action: "a", reason: "r", precheck: precheck},
		},
		"without a reason": {
			noResourcePermission: nothing,
			externalWrite:        &externalWrite{action: "a", precheck: precheck},
		},
		"without a precheck": {
			noResourcePermission: nothing,
			externalWrite:        &externalWrite{action: "a", reason: "r"},
		},
	}
	for label, decl := range cases {
		t.Run(label, func(t *testing.T) {
			problems := validateDeclarations(map[string]toolEntry{"tool": {decl: decl, handle: reachesKubernetes}})
			if len(problems) == 0 {
				t.Error("validateDeclarations accepted a malformed external write gate")
			}
		})
	}
}
