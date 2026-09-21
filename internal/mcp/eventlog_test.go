package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"
	"testing"

	"github.com/dlddu/homelab-k3s-mcp/internal/eventlog"
	"github.com/dlddu/homelab-k3s-mcp/internal/gatekeeper"
	"github.com/dlddu/homelab-k3s-mcp/internal/github"
	"github.com/dlddu/homelab-k3s-mcp/internal/grafana"
	"github.com/dlddu/homelab-k3s-mcp/internal/k8s"
)

// recordingSink keeps records as values so a case can count them and read
// fields, which is what AC1 is about; the text a collector sees is checked
// separately through eventlog.Attrs.
type recordingSink struct{ records []eventlog.Record }

func (s *recordingSink) Emit(_ context.Context, r eventlog.Record) {
	s.records = append(s.records, r)
}

// recordText renders records the way the shipped sink would, so the
// non-exposure sweep (AC3) searches the same bytes a collector would store.
func recordText(t *testing.T, records []eventlog.Record) string {
	t.Helper()
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, nil))
	for _, r := range records {
		logger.LogAttrs(context.Background(), slog.LevelInfo, eventlog.Message, eventlog.Attrs(r)...)
	}
	return buf.String()
}

type secretGitHub struct{ token string }

func (g secretGitHub) CreateInstallationToken(context.Context, []string, map[string]any) (*github.InstallationToken, error) {
	return &github.InstallationToken{Token: g.token, ExpiresAt: "2026-09-21T09:00:00Z"}, nil
}

func (g secretGitHub) CreateCommitStatus(context.Context, github.CommitStatusInput) (*github.CommitStatus, error) {
	return nil, errors.New("unused")
}

type secretGrafana struct{ token string }

func (g secretGrafana) CreateToken(context.Context) (*grafana.Credentials, error) {
	return &grafana.Credentials{Token: g.token, MetricsURL: "https://metrics.example.test", MetricsUser: "1"}, nil
}

func recordedHandler(t *testing.T, gate gatekeeper.Gate) (*Handler, *countingK8s, *recordingSink) {
	t.Helper()
	h, fake := testHandler(t, gate, toolRegistry)
	sink := &recordingSink{}
	h.events = sink
	return h, fake, sink
}

func lastRecord(t *testing.T, sink *recordingSink, wantCount int) eventlog.Record {
	t.Helper()
	if len(sink.records) != wantCount {
		t.Fatalf("recorded %d records, want %d: %+v", len(sink.records), wantCount, sink.records)
	}
	return sink.records[wantCount-1]
}

// AC1: one record per call, whatever class of tool ran, and the coordinate
// only where the tool has one.
func TestEveryToolCallLeavesExactlyOneRecord(t *testing.T) {
	h, _, sink := recordedHandler(t, &scriptedGate{err: errors.New("unused")})
	h.grafana = secretGrafana{token: "glc_unused"}

	if _, rerr := callTool(t, h, "ping", ""); rerr != nil {
		t.Fatalf("ping = %v", rerr)
	}
	got := lastRecord(t, sink, 1)
	if got.Tool != "ping" || got.Result != eventlog.ResultSuccess || got.Target != (eventlog.Target{}) {
		t.Errorf("ping record = %+v, want tool=ping result=success and an empty target", got)
	}

	if _, rerr := callTool(t, h, "resource_get", `{"apiVersion":"v1","kind":"ConfigMap","namespace":"default","name":"cm-1"}`); rerr != nil {
		t.Fatalf("resource_get = %v", rerr)
	}
	got = lastRecord(t, sink, 2)
	want := eventlog.Target{APIVersion: "v1", Kind: "ConfigMap", Namespace: "default", Name: "cm-1"}
	if got.Tool != "resource_get" || got.Result != eventlog.ResultSuccess || got.Target != want {
		t.Errorf("resource_get record = %+v, want the call's coordinate and result=success", got)
	}

	if _, rerr := callTool(t, h, "grafana_token", ""); rerr != nil {
		t.Fatalf("grafana_token = %v", rerr)
	}
	got = lastRecord(t, sink, 3)
	if got.Tool != "grafana_token" || got.Result != eventlog.ResultSuccess || got.Target != (eventlog.Target{}) {
		t.Errorf("grafana_token record = %+v, want tool=grafana_token result=success and an empty target", got)
	}
}

