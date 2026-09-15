package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/dlddu/homelab-k3s-mcp/internal/gatekeeper"
	"github.com/dlddu/homelab-k3s-mcp/internal/k8s"
)

type createGate struct {
	calls     []gatekeeper.Call
	contexts  []string
	decisions []*gatekeeper.Decision
	failAt    int
	err       error
	duplicate bool
	auto      bool
	before    func(int)
}

func (g *createGate) Authorize(ctx context.Context, call gatekeeper.Call) (*gatekeeper.Decision, error) {
	g.calls = append(g.calls, call)
	if g.before != nil {
		g.before(len(g.calls))
	}
	description, err := call.Describe(ctx)
	if err != nil {
		return nil, err
	}
	g.contexts = append(g.contexts, description)
	if len(g.calls) == g.failAt {
		return nil, g.err
	}
	id := len(g.calls)
	if g.duplicate {
		id = 1
	}
	d := &gatekeeper.Decision{RequestID: fmt.Sprintf("req-%d", id), ExternalID: fmt.Sprintf("ext-%d", id), AutoApproved: g.auto}
	g.decisions = append(g.decisions, d)
	return d, nil
}

type createK8s struct {
	countingK8s
	refs   []k8s.CreateRef
	failAt int
	before func(k8s.CreateRef)
}

func (c *createK8s) CreateResource(_ context.Context, ref k8s.CreateRef) (*k8s.ResourceResult, error) {
	c.hit()
	c.refs = append(c.refs, ref)
	if c.before != nil {
		c.before(ref)
	}
	if len(c.refs) == c.failAt {
		return nil, k8s.APIError("409 Conflict: already exists")
	}
	return &k8s.ResourceResult{}, nil
}

func createHandler(t *testing.T, gate gatekeeper.Gate) (*Handler, *createK8s, *stateReader) {
	t.Helper()
	h, _, reader := testHandlerReading(t, gate, toolRegistry, newStateReader())
	service := &createK8s{}
	h.k8s = service
	return h, service, reader
}

