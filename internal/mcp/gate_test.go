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
	mu         sync.Mutex
	calls      int
	lastPatch  k8s.PatchRef
	lastUpdate k8s.UpdateRef
	lastWatch  k8s.WatchQuery
	lastDelete k8s.DeleteRef
	// updateErr lets a case make the cluster layer refuse. The one refusal AC8
	// names — a kind with no replicas — is discovery's answer rather than an
	// argument this level can see, so a fake that only ever succeeds cannot
	// exercise it.
	updateErr error
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

func (c *countingK8s) CreateResource(context.Context, k8s.CreateRef) (*k8s.ResourceResult, error) {
	c.hit()
	return &k8s.ResourceResult{Resource: "configmaps"}, nil
}

func (c *countingK8s) APIResources(context.Context) ([]k8s.APIResource, error) {
	c.hit()
	return []k8s.APIResource{}, nil
}

func (c *countingK8s) ListResources(context.Context, k8s.ListQuery) (*k8s.ListResult, error) {
	c.hit()
	return &k8s.ListResult{}, nil
}

func (c *countingK8s) GetResource(context.Context, k8s.ResourceRef) (*k8s.ResourceResult, error) {
	c.hit()
	return &k8s.ResourceResult{Object: map[string]any{}}, nil
}

// WatchResources records its query for the reason PatchResource records its
// ref: the window and the resume point the tool sent are themselves the
// assertion (AC6), and a refused call has to be visible as a call that never
// arrived.
func (c *countingK8s) WatchResources(_ context.Context, q k8s.WatchQuery) (*k8s.WatchResult, error) {
	c.hit()
	c.mu.Lock()
	c.lastWatch = q
	c.mu.Unlock()
	return &k8s.WatchResult{Resource: "deployments", Events: []k8s.WatchEvent{}}, nil
}

func (c *countingK8s) watch() k8s.WatchQuery {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.lastWatch
}

// PatchResource records what it was handed as well as counting the call: a
// patch is the one verb whose *body* the tests need, because "the server sent
// the operator's bytes on unchanged" is itself an assertion (AC9).
func (c *countingK8s) PatchResource(_ context.Context, ref k8s.PatchRef) (*k8s.ResourceResult, error) {
	c.hit()
	c.mu.Lock()
	c.lastPatch = ref
	c.mu.Unlock()
	return &k8s.ResourceResult{Resource: "deployments", Object: map[string]any{}}, nil
}

// UpdateResource records its ref for the same reason PatchResource does: what
// the tool sent is the assertion (AC8), and the scale half of this tool builds
// a body the caller never wrote.
func (c *countingK8s) UpdateResource(_ context.Context, ref k8s.UpdateRef) (*k8s.ResourceResult, error) {
	c.hit()
	c.mu.Lock()
	c.lastUpdate = ref
	err := c.updateErr
	c.mu.Unlock()
	if err != nil {
		return nil, err
	}
	return &k8s.ResourceResult{Resource: "deployments/scale", Object: map[string]any{}}, nil
}

func (c *countingK8s) update() k8s.UpdateRef {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.lastUpdate
}

func (c *countingK8s) patch() k8s.PatchRef {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.lastPatch
}

// DeleteResource records its ref for the same reason the two above do: what the
// tool sent is the assertion (AC10), and a nil grace period and a zero one are
// different requests that a counter cannot tell apart.
func (c *countingK8s) DeleteResource(_ context.Context, ref k8s.DeleteRef) (*k8s.ResourceResult, error) {
	c.hit()
	c.mu.Lock()
	c.lastDelete = ref
	c.mu.Unlock()
	return &k8s.ResourceResult{Resource: "configmaps", Namespace: "ops"}, nil
}

func (c *countingK8s) delete() k8s.DeleteRef {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.lastDelete
}

func (c *countingK8s) ExecInPod(context.Context, string, string, *string, []string) (*k8s.ExecOutcome, error) {
	c.hit()
	return &k8s.ExecOutcome{Success: true}, nil
}