// AC1: the three results are told apart, and a refusal that never reached a
// handler still counts as one call.
func TestRecordResultFollowsHowTheCallEnded(t *testing.T) {
	h, fake, sink := recordedHandler(t, &scriptedGate{err: errors.New("approval rejected (request req-9)")})

	if _, rerr := callTool(t, h, "no_such_tool", ""); rerr == nil {
		t.Fatal("unknown tool = nil error, want a refusal")
	}
	if got := lastRecord(t, sink, 1); got.Tool != "no_such_tool" || got.Result != eventlog.ResultRefused {
		t.Errorf("unknown-tool record = %+v, want tool=no_such_tool result=refused", got)
	}

	if _, rerr := callTool(t, h, "resource_get", `{"kind":"ConfigMap"}`); rerr == nil {
		t.Fatal("coordinate without apiVersion = nil error, want a refusal")
	}
	if got := lastRecord(t, sink, 2); got.Result != eventlog.ResultRefused || got.Target.Kind != "ConfigMap" {
		t.Errorf("validation record = %+v, want result=refused with the partial coordinate", got)
	}

	if _, rerr := callTool(t, h, "resource_patch", `{"apiVersion":"v1","kind":"ConfigMap","namespace":"default","name":"cm-1","patchType":"merge","patch":{}}`); rerr == nil {
		t.Fatal("gated call = nil error, want the gate's refusal")
	}
	if got := lastRecord(t, sink, 3); got.Result != eventlog.ResultRefused {
		t.Errorf("gate record = %+v, want result=refused", got)
	}
	if fake.count() != 0 {
		t.Fatalf("kubernetes calls = %d, want 0 — every refusal above must be recorded as a call that never ran", fake.count())
	}

	h.github = failingGitHub{}
	if _, rerr := callTool(t, h, "github_app_installation_token", ""); rerr != nil {
		t.Fatalf("github_app_installation_token against a failing backend = %v, want a tool error, not a JSON-RPC error", rerr)
	}
	if got := lastRecord(t, sink, 4); got.Result != eventlog.ResultError || got.Reason != "" {
		t.Errorf("tool-error record = %+v, want result=error and no reason", got)
	}
}

// failingGitHub is a configured integration whose upstream failed: the
// contrast AC4 draws with an integration nobody configured.
type failingGitHub struct{}

func (failingGitHub) CreateInstallationToken(context.Context, []string, map[string]any) (*github.InstallationToken, error) {
	return nil, errors.New("github api error: 502 from api.github.com")
}

func (failingGitHub) CreateCommitStatus(context.Context, github.CommitStatusInput) (*github.CommitStatus, error) {
	return nil, errors.New("unused")
}

// AC2 + AC4: every refusal carries one of the eight reasons, and the reasons
// tell the classes apart — the arguments, the gate's five answers, an
// integration that was never configured. The response the client sees is
// unchanged in each case; only the record learned to say why.
func TestRefusalRecordsCarryTheirReason(t *testing.T) {
	coordinate := `{"apiVersion":"v1","kind":"ConfigMap","namespace":"default","name":"cm-1"}`
	patch := `{"apiVersion":"v1","kind":"ConfigMap","namespace":"default","name":"cm-1","patchType":"merge","patch":{}}`
	verdict := func(v gatekeeper.Verdict, id string) error {
		return &gatekeeper.Refusal{Verdict: v, RequestID: id, Err: errors.New("refusing resource_patch: " + string(v))}
	}
	cases := []struct {
		name    string
		gate    gatekeeper.Gate
		tool    string
		args    string
		reason  eventlog.Reason
		gateRec eventlog.Gate
		rpc     bool // the client sees a JSON-RPC error rather than a tool error
	}{
		{name: "unknown tool", gate: &scriptedGate{}, tool: "no_such_tool", reason: eventlog.ReasonInvalidInput, rpc: true},
		{name: "bad coordinate", gate: &scriptedGate{}, tool: "resource_get", args: `{"kind":"ConfigMap"}`, reason: eventlog.ReasonInvalidInput, rpc: true},
		{name: "gate rejected", gate: &scriptedGate{err: verdict(gatekeeper.VerdictRejected, "req-1")}, tool: "resource_patch", args: patch,
			reason: eventlog.ReasonGateRejected, gateRec: eventlog.Gate{RequestID: "req-1", Decision: "rejected"}, rpc: true},
		{name: "gate expired", gate: &scriptedGate{err: verdict(gatekeeper.VerdictExpired, "req-2")}, tool: "resource_patch", args: patch,
			reason: eventlog.ReasonGateExpired, gateRec: eventlog.Gate{RequestID: "req-2", Decision: "expired"}, rpc: true},
		{name: "gate timeout", gate: &scriptedGate{err: verdict(gatekeeper.VerdictTimeout, "req-3")}, tool: "resource_patch", args: patch,
			reason: eventlog.ReasonGateTimeout, gateRec: eventlog.Gate{RequestID: "req-3", Decision: "timeout"}, rpc: true},
		{name: "gate unreachable", gate: &scriptedGate{err: verdict(gatekeeper.VerdictUnreachable, "")}, tool: "resource_patch", args: patch,
			reason: eventlog.ReasonGateUnreachable, gateRec: eventlog.Gate{Decision: "unreachable"}, rpc: true},
		{name: "gate unconfigured", gate: gatekeeper.NewUnavailable(nil), tool: "resource_patch", args: patch,
			reason: eventlog.ReasonGateUnconfigured, gateRec: eventlog.Gate{Decision: "unconfigured"}, rpc: true},
		{name: "gate answered with prose only", gate: &scriptedGate{err: errors.New("something went wrong")}, tool: "resource_patch", args: patch,
			reason: eventlog.ReasonGateUnreachable, rpc: true},
		{name: "integration unconfigured", gate: &scriptedGate{}, tool: "resource_get", args: coordinate, reason: eventlog.ReasonUnconfigured},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h, fake, sink := recordedHandler(t, tc.gate)
			if tc.reason == eventlog.ReasonUnconfigured {
				h.k8s = k8s.NewUnavailable("kubernetes integration is disabled")
			}
			_, rerr := callTool(t, h, tc.tool, tc.args)
			if (rerr != nil) != tc.rpc {
				t.Fatalf("rpc error = %v, want json-rpc error %v — the response shape must not have changed", rerr, tc.rpc)
			}
			got := lastRecord(t, sink, 1)
			if got.Result != eventlog.ResultRefused || got.Reason != tc.reason || got.Gate != tc.gateRec {
				t.Errorf("record = %+v, want result=refused reason=%s gate=%+v", got, tc.reason, tc.gateRec)
			}
			if fake.count() != 0 {
				t.Errorf("kubernetes calls = %d, want 0 — a refusal is a call that never ran", fake.count())
			}
		})
	}
}

