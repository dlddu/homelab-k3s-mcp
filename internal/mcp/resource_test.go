package mcp

import (
	"fmt"
	"strings"
	"testing"

	"github.com/dlddu/homelab-k3s-mcp/internal/gatekeeper"
)

// restartPatch is AC9's rolling restart.
const restartPatch = `{"spec":{"template":{"metadata":{"annotations":{"kubectl.kubernetes.io/restartedAt":"2026-09-14T12:00:00Z"}}}}}`

func approvingHandler(t *testing.T) (*Handler, *countingK8s) {
	t.Helper()
	return testHandler(t, &scriptedGate{decision: &gatekeeper.Decision{RequestID: "req-1"}}, toolRegistry)
}

// AC9. The refusals share this test because a tool that accepts a fifth name
// is a tool whose enum is decoration.
func TestPatchTypes(t *testing.T) {
	for _, patchType := range []string{"merge", "strategic", "json", "apply"} {
		t.Run(patchType, func(t *testing.T) {
			h, fake := approvingHandler(t)
			body := `{"metadata":{"labels":{"a":"b"}}}`
			extra := ""
			if patchType == "json" {
				body = `[{"op":"add","path":"/metadata/labels/a","value":"b"}]`
			}
			if patchType == "apply" {
				extra = `,"fieldManager":"ops"`
			}
			args := `{"apiVersion":"apps/v1","kind":"Deployment","namespace":"ops","name":"api",` +
				`"patchType":"` + patchType + `","patch":` + body + extra + `}`

			if _, rerr := callTool(t, h, "resource_patch", args); rerr != nil {
				t.Fatalf("tools/call(patchType=%s) = %v, want it to run", patchType, rerr)
			}
			if got := fake.patch().PatchType; got != patchType {
				t.Errorf("PatchType reaching the cluster layer = %q, want %q", got, patchType)
			}
			if got := string(fake.patch().Patch); got != body {
				t.Errorf("patch body = %s, want the caller's bytes %s", got, body)
			}
		})
	}

	refusals := []struct {
		name string
		args string
		want string
	}{
		{"unknown patchType", `{"apiVersion":"v1","kind":"ConfigMap","namespace":"ops","name":"a","patchType":"yaml","patch":{}}`, "patchType must be one of"},
		{"missing patchType", `{"apiVersion":"v1","kind":"ConfigMap","namespace":"ops","name":"a","patch":{}}`, "patchType is required"},
		{"missing patch", `{"apiVersion":"v1","kind":"ConfigMap","namespace":"ops","name":"a","patchType":"merge"}`, "patch is required"},
		{"missing name", `{"apiVersion":"v1","kind":"ConfigMap","namespace":"ops","patchType":"merge","patch":{}}`, "name is required"},
		// Apply without a manager is the one the apiserver would also refuse,
		// but its error arrives after the approval has been spent (AC7), so the
		// tool has to catch it first.
		{"apply without fieldManager", `{"apiVersion":"v1","kind":"ConfigMap","namespace":"ops","name":"a","patchType":"apply","patch":{}}`, "fieldManager is required"},
		{"fieldManager outside apply", `{"apiVersion":"v1","kind":"ConfigMap","namespace":"ops","name":"a","patchType":"merge","patch":{},"fieldManager":"ops"}`, "patchType=apply only"},
	}
	for _, tc := range refusals {
		t.Run(tc.name, func(t *testing.T) {
			h, fake := approvingHandler(t)
			_, rerr := callTool(t, h, "resource_patch", tc.args)
			if rerr == nil {
				t.Fatal("tools/call = nil error, want a refusal")
			}
			if !strings.Contains(rerr.message, tc.want) {
				t.Errorf("error = %q, want it to mention %q", rerr.message, tc.want)
			}
			if fake.count() != 0 {
				t.Errorf("kubernetes calls = %d, want 0 — a rejected argument must not reach the cluster", fake.count())
			}
		})
	}
}

