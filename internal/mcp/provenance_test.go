package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/dlddu/homelab-k3s-mcp/internal/approvalgate"
)

const sampleReason = "배포가 멈춰 있어 레플리카를 3 으로 올린다."

const patchWithReason = `{"apiVersion":"v1","kind":"ConfigMap","namespace":"ops","name":"cm",` +
	`"reason":"  ` + sampleReason + `  ","patchType":"merge","patch":{"data":{"k":"v"}}}`

func TestReasonIsStrippedBeforeTheGateAndTheHandler(t *testing.T) {
	gate := &scriptedGate{decision: &approvalgate.Decision{RequestID: "req-1"}}
	h, fake := testHandler(t, gate, toolRegistry)

	result, rerr := callTool(t, h, "resource_patch", patchWithReason)
	if rerr != nil {
		t.Fatalf("tools/call = %v, want the patch to run with reason present", rerr)
	}
	if m, _ := result.(map[string]any); m["isError"] == true {
		t.Fatalf("result = %v, want success", result)
	}
	if fake.count() != 1 {
		t.Fatalf("kubernetes calls = %d, want 1", fake.count())
	}
	if len(gate.calls) != 1 {
		t.Fatalf("gate calls = %d, want 1", len(gate.calls))
	}
	if got := gate.calls[0].Reason; got != sampleReason {
		t.Errorf("Call.Reason = %q, want the trimmed argument %q", got, sampleReason)
	}
	ctx := gate.context(0)
	if strings.Contains(ctx, sampleReason) || strings.Contains(ctx, `"reason"`) {
		t.Errorf("approval context carries the reason; it travels beside the context, not in it:\n%s", ctx)
	}
	if !strings.Contains(ctx, `"patchType":"merge"`) {
		t.Errorf("approval context lost the verbatim arguments around reason:\n%s", ctx)
	}
	if string(fake.lastPatch.Patch) == "" || strings.Contains(string(fake.lastPatch.Patch), "reason") {
		t.Errorf("patch sent to kubernetes = %s, want the patch alone", fake.lastPatch.Patch)
	}
}

// resource_create's handler refuses any argument besides manifest, so it is the
// one that would break if reason reached it.
func TestStrictHandlersNeverSeeReason(t *testing.T) {
	gate := &createGate{}
	h, service, _ := createHandler(t, gate)
	args := `{"reason":"` + sampleReason + `","manifest":{"apiVersion":"v1","kind":"ConfigMap","metadata":{"name":"one","namespace":"ops"}}}`

	result, rerr := callTool(t, h, "resource_create", args)
	if rerr != nil {
		t.Fatalf("tools/call = %v, want create to accept reason beside manifest", rerr)
	}
	if m, _ := result.(map[string]any); m["isError"] == true {
		t.Fatalf("result = %v, want success", result)
	}
	if service.count() != 1 {
		t.Fatalf("create calls = %d, want 1", service.count())
	}
	if len(gate.calls) != 1 || gate.calls[0].Reason != sampleReason {
		t.Fatalf("gate calls = %+v, want one carrying the reason", gate.calls)
	}
	if strings.Contains(strings.Join(gate.contexts, ""), sampleReason) {
		t.Error("create approval context carries the reason")
	}
}

func TestCollectionDeleteCarriesReasonAndSession(t *testing.T) {
	gate := &scriptedGate{decision: &approvalgate.Decision{RequestID: "req-1"}}
	h, _, _ := testHandlerListing(t, gate, toolRegistry, newStateReader(), newCollectionReader("alpha"))
	ctx := withSessionID(context.Background(), "sess-1")
	params := json.RawMessage(`{"name":"resource_delete_collection","arguments":` +
		`{"apiVersion":"v1","kind":"ConfigMap","namespace":"ops","labelSelector":"app=x","reason":"` + sampleReason + `"}}`)

	if _, rerr := h.toolsCall(ctx, params); rerr != nil {
		t.Fatalf("tools/call = %v", rerr)
	}
	if len(gate.calls) != 1 {
		t.Fatalf("gate calls = %d, want 1", len(gate.calls))
	}
	if c := gate.calls[0]; c.Reason != sampleReason || c.SessionID != "sess-1" {
		t.Errorf("Call = {SessionID:%q Reason:%q}, want both carried", c.SessionID, c.Reason)
	}
	if strings.Contains(gate.context(0), sampleReason) {
		t.Error("collection approval context carries the reason")
	}
}

