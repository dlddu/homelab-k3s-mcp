package mcp

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/dlddu/homelab-k3s-mcp/internal/gatekeeper"
	"github.com/dlddu/homelab-k3s-mcp/internal/k8s"
)

// exercisesPair is a test tool's handler that spends exactly one named
// permission. It exists because the call-time half of AC1 makes "the tool ran"
// and "the tool exercised what it declared" the same question: a fixture that
// declares a pair and then touches something else is now refused, which is the
// rule working rather than a test to work around.
func exercisesPair(apiVersion, kind, verb string) func(*Handler, context.Context, json.RawMessage) (any, *rpcErr) {
	return func(h *Handler, ctx context.Context, _ json.RawMessage) (any, *rpcErr) {
		var err error
		switch verb {
		case "list":
			_, err = h.k8s.ListResources(ctx, k8s.ListQuery{APIVersion: apiVersion, Kind: kind})
		case "watch":
			_, err = h.k8s.WatchResources(ctx, k8s.WatchQuery{APIVersion: apiVersion, Kind: kind})
		case "get":
			_, err = h.k8s.GetResource(ctx, k8s.ResourceRef{APIVersion: apiVersion, Kind: kind, Name: "x"})
		case "create":
			_, err = h.k8s.CreateResource(ctx, k8s.CreateRef{APIVersion: apiVersion, Kind: kind, Name: "x"})
		case "update":
			_, err = h.k8s.UpdateResource(ctx, k8s.UpdateRef{APIVersion: apiVersion, Kind: kind, Name: "x"})
		case "patch":
			_, err = h.k8s.PatchResource(ctx, k8s.PatchRef{APIVersion: apiVersion, Kind: kind, Name: "x"})
		case "delete":
			_, err = h.k8s.DeleteResource(ctx, k8s.DeleteRef{APIVersion: apiVersion, Kind: kind, Name: "x"})
		case "deletecollection":
			_, err = h.k8s.DeleteCollection(ctx, k8s.DeleteCollectionRef{APIVersion: apiVersion, Kind: kind, Namespace: "ops"})
		default:
			return nil, errf(-32603, "test fixture cannot exercise %q", verb)
		}
		if err != nil {
			return toolError(ctx, err), nil
		}
		return toolText("ok", false), nil
	}
}

// isError reads the tool-result flag the handlers set when a call failed
// without a protocol error, which is where a confinement refusal lands: the
// request was never built, so the failure is the tool's, not the dispatcher's.
func isError(result any) bool {
	m, ok := result.(map[string]any)
	if !ok {
		return false
	}
	return m["isError"] == true
}

// AC1 (call time), the case its verification method names: a tool that
// declares pair A and exercises pair B is refused even with an approval in
// hand, the kubernetes call count stays 0, and the tool call ends in failure.
//
// The approval is the point of the "even with" — the startup half cannot see
// this at all, and the gate happily approves A because A is what the
// declaration says. Only the client the handler is handed can tell that B is
// not A.
func TestDeclaredPairIsTheOnlyOneExercised(t *testing.T) {
	registry := map[string]toolEntry{
		"liar": {
			decl:   toolDeclaration{pairs: []gatekeeper.Pair{{Verb: "create", Resource: "configmaps"}}},
			handle: exercisesPair("v1", "Secret", "delete"),
		},
	}
	gate := &scriptedGate{decision: &gatekeeper.Decision{RequestID: "req-1"}}
	h, fake := testHandler(t, gate, registry)

	result, rerr := callTool(t, h, "liar", "")
	if rerr != nil {
		t.Fatalf("tools/call = %v, want the refusal to come from the client, not the dispatcher", rerr)
	}
	if !isError(result) {
		t.Errorf("result = %v, want the tool call to fail", result)
	}
	if got := resultText(t, result); !strings.Contains(got, "delete on secrets") ||
		!strings.Contains(got, "create on configmaps") {
		t.Errorf("refusal = %q, want it to name both what was declared and what was attempted", got)
	}
	if fake.count() != 0 {
		t.Errorf("kubernetes calls = %d, want 0 — the request must not reach the apiserver", fake.count())
	}
	if len(gate.calls) != 1 {
		t.Errorf("gate calls = %d, want 1 — the declared pair is what was approved", len(gate.calls))
	}
}

// AC1's ⑵ at call time: a marker says the tool exercises no resource
// permission, and the client it gets has to make that true. discoveryOnly is
// the one marker that still permits something, and it permits only discovery.
func TestMarkerToolsAreConfinedToWhatTheMarkerAllows(t *testing.T) {
	discoveryMarker := &noResourcePermission{kind: discoveryOnly, reason: "discovery is not a resource permission"}
	nothingMarker := &noResourcePermission{kind: touchesNothing, reason: "reaches no backend at all"}
	registry := map[string]toolEntry{
		"discovery_only":   {decl: toolDeclaration{noResourcePermission: discoveryMarker}, handle: reachesKubernetes},
		"discovery_lister": {decl: toolDeclaration{noResourcePermission: discoveryMarker}, handle: exercisesPair("v1", "Pod", "list")},
		"touches_nothing":  {decl: toolDeclaration{noResourcePermission: nothingMarker}, handle: reachesKubernetes},
	}

	cases := []struct {
		tool      string
		wantCalls int
		wantFail  bool
	}{
		{"discovery_only", 1, false},
		{"discovery_lister", 0, true},
		{"touches_nothing", 0, true},
	}
	for _, tc := range cases {
		t.Run(tc.tool, func(t *testing.T) {
			h, fake := testHandler(t, gatekeeper.NewUnavailable(nil), registry)
			result, rerr := callTool(t, h, tc.tool, "")
			if rerr != nil {
				t.Fatalf("tools/call = %v, want no protocol error", rerr)
			}
			if got := isError(result); got != tc.wantFail {
				t.Errorf("isError = %v, want %v (%q)", got, tc.wantFail, resultText(t, result))
			}
			if fake.count() != tc.wantCalls {
				t.Errorf("kubernetes calls = %d, want %d", fake.count(), tc.wantCalls)
			}
		})
	}
}

