package mcp

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/dlddu/homelab-k3s-mcp/internal/eventlog"
	"github.com/dlddu/homelab-k3s-mcp/internal/gatekeeper"
)

// slowGate approves after a delay, standing in for the operator taking their
// time (prd-metrics AC3's "승인 대기가 길어져도").
type slowGate struct {
	scriptedGate
	delay time.Duration
}

func (g *slowGate) Authorize(ctx context.Context, call gatekeeper.Call) (*gatekeeper.Decision, error) {
	time.Sleep(g.delay)
	return g.scriptedGate.Authorize(ctx, call)
}

// Measured here rather than in the metrics package because the split is
// made where the call is timed.
func TestGateWaitIsRecordedApartFromTheCallsDuration(t *testing.T) {
	const wait = 60 * time.Millisecond
	gate := &slowGate{scriptedGate: scriptedGate{decision: &gatekeeper.Decision{RequestID: "req-slow"}}, delay: wait}
	h, _, sink := recordedHandler(t, gate)
	patch := `{"apiVersion":"v1","kind":"ConfigMap","namespace":"default","name":"cm-1","patchType":"merge","patch":{}}`
	if _, rerr := callTool(t, h, "resource_patch", patch); rerr != nil {
		t.Fatalf("resource_patch = %v", rerr)
	}
	got := lastRecord(t, sink, 1)
	if got.GateWait < wait {
		t.Errorf("GateWait = %v, want >= %v", got.GateWait, wait)
	}
	if got.Duration >= wait {
		t.Errorf("Duration = %v includes the gate wait (%v)", got.Duration, got.GateWait)
	}
	if got.Duration < 0 {
		t.Errorf("Duration = %v, want >= 0", got.Duration)
	}

	if _, rerr := callTool(t, h, "ping", ""); rerr != nil {
		t.Fatalf("ping = %v", rerr)
	}
	if got := lastRecord(t, sink, 2); got.GateWait != 0 || got.Gate.Decision != "" || got.Result != eventlog.ResultSuccess {
		t.Errorf("ping record = %+v, want no gate wait and no decision", got)
	}
}

func TestARefusedGatedCallStillCarriesItsWait(t *testing.T) {
	const wait = 40 * time.Millisecond
	refusal := &gatekeeper.Refusal{Verdict: gatekeeper.VerdictRejected, RequestID: "req-no", Err: errors.New("refusing resource_patch: approval rejected (request req-no)")}
	gate := &slowGate{scriptedGate: scriptedGate{err: refusal}, delay: wait}
	h, _, sink := recordedHandler(t, gate)
	patch := `{"apiVersion":"v1","kind":"ConfigMap","namespace":"default","name":"cm-1","patchType":"merge","patch":{}}`
	if _, rerr := callTool(t, h, "resource_patch", patch); rerr == nil {
		t.Fatal("resource_patch ran under a rejected verdict")
	}
	got := lastRecord(t, sink, 1)
	if got.GateWait < wait || got.Gate.Decision != "rejected" || got.Reason != eventlog.ReasonGateRejected {
		t.Errorf("record = %+v, want GateWait >= %v under decision=rejected", got, wait)
	}
}