// scriptedGate answers with one prepared decision (or one refusal) and records
// what it was asked to approve.
type scriptedGate struct {
	decision *gatekeeper.Decision
	err      error
	calls    []gatekeeper.Call
	contexts []string
}

// stateReader is the gate's own read surface, faked. It is a separate type from
// countingK8s because one counter cannot mean both "the tool reached kubernetes"
// and "the gate read the target it is describing" — k8s.TargetReader holds why.
type stateReader struct {
	mu    sync.Mutex
	reads []k8s.TargetRef
	state k8s.TargetState
	// next is what the second read answers, standing in for someone else
	// changing the object while the operator deliberated (AC6).
	next *k8s.TargetState
	err  error
}

func newStateReader() *stateReader {
	return &stateReader{state: k8s.TargetState{ResourceVersion: "100", UID: "uid-1"}}
}

func (r *stateReader) ReadTarget(_ context.Context, ref k8s.TargetRef) (*k8s.TargetState, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.reads = append(r.reads, ref)
	if r.err != nil {
		return nil, r.err
	}
	if len(r.reads) > 1 && r.next != nil {
		return r.next, nil
	}
	state := r.state
	return &state, nil
}

func (r *stateReader) observed() []k8s.TargetRef {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]k8s.TargetRef(nil), r.reads...)
}

// Authorize renders the context before deciding, the way Client does. Holding
// the rendering until this point is what AC5 requires of the real gate, so a
// double that rendered eagerly would let a case pass that production refuses.
func (g *scriptedGate) Authorize(ctx context.Context, call gatekeeper.Call) (*gatekeeper.Decision, error) {
	g.calls = append(g.calls, call)
	text, err := call.Describe(ctx)
	if err != nil {
		return nil, err
	}
	g.contexts = append(g.contexts, text)
	if g.err != nil {
		return nil, g.err
	}
	return g.decision, nil
}

// context reports what the operator would have been shown for the nth call.
func (g *scriptedGate) context(n int) string {
	if n >= len(g.contexts) {
		return ""
	}
	return g.contexts[n]
}

// testHandler builds a handler over a registry the caller chooses. Most write
// cases still pass a synthetic one: resource_patch is the only shipped tool that
// exercises a state-changing verb, so the other five verbs have no production
// tool to drive them and are declared here instead. The cases that are about
// resource_patch itself pass toolRegistry — a synthetic stand-in would assert
// the test's own declaration rather than the shipped one.
func testHandler(t *testing.T, gate gatekeeper.Gate, registry map[string]toolEntry) (*Handler, *countingK8s) {
	t.Helper()
	h, fake, _ := testHandlerReading(t, gate, registry, newStateReader())
	return h, fake
}

// testHandlerReading is testHandler with the gate's reader in the caller's
// hands, for the cases where what the gate read — or could not read — is the
// assertion (AC6, AC11).
func testHandlerReading(t *testing.T, gate gatekeeper.Gate, registry map[string]toolEntry, reader *stateReader) (*Handler, *countingK8s, *stateReader) {
	t.Helper()
	fake := &countingK8s{}
	h := &Handler{
		k8s:            fake,
		gate:           gate,
		gateReader:     reader,
		sensitiveKinds: []string{defaultSensitiveKind},
		registry:       registry,
	}
	return h, fake, reader
}

// reachesKubernetes is a handler that does nothing but touch the cluster, so a
// test can tell "the tool ran" from "the tool was refused".
func reachesKubernetes(h *Handler, ctx context.Context, _ json.RawMessage) (any, *rpcErr) {
	return h.apiResources(ctx)
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
	ctx := gate.context(0)
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
		gated, err := entry.decl.gatedPairs([]string{defaultSensitiveKind}, json.RawMessage(`{}`))
		if entry.decl.outsideGate == "" && err == nil && len(gated) == 0 {
			t.Errorf("%s changes state but is neither gated nor documented as an exception", name)
		}
	}

	exemptions := Exemptions()
	if reason, ok := exemptions["dear_baby_reset_user"]; !ok || reason != exemptPendingOwnerDecision {
		t.Errorf("dear_baby_reset_user exemption = %q, want the documented pending-owner-decision reason", reason)
	}
	if len(exemptions) != 1 {
		t.Errorf("exemptions = %v, want exactly the one documented exception", exemptions)
	}
}

