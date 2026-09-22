package mcp

import (
	"encoding/base64"
	"errors"
	"strings"
	"testing"

	"github.com/dlddu/homelab-k3s-mcp/internal/gatekeeper"
	"github.com/dlddu/homelab-k3s-mcp/internal/k8s"
)

// TestProxyAllPathsGated is the half of scenario 15 that does not need a
// cluster, and the half the other tools' tests do not have to cover: that the
// verb follows the method, and that *every* method is gated. The second is the
// one this package could get wrong silently — gatedVerbs judges by verb, and a
// GET's verb is "get", which is exactly the verb the read gate lets through
// unless the target is a sensitive kind. A proxy GET is not a read of an
// object; it is an arbitrary request to whatever that object serves.
func TestProxyAllPathsGated(t *testing.T) {
	methods := []struct {
		method string
		kind   string
		args   string
		pair   string
	}{
		{"GET", "Pod", `{"apiVersion":"v1","kind":"Pod","namespace":"ops","name":"api","method":"GET","path":"/metrics"}`, "get on pods/proxy"},
		{"POST", "Service", `{"apiVersion":"v1","kind":"Service","namespace":"ops","name":"api","method":"POST","path":"/reload","body":"{}"}`, "create on services/proxy"},
		{"PUT", "Pod", `{"apiVersion":"v1","kind":"Pod","namespace":"ops","name":"api","method":"PUT","path":"/config","body":"{}"}`, "update on pods/proxy"},
		{"PATCH", "Pod", `{"apiVersion":"v1","kind":"Pod","namespace":"ops","name":"api","method":"PATCH","path":"/config","body":"{}"}`, "patch on pods/proxy"},
		{"DELETE", "Pod", `{"apiVersion":"v1","kind":"Pod","namespace":"ops","name":"api","method":"DELETE","path":"/cache"}`, "delete on pods/proxy"},
		{"GET", "Node", `{"apiVersion":"v1","kind":"Node","name":"worker-1","method":"GET","path":"/healthz"}`, "get on nodes/proxy"},
	}
	for _, tc := range methods {
		t.Run(tc.method+" on "+tc.kind+" spends "+tc.pair, func(t *testing.T) {
			gate := &scriptedGate{decision: &gatekeeper.Decision{RequestID: "req-1"}}
			h, fake := testHandler(t, gate, toolRegistry)

			if _, rerr := callTool(t, h, "resource_proxy", tc.args); rerr != nil {
				t.Fatalf("tools/call = %v, want the approved proxy call to run", rerr)
			}
			if len(gate.calls) != 1 {
				t.Fatalf("approval requests = %d, want 1 — AC15 gates every method", len(gate.calls))
			}
			if got := gate.calls[0].Pair.String(); got != tc.pair {
				t.Errorf("gated pair = %q, want %q", got, tc.pair)
			}
			if fake.count() != 1 {
				t.Errorf("kubernetes calls = %d, want 1", fake.count())
			}
		})
	}

	t.Run("a refused approval stops the call before the cluster is reached", func(t *testing.T) {
		gate := &scriptedGate{err: errors.New("no approval")}
		h, fake := testHandler(t, gate, toolRegistry)

		args := `{"apiVersion":"v1","kind":"Pod","namespace":"ops","name":"api","method":"GET","path":"/metrics"}`
		if _, rerr := callTool(t, h, "resource_proxy", args); rerr == nil {
			t.Fatal("tools/call = nil error, want the denial to refuse the call")
		}
		if fake.count() != 0 {
			t.Errorf("kubernetes calls = %d, want 0 — an unapproved GET is still unapproved", fake.count())
		}
	})

	t.Run("the path travels verbatim, query string and all", func(t *testing.T) {
		h, fake := approvingHandler(t)
		args := `{"apiVersion":"v1","kind":"Node","name":"worker-1","method":"GET","path":"/exec/ops/api/app?command=id"}`
		if _, rerr := callTool(t, h, "resource_proxy", args); rerr != nil {
			t.Fatalf("tools/call = %v, want the approved proxy call to run", rerr)
		}
		ref, method, path, _, readSeconds := fake.proxy()
		if ref.Kind != "Node" || ref.Name != "worker-1" || ref.Namespace != nil {
			t.Errorf("proxy coordinate = %+v, want the cluster-scoped v1 Node worker-1", ref)
		}
		if method != "GET" {
			t.Errorf("method = %q, want GET", method)
		}
		if path != "/exec/ops/api/app?command=id" {
			t.Errorf("path = %q, want it handed down unchanged — there is no allowlist to rewrite it", path)
		}
		if readSeconds != k8s.ProxyDefaultReadSeconds {
			t.Errorf("readSeconds = %d, want the default %d", readSeconds, k8s.ProxyDefaultReadSeconds)
		}
	})

	t.Run("bodyBase64 carries bytes that are not text", func(t *testing.T) {
		h, fake := approvingHandler(t)
		encoded := base64.StdEncoding.EncodeToString([]byte{0x00, 0xff, 0x10})
		args := `{"apiVersion":"v1","kind":"Pod","namespace":"ops","name":"api","method":"POST","path":"/upload","bodyBase64":"` + encoded + `"}`
		if _, rerr := callTool(t, h, "resource_proxy", args); rerr != nil {
			t.Fatalf("tools/call = %v, want the approved proxy call to run", rerr)
		}
		if _, _, _, body, _ := fake.proxy(); len(body) != 3 || body[1] != 0xff {
			t.Errorf("body = %v, want the decoded bytes", body)
		}
	})

	t.Run("the target's own status is the answer, not a failure", func(t *testing.T) {
		h, fake := approvingHandler(t)
		fake.proxyOutcome = &k8s.ProxyOutcome{
			Method: "GET", Verb: "get", Path: "/nope", Name: "api",
			Status: 404, Body: "not found", BodyEncoding: "utf-8",
		}
		result, rerr := callTool(t, h, "resource_proxy", `{"apiVersion":"v1","kind":"Pod","namespace":"ops","name":"api","method":"GET","path":"/nope"}`)
		if rerr != nil {
			t.Fatalf("tools/call = %v, want a 404 from the target to still be an answer", rerr)
		}
		structured := result.(map[string]any)["structuredContent"].(map[string]any)
		if structured["status"] != 404 {
			t.Errorf("status = %v, want 404 reported rather than converted to an error", structured["status"])
		}
	})

	t.Run("a cut body says so rather than shortening silently", func(t *testing.T) {
		h, fake := approvingHandler(t)
		fake.proxyOutcome = &k8s.ProxyOutcome{
			Method: "GET", Verb: "get", Path: "/logs", Name: "api",
			Status: 200, Body: strings.Repeat("y", 100), BodyEncoding: "utf-8", Truncated: true,
		}
		result, rerr := callTool(t, h, "resource_proxy", `{"apiVersion":"v1","kind":"Pod","namespace":"ops","name":"api","method":"GET","path":"/logs"}`)
		if rerr != nil {
			t.Fatalf("tools/call = %v, want a capped answer to still answer", rerr)
		}
		structured := result.(map[string]any)["structuredContent"].(map[string]any)
		if structured["truncated"] != true {
			t.Errorf("truncated = %v, want true", structured["truncated"])
		}
	})
}

