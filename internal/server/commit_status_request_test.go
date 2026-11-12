package server_test

import (
	"strings"
	"testing"

	"github.com/dlddu/homelab-k3s-mcp/internal/pocketaide"
)

func commitStatusArgs() map[string]any {
	return map[string]any{
		"repository":  "dlddu/pocket-aide",
		"sha":         "0123456789abcdef0123456789abcdef01234567",
		"state":       "success",
		"context":     "homelab-k3s-mcp/e2e",
		"description": "e2e",
		"target_url":  "https://example.invalid/pr/1",
		"requester":   "tester",
		"explanation": "검토 끝",
	}
}

func TestToolsListAdvertisesCommitStatusRequest(t *testing.T) {
	tool := findTool(t, toolsList(t, pocketAideApp(nil)), "github_commit_status_request")

	props := at(t, tool, "inputSchema", "properties").(map[string]any)
	for _, name := range []string{"repository", "sha", "state", "context", "description", "target_url", "requester", "explanation"} {
		if _, ok := props[name]; !ok {
			t.Errorf("missing input property %q", name)
		}
	}
	if _, ok := props["reason"]; ok {
		t.Errorf("reason belongs to gated tools; this one never waits for approval")
	}
	required, _ := at(t, tool, "inputSchema", "required").([]any)
	if strings.Join(anyStrings(required), ",") != "repository,sha,state,context,requester" {
		t.Errorf("required = %v", required)
	}
	for hint, want := range map[string]any{
		"readOnlyHint":    false,
		"destructiveHint": false,
		"idempotentHint":  false,
		"openWorldHint":   true,
	} {
		if got := at(t, tool, "annotations", hint); got != want {
			t.Errorf("annotation %s = %v, want %v", hint, got, want)
		}
	}
}

func anyStrings(values []any) []string {
	out := make([]string, 0, len(values))
	for _, v := range values {
		s, _ := v.(string)
		out = append(out, s)
	}
	return out
}

func TestCommitStatusRequestReturnsTheTicketAtOnce(t *testing.T) {
	t.Setenv("GITHUB_COMMIT_STATUS_CONTEXT_PREFIXES", "homelab-k3s-mcp/")
	fake := &fakePocketAide{ticket: &pocketaide.CommitStatusTicket{ID: "req-1", Status: "PENDING", CreatedAt: 1_800_000_000}}

	body := callTool(t, pocketAideApp(fake), 420, "github_commit_status_request", commitStatusArgs())

	if at(t, body, "result", "isError") != false ||
		at(t, body, "result", "structuredContent", "id") != "req-1" ||
		at(t, body, "result", "structuredContent", "status") != "PENDING" ||
		at(t, body, "result", "structuredContent", "created_at") != float64(1_800_000_000) {
		t.Fatalf("result = %v", at(t, body, "result"))
	}
	if message, _ := at(t, body, "result", "structuredContent", "message").(string); !strings.Contains(message, "Nothing is written to GitHub until the owner approves") {
		t.Errorf("message = %q", message)
	}
	want := pocketaide.CommitStatusRequest{
		Repository: "dlddu/pocket-aide", SHA: "0123456789abcdef0123456789abcdef01234567", State: "success",
		Context: "homelab-k3s-mcp/e2e", Description: "e2e", TargetURL: "https://example.invalid/pr/1",
		Requester: "tester", Explanation: "검토 끝",
	}
	if len(fake.requests) != 1 || fake.requests[0] != want {
		t.Fatalf("service saw %+v, want %+v", fake.requests, want)
	}
}

func TestCommitStatusRequestRefusesBeforePocketAide(t *testing.T) {
	t.Setenv("GITHUB_COMMIT_STATUS_CONTEXT_PREFIXES", "homelab-k3s-mcp/")
	fake := &fakePocketAide{ticket: &pocketaide.CommitStatusTicket{ID: "x"}}
	app := pocketAideApp(fake)
	for field, value := range map[string]any{
		"repository": "pocket-aide",
		"sha":        "main",
		"state":      "approved",
		"context":    "ci/build",
		"requester":  "",
	} {
		args := commitStatusArgs()
		args[field] = value
		body := callTool(t, app, 421, "github_commit_status_request", args)
		text, _ := at(t, body, "result", "content", 0, "text").(string)
		if at(t, body, "result", "isError") != true || !strings.HasPrefix(text, "invalid commit status request: ") {
			t.Errorf("%s=%v: result = %v", field, value, at(t, body, "result"))
		}
	}
	args := commitStatusArgs()
	args["sha"] = 123
	if message, _ := at(t, callTool(t, app, 422, "github_commit_status_request", args), "error", "message").(string); !strings.Contains(message, "sha must be a string") {
		t.Errorf("non-string sha: error = %q", message)
	}
	if len(fake.requests) != 0 {
		t.Fatalf("refused calls reached PocketAide: %+v", fake.requests)
	}
}

func TestCommitStatusRequestWithoutPocketAideIsUnavailable(t *testing.T) {
	t.Setenv("GITHUB_COMMIT_STATUS_CONTEXT_PREFIXES", "homelab-k3s-mcp/")
	body := callTool(t, pocketAideApp(pocketaide.NewUnavailable("")), 423, "github_commit_status_request", commitStatusArgs())
	text, _ := at(t, body, "result", "content", 0, "text").(string)
	if at(t, body, "result", "isError") != true || text != "pocket-aide unavailable: PocketAide is not configured (APPROVAL_GATE_URL)" {
		t.Fatalf("result = %v", at(t, body, "result"))
	}
	if survivor := callTool(t, pocketAideApp(pocketaide.NewUnavailable("")), 424, "ping", map[string]any{}); at(t, survivor, "result", "isError") == true {
		t.Fatalf("ping after refusal = %v", survivor)
	}
}
