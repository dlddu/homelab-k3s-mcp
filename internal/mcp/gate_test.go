package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"testing"

	"github.com/dlddu/homelab-k3s-mcp/internal/gatekeeper"
	"github.com/dlddu/homelab-k3s-mcp/internal/k8s"
)

// countingK8s is the "fake kubernetes service" the approval-gate ACs verify
// against: what matters is not what it returns but how many times it was
// reached. A refused call that still incremented this counter would mean the
// gate ran after the fact.
type countingK8s struct {
	mu    sync.Mutex
	calls int
}

func (c *countingK8s) count() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.calls
}

func (c *countingK8s) hit() {
	c.mu.Lock()
	c.calls++
	c.mu.Unlock()
}

func (c *countingK8s) ListNamespaces(context.Context) ([]any, error) { c.hit(); return []any{}, nil }

func (c *countingK8s) ListWorkloads(context.Context, k8s.WorkloadKind, *string) ([]any, error) {
	c.hit()
	return []any{}, nil
}

func (c *countingK8s) RolloutRestart(context.Context, k8s.WorkloadKind, string, string) (string, error) {
	c.hit()
	return "now", nil
}

func (c *countingK8s) ExecInPod(context.Context, string, string, *string, []string) (*k8s.ExecOutcome, error) {
	c.hit()
	return &k8s.ExecOutcome{Success: true}, nil
}

func (c *countingK8s) ScaleWorkload(context.Context, k8s.WorkloadKind, string, string, int32) (int32, error) {
	c.hit()
	return 1, nil
}

func (c *countingK8s) WorkloadLogs(context.Context, k8s.WorkloadKind, string, string, k8s.LogOptions) (*k8s.LogResult, error) {
	c.hit()
	return &k8s.LogResult{}, nil
}

func (c *countingK8s) DescribePod(context.Context, string, k8s.PodTarget) (*k8s.PodDescription, error) {
	c.hit()
	return &k8s.PodDescription{}, nil
}

// scriptedGate answers with one prepared decision (or one refusal) and records
// what it was asked to approve.
type scriptedGate struct {
	decision *gatekeeper.Decision
	err      error
	calls    []gatekeeper.Call
}

func (g *scriptedGate) Authorize(_ context.Context, call gatekeeper.Call) (*gatekeeper.Decision, error) {
	g.calls = append(g.calls, call)
	if g.err != nil {
		return nil, g.err
	}
	return g.decision, nil
}

// testHandler builds a handler over a synthetic registry. Production ships no
// gated tool yet — the gate exists for the resource_* family that is still
// unimplemented — so the enforcement path has to be exercised against a tool
// declared here.
func testHandler(t *testing.T, gate gatekeeper.Gate, registry map[string]toolEntry) (*Handler, *countingK8s) {
	t.Helper()
	fake := &countingK8s{}
	h := &Handler{
		k8s:            fake,
		gate:           gate,
		sensitiveKinds: []string{defaultSensitiveKind},
		registry:       registry,
	}
	return h, fake
}

// reachesKubernetes is a handler that does nothing but touch the cluster, so a
// test can tell "the tool ran" from "the tool was refused".
func reachesKubernetes(h *Handler, ctx context.Context, _ json.RawMessage) (any, *rpcErr) {
	return h.namespaceList(ctx)
}

func callTool(t *testing.T, h *Handler, name string, args string) (any, *rpcErr) {
	t.Helper()
	if args == "" {
		args = "{}"
	}
	params := json.RawMessage(`{"name":"` + name + `","arguments":` + args + `}`)
	return h.toolsCall(context.Background(), params)
}

// AC1: what tools/list advertises and what the dispatcher can run must be the
// same set. A name on one side only is either unreachable or — worse — a tool
// running without anyone having declared the pairs it exercises.
func TestProductionRegistryMatchesAdvertisedTools(t *testing.T) {
	if err := Validate(); err != nil {
		t.Fatalf("Validate() = %v, want the shipped registry to be consistent", err)
	}
}