func TestNonStringReasonIsInvalidParamsBeforeTheGate(t *testing.T) {
	for _, value := range []string{`42`, `{"why":"x"}`, `["x"]`, `true`} {
		gate := &scriptedGate{decision: &approvalgate.Decision{RequestID: "req-1"}}
		h, fake := testHandler(t, gate, toolRegistry)
		args := `{"apiVersion":"v1","kind":"ConfigMap","namespace":"ops","name":"cm","patchType":"merge","patch":{},"reason":` + value + `}`

		_, rerr := callTool(t, h, "resource_patch", args)
		if rerr == nil || rerr.code != -32602 {
			t.Errorf("reason=%s: error = %+v, want -32602", value, rerr)
		}
		if len(gate.calls) != 0 || fake.count() != 0 {
			t.Errorf("reason=%s: gate calls = %d, kubernetes calls = %d, want both 0", value, len(gate.calls), fake.count())
		}
	}
}

func TestOverlongReasonIsInvalidParams(t *testing.T) {
	gate := &scriptedGate{decision: &approvalgate.Decision{RequestID: "req-1"}}
	h, _ := testHandler(t, gate, toolRegistry)
	long := strings.Repeat("가", maxReasonRunes+1)
	args := `{"apiVersion":"v1","kind":"ConfigMap","namespace":"ops","name":"cm","patchType":"merge","patch":{},"reason":"` + long + `"}`

	if _, rerr := callTool(t, h, "resource_patch", args); rerr == nil || rerr.code != -32602 {
		t.Fatalf("error = %+v, want -32602 for a reason PocketAide would refuse", rerr)
	}
	if len(gate.calls) != 0 {
		t.Errorf("gate calls = %d, want 0", len(gate.calls))
	}
}

// A gated call without a reason is refused before the gate is asked, before
// the gate's own pre-read, and with nothing sent to kubernetes.
func TestGatedCallWithoutReasonIsRefusedBeforeTheGate(t *testing.T) {
	cases := []struct{ tool, args string }{
		{"resource_patch", `{"apiVersion":"v1","kind":"ConfigMap","namespace":"ops","name":"cm","patchType":"merge","patch":{}}`},
		{"resource_create", `{"manifest":{"apiVersion":"v1","kind":"ConfigMap","metadata":{"name":"one","namespace":"ops"}}}`},
		{"resource_delete_collection", `{"apiVersion":"v1","kind":"ConfigMap","namespace":"ops","labelSelector":"app=x"}`},
		{"resource_get", `{"apiVersion":"v1","kind":"Secret","namespace":"ops","name":"token"}`},
		{"resource_watch", `{"apiVersion":"v1","kind":"Secret","namespace":"ops"}`},
	}
	for _, tc := range cases {
		for _, reason := range []string{"", `,"reason":""`, `,"reason":"   "`, `,"reason":null`} {
			gate := &scriptedGate{decision: &approvalgate.Decision{RequestID: "req-1"}}
			reader := newStateReader()
			lister := newCollectionReader("alpha")
			h, fake, _ := testHandlerListing(t, gate, toolRegistry, reader, lister)
			args := tc.args[:len(tc.args)-1] + reason + "}"

			_, rerr := callToolAsIs(t, h, tc.tool, args)
			if rerr == nil || rerr.code != -32602 || !strings.Contains(rerr.message, "reason is required") || !strings.Contains(rerr.message, tc.tool) {
				t.Errorf("%s %s: error = %+v, want -32602 naming the tool and saying reason is required", tc.tool, args, rerr)
			}
			if len(gate.calls) != 0 || fake.count() != 0 || len(reader.observed()) != 0 || len(lister.listed()) != 0 {
				t.Errorf("%s %s: gate=%d kubernetes=%d reads=%d lists=%d, want all 0",
					tc.tool, args, len(gate.calls), fake.count(), len(reader.observed()), len(lister.listed()))
			}
		}
	}
}