func createArgs(t *testing.T, manifest any) string {
	t.Helper()
	data, err := json.Marshal(map[string]any{"manifest": manifest})
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

const createTwo = `apiVersion: apps/v1
kind: Deployment
metadata:
  name: first
  namespace: ops
spec:
  replicas: 2
---
apiVersion: v1
kind: Service
metadata:
  name: second
  namespace: ops
spec:
  ports:
    - port: 8080
`

func TestMultiDocCreateRequiresApprovalPerDoc(t *testing.T) {
	gate := &createGate{}
	h, service, reader := createHandler(t, gate)
	var order []string
	gate.before = func(i int) {
		if service.count() != 0 || len(reader.reads) != 0 {
			t.Fatal("Kubernetes reached before every document was approved")
		}
		order = append(order, fmt.Sprintf("approve-%d", i))
	}
	service.before = func(ref k8s.CreateRef) { order = append(order, "create-"+ref.Name) }
	result, rerr := callTool(t, h, "resource_create", createArgs(t, createTwo))
	if rerr != nil || result.(map[string]any)["isError"] != false {
		t.Fatalf("create = %v, %v", result, rerr)
	}
	if !reflect.DeepEqual(order, []string{"approve-1", "approve-2", "create-first", "create-second"}) {
		t.Fatalf("order = %v", order)
	}
	if len(reader.reads) != 0 {
		t.Fatal("create read a nonexistent target")
	}
	for i, want := range []gatekeeper.Pair{{Verb: "create", Resource: "deployments"}, {Verb: "create", Resource: "services"}} {
		if gate.calls[i].Pair != want {
			t.Errorf("pair %d = %v, want %v", i, gate.calls[i].Pair, want)
		}
		if err := gate.decisions[i].Consume(); !errors.Is(err, gatekeeper.ErrConsumed) {
			t.Errorf("decision %d was not consumed by its write", i)
		}
	}
	if strings.Contains(gate.contexts[0], "second") || strings.Contains(gate.contexts[1], "first") ||
		!strings.Contains(gate.contexts[0], "replicas") || !strings.Contains(gate.contexts[1], "8080") {
		t.Fatalf("contexts do not describe exactly one document: %v", gate.contexts)
	}
	if service.refs[0].Manifest["spec"].(map[string]any)["replicas"].(json.Number).String() != "2" {
		t.Fatal("manifest changed before execution")
	}
}

func TestMultiDocCreateRejectionCreatesNothing(t *testing.T) {
	for _, reason := range []error{errors.New("REJECTED"), errors.New("EXPIRED"), context.DeadlineExceeded,
		gatekeeper.ErrNotConfigured, errors.New("gatekeeper HTTP 500"), errors.New("network unavailable")} {
		t.Run(reason.Error(), func(t *testing.T) {
			gate := &createGate{failAt: 2, err: reason}
			h, service, reader := createHandler(t, gate)
			_, rerr := callTool(t, h, "resource_create", createArgs(t, createTwo))
			if rerr == nil || service.count() != 0 || len(reader.reads) != 0 {
				t.Fatalf("refusal=%v calls=%d reads=%d", rerr, service.count(), len(reader.reads))
			}
			if err := gate.decisions[0].Consume(); err != nil {
				t.Fatalf("unused first approval was consumed: %v", err)
			}
		})
	}
}

func TestMultiDocCreateStopsAndReportsOnFailure(t *testing.T) {
	gate := &createGate{}
	h, service, _ := createHandler(t, gate)
	service.failAt = 2
	manifest := createTwo + "---\napiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: third\n  namespace: ops\ndata:\n  value: body-must-not-appear\n"
	result, rerr := callTool(t, h, "resource_create", createArgs(t, manifest))
	if rerr != nil {
		t.Fatal(rerr)
	}
	m := result.(map[string]any)
	payload := m["structuredContent"].(map[string]any)
	created := payload["created"].([]any)
	failed := payload["failed"].(map[string]any)
	unattempted := payload["unattempted"].([]any)
	if m["isError"] != true || len(created) != 1 || created[0].(map[string]any)["name"] != "first" ||
		failed["name"] != "second" || !strings.Contains(failed["error"].(string), "409") ||
		len(unattempted) != 1 || unattempted[0].(map[string]any)["name"] != "third" {
		t.Fatalf("partial failure = %v", payload)
	}
	if service.count() != 2 || len(service.refs) != 2 || len(gate.calls) != 3 {
		t.Fatalf("calls=%d creates=%d approvals=%d", service.count(), len(service.refs), len(gate.calls))
	}
	if err := gate.decisions[2].Consume(); err != nil {
		t.Fatalf("unattempted approval was consumed: %v", err)
	}
	if strings.Contains(prettyJSON(result), "body-must-not-appear") {
		t.Fatal("result contains manifest values")
	}
}

func TestCreateRejectsMalformedBatchBeforeApprovals(t *testing.T) {
	for _, manifest := range []any{nil, "", "---\n# empty\n", []any{}, "- bad", map[string]any{},
		"apiVersion: v1\nkind: ConfigMap\nmetadata: {generateName: random-}",
		"apiVersion: v1\nkind: ConfigMap\nmetadata: {name: one, namespace: 2}",
		"apiVersion: v1\nkind: Secret\nmetadata: {name: one}\ndata: {password: [sensitive-broken-value}",
		"apiVersion: v1\nkind: ConfigMap\nkind: Secret\nmetadata: {name: one}",
		createTwo + "---\napiVersion: v1\nkind: ConfigMap\nmetadata: {}"} {
		gate := &createGate{}
		h, service, reader := createHandler(t, gate)
		_, rerr := callTool(t, h, "resource_create", createArgs(t, manifest))
		if rerr == nil || len(gate.calls) != 0 || service.count() != 0 || len(reader.reads) != 0 {
			t.Fatalf("invalid manifest accepted: error=%v approvals=%d writes=%d", rerr, len(gate.calls), service.count())
		}
		if strings.Contains(rerr.message, "sensitive-broken-value") {
			t.Fatal("parse error echoed manifest content")
		}
	}
}

func TestCreateMasksEachSensitiveDocument(t *testing.T) {
	gate := &createGate{}
	h, service, _ := createHandler(t, gate)
	manifest := "apiVersion: v1\nkind: Secret\nmetadata: {name: credentials, namespace: ops}\ndata: {password: c2VjcmV0}\nstringData: {token: sensitive-token}\n"
	result, rerr := callTool(t, h, "resource_create", createArgs(t, manifest))
	if rerr != nil || len(gate.contexts) != 1 {
		t.Fatalf("create = %v, %v", result, rerr)
	}
	for _, marker := range []string{"c2VjcmV0", "sensitive-token"} {
		if strings.Contains(gate.contexts[0], marker) || strings.Contains(prettyJSON(result), marker) {
			t.Fatalf("secret value escaped: %s", marker)
		}
	}
	if !strings.Contains(gate.contexts[0], "password") || !strings.Contains(gate.contexts[0], "6B") ||
		service.refs[0].Manifest["stringData"].(map[string]any)["token"] != "sensitive-token" {
		t.Fatal("masking changed the written value or omitted the key/size")
	}
}

func TestCreateNeedsDistinctFreshApprovals(t *testing.T) {
	gate := &createGate{duplicate: true}
	h, service, _ := createHandler(t, gate)
	if _, rerr := callTool(t, h, "resource_create", createArgs(t, createTwo)); rerr == nil || service.count() != 0 {
		t.Fatal("duplicate approval authorized a batch")
	}
	decision := &gatekeeper.Decision{RequestID: "one-use"}
	replay := &scriptedGate{decision: decision}
	h, service, _ = createHandler(t, replay)
	args := createArgs(t, map[string]any{"apiVersion": "v1", "kind": "Namespace", "metadata": map[string]any{"name": "one"}})
	if _, rerr := callTool(t, h, "resource_create", args); rerr != nil {
		t.Fatal(rerr)
	}
	result, rerr := callTool(t, h, "resource_create", args)
	if rerr != nil || result.(map[string]any)["isError"] != true || service.count() != 1 {
		t.Fatalf("spent approval reused: result=%v error=%v calls=%d", result, rerr, service.count())
	}
}

func TestCreateCannotBypassItsDeclaration(t *testing.T) {
	gate := &createGate{}
	h, service, _ := createHandler(t, gate)
	entry := toolRegistry["resource_create"]
	entry.decl.outsideGate = "invalid exemption"
	h.registry = map[string]toolEntry{"resource_create": entry}
	if _, rerr := callTool(t, h, "resource_create", createArgs(t, createTwo)); rerr == nil || service.count() != 0 || len(gate.calls) != 0 {
		t.Fatal("create accepted a declaration that omitted its approval")
	}
}

func TestCreateMasksMalformedCredentialFields(t *testing.T) {
	for _, value := range []any{"malformed-secret-value", []any{"malformed-secret-value"}, map[string]any{"key": []any{"malformed-secret-value"}}} {
		gate := &createGate{}
		h, _, _ := createHandler(t, gate)
		manifest := map[string]any{"apiVersion": "v1", "kind": "Secret", "metadata": map[string]any{"name": "one", "namespace": "ops"}, "data": value}
		if _, rerr := callTool(t, h, "resource_create", createArgs(t, manifest)); rerr != nil {
			t.Fatal(rerr)
		}
		if strings.Contains(strings.Join(gate.contexts, ""), "malformed-secret-value") {
			t.Fatal("malformed credential field leaked before API validation")
		}
	}
}

func TestCreateAutoApprovalIsVisibleForEachAttemptedDocument(t *testing.T) {
	gate := &createGate{auto: true}
	h, service, _ := createHandler(t, gate)
	service.failAt = 2
	result, rerr := callTool(t, h, "resource_create", createArgs(t, createTwo))
	if rerr != nil {
		t.Fatal(rerr)
	}
	text := prettyJSON(result)
	if !strings.Contains(text, "without human review") || !strings.Contains(text, "req-1") || !strings.Contains(text, "req-2") {
		t.Fatalf("auto approvals hidden on partial failure: %s", text)
	}
}