// AC1: registering a tool nobody declared has to stop startup rather than pass
// silently.
func TestValidateRegistryRejectsMismatches(t *testing.T) {
	cases := []struct {
		name       string
		registered map[string]toolEntry
		advertised []string
		want       string
	}{
		{
			name:       "advertised without a declaration",
			registered: map[string]toolEntry{},
			advertised: []string{"resource_delete"},
			want:       "advertised but not registered",
		},
		{
			name:       "registered without being advertised",
			registered: map[string]toolEntry{"secret_tool": {handle: reachesKubernetes}},
			advertised: []string{},
			want:       "registered but not advertised",
		},
		{
			name:       "declared without a handler",
			registered: map[string]toolEntry{"resource_delete": {}},
			advertised: []string{"resource_delete"},
			want:       "has no handler",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := validateRegistry(tc.registered, tc.advertised)
			if err == nil {
				t.Fatal("validateRegistry() = nil, want a startup-stopping error")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error = %q, want it to mention %q", err, tc.want)
			}
		})
	}

	if err := validateRegistry(map[string]toolEntry{"ping": {handle: reachesKubernetes}}, []string{"ping"}); err != nil {
		t.Errorf("validateRegistry() on a consistent pair = %v, want nil", err)
	}
}

// AC1/AC5: a state-changing call with no approval never reaches the cluster.
func TestGatedCallIsRefusedBeforeKubernetes(t *testing.T) {
	for _, verb := range []string{"create", "update", "patch", "delete", "deletecollection"} {
		t.Run(verb, func(t *testing.T) {
			registry := map[string]toolEntry{
				"writer": {
					decl:   toolDeclaration{pairs: []gatekeeper.Pair{{Verb: verb, Resource: "deployments"}}},
					handle: reachesKubernetes,
				},
			}
			h, fake := testHandler(t, gatekeeper.NewUnavailable(nil), registry)

			_, rerr := callTool(t, h, "writer", "")
			if rerr == nil {
				t.Fatal("tools/call = nil error, want a refusal")
			}
			if fake.count() != 0 {
				t.Errorf("kubernetes calls = %d, want 0", fake.count())
			}
		})
	}
}

// AC1: create on pods/exec is a write even though "exec" does not look like
// one, because the SPDY executor opens the stream with a POST.
func TestExecSubresourceIsGated(t *testing.T) {
	registry := map[string]toolEntry{
		"exec": {
			decl:   toolDeclaration{pairs: []gatekeeper.Pair{{Verb: "create", Resource: "pods/exec"}}},
			handle: reachesKubernetes,
		},
	}
	h, fake := testHandler(t, gatekeeper.NewUnavailable(nil), registry)

	if _, rerr := callTool(t, h, "exec", ""); rerr == nil {
		t.Fatal("tools/call = nil error, want a refusal")
	}
	if fake.count() != 0 {
		t.Errorf("kubernetes calls = %d, want 0", fake.count())
	}
}

// AC1/AC5: reads that are neither state-changing nor sensitive keep working
// while the approval path is down. If they did not, an outage in gatekeeper
// would take the whole server with it.
func TestUngatedReadsStillRunWhileTheGateRefuses(t *testing.T) {
	registry := map[string]toolEntry{
		"lister": {
			decl:   toolDeclaration{pairs: []gatekeeper.Pair{{Verb: "list", Resource: "deployments"}}},
			handle: reachesKubernetes,
		},
		"getter": {
			decl:   toolDeclaration{pairs: []gatekeeper.Pair{{Verb: "get", Resource: "pods"}}},
			handle: reachesKubernetes,
		},
		"platform": {handle: reachesKubernetes},
	}
	h, fake := testHandler(t, gatekeeper.NewUnavailable(nil), registry)

	for _, name := range []string{"lister", "getter", "platform"} {
		if _, rerr := callTool(t, h, name, ""); rerr != nil {
			t.Errorf("tools/call(%s) = %v, want it to run ungated", name, rerr)
		}
	}
	if fake.count() != 3 {
		t.Errorf("kubernetes calls = %d, want 3", fake.count())
	}
}

