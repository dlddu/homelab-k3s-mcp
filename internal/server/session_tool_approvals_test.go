package server_test

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/dlddu/homelab-k3s-mcp/internal/server"
	"github.com/dlddu/homelab-k3s-mcp/internal/sessionplatform"
)

const (
	testApprovalEpoch = "ep_1111111111111111"
	testApprovalHash  = "sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
)

func approvalPage() *sessionplatform.ToolApprovalPage {
	return &sessionplatform.ToolApprovalPage{
		Epoch: testApprovalEpoch,
		Approvals: []sessionplatform.PendingToolApproval{{
			ID:        "ap_1",
			Tool:      "Bash",
			Input:     []byte(`{"command":"rm -rf /tmp/scratch"}`),
			ArgsHash:  testApprovalHash,
			ToolUseID: "toolu_1",
			Epoch:     testApprovalEpoch,
			AskedAt:   "2026-11-16T00:00:00Z",
			ExpiresAt: "2026-11-16T00:30:00Z",
		}},
	}
}

// Both tools are declared on the surface, with the shape the owner needs to
// bind a decision to the call it reads.
func TestSessionToolApprovalToolsAreDeclared(t *testing.T) {
	app := server.App(nil, unavailableK8s(), unavailableGitHub(), unavailableAWS(), unavailableGrafana(), unavailableOpenSearch(), &fakeSessionPlatform{})
	tools := toolsList(t, app)

	list := findTool(t, tools, "session_tool_approvals")
	if at(t, list, "annotations", "readOnlyHint") != true {
		t.Fatal("listing what waits is a passive read")
	}
	if required, _ := at(t, list, "inputSchema", "required").([]any); len(required) != 1 || required[0] != "id" {
		t.Fatalf("required = %v, want [id]", required)
	}

	decide := findTool(t, tools, "session_tool_approval_decide")
	props := at(t, decide, "inputSchema", "properties").(map[string]any)
	// "note" rather than "reason": this server reserves "reason" for the
	// approval gate's own argument (internal/mcp/provenance.go).
	for _, field := range []string{"id", "approvalId", "decision", "argsHash", "epoch", "note"} {
		if _, ok := props[field]; !ok {
			t.Fatalf("inputSchema is missing %s: %v", field, props)
		}
	}
	required, _ := at(t, decide, "inputSchema", "required").([]any)
	got := map[string]bool{}
	for _, r := range required {
		got[fmt.Sprint(r)] = true
	}
	// note is the only optional one: the three bindings are required so a
	// decision can never travel without naming the call it was taken on.
	for _, field := range []string{"id", "approvalId", "decision", "argsHash", "epoch"} {
		if !got[field] {
			t.Fatalf("required = %v, want %s in it", required, field)
		}
	}
	if got["note"] || len(required) != 5 {
		t.Fatalf("required = %v, want exactly the five bindings", required)
	}
	if decisions, _ := at(t, decide, "inputSchema", "properties", "decision", "enum").([]any); len(decisions) != 2 ||
		decisions[0] != "approve" || decisions[1] != "deny" {
		t.Fatalf("decision enum = %v, want [approve deny]", decisions)
	}
	if at(t, decide, "annotations", "readOnlyHint") != false || at(t, decide, "annotations", "idempotentHint") != false {
		t.Fatal("deciding changes what the turn does and is not idempotent")
	}
}

