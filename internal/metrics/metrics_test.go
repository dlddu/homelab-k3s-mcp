package metrics

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
	"time"

	dto "github.com/prometheus/client_model/go"

	"github.com/dlddu/homelab-k3s-mcp/internal/eventlog"
	"github.com/dlddu/homelab-k3s-mcp/internal/gatekeeper"
)

var testTools = []string{"ping", "resource_get", "resource_patch"}

func newTest(t *testing.T) *Metrics {
	t.Helper()
	m, err := New(testTools)
	if err != nil {
		t.Fatalf("New() = %v", err)
	}
	return m
}

func scrape(t *testing.T, m *Metrics) string {
	t.Helper()
	rec := httptest.NewRecorder()
	m.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("scrape status = %d", rec.Code)
	}
	body, _ := io.ReadAll(rec.Body)
	return string(body)
}

// family returns one family's samples keyed by their label set, rendered
// as "k=v,k=v" in label order.
func family(t *testing.T, m *Metrics, name string) map[string]*dto.Metric {
	t.Helper()
	families, err := m.Gather()
	if err != nil {
		t.Fatalf("Gather() = %v", err)
	}
	for _, f := range families {
		if f.GetName() != name {
			continue
		}
		out := map[string]*dto.Metric{}
		for _, s := range f.GetMetric() {
			parts := make([]string, 0, len(s.GetLabel()))
			for _, l := range s.GetLabel() {
				parts = append(parts, l.GetName()+"="+l.GetValue())
			}
			out[strings.Join(parts, ",")] = s
		}
		return out
	}
	t.Fatalf("family %s is not exposed", name)
	return nil
}

func counter(t *testing.T, m *Metrics, name, labels string) float64 {
	t.Helper()
	s, ok := family(t, m, name)[labels]
	if !ok {
		t.Fatalf("%s{%s} is not exposed", name, labels)
	}
	return s.GetCounter().GetValue()
}

func histogramCount(t *testing.T, m *Metrics, name, labels string) uint64 {
	t.Helper()
	s, ok := family(t, m, name)[labels]
	if !ok {
		t.Fatalf("%s{%s} is not exposed", name, labels)
	}
	return s.GetHistogram().GetSampleCount()
}

// AC1: every registered tool × result is a series from the start, at 0 —
// absence and zero are distinguishable — and a call moves exactly the
// series its result names.
func TestCallsAreCountedPerToolAndResultWithUncalledToolsAtZero(t *testing.T) {
	m := newTest(t)
	calls := family(t, m, "mcp_tool_calls_total")
	if got, want := len(calls), (len(testTools)+1)*len(eventlog.Results); got != want {
		t.Fatalf("calls series = %d, want %d (registered+unregistered × results)", got, want)
	}
	for k, s := range calls {
		if s.GetCounter().GetValue() != 0 {
			t.Errorf("%s starts at %v, want 0", k, s.GetCounter().GetValue())
		}
	}

	ctx := context.Background()
	m.Emit(ctx, eventlog.Record{Tool: "resource_get", Result: eventlog.ResultSuccess})
	m.Emit(ctx, eventlog.Record{Tool: "resource_get", Result: eventlog.ResultRefused, Reason: eventlog.ReasonInvalidInput})
	m.Emit(ctx, eventlog.Record{Tool: "resource_get", Result: eventlog.ResultError})

	for _, result := range eventlog.Results {
		if got := counter(t, m, "mcp_tool_calls_total", "result="+string(result)+",tool=resource_get"); got != 1 {
			t.Errorf("calls{resource_get,%s} = %v, want 1", result, got)
		}
	}
	if got := counter(t, m, "mcp_tool_calls_total", "result=success,tool=ping"); got != 0 {
		t.Errorf("uncalled ping = %v, want 0 (and present)", got)
	}
}