// TestProxyKubeletHighPowerPathFlagged is AC3's side of scenario 15, and the
// reason AC15 names AC3 at all: /healthz and /exec on a node arrive wearing the
// same pair, so the pair on the approval screen cannot tell an operator which
// one they are being asked about. The path can, and the marking is what makes
// them notice it. AC3 says mark, not block — so the call still runs.
func TestProxyKubeletHighPowerPathFlagged(t *testing.T) {
	t.Run("a node exec path is marked and still runs", func(t *testing.T) {
		reader := newStateReader()
		gate := &scriptedGate{decision: &gatekeeper.Decision{RequestID: "req-1"}}
		h, fake, _ := testHandlerReading(t, gate, toolRegistry, reader)

		args := `{"apiVersion":"v1","kind":"Node","name":"worker-1","method":"GET","path":"/exec/ops/api/app?command=id"}`
		if _, rerr := callTool(t, h, "resource_proxy", args); rerr != nil {
			t.Fatalf("tools/call = %v, want AC15's 실행되되 — the marking does not block", rerr)
		}
		if fake.count() != 1 {
			t.Errorf("kubernetes calls = %d, want 1 — AC3 marks the path, it does not refuse it", fake.count())
		}
		ctx := gate.context(0)
		for _, want := range []string{
			"/exec/ops/api/app?command=id",
			"kubelet high-power endpoint",
			"get on nodes/proxy",
			"v1 Node worker-1",
		} {
			if !strings.Contains(ctx, want) {
				t.Errorf("approval context is missing %q:\n%s", want, ctx)
			}
		}
	})

	t.Run("every one of AC3's six is marked, in either spelling", func(t *testing.T) {
		for _, path := range []string{
			"/exec/ops/api/app", "/attach/ops/api/app", "/portForward/ops/api",
			"/portforward/ops/api", "/run/ops/api/app", "/logs/messages",
			"/containerLogs/ops/api/app",
		} {
			gate := &scriptedGate{decision: &gatekeeper.Decision{RequestID: "req-1"}}
			h, _, _ := testHandlerReading(t, gate, toolRegistry, newStateReader())
			args := `{"apiVersion":"v1","kind":"Node","name":"worker-1","method":"GET","path":"` + path + `"}`
			if _, rerr := callTool(t, h, "resource_proxy", args); rerr != nil {
				t.Fatalf("tools/call = %v for %s", rerr, path)
			}
			if !strings.Contains(gate.context(0), "kubelet high-power endpoint") {
				t.Errorf("%s was not marked:\n%s", path, gate.context(0))
			}
		}
	})

	t.Run("an observation endpoint carries the path but no marking", func(t *testing.T) {
		gate := &scriptedGate{decision: &gatekeeper.Decision{RequestID: "req-1"}}
		h, _, _ := testHandlerReading(t, gate, toolRegistry, newStateReader())
		args := `{"apiVersion":"v1","kind":"Node","name":"worker-1","method":"GET","path":"/healthz"}`
		if _, rerr := callTool(t, h, "resource_proxy", args); rerr != nil {
			t.Fatalf("tools/call = %v, want the approved proxy call to run", rerr)
		}
		ctx := gate.context(0)
		if !strings.Contains(ctx, "/healthz") {
			t.Errorf("approval context is missing the path:\n%s", ctx)
		}
		if strings.Contains(ctx, "kubelet high-power endpoint") {
			t.Errorf("/healthz was marked high-power; a marking that fires on everything marks nothing:\n%s", ctx)
		}
	})

	t.Run("the same path on a pod is not a kubelet endpoint", func(t *testing.T) {
		gate := &scriptedGate{decision: &gatekeeper.Decision{RequestID: "req-1"}}
		h, _, _ := testHandlerReading(t, gate, toolRegistry, newStateReader())
		args := `{"apiVersion":"v1","kind":"Pod","namespace":"ops","name":"api","method":"GET","path":"/exec"}`
		if _, rerr := callTool(t, h, "resource_proxy", args); rerr != nil {
			t.Fatalf("tools/call = %v, want the approved proxy call to run", rerr)
		}
		if strings.Contains(gate.context(0), "kubelet high-power endpoint") {
			t.Errorf("a pod's own /exec path was marked as kubelet's:\n%s", gate.context(0))
		}
	})
}