// session-platform AC-E2: the owner reads the waiting call as the model made
// it — tool, arguments, hash and the run's epoch — which is what a decision
// has to carry back.
func TestSessionToolApprovalsListsWhatWaits(t *testing.T) {
	fake := &fakeSessionPlatform{approvalsResponse: func(string) (*sessionplatform.ToolApprovalPage, error) {
		return approvalPage(), nil
	}}
	app := server.App(nil, unavailableK8s(), unavailableGitHub(), unavailableAWS(), unavailableGrafana(), unavailableOpenSearch(), fake)

	body := callTool(t, app, 300, "session_tool_approvals", map[string]any{"id": "s-1"})
	if at(t, body, "result", "isError") != false {
		t.Fatalf("isError = %v", at(t, body, "result", "isError"))
	}
	if at(t, body, "result", "structuredContent", "epoch") != testApprovalEpoch {
		t.Fatalf("epoch = %v, want the run that put the calls up", at(t, body, "result", "structuredContent", "epoch"))
	}
	approvals, ok := at(t, body, "result", "structuredContent", "approvals").([]any)
	if !ok || len(approvals) != 1 {
		t.Fatalf("approvals = %v", at(t, body, "result", "structuredContent", "approvals"))
	}
	call := approvals[0].(map[string]any)
	for field, want := range map[string]any{
		"id":        "ap_1",
		"tool":      "Bash",
		"argsHash":  testApprovalHash,
		"toolUseId": "toolu_1",
		"epoch":     testApprovalEpoch,
		"expiresAt": "2026-11-16T00:30:00Z",
	} {
		if call[field] != want {
			t.Fatalf("call[%q] = %v, want %v", field, call[field], want)
		}
	}
	input, ok := call["input"].(map[string]any)
	if !ok || input["command"] != "rm -rf /tmp/scratch" {
		t.Fatalf("input = %v, want the arguments the model asked for", call["input"])
	}
	if len(fake.approvalList) != 1 || fake.approvalList[0] != "s-1" {
		t.Fatalf("listed = %v", fake.approvalList)
	}
}

// The decision reaches the control plane with all three bindings unchanged.
func TestSessionToolApprovalDecidePassesTheBindingsThrough(t *testing.T) {
	fake := &fakeSessionPlatform{}
	app := server.App(nil, unavailableK8s(), unavailableGitHub(), unavailableAWS(), unavailableGrafana(), unavailableOpenSearch(), fake)

	body := callTool(t, app, 301, "session_tool_approval_decide", map[string]any{
		"id": "s-1", "approvalId": "ap_1", "decision": "approve",
		"argsHash": testApprovalHash, "epoch": testApprovalEpoch, "note": "read the command",
	})
	if at(t, body, "result", "isError") != false {
		t.Fatalf("isError = %v", at(t, body, "result", "isError"))
	}
	if at(t, body, "result", "structuredContent", "id") != "ap_1" ||
		at(t, body, "result", "structuredContent", "decision") != "approve" {
		t.Fatalf("answer = %v", at(t, body, "result", "structuredContent"))
	}
	if len(fake.decideCalls) != 1 {
		t.Fatalf("decideCalls = %+v", fake.decideCalls)
	}
	got := fake.decideCalls[0]
	if got.id != "s-1" || got.approvalID != "ap_1" {
		t.Fatalf("decided %q %q", got.id, got.approvalID)
	}
	want := sessionplatform.ToolApprovalDecision{
		Decision: "approve", ArgsHash: testApprovalHash, Epoch: testApprovalEpoch, Reason: "read the command",
	}
	if got.decision != want {
		t.Fatalf("decision = %+v, want %+v", got.decision, want)
	}

	// A denial travels the same way; the note is what the model receives.
	body = callTool(t, app, 302, "session_tool_approval_decide", map[string]any{
		"id": "s-1", "approvalId": "ap_2", "decision": "deny",
		"argsHash": testApprovalHash, "epoch": testApprovalEpoch, "note": "not on production data",
	})
	if at(t, body, "result", "isError") != false {
		t.Fatalf("deny isError = %v", at(t, body, "result", "isError"))
	}
	if len(fake.decideCalls) != 2 || fake.decideCalls[1].decision.Decision != "deny" {
		t.Fatalf("decideCalls = %+v", fake.decideCalls)
	}
}