// AC2: an approved call carries the request id it ran under, and an
// AUTO_APPROVE'd one says so as a field — the two are told apart by nothing
// else, since the verdict is "approved" on both.
func TestApprovedRecordsCarryTheRequestIdAndTheAutoApprovalFlag(t *testing.T) {
	patch := `{"apiVersion":"v1","kind":"ConfigMap","namespace":"default","name":"cm-1","patchType":"merge","patch":{}}`
	for name, auto := range map[string]bool{"human": false, "auto": true} {
		t.Run(name, func(t *testing.T) {
			h, _, sink := recordedHandler(t, &scriptedGate{decision: &gatekeeper.Decision{RequestID: "req-" + name, AutoApproved: auto}})
			if _, rerr := callTool(t, h, "resource_patch", patch); rerr != nil {
				t.Fatalf("resource_patch = %v", rerr)
			}
			got := lastRecord(t, sink, 1)
			want := eventlog.Gate{RequestID: "req-" + name, Decision: "approved", AutoApproved: auto}
			if got.Result != eventlog.ResultSuccess || got.Reason != "" || got.Gate != want {
				t.Errorf("record = %+v, want result=success and gate=%+v", got, want)
			}
		})
	}
}

// AC2's two fields disagree on purpose when the gate layer withholds an
// approval the backend granted (AC7): the verdict stays "approved", the
// reason says the call was refused by the gate all the same.
func TestASpentApprovalIsRefusedWithTheVerdictKept(t *testing.T) {
	decision := &gatekeeper.Decision{RequestID: "req-spent"}
	if err := decision.Consume(); err != nil {
		t.Fatal(err)
	}
	h, _, sink := recordedHandler(t, &scriptedGate{decision: decision})
	if _, rerr := callTool(t, h, "resource_patch", `{"apiVersion":"v1","kind":"ConfigMap","namespace":"default","name":"cm-1","patchType":"merge","patch":{}}`); rerr == nil {
		t.Fatal("a spent approval ran the call")
	}
	got := lastRecord(t, sink, 1)
	want := eventlog.Gate{RequestID: "req-spent", Decision: "approved"}
	if got.Result != eventlog.ResultRefused || got.Reason != eventlog.ReasonGateRejected || got.Gate != want {
		t.Errorf("record = %+v, want result=refused reason=gate_rejected gate=%+v", got, want)
	}
}

// AC1: the principal on the record is whoever auth put on the context.
func TestRecordNamesThePrincipalAuthEstablished(t *testing.T) {
	h, _, sink := recordedHandler(t, &scriptedGate{})
	ctx := eventlog.WithPrincipal(context.Background(), eventlog.Principal{Method: "jwt", ID: "operator@example.test"})
	if _, rerr := h.toolsCall(ctx, json.RawMessage(`{"name":"ping"}`)); rerr != nil {
		t.Fatalf("ping = %v", rerr)
	}
	if got := lastRecord(t, sink, 1).Principal.String(); got != "jwt:operator@example.test" {
		t.Errorf("principal = %q, want jwt:operator@example.test", got)
	}
}

