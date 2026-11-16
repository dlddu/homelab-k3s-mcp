package sessionplatform

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const (
	testEpoch    = "ep_1111111111111111"
	testArgsHash = "sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
)

func approvalClient(t *testing.T, handler http.HandlerFunc) *Client {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return &Client{endpoint: srv.URL, userAgent: "homelab-k3s-mcp/test", http: srv.Client()}
}

func validDecision() ToolApprovalDecision {
	return ToolApprovalDecision{Decision: ToolApprovalApprove, ArgsHash: testArgsHash, Epoch: testEpoch}
}

// session-platform AC-E2: the owner reads the waiting call as the model made
// it, with the hash and the run epoch a decision has to carry back.
func TestToolApprovalsReadsWhatWaits(t *testing.T) {
	var method, path string
	c := approvalClient(t, func(w http.ResponseWriter, r *http.Request) {
		method, path = r.Method, r.URL.Path
		_, _ = w.Write([]byte(`{"epoch":"` + testEpoch + `","approvals":[{"id":"ap_1","tool":"Bash",` +
			`"input":{"command":"ls"},"argsHash":"` + testArgsHash + `","toolUseId":"toolu_1",` +
			`"epoch":"` + testEpoch + `","askedAt":"2026-11-16T00:00:00Z","expiresAt":"2026-11-16T00:30:00Z"}]}`))
	})

	page, err := c.ToolApprovals(context.Background(), "s-1")
	if err != nil {
		t.Fatal(err)
	}
	if method != http.MethodGet || path != "/api/v1/sessions/s-1/tool-approvals" {
		t.Fatalf("request = %s %s", method, path)
	}
	if page.Epoch != testEpoch || len(page.Approvals) != 1 {
		t.Fatalf("page = %+v", page)
	}
	call := page.Approvals[0]
	if call.ID != "ap_1" || call.Tool != "Bash" || call.ArgsHash != testArgsHash ||
		call.ToolUseID != "toolu_1" || call.Epoch != testEpoch || call.ExpiresAt != "2026-11-16T00:30:00Z" {
		t.Fatalf("call = %+v", call)
	}
	var input map[string]any
	if err := json.Unmarshal(call.Input, &input); err != nil || input["command"] != "ls" {
		t.Fatalf("input = %s (%v)", call.Input, err)
	}
}

