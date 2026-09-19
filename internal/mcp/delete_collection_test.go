package mcp

import (
	"fmt"
	"strings"
	"testing"

	"github.com/dlddu/homelab-k3s-mcp/internal/gatekeeper"
	"github.com/dlddu/homelab-k3s-mcp/internal/k8s"
)

const collectionArgs = `{"apiVersion":"v1","kind":"ConfigMap","namespace":"ops","labelSelector":"app=api"}`

// AC11: "셀렉터가 0건을 가리키면 승인 요청을 만들지 않고 그대로 알린다". Both halves
// are asserted, because either one alone is the wrong behaviour: an approval
// request for nothing wastes an operator's attention, and a deletecollection
// that ran without one is a gated verb reaching the cluster unjudged.
func TestEmptySelectionAsksNobodyAndDeletesNothing(t *testing.T) {
	gate := &scriptedGate{decision: &gatekeeper.Decision{RequestID: "req-1"}}
	empty := newCollectionReader()
	empty.state = k8s.CollectionTargetState{ResourceVersion: "500", Targets: nil}
	h, fake, lister := testHandlerListing(t, gate, toolRegistry, newStateReader(), empty)

	result, rerr := callTool(t, h, "resource_delete_collection", collectionArgs)
	if rerr != nil {
		t.Fatalf("tools/call = %v, want an empty selection to be reported rather than refused", rerr)
	}
	if len(gate.calls) != 0 {
		t.Errorf("approval requests = %d, want 0 for a selector that matches nothing", len(gate.calls))
	}
	if _, calls := fake.deleteCollection(); calls != 0 {
		t.Errorf("deletecollection calls = %d, want 0", calls)
	}
	if len(lister.listed()) != 1 {
		t.Errorf("gate listings = %d, want exactly the one that found nothing", len(lister.listed()))
	}
	if !strings.Contains(resultText(t, result), "matched no objects") {
		t.Errorf("result %v does not say the selector matched nothing", result)
	}
}

// AC3's deletecollection row: the count and the names are what the operator is
// judging. A context that named the selector but not its members would be
// asking someone to approve a number they cannot see.
func TestCollectionContextCarriesCountAndNames(t *testing.T) {
	gate := &scriptedGate{decision: &gatekeeper.Decision{RequestID: "req-1"}}
	h, _, _ := testHandlerListing(t, gate, toolRegistry, newStateReader(),
		newCollectionReader("api-1", "api-2", "api-3"))

	if _, rerr := callTool(t, h, "resource_delete_collection", collectionArgs); rerr != nil {
		t.Fatalf("tools/call = %v", rerr)
	}
	shown := gate.context(0)
	for _, want := range []string{"targets: 3", "ops/api-1", "ops/api-2", "ops/api-3", "app=api", "deletecollection"} {
		if !strings.Contains(shown, want) {
			t.Errorf("approval context does not contain %q:\n%s", want, shown)
		}
	}
}

// AC3: "많으면 앞 20개와 총 개수". The cap is on the names, never on the count —
// an operator who sees 20 names and no total has been told the selection is
// smaller than it is.
func TestCollectionContextTruncatesNamesButNotTheCount(t *testing.T) {
	names := make([]string, 0, 25)
	for i := 0; i < 25; i++ {
		names = append(names, fmt.Sprintf("cm-%02d", i))
	}
	gate := &scriptedGate{decision: &gatekeeper.Decision{RequestID: "req-1"}}
	h, _, _ := testHandlerListing(t, gate, toolRegistry, newStateReader(), newCollectionReader(names...))

	if _, rerr := callTool(t, h, "resource_delete_collection", collectionArgs); rerr != nil {
		t.Fatalf("tools/call = %v", rerr)
	}
	shown := gate.context(0)
	if !strings.Contains(shown, "targets: 25") {
		t.Errorf("approval context does not carry the full count:\n%s", shown)
	}
	if !strings.Contains(shown, "and 5 more (25 total)") {
		t.Errorf("approval context does not say how many names it withheld:\n%s", shown)
	}
	if strings.Contains(shown, "ops/cm-20") {
		t.Errorf("approval context listed past the %d-name cap:\n%s", contextNameLimit, shown)
	}
	if !strings.Contains(shown, "ops/cm-19") {
		t.Errorf("approval context stopped short of the %d-name cap:\n%s", contextNameLimit, shown)
	}
}