// AC9. The integration suite owns the half this level cannot see; what is left
// here is its complement — the assertion that would have to fail first for the
// replaced pods to be carrying anything the caller did not send.
func TestRestartPatchTouchesOnlyAnnotation(t *testing.T) {
	h, fake := approvingHandler(t)
	args := `{"apiVersion":"apps/v1","kind":"Deployment","namespace":"ops","name":"api",` +
		`"patchType":"strategic","patch":` + restartPatch + `}`

	if _, rerr := callTool(t, h, "resource_patch", args); rerr != nil {
		t.Fatalf("tools/call = %v, want the restart patch to run", rerr)
	}
	ref := fake.patch()
	if string(ref.Patch) != restartPatch {
		t.Errorf("patch body = %s, want it passed through unchanged as %s", ref.Patch, restartPatch)
	}
	if strings.Contains(string(ref.Patch), "replicas") {
		t.Errorf("patch body mentions replicas, want the restart to leave the rest of the spec alone: %s", ref.Patch)
	}
	if ref.Name != "api" || ref.Namespace == nil || *ref.Namespace != "ops" {
		t.Errorf("coordinate = %s/%v, want ops/api", ref.Name, ref.Namespace)
	}
}

// AC1/AC9.
func TestRestartPatchIsGatedLikeAnyPatch(t *testing.T) {
	h, fake := testHandler(t, gatekeeper.NewUnavailable(nil), toolRegistry)
	args := `{"apiVersion":"apps/v1","kind":"Deployment","namespace":"ops","name":"api",` +
		`"patchType":"strategic","patch":` + restartPatch + `}`

	if _, rerr := callTool(t, h, "resource_patch", args); rerr == nil {
		t.Fatal("tools/call = nil error, want the restart patch to be refused without approval")
	}
	if fake.count() != 0 {
		t.Errorf("kubernetes calls = %d, want 0", fake.count())
	}
}

// The credential values below are written the way a caller would send them, and
// are distinctive strings so a test can assert their absence by searching for
// them rather than by trusting the shape of what came back.
const (
	secretTokenPlain   = "s3cr3t-rotation-value"
	secretTokenEncoded = "c3VwZXItc2VjcmV0LWJ5dGVz" // "super-secret-bytes"
)

// AC3/AC10/AC16.
func TestSecretWriteContextRedactsValues(t *testing.T) {
	gate := &scriptedGate{decision: &gatekeeper.Decision{RequestID: "req-1"}}
	h, _ := testHandler(t, gate, toolRegistry)

	args := `{"apiVersion":"v1","kind":"Secret","namespace":"ops","name":"api","patchType":"merge",` +
		`"patch":{"stringData":{"token":"` + secretTokenPlain + `"},"data":{"ca.crt":"` + secretTokenEncoded + `"}}}`
	if _, rerr := callTool(t, h, "resource_patch", args); rerr != nil {
		t.Fatalf("tools/call = %v, want the approved patch to run", rerr)
	}
	if len(gate.calls) != 1 {
		t.Fatalf("gate calls = %d, want 1", len(gate.calls))
	}

	ctx := gate.calls[0].Context
	for _, leaked := range []string{secretTokenPlain, secretTokenEncoded} {
		if strings.Contains(ctx, leaked) {
			t.Errorf("approval context carries the credential value %q:\n%s", leaked, ctx)
		}
	}
	for _, want := range []string{"token", "ca.crt", "(masked, 21B)", "(masked, 18B)", "patch on secrets"} {
		if !strings.Contains(ctx, want) {
			t.Errorf("approval context is missing %q:\n%s", want, ctx)
		}
	}
}

// The control group for the case above: a masker that hid everything would
// pass a leak test and fail the operator.
func TestOrdinaryWriteKeepsItsBodyInTheContext(t *testing.T) {
	gate := &scriptedGate{decision: &gatekeeper.Decision{RequestID: "req-1"}}
	h, _ := testHandler(t, gate, toolRegistry)

	args := `{"apiVersion":"v1","kind":"ConfigMap","namespace":"ops","name":"app","patchType":"merge",` +
		`"patch":{"data":{"LOG_LEVEL":"debug"}}}`
	if _, rerr := callTool(t, h, "resource_patch", args); rerr != nil {
		t.Fatalf("tools/call = %v, want the approved patch to run", rerr)
	}
	ctx := gate.calls[0].Context
	if !strings.Contains(ctx, "debug") {
		t.Errorf("approval context dropped an ordinary kind's patch body:\n%s", ctx)
	}
}