// A decision that does not name the call it was taken on is refused as
// invalid-params and — the part that matters — never reaches the control plane,
// so a half-specified decision cannot land on whatever happens to be waiting.
func TestSessionToolApprovalDecideRejectsUnboundDecisions(t *testing.T) {
	full := func(over map[string]any) map[string]any {
		args := map[string]any{
			"id": "s-1", "approvalId": "ap_1", "decision": "approve",
			"argsHash": testApprovalHash, "epoch": testApprovalEpoch,
		}
		for k, v := range over {
			if v == nil {
				delete(args, k)
				continue
			}
			args[k] = v
		}
		return args
	}
	for _, tc := range []struct {
		name string
		args map[string]any
		want string
	}{
		{name: "missing id", args: full(map[string]any{"id": nil}), want: "id is required"},
		{name: "missing approval id", args: full(map[string]any{"approvalId": nil}), want: "approvalId is required"},
		{name: "missing decision", args: full(map[string]any{"decision": nil}), want: "decision is required"},
		{name: "not a decision", args: full(map[string]any{"decision": "allow"}), want: "decision must be"},
		{name: "missing args hash", args: full(map[string]any{"argsHash": nil}), want: "argsHash is required"},
		{name: "missing epoch", args: full(map[string]any{"epoch": nil}), want: "epoch is required"},
		{name: "numeric epoch", args: full(map[string]any{"epoch": 1}), want: "epoch is required"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fake := &fakeSessionPlatform{}
			app := server.App(nil, unavailableK8s(), unavailableGitHub(), unavailableAWS(), unavailableGrafana(), unavailableOpenSearch(), fake)

			body := callTool(t, app, 310, "session_tool_approval_decide", tc.args)
			message := fmt.Sprint(at(t, body, "error", "message"))
			if !strings.Contains(message, tc.want) {
				t.Fatalf("error = %q, want an invalid-params error mentioning %q", message, tc.want)
			}
			if len(fake.decideCalls) != 0 {
				t.Fatalf("a refused decision reached the control plane: %+v", fake.decideCalls)
			}
		})
	}
}

// A refusal from the control plane keeps its meaning on the way back: it is a
// tool error the owner can act on, not a silent success.
func TestSessionToolApprovalDecideSurfacesRefusals(t *testing.T) {
	fake := &fakeSessionPlatform{decideResponse: func(string, string, sessionplatform.ToolApprovalDecision) error {
		return errors.New("the decision names another call")
	}}
	app := server.App(nil, unavailableK8s(), unavailableGitHub(), unavailableAWS(), unavailableGrafana(), unavailableOpenSearch(), fake)

	body := callTool(t, app, 311, "session_tool_approval_decide", map[string]any{
		"id": "s-1", "approvalId": "ap_1", "decision": "approve",
		"argsHash": testApprovalHash, "epoch": testApprovalEpoch,
	})
	if at(t, body, "result", "isError") != true {
		t.Fatalf("a refused decision must be a tool error: %v", body)
	}
	if len(fake.decideCalls) != 1 {
		t.Fatalf("the decision must still have been attempted once: %+v", fake.decideCalls)
	}
}

// Without SESSION_PLATFORM_ENDPOINT both tools refuse rather than pretending
// nothing waits.
func TestSessionToolApprovalsNeedTheControlPlane(t *testing.T) {
	app := server.App(nil, unavailableK8s(), unavailableGitHub(), unavailableAWS(), unavailableGrafana(), unavailableOpenSearch(), unavailableSessionPlatform())

	for i, call := range []struct {
		tool string
		args map[string]any
	}{
		{tool: "session_tool_approvals", args: map[string]any{"id": "s-1"}},
		{tool: "session_tool_approval_decide", args: map[string]any{
			"id": "s-1", "approvalId": "ap_1", "decision": "deny",
			"argsHash": testApprovalHash, "epoch": testApprovalEpoch,
		}},
	} {
		body := callTool(t, app, 320+i, call.tool, call.args)
		if at(t, body, "result", "isError") != true {
			t.Fatalf("%s = %v, want a tool error", call.tool, body)
		}
	}
}