// AC3: the four classes the PRD names — issued credentials, Secret data,
// exec payloads, request/response bodies — are searched for as values in the
// rendered records and must not be found. The tool responses are checked to
// contain them first, so the negative assertion is about the record and not
// about a fixture that never carried the value.
func TestRecordsCarryNoCredentialsPayloadsOrBodies(t *testing.T) {
	const (
		ghToken    = "ghs_RECORDS_MUST_NOT_CARRY_THIS_TOKEN"
		grafanaTok = "glc_RECORDS_MUST_NOT_CARRY_THIS_EITHER"
		secretData = "c2VjcmV0LXZhbHVlLXRoYXQtbXVzdC1zdGF5LW91dA=="
		execStdout = "stdout-body-that-must-stay-out"
		execArg    = "argument-that-must-stay-out"
		rawJWT     = "eyJhbGciOiJSUzI1NiJ9.RAW-JWT-THAT-MUST-STAY-OUT.sig"
	)

	approved := &scriptedGate{decision: &gatekeeper.Decision{RequestID: "req-ac3"}}
	h, fake, sink := recordedHandler(t, approved)
	h.github = secretGitHub{token: ghToken}
	h.grafana = secretGrafana{token: grafanaTok}
	fake.execOutcome = &k8s.ExecOutcome{Stdout: execStdout, Success: true}

	// The principal was authenticated with rawJWT; only its subject may
	// appear.
	ctx := eventlog.WithPrincipal(context.Background(), eventlog.Principal{Method: "jwt", ID: "operator@example.test"})
	call := func(name, args string) any {
		t.Helper()
		if args == "" {
			args = "{}"
		}
		result, rerr := h.toolsCall(ctx, json.RawMessage(`{"name":"`+name+`","arguments":`+args+`}`))
		if rerr != nil {
			t.Fatalf("%s = %v", name, rerr)
		}
		return result
	}
	rendered := func(v any) string {
		b, err := json.Marshal(v)
		if err != nil {
			t.Fatal(err)
		}
		return string(b)
	}

	if out := rendered(call("github_app_installation_token", "")); !strings.Contains(out, ghToken) {
		t.Fatalf("github response %q does not carry the token; the sweep below would be vacuous", out)
	}
	if out := rendered(call("grafana_token", "")); !strings.Contains(out, grafanaTok) {
		t.Fatalf("grafana response %q does not carry the token; the sweep below would be vacuous", out)
	}
	execArgs := `{"apiVersion":"v1","kind":"Pod","namespace":"default","name":"pod-1","container":"app","command":["sh","-c","` + execArg + `"]}`
	if out := rendered(call("resource_exec", execArgs)); !strings.Contains(out, execStdout) {
		t.Fatalf("exec response %q does not carry the stdout; the sweep below would be vacuous", out)
	}
	// A Secret write carries the data in the request body; the approval
	// gate is scripted to approve so the call runs end to end.
	// An approval is spent by the exec above (AC7), so the patch needs its own.
	approved.decision = &gatekeeper.Decision{RequestID: "req-ac3-patch"}
	secretArgs := `{"apiVersion":"v1","kind":"Secret","namespace":"default","name":"s-1","patchType":"merge","patch":{"data":{"password":"` + secretData + `"}}}`
	call("resource_patch", secretArgs)
	if got := fake.patch(); !strings.Contains(string(got.Patch), secretData) {
		t.Fatalf("the Secret patch %q never carried the data; the sweep below would be vacuous", got.Patch)
	}

	if len(sink.records) != 4 {
		t.Fatalf("recorded %d records, want 4", len(sink.records))
	}
	text := recordText(t, sink.records)
	for _, forbidden := range []string{ghToken, grafanaTok, secretData, execStdout, execArg, rawJWT, `"data"`, "password", "sh -c", "command"} {
		if strings.Contains(text, forbidden) {
			t.Errorf("records carry %q:\n%s", forbidden, text)
		}
	}
	for _, want := range []string{"tool=resource_exec", "target.kind=Pod", "target.name=pod-1", "tool=resource_patch", "target.kind=Secret", "principal=jwt:operator@example.test", "result=success"} {
		if !strings.Contains(text, want) {
			t.Errorf("records are missing %q — the sweep passed on records that say nothing:\n%s", want, text)
		}
	}
}