// A JSON patch carries its value beside a path rather than under a data key, so
// a masker that only walks for the field name lets this shape straight through.
func TestJSONPatchCredentialValuesAreMasked(t *testing.T) {
	gate := &scriptedGate{decision: &gatekeeper.Decision{RequestID: "req-1"}}
	h, _ := testHandler(t, gate, toolRegistry)

	args := `{"apiVersion":"v1","kind":"Secret","namespace":"ops","name":"api","patchType":"json",` +
		`"patch":[{"op":"replace","path":"/data/token","value":"` + secretTokenEncoded + `"},` +
		`{"op":"replace","path":"/metadata/labels/rotated","value":"true"}]}`
	if _, rerr := callTool(t, h, "resource_patch", args); rerr != nil {
		t.Fatalf("tools/call = %v, want the approved patch to run", rerr)
	}
	ctx := gate.calls[0].Context
	if strings.Contains(ctx, secretTokenEncoded) {
		t.Errorf("approval context carries a json-patch credential value:\n%s", ctx)
	}
	// The path stays — it is what tells the operator which key is being rewritten.
	for _, want := range []string{"/data/token", "(masked, 18B)", "/metadata/labels/rotated", "true"} {
		if !strings.Contains(ctx, want) {
			t.Errorf("approval context is missing %q:\n%s", want, ctx)
		}
	}
}

// AC8. The accepted values and the refused ones share a test because the tool's
// bounds are one statement: 0 is a replica count and -1 is not.
//
// The refusals assert the gate was never called, which is the half scenario 8
// spells out — "승인 요청조차 만들지 않음". It holds only because these checks run
// during pair resolution rather than in the handler, and the handler is
// downstream of authorize. The set that gets this treatment is exactly the set
// scenario 8 names; a missing `name` is still refused in the handler, the way
// resource_patch refuses it.
func TestUpdateScaleBounds(t *testing.T) {
	for _, replicas := range []int64{3, 0, 1} {
		t.Run(fmt.Sprintf("replicas=%d", replicas), func(t *testing.T) {
			gate := &scriptedGate{decision: &gatekeeper.Decision{RequestID: "req-1"}}
			h, fake := testHandler(t, gate, toolRegistry)
			args := fmt.Sprintf(
				`{"apiVersion":"apps/v1","kind":"Deployment","namespace":"ops","name":"api",`+
					`"subresource":"scale","replicas":%d}`, replicas)

			if _, rerr := callTool(t, h, "resource_update", args); rerr != nil {
				t.Fatalf("tools/call(replicas=%d) = %v, want it to run", replicas, rerr)
			}
			ref := fake.update()
			if ref.Replicas == nil || *ref.Replicas != replicas {
				t.Errorf("replicas reaching the cluster layer = %v, want %d", ref.Replicas, replicas)
			}
			if ref.Subresource != "scale" {
				t.Errorf("subresource = %q, want scale — a replica change written to the object itself is a different verb's business", ref.Subresource)
			}
			if ref.Manifest != nil {
				t.Errorf("manifest = %v, want nil; the scale body is this server's, not the caller's", ref.Manifest)
			}
			if len(gate.calls) != 1 {
				t.Errorf("gate calls = %d, want 1 — every replica change is approved", len(gate.calls))
			}
			if got := gate.calls[0].Pair.Resource; got != "deployments/scale" {
				t.Errorf("approved pair resource = %q, want deployments/scale", got)
			}
		})
	}

	refusals := []struct {
		name string
		args string
		want string
	}{
		{"negative replicas", `{"apiVersion":"apps/v1","kind":"Deployment","namespace":"ops","name":"api","subresource":"scale","replicas":-1}`, "replicas must be >= 0"},
		{"missing replicas", `{"apiVersion":"apps/v1","kind":"Deployment","namespace":"ops","name":"api","subresource":"scale"}`, "replicas is required"},
		{"non-integer replicas", `{"apiVersion":"apps/v1","kind":"Deployment","namespace":"ops","name":"api","subresource":"scale","replicas":"two"}`, "replicas must be an integer"},
		{"replicas without the subresource", `{"apiVersion":"apps/v1","kind":"Deployment","namespace":"ops","name":"api","replicas":3}`, "subresource=scale only"},
		{"manifest with the subresource", `{"apiVersion":"apps/v1","kind":"Deployment","namespace":"ops","name":"api","subresource":"scale","manifest":{"spec":{}}}`, "does not apply to subresource=scale"},
		{"neither manifest nor replicas", `{"apiVersion":"apps/v1","kind":"Deployment","namespace":"ops","name":"api"}`, "manifest is required"},
		{"a subresource this tool does not write", `{"apiVersion":"apps/v1","kind":"Deployment","namespace":"ops","name":"api","subresource":"status","replicas":1}`, "subresource must be scale"},
	}
	for _, tc := range refusals {
		t.Run(tc.name, func(t *testing.T) {
			gate := &scriptedGate{decision: &gatekeeper.Decision{RequestID: "req-1"}}
			h, fake := testHandler(t, gate, toolRegistry)

			_, rerr := callTool(t, h, "resource_update", tc.args)
			if rerr == nil {
				t.Fatal("tools/call = nil error, want a refusal")
			}
			if !strings.Contains(rerr.message, tc.want) {
				t.Errorf("error = %q, want it to mention %q", rerr.message, tc.want)
			}
			if len(gate.calls) != 0 {
				t.Errorf("gate calls = %d, want 0 — an argument that can never be executed must not be put in front of a human", len(gate.calls))
			}
			if fake.count() != 0 {
				t.Errorf("kubernetes calls = %d, want 0", fake.count())
			}
		})
	}
}