// A handler built the normal way refuses gated calls, because the gate is only
// ever absent when nobody configured one (AC5).
func TestDefaultHandlerHasARefusingGate(t *testing.T) {
	h := NewHandler(k8s.NewUnavailable(""), nil, nil, nil, nil, nil)
	if h.gate == nil {
		t.Fatal("NewHandler() left the gate nil, which would mean gated calls run unattended")
	}
	if _, err := h.gate.Authorize(context.Background(), gatekeeper.Call{Tool: "x", Describe: func(context.Context) (string, error) { return "y", nil }}); err == nil {
		t.Error("default gate approved a call, want a refusal")
	}
}

// AC16 (read half): a sensitive kind is decided from the call's own arguments,
// before anything is asked of the cluster. The counter is the assertion — a
// refusal that still reached kubernetes would mean the value was already read
// by the time the operator was asked.
func TestSensitiveGenericReadIsRefusedBeforeKubernetes(t *testing.T) {
	h, fake := testHandler(t, gatekeeper.NewUnavailable(nil), toolRegistry)

	_, rerr := callTool(t, h, "resource_get", `{"apiVersion":"v1","kind":"Secret","namespace":"default","name":"db"}`)
	if rerr == nil {
		t.Fatal("resource_get on a Secret was allowed with no gate configured")
	}
	if fake.count() != 0 {
		t.Errorf("kubernetes calls = %d, want 0 — the read happened before the refusal", fake.count())
	}
}

// AC16: the additions stop at the read half of sensitive kinds. Ordinary kinds
// read ungated, and list stays outside the gate for every kind because the
// table is rendered server-side and carries no values (AC17).
func TestOrdinaryReadsAndSecretListsStayUngated(t *testing.T) {
	h, fake := testHandler(t, gatekeeper.NewUnavailable(nil), toolRegistry)

	if _, rerr := callTool(t, h, "resource_get", `{"apiVersion":"v1","kind":"ConfigMap","namespace":"default","name":"app"}`); rerr != nil {
		t.Fatalf("resource_get on a ConfigMap was refused: %v", rerr.message)
	}
	if _, rerr := callTool(t, h, "resource_list", `{"apiVersion":"v1","kind":"Secret"}`); rerr != nil {
		t.Fatalf("resource_list on Secrets was refused: %v", rerr.message)
	}
	if fake.count() != 2 {
		t.Errorf("kubernetes calls = %d, want 2 — both ungated reads should have run", fake.count())
	}
}

// AC5: a call whose coordinate cannot be read is a call whose pair cannot be
// named, and an unnamed pair cannot be judged sensitive or not.
func TestGenericCallWithoutACoordinateIsRefused(t *testing.T) {
	h, fake := testHandler(t, gatekeeper.NewUnavailable(nil), toolRegistry)

	if _, rerr := callTool(t, h, "resource_get", `{"namespace":"default","name":"db"}`); rerr == nil {
		t.Error("resource_get without apiVersion/kind was allowed")
	}
	if fake.count() != 0 {
		t.Errorf("kubernetes calls = %d, want 0", fake.count())
	}
}