// AC1's last verification sentence: "각 도구의 정상 호출이 호출 제한에 걸리지
// 않는다(선언이 실제 행사와 같다)". Every shipped tool that may touch the
// cluster is driven through the real dispatcher here, and reaching the fake is
// the assertion — a declaration that named the wrong pair would refuse the call
// before the fake saw it.
//
// The coverage check at the end is what keeps this honest as tools are added: a
// new tool that declares pairs, or one marked discovery-only, has to appear in
// the table, so "we forgot to check this one" fails the build instead of
// passing quietly.
func TestShippedToolsExerciseOnlyWhatTheyDeclare(t *testing.T) {
	createOne := `apiVersion: v1
kind: ConfigMap
metadata:
  name: only
`
	cases := []struct {
		tool string
		args string
	}{
		{"api_resources", `{}`},
		{"resource_list", `{"apiVersion":"v1","kind":"Secret"}`},
		{"resource_get", `{"apiVersion":"v1","kind":"ConfigMap","namespace":"ops","name":"app"}`},
		{"resource_get", `{"apiVersion":"v1","kind":"Pod","namespace":"ops","name":"api","subresource":"log"}`},
		{"resource_watch", `{"apiVersion":"v1","kind":"ConfigMap","namespace":"ops"}`},
		{"resource_update", `{"apiVersion":"apps/v1","kind":"Deployment","namespace":"ops","name":"api","manifest":{"apiVersion":"apps/v1","kind":"Deployment","metadata":{"name":"api"}}}`},
		{"resource_update", `{"apiVersion":"apps/v1","kind":"Deployment","namespace":"ops","name":"api","subresource":"scale","replicas":3}`},
		{"resource_create", `{"manifest":` + jsonString(createOne) + `}`},
		{"resource_patch", `{"apiVersion":"apps/v1","kind":"Deployment","namespace":"ops","name":"api","patchType":"merge","patch":{"spec":{"replicas":2}}}`},
		{"resource_delete", `{"apiVersion":"v1","kind":"ConfigMap","namespace":"ops","name":"app"}`},
		{"resource_delete_collection", collectionArgs},
		{"resource_exec", `{"apiVersion":"v1","kind":"Pod","namespace":"ops","name":"api","command":["echo","hi"]}`},
		{"resource_attach", `{"apiVersion":"v1","kind":"Pod","namespace":"ops","name":"api","readSeconds":1}`},
		{"resource_port_forward", `{"apiVersion":"v1","kind":"Pod","namespace":"ops","name":"api","port":8080}`},
		{"resource_proxy", `{"apiVersion":"v1","kind":"Pod","namespace":"ops","name":"api","method":"GET","path":"/metrics"}`},
		{"resource_proxy", `{"apiVersion":"v1","kind":"Pod","namespace":"ops","name":"api","method":"POST","path":"/upload","bodyBase64":"aGk="}`},
		{"dear_baby_reset_user", `{"namespace":"ops","email":"someone@example.com"}`},
	}

	covered := map[string]bool{}
	for _, tc := range cases {
		covered[tc.tool] = true
		t.Run(tc.tool+" "+tc.args, func(t *testing.T) {
			h, fake := approvingHandler(t)
			result, rerr := callTool(t, h, tc.tool, tc.args)
			if rerr != nil {
				t.Fatalf("tools/call(%s) = %v, want a normal call to run", tc.tool, rerr)
			}
			if isError(result) {
				t.Fatalf("tools/call(%s) failed: %q — a declared call must not hit the confinement", tc.tool, resultText(t, result))
			}
			if fake.count() == 0 {
				t.Errorf("kubernetes calls = 0, want the request to reach the cluster layer")
			}
		})
	}

	for name, entry := range toolRegistry {
		marker := entry.decl.noResourcePermission
		if marker != nil && marker.kind == touchesNothing {
			continue // reaches no kubernetes resource, so there is nothing to confine
		}
		if !covered[name] {
			t.Errorf("%s may touch the cluster but no case checks that its declaration matches what it exercises", name)
		}
	}
}

// AC1 says the gate's own pre-read (AC11) is scoped by AC11's table rather than
// by the tool's declaration, so confining the handler must not confine the
// reader. resource_patch declares only `patch on deployments`; the read that
// builds AC6's precondition is a `get`, and it still has to happen.
func TestGateOwnReadIsNotConfinedByTheToolDeclaration(t *testing.T) {
	gate := &scriptedGate{decision: &gatekeeper.Decision{RequestID: "req-1"}}
	h, fake, reader := testHandlerReading(t, gate, toolRegistry, newStateReader())

	args := `{"apiVersion":"apps/v1","kind":"Deployment","namespace":"ops","name":"api","patchType":"merge","patch":{"spec":{"replicas":2}}}`
	result, rerr := callTool(t, h, "resource_patch", args)
	if rerr != nil {
		t.Fatalf("tools/call = %v, want the patch to run", rerr)
	}
	if isError(result) {
		t.Fatalf("tools/call failed: %q", resultText(t, result))
	}
	if len(reader.observed()) == 0 {
		t.Error("gate reads = 0, want the declared pre-read to have happened")
	}
	if fake.count() != 1 {
		t.Errorf("kubernetes calls = %d, want 1", fake.count())
	}
}

func jsonString(s string) string {
	encoded, err := json.Marshal(s)
	if err != nil {
		return `""`
	}
	return string(encoded)
}
