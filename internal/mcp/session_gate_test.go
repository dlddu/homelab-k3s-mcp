package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/dlddu/homelab-k3s-mcp/internal/approvalgate"
	"github.com/dlddu/homelab-k3s-mcp/internal/sessionplatform"
)

// countingSessions stands in for the control plane. What prd-session-create
// AC5 asserts is how many create requests reached it, so that is all it keeps.
type countingSessions struct {
	sessionplatform.Unavailable

	mu      sync.Mutex
	creates []sessionplatform.CreateRequest
}

func (c *countingSessions) CreateSession(_ context.Context, req sessionplatform.CreateRequest) (*sessionplatform.Session, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.creates = append(c.creates, req)
	return &sessionplatform.Session{ID: "s-1", Name: req.Name, WorkloadType: req.WorkloadType, State: "active", Pod: "session-s-1"}, nil
}

func (c *countingSessions) count() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.creates)
}

func sessionGateHandler(t *testing.T, gate approvalgate.Gate) (*Handler, *countingSessions) {
	t.Helper()
	h, _ := testHandler(t, gate, toolRegistry)
	sessions := &countingSessions{}
	h.sessionPlatform = sessions
	return h, sessions
}

func TestCodexSessionCreateWaitsForApproval(t *testing.T) {
	gate := &scriptedGate{decision: &approvalgate.Decision{RequestID: "req-7"}}
	h, sessions := sessionGateHandler(t, gate)

	result, rerr := callTool(t, h, "session_create", `{"name":"agent","workloadType":"codex"}`)
	if rerr != nil {
		t.Fatalf("tools/call = %v, want it to run after approval", rerr)
	}
	if m, _ := result.(map[string]any); m["isError"] != false {
		t.Fatalf("result = %v, want success", result)
	}
	if len(gate.calls) != 1 {
		t.Fatalf("gate calls = %d, want 1", len(gate.calls))
	}
	if sessions.count() != 1 {
		t.Errorf("create requests = %d, want 1", sessions.count())
	}

	call := gate.calls[0]
	if call.Tool != "session_create" || call.Reason != testReason {
		t.Errorf("approval call = %+v, want session_create with the reason", call)
	}
	if call.Pair != (approvalgate.Pair{}) {
		t.Errorf("approval pair = %v, want none: session_create holds no kubernetes permission", call.Pair)
	}
	ctx := gate.context(0)
	for _, want := range []string{"tool: session_create", `workload type "codex"`, "requested at: ", `"workloadType":"codex"`, `"name":"agent"`} {
		if !strings.Contains(ctx, want) {
			t.Errorf("approval context %q is missing %q", ctx, want)
		}
	}
	if strings.Contains(ctx, testReason) {
		t.Errorf("approval context %q repeats the reason, which travels separately", ctx)
	}
}

func TestCodexSessionCreateCreatesNothingWithoutApproval(t *testing.T) {
	for name, gate := range map[string]approvalgate.Gate{
		"rejected":     &scriptedGate{err: errors.New("approval rejected (request req-8)")},
		"unconfigured": approvalgate.NewUnavailable(nil),
	} {
		t.Run(name, func(t *testing.T) {
			h, sessions := sessionGateHandler(t, gate)
			if _, rerr := callTool(t, h, "session_create", `{"name":"agent","workloadType":"codex"}`); rerr == nil {
				t.Fatal("tools/call = nil error, want a refusal")
			}
			if sessions.count() != 0 {
				t.Errorf("create requests = %d, want 0", sessions.count())
			}
		})
	}
}

func TestCodexSessionCreateRequiresAReason(t *testing.T) {
	gate := &scriptedGate{decision: &approvalgate.Decision{RequestID: "req-9"}}
	h, sessions := sessionGateHandler(t, gate)

	_, rerr := callToolAsIs(t, h, "session_create", `{"name":"agent","workloadType":"codex"}`)
	if rerr == nil || rerr.code != -32602 || !strings.Contains(rerr.message, "reason is required") {
		t.Fatalf("tools/call = %v, want -32602 asking for reason", rerr)
	}
	if len(gate.calls) != 0 || sessions.count() != 0 {
		t.Errorf("gate calls = %d, create requests = %d, want 0 and 0", len(gate.calls), sessions.count())
	}
}

