package mcp

import (
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