// AC2: refusals are split by reason, every reason is a series from the
// start, and sum(refusals_total) == calls_total{result="refused"} holds
// across the whole closed set because the record layer has no reason
// outside it.
func TestRefusalsAreCountedPerReasonAndSumToRefusedCalls(t *testing.T) {
	m := newTest(t)
	ctx := context.Background()
	for i, reason := range eventlog.Reasons {
		for n := 0; n <= i; n++ {
			m.Emit(ctx, eventlog.Record{Tool: "resource_patch", Result: eventlog.ResultRefused, Reason: reason})
		}
	}
	var sum float64
	for i, reason := range eventlog.Reasons {
		got := counter(t, m, "mcp_tool_refusals_total", "reason="+string(reason)+",tool=resource_patch")
		if got != float64(i+1) {
			t.Errorf("refusals{%s} = %v, want %d", reason, got, i+1)
		}
		sum += got
	}
	if refused := counter(t, m, "mcp_tool_calls_total", "result=refused,tool=resource_patch"); sum != refused {
		t.Errorf("sum(refusals_total) = %v, calls_total{refused} = %v: the AC2 invariant is broken", sum, refused)
	}
	if got := len(family(t, m, "mcp_tool_refusals_total")); got != (len(testTools)+1)*len(eventlog.Reasons) {
		t.Errorf("refusal series = %d, want %d", got, (len(testTools)+1)*len(eventlog.Reasons))
	}
}

// AC3: the two histograms are separate — a long gate wait lands in
// mcp_gate_wait_seconds under its verdict and does not stretch
// mcp_tool_duration_seconds, and a refused call observes no duration at all.
func TestGateWaitIsObservedApartFromHandlerLatency(t *testing.T) {
	m := newTest(t)
	ctx := context.Background()
	m.Emit(ctx, eventlog.Record{Tool: "ping", Result: eventlog.ResultSuccess, Duration: 2 * time.Millisecond})
	m.Emit(ctx, eventlog.Record{Tool: "resource_patch", Result: eventlog.ResultSuccess,
		Duration: 3 * time.Millisecond, GateWait: 90 * time.Second,
		Gate: eventlog.Gate{RequestID: "req-1", Decision: string(gatekeeper.VerdictApproved)}})
	m.Emit(ctx, eventlog.Record{Tool: "resource_patch", Result: eventlog.ResultRefused, Reason: eventlog.ReasonGateRejected,
		Duration: time.Millisecond, GateWait: 40 * time.Second,
		Gate: eventlog.Gate{RequestID: "req-2", Decision: string(gatekeeper.VerdictRejected)}})

	durations := family(t, m, "mcp_tool_duration_seconds")
	if got := durations["tool=resource_patch"].GetHistogram().GetSampleSum(); got > 0.01 {
		t.Errorf("duration{resource_patch} sum = %v: the gate wait leaked into handler latency", got)
	}
	if got := histogramCount(t, m, "mcp_tool_duration_seconds", "tool=resource_patch"); got != 1 {
		t.Errorf("duration{resource_patch} count = %d, want 1 (the refused call ran no handler)", got)
	}
	if got := histogramCount(t, m, "mcp_tool_duration_seconds", "tool=ping"); got != 1 {
		t.Errorf("duration{ping} count = %d, want 1", got)
	}
	if got := family(t, m, "mcp_gate_wait_seconds")["decision=approved"].GetHistogram().GetSampleSum(); got != 90 {
		t.Errorf("gate_wait{approved} sum = %v, want 90", got)
	}
	if got := family(t, m, "mcp_gate_wait_seconds")["decision=rejected"].GetHistogram().GetSampleSum(); got != 40 {
		t.Errorf("gate_wait{rejected} sum = %v, want 40", got)
	}
	if got := len(family(t, m, "mcp_gate_wait_seconds")); got != len(gatekeeper.Verdicts) {
		t.Errorf("gate_wait series = %d, want %d verdicts", got, len(gatekeeper.Verdicts))
	}
	// A wait without a verdict is not a wait a human took; ping was never gated.
	if got := histogramCount(t, m, "mcp_gate_wait_seconds", "decision=approved"); got != 1 {
		t.Errorf("gate_wait{approved} count = %d, want 1", got)
	}
}