// The declaration a generic call resolves to is the pair the approval request
// puts in front of the reviewer, subresources included.
func TestGenericPairsFollowTheCoordinate(t *testing.T) {
	cases := []struct {
		verb string
		args string
		want string
	}{
		{"list", `{"apiVersion":"v1","kind":"Pod"}`, "list on pods"},
		{"list", `{"apiVersion":"networking.k8s.io/v1","kind":"Ingress"}`, "list on ingresses"},
		{"get", `{"apiVersion":"v1","kind":"Pod","subresource":"log"}`, "get on pods/log"},
		{"get", `{"apiVersion":"v1","kind":"Secret"}`, "get on secrets"},
		{"get", `{"apiVersion":"networking.k8s.io/v1","kind":"NetworkPolicy"}`, "get on networkpolicies"},
	}
	for _, c := range cases {
		pairs, err := genericPairs(c.verb)(json.RawMessage(c.args))
		if err != nil {
			t.Fatalf("%s: %v", c.args, err)
		}
		if got := pairsText(pairs); got != c.want {
			t.Errorf("%s -> %q, want %q", c.args, got, c.want)
		}
	}
}

// A subresource of a sensitive kind is still that kind. Matching the pair as a
// whole string would have let "secrets/anything" through the read gate.
func TestSensitiveReadGateLooksThroughSubresources(t *testing.T) {
	kinds := []string{defaultSensitiveKind}
	for _, resource := range []string{"secrets", "secrets/status"} {
		if !readIsSensitive(gatekeeper.Pair{Verb: "get", Resource: resource}, kinds) {
			t.Errorf("get on %s was judged insensitive", resource)
		}
	}
	if readIsSensitive(gatekeeper.Pair{Verb: "get", Resource: "pods/log"}, kinds) {
		t.Error("get on pods/log was judged sensitive")
	}
	if readIsSensitive(gatekeeper.Pair{Verb: "list", Resource: "secrets"}, kinds) {
		t.Error("list on secrets was judged sensitive; AC16 keeps list outside the gate")
	}
}

// AC3: the scale half of resource_update is the one place where listing the
// arguments verbatim is not enough (approvalContext says why). The current count
// comes from the cluster, which is the first pair AC11 declares.
func TestContextIncludesCurrentAndTargetReplicas(t *testing.T) {
	reader := newStateReader()
	current := int64(3)
	reader.state = k8s.TargetState{ResourceVersion: "100", UID: "uid-1", Replicas: &current}
	gate := &scriptedGate{decision: &gatekeeper.Decision{RequestID: "req-1"}}
	h, _, _ := testHandlerReading(t, gate, toolRegistry, reader)

	args := `{"apiVersion":"apps/v1","kind":"Deployment","namespace":"ops","name":"api","subresource":"scale","replicas":10}`
	if _, rerr := callTool(t, h, "resource_update", args); rerr != nil {
		t.Fatalf("tools/call = %v, want the approved scale to run", rerr)
	}

	ctx := gate.context(0)
	for _, want := range []string{"3 → 10", "apps/v1 Deployment/scale ops/api", "update on deployments/scale"} {
		if !strings.Contains(ctx, want) {
			t.Errorf("approval context is missing %q:\n%s", want, ctx)
		}
	}
}

// The control group for the case above, over the branch replicaChange documents:
// a count the cluster could not report is said to be unknown, not left out.
func TestContextSaysSoWhenTheCurrentReplicaCountIsUnknown(t *testing.T) {
	reader := newStateReader() // Replicas nil — a server that answered without a spec
	gate := &scriptedGate{decision: &gatekeeper.Decision{RequestID: "req-1"}}
	h, _, _ := testHandlerReading(t, gate, toolRegistry, reader)

	args := `{"apiVersion":"apps/v1","kind":"Deployment","namespace":"ops","name":"api","subresource":"scale","replicas":10}`
	if _, rerr := callTool(t, h, "resource_update", args); rerr != nil {
		t.Fatalf("tools/call = %v", rerr)
	}
	if ctx := gate.context(0); !strings.Contains(ctx, "(current unknown) → 10") {
		t.Errorf("approval context hides that the current count is unknown:\n%s", ctx)
	}
}