// TestProxyWideningIsDocumented is the mirror of the exemption assertion in
// gate_test.go. That one fails when a tool leaves the gate without a document
// saying why; this one fails when a tool widens it without one. Both directions
// need the same guard, because "everything here needs approval" is as much a
// judgement about a tool as "this does not" — it just errs the safe way, which
// is exactly how an undocumented one would survive review.
func TestProxyWideningIsDocumented(t *testing.T) {
	entry, ok := toolRegistry["resource_proxy"]
	if !ok {
		t.Fatal("resource_proxy is not registered")
	}
	if entry.decl.alwaysGated == "" {
		t.Error("resource_proxy widens the gate to every method with no documented reason")
	}
	if !strings.Contains(entry.decl.alwaysGated, "AC15") {
		t.Errorf("alwaysGated = %q, want it to cite the AC that made the decision", entry.decl.alwaysGated)
	}
	widened := 0
	for _, e := range toolRegistry {
		if e.decl.alwaysGated != "" {
			widened++
		}
	}
	if widened != 1 {
		t.Errorf("tools widening the gate = %d, want exactly the one AC15 names", widened)
	}
}

// TestProxyRefusalsCostNoApproval mirrors the port forward case of the same
// name, and matters more here: this is the tool whose approval screen can show
// a kubelet exec path, so an argument typo must not be what puts one there.
func TestProxyRefusalsCostNoApproval(t *testing.T) {
	refusals := []struct {
		name string
		args string
		want string
	}{
		{"missing method", `{"apiVersion":"v1","kind":"Pod","namespace":"ops","name":"api","path":"/metrics"}`, "method is required"},
		{"method with no verb", `{"apiVersion":"v1","kind":"Pod","namespace":"ops","name":"api","method":"HEAD","path":"/metrics"}`, "GET, POST, PUT, PATCH, DELETE"},
		{"missing path", `{"apiVersion":"v1","kind":"Pod","namespace":"ops","name":"api","method":"GET"}`, "path is required"},
		{"relative path", `{"apiVersion":"v1","kind":"Pod","namespace":"ops","name":"api","method":"GET","path":"metrics"}`, "must start with"},
		{"missing name", `{"apiVersion":"v1","kind":"Pod","namespace":"ops","method":"GET","path":"/metrics"}`, "name is required"},
		{"missing kind", `{"apiVersion":"v1","namespace":"ops","name":"api","method":"GET","path":"/metrics"}`, "kind is required"},
		{"explicit subresource", `{"apiVersion":"v1","kind":"Pod","namespace":"ops","name":"api","method":"GET","path":"/metrics","subresource":"proxy"}`, "pass no subresource"},
		{"both body spellings", `{"apiVersion":"v1","kind":"Pod","namespace":"ops","name":"api","method":"POST","path":"/x","body":"a","bodyBase64":"YQ=="}`, "not both"},
		{"body not a string", `{"apiVersion":"v1","kind":"Pod","namespace":"ops","name":"api","method":"POST","path":"/x","body":3}`, "body must be a string"},
		{"bodyBase64 not base64", `{"apiVersion":"v1","kind":"Pod","namespace":"ops","name":"api","method":"POST","path":"/x","bodyBase64":"!!!"}`, "not valid base64"},
		{"readSeconds over the cap", `{"apiVersion":"v1","kind":"Pod","namespace":"ops","name":"api","method":"GET","path":"/x","readSeconds":31}`, "between 1 and 30"},
		{"readSeconds below one", `{"apiVersion":"v1","kind":"Pod","namespace":"ops","name":"api","method":"GET","path":"/x","readSeconds":0}`, "between 1 and 30"},
		{"port folded into the name", `{"apiVersion":"v1","kind":"Pod","namespace":"ops","name":"api:9090","method":"GET","path":"/metrics"}`, "separate port argument"},
		{"port on a node", `{"apiVersion":"v1","kind":"Node","name":"worker-1","method":"GET","path":"/metrics","port":10250}`, "Pod and Service"},
		{"port zero", `{"apiVersion":"v1","kind":"Pod","namespace":"ops","name":"api","method":"GET","path":"/metrics","port":0}`, "between 1 and 65535"},
		{"port above the range", `{"apiVersion":"v1","kind":"Pod","namespace":"ops","name":"api","method":"GET","path":"/metrics","port":"70000"}`, "between 1 and 65535"},
		{"named port on a pod", `{"apiVersion":"v1","kind":"Pod","namespace":"ops","name":"api","method":"GET","path":"/metrics","port":"metrics"}`, "takes a port number"},
		{"port name with a colon", `{"apiVersion":"v1","kind":"Service","namespace":"ops","name":"api","method":"GET","path":"/metrics","port":"https:metrics"}`, "valid port name"},
		{"port name too long", `{"apiVersion":"v1","kind":"Service","namespace":"ops","name":"api","method":"GET","path":"/metrics","port":"a-very-long-port-name"}`, "valid port name"},
		{"port not a number or string", `{"apiVersion":"v1","kind":"Pod","namespace":"ops","name":"api","method":"GET","path":"/metrics","port":true}`, "port number or a named"},
	}
	for _, tc := range refusals {
		t.Run(tc.name, func(t *testing.T) {
			gate := &scriptedGate{decision: &gatekeeper.Decision{RequestID: "req-1"}}
			h, fake := testHandler(t, gate, toolRegistry)
			_, rerr := callTool(t, h, "resource_proxy", tc.args)
			if rerr == nil {
				t.Fatal("tools/call = nil error, want a refusal")
			}
			if !strings.Contains(rerr.message, tc.want) {
				t.Errorf("error = %q, want it to mention %q", rerr.message, tc.want)
			}
			if fake.count() != 0 {
				t.Errorf("kubernetes calls = %d, want 0 — a rejected argument must not reach the cluster", fake.count())
			}
			if len(gate.calls) != 0 {
				t.Errorf("approval requests = %d, want 0 — the refusal has to land before the operator is asked", len(gate.calls))
			}
		})
	}
}