func TestBadSessionArgumentsAreRefusedBeforeApproval(t *testing.T) {
	gate := &scriptedGate{decision: &approvalgate.Decision{RequestID: "req-10"}}
	h, sessions := sessionGateHandler(t, gate)

	for _, args := range []string{`{"name":"  ","workloadType":"codex"}`, `{"name":"agent","workloadType":"codex","model":3}`} {
		_, rerr := callTool(t, h, "session_create", args)
		if rerr == nil || rerr.code != -32602 {
			t.Errorf("%s: tools/call = %v, want -32602", args, rerr)
		}
	}
	if len(gate.calls) != 0 || sessions.count() != 0 {
		t.Errorf("gate calls = %d, create requests = %d, want 0 and 0", len(gate.calls), sessions.count())
	}
}

func TestOtherWorkloadsCreateWithoutApproval(t *testing.T) {
	gate := &scriptedGate{err: errors.New("the gate must not be asked")}
	h, sessions := sessionGateHandler(t, gate)

	for _, args := range []string{
		`{"name":"plain"}`,
		`{"name":"shell","workloadType":"shell"}`,
		`{"name":"agent","workloadType":"claude-code"}`,
		`{"name":"agent","workloadType":"minimax-code"}`,
	} {
		if _, rerr := callToolAsIs(t, h, "session_create", args); rerr != nil {
			t.Errorf("%s: tools/call = %v, want it to run without approval or reason", args, rerr)
		}
	}
	if len(gate.calls) != 0 {
		t.Errorf("gate calls = %d, want 0", len(gate.calls))
	}
	if sessions.count() != 4 {
		t.Errorf("create requests = %d, want 4", sessions.count())
	}
}

func TestCodexAutoApprovalIsVisibleInTheToolResponse(t *testing.T) {
	gate := &scriptedGate{decision: &approvalgate.Decision{RequestID: "req-11", AutoApproved: true}}
	h, _ := sessionGateHandler(t, gate)

	result, rerr := callTool(t, h, "session_create", `{"name":"agent","workloadType":"codex"}`)
	if rerr != nil {
		t.Fatalf("tools/call = %v", rerr)
	}
	encoded, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("marshal result: %v", err)
	}
	if !strings.Contains(string(encoded), "auto-approved") {
		t.Errorf("result %s does not disclose that no human reviewed it", encoded)
	}
}

func TestCodexWorkloadFamily(t *testing.T) {
	for workloadType, want := range map[string]bool{
		"codex": true, "codex-mini": true, " Codex ": true, "CODEX-large": true,
		"": false, "shell": false, "claude-code": false, "minimax-code": false, "codexy": false, "my-codex": false,
	} {
		if got := isCodexWorkload(workloadType); got != want {
			t.Errorf("isCodexWorkload(%q) = %v, want %v", workloadType, got, want)
		}
	}
}

func TestArgumentGateMustSitOnAToolThatReachesNoResource(t *testing.T) {
	subject := func(json.RawMessage) (string, error) { return "x", nil }
	problems := validateDeclarations(map[string]toolEntry{
		"paired": {
			handle: reachesKubernetes,
			decl: toolDeclaration{
				pairs:        []approvalgate.Pair{{Verb: "get", Resource: "pods"}},
				argumentGate: &argumentGate{basis: "b", condition: "c", subject: subject},
			},
		},
		"undocumented": {
			handle: reachesKubernetes,
			decl: toolDeclaration{
				noResourcePermission: &noResourcePermission{kind: touchesNothing, reason: "r"},
				argumentGate:         &argumentGate{subject: subject},
			},
		},
	})
	joined := strings.Join(problems, "\n")
	for _, want := range []string{"paired has an argument gate but is not marked", "undocumented has an argument gate without a documented basis"} {
		if !strings.Contains(joined, want) {
			t.Errorf("problems %q are missing %q", joined, want)
		}
	}
}