// AC6: what the operator approved was that object in that state. If it moved
// while they were deciding, the approval does not describe what would now
// happen, so the call is refused and a new approval is asked for rather than the
// old one being stretched to cover a different object.
func TestExecutionIsRefusedWhenTargetChangedAfterApproval(t *testing.T) {
	for _, tc := range []struct {
		name string
		tool string
		args string
		next k8s.TargetState
	}{
		{
			name: "patch",
			tool: "resource_patch",
			args: `{"apiVersion":"apps/v1","kind":"Deployment","namespace":"ops","name":"api","patchType":"merge","patch":{"spec":{"replicas":2}}}`,
			next: k8s.TargetState{ResourceVersion: "101", UID: "uid-1"},
		},
		{
			name: "scale update",
			tool: "resource_update",
			args: `{"apiVersion":"apps/v1","kind":"Deployment","namespace":"ops","name":"api","subresource":"scale","replicas":10}`,
			next: k8s.TargetState{ResourceVersion: "101", UID: "uid-1"},
		},
		{
			name: "sensitive read",
			tool: "resource_get",
			args: `{"apiVersion":"v1","kind":"Secret","namespace":"ops","name":"db"}`,
			next: k8s.TargetState{ResourceVersion: "101", UID: "uid-1"},
		},
		{
			// AC6's first paragraph is written about the target of any gated
			// call, and delete is the one where getting it wrong is least
			// recoverable: the object the operator looked at is not the object
			// that would be removed.
			name: "delete",
			tool: "resource_delete",
			args: `{"apiVersion":"v1","kind":"ConfigMap","namespace":"ops","name":"app"}`,
			next: k8s.TargetState{ResourceVersion: "101", UID: "uid-1"},
		},
		{
			// A recreated object keeps the name and is not the object that was
			// approved. AC6 says this of exec's pod and it is true of every kind.
			name: "recreated under the same name",
			tool: "resource_patch",
			args: `{"apiVersion":"apps/v1","kind":"Deployment","namespace":"ops","name":"api","patchType":"merge","patch":{"spec":{"replicas":2}}}`,
			next: k8s.TargetState{ResourceVersion: "100", UID: "uid-2"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			reader := newStateReader()
			next := tc.next
			reader.next = &next
			gate := &scriptedGate{decision: &gatekeeper.Decision{RequestID: "req-1"}}
			h, fake, _ := testHandlerReading(t, gate, toolRegistry, reader)

			_, rerr := callTool(t, h, tc.tool, tc.args)
			if rerr == nil {
				t.Fatal("tools/call = nil error, want a refusal for a target that moved after approval")
			}
			if !strings.Contains(rerr.message, "needs a new one") {
				t.Errorf("refusal = %q, want it to say a new approval is needed", rerr.message)
			}
			if fake.count() != 0 {
				t.Errorf("kubernetes calls = %d, want 0 — the stale approval must not execute", fake.count())
			}
			if len(reader.observed()) != 2 {
				t.Errorf("gate reads = %d, want 2 (once to describe, once to confirm)", len(reader.observed()))
			}
		})
	}
}

// The other half of AC6: an unchanged target executes. Without this the case
// above would also pass for an implementation that refuses everything.
func TestExecutionProceedsWhenTheTargetIsUnchanged(t *testing.T) {
	reader := newStateReader()
	gate := &scriptedGate{decision: &gatekeeper.Decision{RequestID: "req-1"}}
	h, fake, _ := testHandlerReading(t, gate, toolRegistry, reader)

	args := `{"apiVersion":"apps/v1","kind":"Deployment","namespace":"ops","name":"api","patchType":"merge","patch":{"spec":{"replicas":2}}}`
	if _, rerr := callTool(t, h, "resource_patch", args); rerr != nil {
		t.Fatalf("tools/call = %v, want an unchanged target to run", rerr)
	}
	if fake.count() != 1 {
		t.Errorf("kubernetes calls = %d, want 1", fake.count())
	}
}