// The same tools keep working without a reason when the call itself is not
// gated: only a call that would reach a human needs one.
func TestUngatedCallsNeedNoReason(t *testing.T) {
	cases := []struct{ tool, args string }{
		{"resource_get", `{"apiVersion":"v1","kind":"ConfigMap","namespace":"ops","name":"cm"}`},
		{"resource_watch", `{"apiVersion":"v1","kind":"ConfigMap","namespace":"ops"}`},
		{"resource_list", `{"apiVersion":"v1","kind":"Secret","namespace":"ops"}`},
	}
	for _, tc := range cases {
		gate := &scriptedGate{err: context.Canceled}
		h, fake := testHandler(t, gate, toolRegistry)
		if _, rerr := callToolAsIs(t, h, tc.tool, tc.args); rerr != nil {
			t.Errorf("%s: %v, want it to run without a reason", tc.tool, rerr)
		}
		if len(gate.calls) != 0 || fake.count() != 1 {
			t.Errorf("%s: gate=%d kubernetes=%d, want an ungated call that ran", tc.tool, len(gate.calls), fake.count())
		}
	}
}

// The requirement is checked before the gate's own configuration: an
// unconfigured gate refuses too, but a call missing its reason is the
// caller's to fix first and is recorded as invalid input.
func TestMissingReasonIsInvalidInputEvenWithAnUnconfiguredGate(t *testing.T) {
	h, fake, sink := recordedHandler(t, approvalgate.NewUnavailable(nil))
	_, rerr := callToolAsIs(t, h, "resource_patch", `{"apiVersion":"v1","kind":"ConfigMap","namespace":"ops","name":"cm","patchType":"merge","patch":{}}`)
	if rerr == nil || rerr.code != -32602 {
		t.Fatalf("error = %+v, want -32602", rerr)
	}
	if fake.count() != 0 {
		t.Errorf("kubernetes calls = %d, want 0", fake.count())
	}
	rec := lastRecord(t, sink, 1)
	if rec.Result != "refused" || rec.Reason != "invalid_input" || rec.Gate.Decision != "" {
		t.Errorf("record = %+v, want refused/invalid_input with no verdict", rec)
	}
}

func TestReasonIsLeftForToolsThatNeverAskForApproval(t *testing.T) {
	stripped, reason, rerr := splitReason(json.RawMessage(`{"id":"s1","reason":"x"}`))
	if rerr != nil || reason != "x" || string(stripped) != `{"id":"s1"}` {
		t.Fatalf("splitReason sanity: %s %q %v", stripped, reason, rerr)
	}
	if toolRegistry["session_delete"].decl.mayAskForApproval() {
		t.Fatal("session_delete is a no-resource-permission tool; it must not take reason")
	}
	if toolRegistry["dear_baby_reset_user"].decl.mayAskForApproval() {
		t.Fatal("dear_baby_reset_user is outside the gate; it must not take reason")
	}
	if toolRegistry["resource_list"].decl.mayAskForApproval() {
		t.Fatal("resource_list never gates; it must not take reason")
	}
}

func TestSplitReasonKeepsTheOtherArgumentsByteForByte(t *testing.T) {
	in := `{ "b" : {"z":1,"a":[1, 2]}, "reason":"why", "a":"<&>", "n":1.50 }`
	got, reason, rerr := splitReason(json.RawMessage(in))
	if rerr != nil || reason != "why" {
		t.Fatalf("splitReason = %q, %v", reason, rerr)
	}
	if want := `{"b":{"z":1,"a":[1, 2]},"a":"<&>","n":1.50}`; string(got) != want {
		t.Errorf("stripped = %s, want %s", got, want)
	}

	for _, untouched := range []string{``, `null`, `[]`, `{"a":1}`, `{"a":`, `"text"`} {
		got, reason, rerr := splitReason(json.RawMessage(untouched))
		if rerr != nil || reason != "" || string(got) != untouched {
			t.Errorf("splitReason(%q) = %q, %q, %v; want it returned unchanged", untouched, got, reason, rerr)
		}
	}
}