// AC11's verification method: "승인과 실행 사이에 대상이 늘면 실행이 거부된다"
// (prd-approval-gate AC6). The operator approved a list of names, so a list
// with a different name on it is a different approval.
func TestCollectionThatGrewAfterApprovalIsRefused(t *testing.T) {
	gate := &scriptedGate{decision: &gatekeeper.Decision{RequestID: "req-1"}}
	lister := newCollectionReader("api-1", "api-2")
	grown := collectionState("api-1", "api-2", "api-3")
	lister.next = &grown
	h, fake, _ := testHandlerListing(t, gate, toolRegistry, newStateReader(), lister)

	_, rerr := callTool(t, h, "resource_delete_collection", collectionArgs)
	if rerr == nil {
		t.Fatal("tools/call = nil error, want a refusal")
	}
	if !strings.Contains(rerr.message, "needs a new one") {
		t.Errorf("refusal = %q, want it to say a new approval is needed", rerr.message)
	}
	if _, calls := fake.deleteCollection(); calls != 0 {
		t.Errorf("deletecollection calls = %d, want 0 after a changed selection", calls)
	}
}

// AC11: namespace is required, and the refusal has to come before the approval
// request. A missing namespace is not a narrower call than an approved one —
// the apiserver reads an absent namespace on a collection delete as every
// namespace, so this is the widest deletion the tool could make.
func TestCollectionWithoutNamespaceIsRefusedBeforeAnyApproval(t *testing.T) {
	gate := &scriptedGate{decision: &gatekeeper.Decision{RequestID: "req-1"}}
	h, fake, lister := testHandlerListing(t, gate, toolRegistry, newStateReader(), newCollectionReader())

	_, rerr := callTool(t, h, "resource_delete_collection", `{"apiVersion":"v1","kind":"ConfigMap","labelSelector":"app=api"}`)
	if rerr == nil {
		t.Fatal("tools/call = nil error, want a refusal")
	}
	if !strings.Contains(rerr.message, "namespace is required") {
		t.Errorf("refusal = %q, want it to name the missing namespace", rerr.message)
	}
	if len(gate.calls) != 0 {
		t.Errorf("approval requests = %d, want 0 for a call that was never valid", len(gate.calls))
	}
	if len(lister.listed()) != 0 {
		t.Errorf("gate listings = %d, want 0 — the coordinate was never bounded", len(lister.listed()))
	}
	if fake.count() != 0 {
		t.Errorf("kubernetes calls = %d, want 0", fake.count())
	}
}

// The selectors reach the apiserver exactly as they reached the gate. This is
// the assertion the call counter cannot make: a selection narrowed between the
// approval screen and the request is an approval for one thing and a deletion
// of another.
func TestApprovedCollectionDeleteSendsTheSelectorsUnchanged(t *testing.T) {
	gate := &scriptedGate{decision: &gatekeeper.Decision{RequestID: "req-1"}}
	h, fake, lister := testHandlerListing(t, gate, toolRegistry, newStateReader(), newCollectionReader("api-1"))

	if _, rerr := callTool(t, h, "resource_delete_collection",
		`{"apiVersion":"v1","kind":"ConfigMap","namespace":"ops","labelSelector":"app=api","fieldSelector":"metadata.name!=keep"}`); rerr != nil {
		t.Fatalf("tools/call = %v", rerr)
	}

	ref, calls := fake.deleteCollection()
	if calls != 1 {
		t.Fatalf("deletecollection calls = %d, want 1", calls)
	}
	if ref.Namespace != "ops" || ref.LabelSelector != "app=api" || ref.FieldSelector != "metadata.name!=keep" {
		t.Errorf("deletecollection ref = %+v, want the selection the operator approved", ref)
	}
	listings := lister.listed()
	if len(listings) != 2 {
		t.Fatalf("gate listings = %d, want 2 (describe, then the pre-execution re-read)", len(listings))
	}
	for i, listing := range listings {
		if listing.LabelSelector != "app=api" || listing.FieldSelector != "metadata.name!=keep" || listing.Namespace != "ops" {
			t.Errorf("listing %d = %+v, want the call's own selection", i, listing)
		}
	}
}

// resultText pulls the first text block out of a tool result.
func resultText(t *testing.T, result any) string {
	t.Helper()
	m, ok := result.(map[string]any)
	if !ok {
		t.Fatalf("result %v is not a tool result", result)
	}
	content, ok := m["content"].([]any)
	if !ok || len(content) == 0 {
		t.Fatalf("result %v has no content", result)
	}
	block, ok := content[0].(map[string]any)
	if !ok {
		t.Fatalf("result content %v is not a block", content[0])
	}
	text, _ := block["text"].(string)
	return text
}
