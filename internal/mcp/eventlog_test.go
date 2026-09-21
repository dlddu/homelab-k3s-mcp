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

	h.k8s = k8s.NewUnavailable("kubernetes integration is disabled")
	if _, rerr := callTool(t, h, "resource_get", `{"apiVersion":"v1","kind":"ConfigMap","namespace":"default","name":"cm-1"}`); rerr != nil {
		t.Fatalf("resource_get against an unavailable client = %v, want a tool error, not a JSON-RPC error", rerr)
	}
	if got := lastRecord(t, sink, 4); got.Result != eventlog.ResultError {
		t.Errorf("tool-error record = %+v, want result=error", got)
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