// AC11's exception is chosen by the kind and not by the verb: a Secret read
// whole to learn its resourceVersion has leaked the value whether the call that
// prompted the read was a get or a patch.
func TestGateReadsSensitiveKindsAsMetadataOnly(t *testing.T) {
	for _, tc := range []struct {
		name         string
		tool         string
		args         string
		metadataOnly bool
	}{
		{"sensitive read", "resource_get", `{"apiVersion":"v1","kind":"Secret","namespace":"ops","name":"db"}`, true},
		{"sensitive write", "resource_patch", `{"apiVersion":"v1","kind":"Secret","namespace":"ops","name":"db","patchType":"merge","patch":{"stringData":{"t":"x"}}}`, true},
		{"ordinary write", "resource_patch", `{"apiVersion":"v1","kind":"ConfigMap","namespace":"ops","name":"app","patchType":"merge","patch":{"data":{"a":"b"}}}`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			reader := newStateReader()
			gate := &scriptedGate{decision: &gatekeeper.Decision{RequestID: "req-1"}}
			h, _, _ := testHandlerReading(t, gate, toolRegistry, reader)

			if _, rerr := callTool(t, h, tc.tool, tc.args); rerr != nil {
				t.Fatalf("tools/call = %v", rerr)
			}
			reads := reader.observed()
			if len(reads) == 0 {
				t.Fatal("the gate described the call without reading the target")
			}
			for i, ref := range reads {
				if ref.MetadataOnly != tc.metadataOnly {
					t.Errorf("read %d MetadataOnly = %v, want %v", i, ref.MetadataOnly, tc.metadataOnly)
				}
			}
		})
	}
}

// AC3's closing clause: a call whose target cannot be named gets no approval
// request at all. An approval screen that cannot say what it is approving turns
// the button into a formality, so the refusal comes before the request.
func TestUnresolvableTargetIsRejectedBeforeRequest(t *testing.T) {
	reader := newStateReader()
	gate := &scriptedGate{decision: &gatekeeper.Decision{RequestID: "req-1"}}
	h, fake, _ := testHandlerReading(t, gate, toolRegistry, reader)

	// A patch that names a kind but no object: enough to judge the pair, not
	// enough to describe the call.
	args := `{"apiVersion":"apps/v1","kind":"Deployment","namespace":"ops","patchType":"merge","patch":{}}`
	_, rerr := callTool(t, h, "resource_patch", args)
	if rerr == nil {
		t.Fatal("tools/call = nil error, want a refusal for a call with no target")
	}
	if len(reader.observed()) != 0 {
		t.Errorf("gate reads = %d, want 0 — there was no object to read", len(reader.observed()))
	}
	if fake.count() != 0 {
		t.Errorf("kubernetes calls = %d, want 0", fake.count())
	}
}

// AC5, at the seam this slice added: the gate and its reader fail independently.
// An approval backend that is up while the cluster client is down must refuse,
// because the alternative is an approval screen describing a state nobody read.
func TestGatedCallIsRefusedWhenTheGateCannotReadItsTarget(t *testing.T) {
	reader := newStateReader()
	reader.err = errors.New("kubernetes client unavailable")
	gate := &scriptedGate{decision: &gatekeeper.Decision{RequestID: "req-1"}}
	h, fake, _ := testHandlerReading(t, gate, toolRegistry, reader)

	args := `{"apiVersion":"apps/v1","kind":"Deployment","namespace":"ops","name":"api","patchType":"merge","patch":{}}`
	if _, rerr := callTool(t, h, "resource_patch", args); rerr == nil {
		t.Fatal("tools/call = nil error, want a refusal when the target cannot be read")
	}
	if fake.count() != 0 {
		t.Errorf("kubernetes calls = %d, want 0", fake.count())
	}
}