// AC1: a sensitive kind is the one case where a read is gated — a Secret's
// value leaves the cluster whether it was fetched or streamed.
func TestSensitiveReadsAreGated(t *testing.T) {
	registry := map[string]toolEntry{
		"secret_get":   {decl: toolDeclaration{pairs: []gatekeeper.Pair{{Verb: "get", Resource: "secrets"}}}, handle: reachesKubernetes},
		"secret_watch": {decl: toolDeclaration{pairs: []gatekeeper.Pair{{Verb: "watch", Resource: "secrets"}}}, handle: reachesKubernetes},
		"pod_watch":    {decl: toolDeclaration{pairs: []gatekeeper.Pair{{Verb: "watch", Resource: "pods"}}}, handle: reachesKubernetes},
	}
	h, fake := testHandler(t, gatekeeper.NewUnavailable(nil), registry)

	for _, name := range []string{"secret_get", "secret_watch"} {
		if _, rerr := callTool(t, h, name, ""); rerr == nil {
			t.Errorf("tools/call(%s) = nil error, want a refusal", name)
		}
	}
	if fake.count() != 0 {
		t.Fatalf("kubernetes calls after sensitive reads = %d, want 0", fake.count())
	}
	if _, rerr := callTool(t, h, "pod_watch", ""); rerr != nil {
		t.Errorf("tools/call(pod_watch) = %v, want a non-sensitive watch to run ungated", rerr)
	}
}

// AC1/AC3: the gate is handed the pair it is judging and a context that names
// the tool and carries the arguments in full.
func TestApprovedCallReachesKubernetesWithAJudgeableContext(t *testing.T) {
	gate := &scriptedGate{decision: &gatekeeper.Decision{RequestID: "req-1", ProcessedByID: "op-1"}}
	registry := map[string]toolEntry{
		"writer": {
			decl:   toolDeclaration{pairs: []gatekeeper.Pair{{Verb: "patch", Resource: "deployments"}}},
			handle: reachesKubernetes,
		},
	}
	h, fake := testHandler(t, gate, registry)

	if _, rerr := callTool(t, h, "writer", `{"patch":{"spec":{"replicas":3}}}`); rerr != nil {
		t.Fatalf("tools/call = %v, want it to run after approval", rerr)
	}
	if fake.count() != 1 {
		t.Errorf("kubernetes calls = %d, want 1", fake.count())
	}
	if len(gate.calls) != 1 {
		t.Fatalf("gate calls = %d, want 1", len(gate.calls))
	}
	ctx := gate.calls[0].Context
	for _, want := range []string{"writer", "patch on deployments", `"replicas":3`} {
		if !strings.Contains(ctx, want) {
			t.Errorf("approval context %q is missing %q", ctx, want)
		}
	}
}

// AC7: the dispatcher spends the approval, so handing back the same decision
// twice cannot buy a second call.
func TestApprovalCannotBeSpentTwice(t *testing.T) {
	gate := &scriptedGate{decision: &gatekeeper.Decision{RequestID: "req-1"}}
	registry := map[string]toolEntry{
		"writer": {
			decl:   toolDeclaration{pairs: []gatekeeper.Pair{{Verb: "delete", Resource: "deployments"}}},
			handle: reachesKubernetes,
		},
	}
	h, fake := testHandler(t, gate, registry)

	if _, rerr := callTool(t, h, "writer", ""); rerr != nil {
		t.Fatalf("first call = %v, want it to run", rerr)
	}
	if _, rerr := callTool(t, h, "writer", ""); rerr == nil {
		t.Fatal("second call reused the same approval, want a refusal")
	}
	if fake.count() != 1 {
		t.Errorf("kubernetes calls = %d, want 1 — the reused approval must not execute", fake.count())
	}
}