func TestProxyPortReachesTheChosenPort(t *testing.T) {
	cases := []struct {
		name string
		kind string
		port string
		want string
	}{
		{"numeric port on a pod", "Pod", `9090`, "9090"},
		{"numeric string is normalised", "Pod", `"09090"`, "9090"},
		{"named port on a service", "Service", `"metrics"`, "metrics"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			gate := &scriptedGate{decision: &gatekeeper.Decision{RequestID: "req-1"}}
			h, fake, _ := testHandlerReading(t, gate, toolRegistry, newStateReader())
			args := `{"apiVersion":"v1","kind":"` + tc.kind + `","namespace":"ops","name":"api","method":"GET","path":"/metrics","port":` + tc.port + `}`
			result, rerr := callTool(t, h, "resource_proxy", args)
			if rerr != nil {
				t.Fatalf("tools/call = %v, want the approved proxy call to run", rerr)
			}
			ref, _, _, _, _ := fake.proxy()
			if ref.Name != "api" {
				t.Errorf("name = %q, want the object name alone", ref.Name)
			}
			if ref.Port == nil || *ref.Port != tc.want {
				t.Errorf("port = %v, want %q", ref.Port, tc.want)
			}
			if ctx := gate.context(0); !strings.Contains(ctx, "GET /metrics on port "+tc.want) {
				t.Errorf("approval context does not name the port:\n%s", ctx)
			}
			structured := result.(map[string]any)["structuredContent"].(map[string]any)
			if got, _ := structured["port"].(*string); got == nil || *got != tc.want {
				t.Errorf("response port = %v, want %q", structured["port"], tc.want)
			}
		})
	}

	t.Run("no port leaves the apiserver default", func(t *testing.T) {
		h, fake := approvingHandler(t)
		args := `{"apiVersion":"v1","kind":"Pod","namespace":"ops","name":"api","method":"GET","path":"/metrics"}`
		if _, rerr := callTool(t, h, "resource_proxy", args); rerr != nil {
			t.Fatalf("tools/call = %v, want the approved proxy call to run", rerr)
		}
		if ref, _, _, _, _ := fake.proxy(); ref.Port != nil {
			t.Errorf("port = %q, want none", *ref.Port)
		}
	})
}