// An empty desk is an empty list, not a nil one: nothing waiting is an answer.
func TestToolApprovalsEmptyIsAList(t *testing.T) {
	c := approvalClient(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"epoch":"` + testEpoch + `"}`))
	})
	page, err := c.ToolApprovals(context.Background(), "s-1")
	if err != nil || page.Approvals == nil || len(page.Approvals) != 0 {
		t.Fatalf("page = %+v, %v", page, err)
	}
}

// The decision travels with all three bindings, exactly as given.
func TestDecideToolApprovalSendsTheBindings(t *testing.T) {
	var method, path, body string
	c := approvalClient(t, func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		method, path, body = r.Method, r.URL.Path, string(b)
		_, _ = w.Write([]byte(`{"status":"approved"}`))
	})

	decision := validDecision()
	decision.Reason = "read the command"
	if err := c.DecideToolApproval(context.Background(), "s-1", "ap_1", decision); err != nil {
		t.Fatal(err)
	}
	if method != http.MethodPost || path != "/api/v1/sessions/s-1/tool-approvals/ap_1/decide" {
		t.Fatalf("request = %s %s", method, path)
	}
	want := `{"decision":"approve","argsHash":"` + testArgsHash + `","epoch":"` + testEpoch + `","reason":"read the command"}`
	if body != want {
		t.Fatalf("body = %s, want %s", body, want)
	}

	// Without a note the field is dropped rather than sent empty.
	if err := c.DecideToolApproval(context.Background(), "s-1", "ap_1", validDecision()); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(body, "reason") {
		t.Fatalf("body = %s, want no reason field", body)
	}
}

// A decision that does not name the call it was taken on never reaches the
// control plane: the refusal happens before the request is built.
func TestDecideToolApprovalRefusesUnboundDecisions(t *testing.T) {
	calls := 0
	c := approvalClient(t, func(w http.ResponseWriter, _ *http.Request) {
		calls++
		_, _ = w.Write([]byte(`{"status":"approved"}`))
	})

	for name, tc := range map[string]struct {
		id, approvalID string
		decision       ToolApprovalDecision
	}{
		"no session":   {id: "", approvalID: "ap_1", decision: validDecision()},
		"no call":      {id: "s-1", approvalID: "", decision: validDecision()},
		"no decision":  {id: "s-1", approvalID: "ap_1", decision: ToolApprovalDecision{ArgsHash: testArgsHash, Epoch: testEpoch}},
		"not a choice": {id: "s-1", approvalID: "ap_1", decision: ToolApprovalDecision{Decision: "allow", ArgsHash: testArgsHash, Epoch: testEpoch}},
		"no args hash": {id: "s-1", approvalID: "ap_1", decision: ToolApprovalDecision{Decision: ToolApprovalApprove, Epoch: testEpoch}},
		"no epoch":     {id: "s-1", approvalID: "ap_1", decision: ToolApprovalDecision{Decision: ToolApprovalApprove, ArgsHash: testArgsHash}},
	} {
		err := c.DecideToolApproval(context.Background(), tc.id, tc.approvalID, tc.decision)
		var e *Error
		if err == nil {
			t.Fatalf("%s: want a refusal", name)
		}
		if !errors.As(err, &e) || e.kind != kindInvalidArgument {
			t.Fatalf("%s: err = %v, want an invalid-argument refusal", name, err)
		}
	}
	if calls != 0 {
		t.Fatalf("an unbound decision reached the control plane %d time(s)", calls)
	}
}

// Each control plane refusal keeps its meaning: not found is nothing to retry,
// conflict means reading the list again, and the rest stay distinguishable.
func TestDecideToolApprovalDistinguishesRefusals(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status int
		kind   errKind
	}{
		{name: "no longer waiting", status: http.StatusNotFound, kind: kindNotFound},
		{name: "another call or run", status: http.StatusConflict, kind: kindConflict},
		{name: "malformed", status: http.StatusBadRequest, kind: kindInvalidArgument},
		{name: "not claude-code", status: http.StatusUnprocessableEntity, kind: kindInvalidArgument},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := approvalClient(t, func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(`{"error":"no"}`))
			})
			err := c.DecideToolApproval(context.Background(), "s-1", "ap_1", validDecision())
			var e *Error
			if !errors.As(err, &e) || e.kind != tc.kind {
				t.Fatalf("status %d gave %v", tc.status, err)
			}
		})
	}
}

// Listing refuses the same way, so "nothing waits" is never a refusal in
// disguise.
func TestToolApprovalsDistinguishesRefusals(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status int
		kind   errKind
	}{
		{name: "unknown session", status: http.StatusNotFound, kind: kindNotFound},
		{name: "not claude-code", status: http.StatusUnprocessableEntity, kind: kindInvalidArgument},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := approvalClient(t, func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(`{"error":"no"}`))
			})
			page, err := c.ToolApprovals(context.Background(), "s-1")
			var e *Error
			if page != nil || !errors.As(err, &e) || e.kind != tc.kind {
				t.Fatalf("status %d gave %+v, %v", tc.status, page, err)
			}
		})
	}
}

// Without an endpoint both refuse rather than reporting an empty desk.
func TestUnavailableRefusesToolApprovals(t *testing.T) {
	u := NewUnavailable("SESSION_PLATFORM_ENDPOINT is not set")
	if _, err := u.ToolApprovals(context.Background(), "s-1"); err == nil {
		t.Fatal("listing must refuse")
	}
	if err := u.DecideToolApproval(context.Background(), "s-1", "ap_1", validDecision()); err == nil {
		t.Fatal("deciding must refuse")
	}
}