// AC8: an executed gated call leaves a record carrying the request id and who
// decided it; a refused one records why.
func TestGatedCallsAreAudited(t *testing.T) {
	var buf bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(previous) })

	registry := map[string]toolEntry{
		"writer": {
			decl:   toolDeclaration{pairs: []gatekeeper.Pair{{Verb: "patch", Resource: "deployments"}}},
			handle: reachesKubernetes,
		},
	}

	approved := &scriptedGate{decision: &gatekeeper.Decision{RequestID: "req-42", ExternalID: "ext-42", ProcessedByID: "op-7"}}
	h, _ := testHandler(t, approved, registry)
	if _, rerr := callTool(t, h, "writer", ""); rerr != nil {
		t.Fatalf("tools/call = %v", rerr)
	}
	for _, want := range []string{"req-42", "op-7", "writer"} {
		if !strings.Contains(buf.String(), want) {
			t.Errorf("audit log %q is missing %q", buf.String(), want)
		}
	}

	buf.Reset()
	refused := &scriptedGate{err: errors.New("approval rejected (request req-43)")}
	h, _ = testHandler(t, refused, registry)
	if _, rerr := callTool(t, h, "writer", ""); rerr == nil {
		t.Fatal("tools/call = nil error, want a refusal")
	}
	if !strings.Contains(buf.String(), "req-43") {
		t.Errorf("refusal log %q does not record why", buf.String())
	}
}

// AC9: an auto-approved execution says so in its own answer, not only in a log
// the caller never reads.
func TestAutoApprovalIsVisibleInTheToolResponse(t *testing.T) {
	gate := &scriptedGate{decision: &gatekeeper.Decision{RequestID: "req-1", AutoApproved: true}}
	registry := map[string]toolEntry{
		"writer": {
			decl:   toolDeclaration{pairs: []gatekeeper.Pair{{Verb: "patch", Resource: "deployments"}}},
			handle: reachesKubernetes,
		},
	}
	h, _ := testHandler(t, gate, registry)

	result, rerr := callTool(t, h, "writer", "")
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

// AC1: every tool that exercises a state-changing verb is either behind the
// gate or held out by a reason the documents give. This is the test that fails
// when a new write tool is added without deciding which of the two it is.
func TestEveryStateChangingToolIsGatedOrDocumented(t *testing.T) {
	for name, entry := range toolRegistry {
		changesState := false
		for _, p := range entry.decl.pairs {
			if gatedVerbs[p.Verb] {
				changesState = true
				break
			}
		}
		if !changesState {
			if entry.decl.outsideGate != "" {
				t.Errorf("%s is marked outside the gate but exercises no gated verb; drop the exemption", name)
			}
			continue
		}
		if entry.decl.outsideGate == "" && len(entry.decl.gatedPairs([]string{defaultSensitiveKind})) == 0 {
			t.Errorf("%s changes state but is neither gated nor documented as an exception", name)
		}
	}

	exemptions := Exemptions()
	if reason, ok := exemptions["dear_baby_reset_user"]; !ok || reason != exemptPendingOwnerDecision {
		t.Errorf("dear_baby_reset_user exemption = %q, want the documented pending-owner-decision reason", reason)
	}
	for _, name := range []string{"workload_restart", "workload_scale"} {
		if reason := exemptions[name]; reason != exemptRetiredPendingRemoval {
			t.Errorf("%s exemption = %q, want the retired-pending-removal reason", name, reason)
		}
	}
	if len(exemptions) != 3 {
		t.Errorf("exemptions = %v, want exactly the three documented ones", exemptions)
	}
}

// A handler built the normal way refuses gated calls, because the gate is only
// ever absent when nobody configured one (AC5).
func TestDefaultHandlerHasARefusingGate(t *testing.T) {
	h := NewHandler(k8s.NewUnavailable(""), nil, nil, nil, nil, nil)
	if h.gate == nil {
		t.Fatal("NewHandler() left the gate nil, which would mean gated calls run unattended")
	}
	if _, err := h.gate.Authorize(context.Background(), gatekeeper.Call{Tool: "x", Context: "y"}); err == nil {
		t.Error("default gate approved a call, want a refusal")
	}
}
