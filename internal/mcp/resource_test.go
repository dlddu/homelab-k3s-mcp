package mcp

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/dlddu/homelab-k3s-mcp/internal/gatekeeper"
	"github.com/dlddu/homelab-k3s-mcp/internal/k8s"
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

	ctx := gate.context(0)
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
	ctx := gate.context(0)
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
	ctx := gate.context(0)
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

// AC10. The accepted call and the refused ones share a test because this tool's
// contract is one statement: it removes the object you named, and it has no way
// to say "the ones matching this".
//
// Every refusal also asserts the gate was never called — the half scenario 10
// spells out as "인자 검증에서 거부됨". It holds only because these checks run
// during pair resolution rather than in the handler, which is downstream of
// authorize.
func TestDeleteIsSingleObjectOnly(t *testing.T) {
	t.Run("named object", func(t *testing.T) {
		gate := &scriptedGate{decision: &gatekeeper.Decision{RequestID: "req-1"}}
		h, fake := testHandler(t, gate, toolRegistry)
		args := `{"apiVersion":"v1","kind":"ConfigMap","namespace":"ops","name":"app","gracePeriodSeconds":0}`

		if _, rerr := callTool(t, h, "resource_delete", args); rerr != nil {
			t.Fatalf("tools/call = %v, want the deletion to run", rerr)
		}
		ref := fake.delete()
		if ref.Name != "app" || ref.Namespace == nil || *ref.Namespace != "ops" {
			t.Errorf("coordinate = %s/%v, want ops/app", ref.Name, ref.Namespace)
		}
		// 0 is the value a pointer-less field could not carry: "delete now" and
		// "use the kind's default" are different requests.
		if ref.GracePeriodSeconds == nil || *ref.GracePeriodSeconds != 0 {
			t.Errorf("gracePeriodSeconds reaching the cluster layer = %v, want 0", ref.GracePeriodSeconds)
		}
		if len(gate.calls) != 1 {
			t.Fatalf("gate calls = %d, want 1 — every deletion is approved", len(gate.calls))
		}
		if got := gate.calls[0].Pair.String(); got != "delete on configmaps" {
			t.Errorf("approved pair = %q, want \"delete on configmaps\"", got)
		}
	})

	t.Run("grace period left to the kind", func(t *testing.T) {
		h, fake := approvingHandler(t)
		args := `{"apiVersion":"v1","kind":"ConfigMap","namespace":"ops","name":"app"}`

		if _, rerr := callTool(t, h, "resource_delete", args); rerr != nil {
			t.Fatalf("tools/call = %v, want the deletion to run", rerr)
		}
		if got := fake.delete().GracePeriodSeconds; got != nil {
			t.Errorf("gracePeriodSeconds = %d, want it left unset so the kind's own default stands", *got)
		}
	})

	refusals := []struct {
		name string
		args string
		want string
	}{
		{"selector instead of a name", `{"apiVersion":"v1","kind":"ConfigMap","namespace":"ops","labelSelector":"app=api"}`, "resource_delete_collection"},
		{"selector alongside a name", `{"apiVersion":"v1","kind":"ConfigMap","namespace":"ops","name":"app","labelSelector":"app=api"}`, "labelSelector does not apply"},
		{"field selector", `{"apiVersion":"v1","kind":"Pod","namespace":"ops","name":"api","fieldSelector":"status.phase=Failed"}`, "fieldSelector does not apply"},
		// A subresource would be appended to the resolved pair, so the operator
		// would approve "delete on configmaps/<sub>" while the handler removes
		// the object itself (prd-approval-gate AC6).
		{"subresource", `{"apiVersion":"apps/v1","kind":"Deployment","namespace":"ops","name":"api","subresource":"scale"}`, "subresource does not apply"},
		{"missing name", `{"apiVersion":"v1","kind":"ConfigMap","namespace":"ops"}`, "name is required"},
		{"negative grace period", `{"apiVersion":"v1","kind":"ConfigMap","namespace":"ops","name":"app","gracePeriodSeconds":-1}`, "gracePeriodSeconds must be >= 0"},
		{"non-integer grace period", `{"apiVersion":"v1","kind":"ConfigMap","namespace":"ops","name":"app","gracePeriodSeconds":"none"}`, "gracePeriodSeconds must be an integer"},
	}
	for _, tc := range refusals {
		t.Run(tc.name, func(t *testing.T) {
			gate := &scriptedGate{decision: &gatekeeper.Decision{RequestID: "req-1"}}
			h, fake := testHandler(t, gate, toolRegistry)

			_, rerr := callTool(t, h, "resource_delete", tc.args)
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

// AC1/AC10. The control group for the case above: the refusals all assert the
// gate was not called, and a tool that was never gated would pass every one of
// them.
func TestDeleteIsRefusedWithoutApproval(t *testing.T) {
	h, fake := testHandler(t, gatekeeper.NewUnavailable(nil), toolRegistry)
	args := `{"apiVersion":"v1","kind":"ConfigMap","namespace":"ops","name":"app"}`

	if _, rerr := callTool(t, h, "resource_delete", args); rerr == nil {
		t.Fatal("tools/call = nil error, want the deletion refused without approval")
	}
	if fake.count() != 0 {
		t.Errorf("kubernetes calls = %d, want 0", fake.count())
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

// AC6. The window is the one bound this level owns whole — how long the
// apiserver is asked to push for, and what happens to a value past the
// ceiling. The refusals share the table with the accepted values on purpose:
// "61 is refused" only means something beside "60 is not".
func TestWatchWindowBounds(t *testing.T) {
	accepted := []struct {
		name string
		args string
		want int64
	}{
		{"default", `{"apiVersion":"apps/v1","kind":"Deployment","namespace":"ops"}`, 10},
		{"explicit", `{"apiVersion":"apps/v1","kind":"Deployment","namespace":"ops","watchSeconds":30}`, 30},
		{"ceiling", `{"apiVersion":"apps/v1","kind":"Deployment","namespace":"ops","watchSeconds":60}`, 60},
	}
	for _, tc := range accepted {
		t.Run(tc.name, func(t *testing.T) {
			h, fake := approvingHandler(t)
			if _, rerr := callTool(t, h, "resource_watch", tc.args); rerr != nil {
				t.Fatalf("tools/call = %v, want the watch to run", rerr)
			}
			if got := fake.watch().Seconds; got != tc.want {
				t.Errorf("window reaching the cluster layer = %ds, want %ds", got, tc.want)
			}
		})
	}

	refusals := []struct {
		name string
		args string
		want string
	}{
		{"above the ceiling", `{"apiVersion":"apps/v1","kind":"Deployment","namespace":"ops","watchSeconds":61}`, "watchSeconds must be <= 60"},
		{"zero", `{"apiVersion":"apps/v1","kind":"Deployment","namespace":"ops","watchSeconds":0}`, "watchSeconds must be >= 1"},
		{"not an integer", `{"apiVersion":"apps/v1","kind":"Deployment","namespace":"ops","watchSeconds":"ten"}`, "watchSeconds must be an integer"},
		{"no coordinate", `{"kind":"Deployment","namespace":"ops"}`, "apiVersion"},
	}
	for _, tc := range refusals {
		t.Run(tc.name, func(t *testing.T) {
			h, fake := approvingHandler(t)
			_, rerr := callTool(t, h, "resource_watch", tc.args)
			if rerr == nil {
				t.Fatal("tools/call = nil error, want a refusal rather than a silent clamp")
			}
			if !strings.Contains(rerr.message, tc.want) {
				t.Errorf("error = %q, want it to mention %q", rerr.message, tc.want)
			}
			if fake.count() != 0 {
				t.Errorf("kubernetes calls = %d, want 0 — a refused window must not reach the cluster", fake.count())
			}
		})
	}
}

// AC6 carries the resume point through untouched: a window the caller cannot
// continue from is a window that has to start over, and starting over is where
// a change goes missing.
func TestWatchCarriesTheResumePoint(t *testing.T) {
	h, fake := approvingHandler(t)
	args := `{"apiVersion":"apps/v1","kind":"Deployment","namespace":"ops",` +
		`"resourceVersion":"4816","labelSelector":"app=api"}`

	if _, rerr := callTool(t, h, "resource_watch", args); rerr != nil {
		t.Fatalf("tools/call = %v, want the watch to run", rerr)
	}
	query := fake.watch()
	if query.ResourceVersion != "4816" {
		t.Errorf("resourceVersion reaching the cluster layer = %q, want %q", query.ResourceVersion, "4816")
	}
	if query.LabelSelector == nil || *query.LabelSelector != "app=api" {
		t.Errorf("labelSelector = %v, want it passed through for the apiserver to apply", query.LabelSelector)
	}
}

// AC6/AC16/AC17. A watch of a sensitive kind is gated for the reason a get is:
// the stream hands over the whole object. The ordinary kind is in the same
// test because "Secret is refused" is only an assertion about the gate if
// something else is not.
func TestWatchOnGatedKindRequiresApproval(t *testing.T) {
	secret := `{"apiVersion":"v1","kind":"Secret","namespace":"ops"}`
	ordinary := `{"apiVersion":"apps/v1","kind":"Deployment","namespace":"ops"}`

	t.Run("refused without approval", func(t *testing.T) {
		gate := &scriptedGate{err: errors.New("no approval")}
		h, fake := testHandler(t, gate, toolRegistry)
		_, rerr := callTool(t, h, "resource_watch", secret)
		if rerr == nil {
			t.Fatal("tools/call = nil error, want the gate to refuse")
		}
		if fake.count() != 0 {
			t.Errorf("kubernetes calls = %d, want 0 — AC16 refuses before the cluster is touched", fake.count())
		}
		if len(gate.calls) != 1 {
			t.Fatalf("approval requests = %d, want 1", len(gate.calls))
		}
		if got := gate.calls[0].Pair.String(); got != "watch on secrets" {
			t.Errorf("gated pair = %q, want %q", got, "watch on secrets")
		}
	})

	t.Run("runs once approved", func(t *testing.T) {
		gate := &scriptedGate{decision: &gatekeeper.Decision{RequestID: "req-1"}}
		h, fake := testHandler(t, gate, toolRegistry)
		if _, rerr := callTool(t, h, "resource_watch", secret); rerr != nil {
			t.Fatalf("tools/call = %v, want the approved watch to run", rerr)
		}
		if fake.count() != 1 {
			t.Errorf("kubernetes calls = %d, want 1", fake.count())
		}
	})

	t.Run("ordinary kind needs no approval", func(t *testing.T) {
		gate := &scriptedGate{err: errors.New("no approval")}
		h, fake := testHandler(t, gate, toolRegistry)
		if _, rerr := callTool(t, h, "resource_watch", ordinary); rerr != nil {
			t.Fatalf("tools/call = %v, want an ungated watch to run", rerr)
		}
		if len(gate.calls) != 0 {
			t.Errorf("approval requests = %d, want 0 — watch is not a state-changing verb", len(gate.calls))
		}
		if fake.count() != 1 {
			t.Errorf("kubernetes calls = %d, want 1", fake.count())
		}
	})
}

// AC12: exec runs one command in one named pod and answers with stdout and
// stderr separated, the refusals that arguments can see happen before an
// approval is spent, and a response the caps cut says so.
func TestExecStreamsAndCaps(t *testing.T) {
	t.Run("runs and hands the pod, the container and the command to the cluster", func(t *testing.T) {
		gate := &scriptedGate{decision: &gatekeeper.Decision{RequestID: "req-1"}}
		h, fake := testHandler(t, gate, toolRegistry)

		outcome := &k8s.ExecOutcome{
			Pod:      "api",
			Stdout:   "out\n",
			Stderr:   "err\n",
			ExitCode: int32PtrExec(1),
			Success:  false,
		}
		fake.execOutcome = outcome

		result, rerr := callTool(t, h, "resource_exec", `{"apiVersion":"v1","kind":"Pod","namespace":"ops","name":"api","container":"chatty","command":["echo","hi"]}`)
		if rerr != nil {
			t.Fatalf("tools/call = %v, want the approved exec to run", rerr)
		}
		ref, container, command := fake.exec()
		if ref.APIVersion != "v1" || ref.Kind != "Pod" || ref.Namespace == nil || *ref.Namespace != "ops" || ref.Name != "api" {
			t.Errorf("exec coordinate = %+v, want v1 Pod ops/api", ref)
		}
		if container == nil || *container != "chatty" {
			if container == nil {
				t.Error("exec container = nil, want chatty")
			} else {
				t.Errorf("exec container = %q, want chatty", *container)
			}
		}
		if strings.Join(command, " ") != "echo hi" {
			t.Errorf("exec command = %v, want [echo hi]", command)
		}
		if fake.count() != 1 {
			t.Errorf("kubernetes calls = %d, want 1", fake.count())
		}

		payload := result.(map[string]any)["structuredContent"].(map[string]any)
		if got := payload["stdout"]; got != "out\n" {
			t.Errorf("stdout = %v, want out\n", got)
		}
		if got := payload["stderr"]; got != "err\n" {
			t.Errorf("stderr = %v, want err\n", got)
		}
		if code, ok := payload["exitCode"].(*int32); !ok || code == nil || *code != 1 {
			t.Errorf("exitCode = %v, want 1", payload["exitCode"])
		}
		if got := gate.calls[0].Pair.String(); got != "create on pods/exec" {
			t.Errorf("gated pair = %q, want %q", got, "create on pods/exec")
		}
	})

	t.Run("stream truncation is marked, not silent", func(t *testing.T) {
		gate := &scriptedGate{decision: &gatekeeper.Decision{RequestID: "req-1"}}
		h, fake := testHandler(t, gate, toolRegistry)
		fake.execOutcome = &k8s.ExecOutcome{
			Stdout:          strings.Repeat("y", 100),
			StdoutTruncated: true,
			StderrTruncated: true,
			TimeLimited:     true,
		}
		result, rerr := callTool(t, h, "resource_exec", `{"apiVersion":"v1","kind":"Pod","namespace":"ops","name":"api","command":["yes"]}`)
		if rerr != nil {
			t.Fatalf("tools/call = %v, want a capped exec to still answer", rerr)
		}
		payload := result.(map[string]any)["structuredContent"].(map[string]any)
		if payload["stdoutTruncated"] != true || payload["stderrTruncated"] != true {
			t.Errorf("truncation flags = %v/%v, want both true", payload["stdoutTruncated"], payload["stderrTruncated"])
		}
		if payload["timeLimited"] != true {
			t.Errorf("timeLimited = %v, want true — the time cap is what stopped the command", payload["timeLimited"])
		}
	})

	refusals := []struct {
		name string
		args string
		want string
	}{
		{"missing command", `{"apiVersion":"v1","kind":"Pod","namespace":"ops","name":"api"}`, "command is required"},
		{"empty command", `{"apiVersion":"v1","kind":"Pod","namespace":"ops","name":"api","command":[]}`, "non-empty array"},
		{"non-string command element", `{"apiVersion":"v1","kind":"Pod","namespace":"ops","name":"api","command":["echo",1]}`, "command element 1"},
		{"missing name", `{"apiVersion":"v1","kind":"Pod","namespace":"ops","command":["echo"]}`, "name is required"},
		{"missing kind", `{"apiVersion":"v1","namespace":"ops","name":"api","command":["echo"]}`, "kind is required"},
		{"explicit subresource", `{"apiVersion":"v1","kind":"Pod","namespace":"ops","name":"api","subresource":"exec","command":["echo"]}`, "pass no subresource"},
	}
	for _, tc := range refusals {
		t.Run(tc.name, func(t *testing.T) {
			gate := &scriptedGate{decision: &gatekeeper.Decision{RequestID: "req-1"}}
			h, fake := testHandler(t, gate, toolRegistry)
			_, rerr := callTool(t, h, "resource_exec", tc.args)
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

// The exec refusal AC6 names is a pod recreated under the same name; the shared
// table over every gated tool carries the case.
func TestExecContextCarriesTheCommand(t *testing.T) {
	reader := newStateReader()
	gate := &scriptedGate{decision: &gatekeeper.Decision{RequestID: "req-1"}}
	h, _, _ := testHandlerReading(t, gate, toolRegistry, reader)

	args := `{"apiVersion":"v1","kind":"Pod","namespace":"ops","name":"api","container":"chatty","command":["echo","hi"]}`
	if _, rerr := callTool(t, h, "resource_exec", args); rerr != nil {
		t.Fatalf("tools/call = %v, want the approved exec to run", rerr)
	}
	ctx := gate.context(0)
	for _, want := range []string{"echo", "hi", "chatty", "create on pods/exec", "v1 Pod ops/api"} {
		if !strings.Contains(ctx, want) {
			t.Errorf("approval context is missing %q:\n%s", want, ctx)
		}
	}
}

func int32PtrExec(n int32) *int32 { return &n }