// AC8. The whole-object half: what the caller wrote is what gets PUT, and a
// manifest describing some other object is refused here rather than by the
// apiserver — whose refusal would arrive after the approval was spent (AC7).
func TestUpdateReplacesWithTheCallersManifest(t *testing.T) {
	h, fake := approvingHandler(t)
	args := `{"apiVersion":"v1","kind":"ConfigMap","namespace":"ops","name":"app",` +
		`"manifest":{"apiVersion":"v1","kind":"ConfigMap","metadata":{"name":"app"},"data":{"LOG_LEVEL":"debug"}}}`

	if _, rerr := callTool(t, h, "resource_update", args); rerr != nil {
		t.Fatalf("tools/call = %v, want the replacement to run", rerr)
	}
	ref := fake.update()
	if ref.Subresource != "" {
		t.Errorf("subresource = %q, want empty for a whole-object replacement", ref.Subresource)
	}
	data, _ := ref.Manifest["data"].(map[string]any)
	if data["LOG_LEVEL"] != "debug" {
		t.Errorf("manifest = %v, want the caller's body carried through", ref.Manifest)
	}

	mismatches := []struct {
		name string
		args string
		want string
	}{
		{"name", `{"apiVersion":"v1","kind":"ConfigMap","namespace":"ops","name":"app","manifest":{"metadata":{"name":"other"}}}`, "metadata.name"},
		{"kind", `{"apiVersion":"v1","kind":"ConfigMap","namespace":"ops","name":"app","manifest":{"kind":"Secret","metadata":{"name":"app"}}}`, "manifest kind"},
		{"empty manifest", `{"apiVersion":"v1","kind":"ConfigMap","namespace":"ops","name":"app","manifest":{}}`, "non-empty object"},
	}
	for _, tc := range mismatches {
		t.Run(tc.name, func(t *testing.T) {
			h, fake := approvingHandler(t)
			_, rerr := callTool(t, h, "resource_update", tc.args)
			if rerr == nil {
				t.Fatal("tools/call = nil error, want a refusal")
			}
			if !strings.Contains(rerr.message, tc.want) {
				t.Errorf("error = %q, want it to mention %q", rerr.message, tc.want)
			}
			if fake.count() != 0 {
				t.Errorf("kubernetes calls = %d, want 0", fake.count())
			}
		})
	}
}