// AC4: the label names are the table's and nothing on a record but the
// tool, result, reason and verdict reaches a label — coordinates, principals
// and caller-supplied tool names do not, and the series count does not move
// with what was called.
func TestLabelsAreClosedAndCallerTextNeverBecomesASeries(t *testing.T) {
	m := newTest(t)
	ctx := context.Background()
	before := scrape(t, m)
	seriesBefore := len(sampleLines(before))

	for i := 0; i < 50; i++ {
		m.Emit(ctx, eventlog.Record{
			Tool:      "resource_get",
			Principal: eventlog.Principal{Method: "jwt", ID: "alice@example.com"},
			Target:    eventlog.Target{APIVersion: "widgets.example.com/v1", Kind: "Widget", Namespace: "ns-" + string(rune('a'+i%26)), Name: "w"},
			Result:    eventlog.ResultSuccess,
		})
	}
	m.Emit(ctx, eventlog.Record{Tool: "../../etc/passwd", Result: eventlog.ResultRefused, Reason: eventlog.ReasonInvalidInput})
	m.Emit(ctx, eventlog.Record{Tool: "", Result: eventlog.ResultRefused, Reason: eventlog.ReasonAuthFailed})

	after := scrape(t, m)
	if got := len(sampleLines(after)); got != seriesBefore {
		t.Errorf("series count moved %d -> %d: a label took a value the server did not enumerate", seriesBefore, got)
	}
	for _, leak := range []string{"Widget", "widgets.example.com", "ns-", "alice", "passwd", `tool=""`} {
		if strings.Contains(after, leak) {
			t.Errorf("exposition carries %q", leak)
		}
	}
	labelNames := map[string]bool{}
	for _, match := range regexp.MustCompile(`\{([^}]*)\}`).FindAllStringSubmatch(after, -1) {
		for _, pair := range strings.Split(match[1], ",") {
			labelNames[strings.SplitN(pair, "=", 2)[0]] = true
		}
	}
	want := map[string]bool{"tool": true, "result": true, "reason": true, "decision": true, "le": true}
	for name := range labelNames {
		if !want[name] {
			t.Errorf("label %q is exposed; the closed set is tool, result, reason, decision (+le)", name)
		}
	}
	if got := counter(t, m, "mcp_tool_refusals_total", "reason=invalid_input,tool="+Unregistered); got != 1 {
		t.Errorf("unregistered fold = %v, want 1", got)
	}
	if got := counter(t, m, "mcp_tool_refusals_total", "reason=auth_failed,tool="+Unregistered); got != 1 {
		t.Errorf("auth-refused call with no tool = %v, want 1 under the fold", got)
	}
}

// AC4: the fold value cannot be shadowed by a tool of the same name.
func TestNewRefusesAToolNamedLikeTheFold(t *testing.T) {
	if _, err := New([]string{"ping", Unregistered}); err == nil {
		t.Fatal("New() accepted a tool named like the fold value")
	}
}

// AC5: the exposition is text with the four families and nothing that
// could execute or return anything — no other family is registered.
func TestExpositionCarriesExactlyTheFourFamilies(t *testing.T) {
	m := newTest(t)
	families, err := m.Gather()
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]bool{}
	for _, f := range families {
		got[f.GetName()] = true
	}
	want := []string{"mcp_tool_calls_total", "mcp_tool_refusals_total", "mcp_tool_duration_seconds", "mcp_gate_wait_seconds"}
	if len(got) != len(want) {
		t.Errorf("families = %v, want exactly %v", got, want)
	}
	for _, name := range want {
		if !got[name] {
			t.Errorf("family %s missing", name)
		}
	}
	rec := httptest.NewRecorder()
	m.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/plain") {
		t.Errorf("Content-Type = %q, want the text exposition", ct)
	}
}

func sampleLines(exposition string) []string {
	var lines []string
	for _, line := range strings.Split(exposition, "\n") {
		if line != "" && !strings.HasPrefix(line, "#") {
			lines = append(lines, line)
		}
	}
	return lines
}