// AC11: the gate publishes the pairs it exercises on its own behalf, and
// exercises nothing beyond them. The declaration is the reviewable half; the
// reads recorded on the way through are the half that could drift from it.
func TestGateDeclaresTheTwoPairsItMayExercise(t *testing.T) {
	want := []gatekeeper.Pair{
		{Verb: "get", Resource: gateTargetKind},
		{Verb: "list", Resource: gateTargetKind},
	}
	got := GatePairs()
	if len(got) != len(want) {
		t.Fatalf("GatePairs() = %v, want AC11's two rows %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("GatePairs()[%d] = %v, want %v", i, got[i], want[i])
		}
	}

	// Every read the gate makes addresses one named object, which is a get. Why
	// the list half is declared with no call site is on GatePairs.
	reader := newStateReader()
	gate := &scriptedGate{decision: &gatekeeper.Decision{RequestID: "req-1"}}
	h, _, _ := testHandlerReading(t, gate, toolRegistry, reader)
	args := `{"apiVersion":"apps/v1","kind":"Deployment","namespace":"ops","name":"api","patchType":"merge","patch":{}}`
	if _, rerr := callTool(t, h, "resource_patch", args); rerr != nil {
		t.Fatalf("tools/call = %v", rerr)
	}
	for i, ref := range reader.observed() {
		if ref.Name == "" {
			t.Errorf("read %d addressed no object, which would be a list rather than a get", i)
		}
	}
}

// A gated tool whose coordinate is an argument but which declares no target
// leaves the gate unable to build AC3's detail or AC6's precondition. Refusing
// registration is not available here — the pair is only known per call — so the
// call is refused instead, which is the direction AC5 sets for anything the gate
// cannot decide.
func TestGatedToolWithNoDeclaredTargetIsRefused(t *testing.T) {
	registry := map[string]toolEntry{
		"writer": {
			decl:   toolDeclaration{resolve: genericPairs("patch")},
			handle: reachesKubernetes,
		},
	}
	gate := &scriptedGate{decision: &gatekeeper.Decision{RequestID: "req-1"}}
	h, fake, _ := testHandlerReading(t, gate, registry, newStateReader())

	_, rerr := callTool(t, h, "writer", `{"apiVersion":"apps/v1","kind":"Deployment","namespace":"ops","name":"api"}`)
	if rerr == nil {
		t.Fatal("tools/call = nil error, want a refusal for a gated tool with no target")
	}
	if !strings.Contains(rerr.message, "declares no target") {
		t.Errorf("refusal = %q, want it to name what is missing", rerr.message)
	}
	if len(gate.calls) != 0 {
		t.Errorf("gate calls = %d, want 0 — nothing describable was ever built", len(gate.calls))
	}
	if fake.count() != 0 {
		t.Errorf("kubernetes calls = %d, want 0", fake.count())
	}
}

// The read half of the rule above. A gated read with no declared target is
// described from its arguments, which is what AC3 asks of one ("어떤 종류의
// 어떤 대상을 읽는지"), and it is not refused.
//
// The shape being protected is a collection watch of a sensitive kind: it names
// a selector rather than an object, so there is no single resourceVersion for a
// precondition to be about. AC6 words its read half around one Secret being
// swapped, not around a selector's membership changing, and refusing the watch
// on the strength of a precondition the documents do not define for it would be
// this layer inventing the rule AC1 says it must not.
func TestGatedReadWithNoDeclaredTargetIsDescribedFromItsArguments(t *testing.T) {
	registry := map[string]toolEntry{
		"watcher": {
			decl:   toolDeclaration{resolve: genericPairs("watch")},
			handle: reachesKubernetes,
		},
	}
	gate := &scriptedGate{decision: &gatekeeper.Decision{RequestID: "req-1"}}
	h, fake, reader := testHandlerReading(t, gate, registry, newStateReader())

	args := `{"apiVersion":"v1","kind":"Secret","namespace":"ops","labelSelector":"app=api"}`
	if _, rerr := callTool(t, h, "watcher", args); rerr != nil {
		t.Fatalf("tools/call = %v, want an approved collection watch to run", rerr)
	}
	if len(reader.observed()) != 0 {
		t.Errorf("gate reads = %d, want 0 — there is no single object to read", len(reader.observed()))
	}
	for _, want := range []string{"watch on secrets", "app=api"} {
		if ctx := gate.context(0); !strings.Contains(ctx, want) {
			t.Errorf("approval context is missing %q:\n%s", want, ctx)
		}
	}
	if fake.count() != 1 {
		t.Errorf("kubernetes calls = %d, want 1", fake.count())
	}
}