func TestReasonIsAdvertisedOnExactlyTheToolsThatMayAskForApproval(t *testing.T) {
	if err := validateReasonArgument(toolRegistry, toolsListJSON); err != nil {
		t.Fatal(err)
	}
	want := map[string]bool{
		"resource_get": true, "resource_watch": true, "resource_create": true,
		"resource_update": true, "resource_patch": true, "resource_delete": true,
		"resource_delete_collection": true, "resource_exec": true, "resource_attach": true,
		"resource_port_forward": true, "resource_proxy": true,
	}
	for name, entry := range toolRegistry {
		if got := entry.decl.mayAskForApproval(); got != want[name] {
			t.Errorf("%s mayAskForApproval = %v, want %v", name, got, want[name])
		}
	}

	needs := map[string]approvalNeed{
		"resource_get": approvedForSensitiveKinds, "resource_watch": approvedForSensitiveKinds,
	}
	for name, entry := range toolRegistry {
		want := neverApproved
		if entry.decl.mayAskForApproval() {
			want = alwaysApproved
		}
		if n, ok := needs[name]; ok {
			want = n
		}
		if got := entry.decl.approvalNeed(); got != want {
			t.Errorf("%s approvalNeed = %v, want %v", name, got, want)
		}
	}

	for name, broken := range map[string]string{
		"reworded":            strings.Replace(toolsListJSON, reasonDescriptionAlways, "why", 1),
		"optional on a write": strings.Replace(toolsListJSON, `"required": ["manifest", "reason"]`, `"required": ["manifest"]`, 1),
		"required on a read":  strings.Replace(toolsListJSON, `"required": ["apiVersion", "kind", "name"],`, `"required": ["apiVersion", "kind", "name", "reason"],`, 1),
	} {
		if broken == toolsListJSON {
			t.Fatalf("%s: the mutation did not apply", name)
		}
		if err := validateReasonArgument(toolRegistry, broken); err == nil {
			t.Errorf("%s reason schema passed the startup check", name)
		}
	}
	missing := map[string]toolEntry{"resource_list": {decl: toolDeclaration{alwaysGated: "test"}, handle: reachesKubernetes}}
	if err := validateReasonArgument(missing, toolsListJSON); err == nil || !strings.Contains(err.Error(), "does not advertise") {
		t.Errorf("a gateable tool without reason passed the startup check: %v", err)
	}
	extra := map[string]toolEntry{"resource_patch": {decl: toolDeclaration{outsideGate: "test"}, handle: reachesKubernetes}}
	if err := validateReasonArgument(extra, toolsListJSON); err == nil || !strings.Contains(err.Error(), "never asks") {
		t.Errorf("an ungated tool advertising reason passed the startup check: %v", err)
	}
}

func TestSessionHeaderReachesTheGateAndTheAuditLine(t *testing.T) {
	var logged bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logged, nil)))
	t.Cleanup(func() { slog.SetDefault(prev) })

	gate := &scriptedGate{decision: &approvalgate.Decision{RequestID: "req-9"}}
	h, _ := testHandler(t, gate, toolRegistry)
	body := `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"resource_patch","arguments":` + patchWithReason + `}}`
	req := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(body))
	req.Header.Set(SessionIDHeader, "  sess-42  ")
	h.ServeHTTP(httptest.NewRecorder(), req)

	if len(gate.calls) != 1 || gate.calls[0].SessionID != "sess-42" {
		t.Fatalf("gate calls = %+v, want SessionID sess-42", gate.calls)
	}
	line := ""
	for _, l := range strings.Split(logged.String(), "\n") {
		if strings.Contains(l, "approval granted") {
			line = l
		}
	}
	if !strings.Contains(line, "session_id=sess-42") || !strings.Contains(line, "reason=") || !strings.Contains(line, sampleReason) {
		t.Errorf("approval granted line = %q, want session_id and reason", line)
	}
}

func TestSessionHeaderIsDroppedWhenUnusable(t *testing.T) {
	for name, header := range map[string]string{
		"empty":      "",
		"blank":      "   ",
		"too long":   strings.Repeat("s", maxSessionIDRunes+1),
		"not utf-8":  "\xff\xfe",
		"at the cap": strings.Repeat("세", maxSessionIDRunes),
	} {
		got := sessionIDFrom(withSessionID(context.Background(), header))
		want := ""
		if name == "at the cap" {
			want = header
		}
		if got != want {
			t.Errorf("%s: session id = %q, want %q", name, got, want)
		}
	}
}

func TestUngatedCallOfAGateableToolAlsoDropsReason(t *testing.T) {
	gate := &scriptedGate{err: context.Canceled}
	h, fake := testHandler(t, gate, toolRegistry)
	args := `{"apiVersion":"v1","kind":"ConfigMap","namespace":"ops","name":"cm","reason":"` + sampleReason + `"}`

	if _, rerr := callTool(t, h, "resource_get", args); rerr != nil {
		t.Fatalf("tools/call = %v", rerr)
	}
	if len(gate.calls) != 0 || fake.count() != 1 {
		t.Errorf("gate calls = %d, kubernetes calls = %d; want an ungated read that ran", len(gate.calls), fake.count())
	}
}
