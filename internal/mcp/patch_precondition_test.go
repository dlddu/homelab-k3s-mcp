package mcp

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"

	"github.com/dlddu/homelab-k3s-mcp/internal/gatekeeper"
	"github.com/dlddu/homelab-k3s-mcp/internal/k8s"
)

const conditionalPatchArgs = `{"apiVersion":"v1","kind":"ConfigMap","namespace":"ops","name":"api","patchType":"merge","patch":{"data":{"key":"value"}}}`

func TestPatchCarriesOnlyGateApprovedTarget(t *testing.T) {
	h, fake := approvingHandler(t)
	args := strings.TrimSuffix(conditionalPatchArgs, "}") + `,"approvedResourceVersion":"forged","approvedUID":"forged"}`
	if _, err := callTool(t, h, "resource_patch", args); err != nil {
		t.Fatal(err)
	}
	ref := fake.patch()
	if ref.ApprovedResourceVersion != "100" || ref.ApprovedUID != "uid-1" {
		t.Fatalf("approved target=%+v", ref)
	}
	if string(ref.Patch) != `{"data":{"key":"value"}}` {
		t.Fatal("caller patch bytes changed before service")
	}
}

func TestPatchMissingOrChangedApprovalMakesNoWrite(t *testing.T) {
	for _, state := range []k8s.TargetState{{ResourceVersion: "100"}, {UID: "uid-1"}, {ResourceVersion: "101", UID: "uid-1"}, {ResourceVersion: "100", UID: "new-uid"}} {
		gate := &scriptedGate{decision: &gatekeeper.Decision{RequestID: "req-1"}}
		reader := newStateReader()
		if state.ResourceVersion == "" || state.UID == "" {
			reader.state = state
		} else {
			reader.next = &state
		}
		h, fake, _ := testHandlerReading(t, gate, toolRegistry, reader)
		if _, err := callTool(t, h, "resource_patch", conditionalPatchArgs); err == nil {
			t.Fatal("missing/changed state accepted")
		}
		if fake.count() != 0 {
			t.Fatal("refusal reached service")
		}
	}
	h, fake := approvingHandler(t)
	if _, err := h.resourcePatch(context.Background(), json.RawMessage(conditionalPatchArgs)); err == nil {
		t.Fatal("handler accepted absent approval context")
	}
	if fake.count() != 0 {
		t.Fatal("unguarded handler reached service")
	}
}

type refusingPatchService struct{ *countingK8s }

func (s *refusingPatchService) PatchResource(_ context.Context, ref k8s.PatchRef) (*k8s.ResourceResult, error) {
	s.hit()
	return nil, k8s.APIError("patch refused: approved target version conflicts; call ended without retry or automatic reapproval")
}

func TestPatchConflictEndsToolsCall(t *testing.T) {
	gate := &scriptedGate{decision: &gatekeeper.Decision{RequestID: "req-1"}}
	h, fake, reader := testHandlerReading(t, gate, toolRegistry, newStateReader())
	h.k8s = &refusingPatchService{fake}
	result, rerr := callTool(t, h, "resource_patch", conditionalPatchArgs)
	if rerr != nil {
		t.Fatal(rerr)
	}
	body := result.(map[string]any)
	if body["isError"] != true || body["structuredContent"] != nil {
		t.Fatalf("conflict returned success: %v", body)
	}
	if len(gate.calls) != 1 || fake.count() != 1 || len(reader.observed()) != 2 {
		t.Fatalf("approval=%d writes=%d reads=%d", len(gate.calls), fake.count(), len(reader.observed()))
	}
}

type isolatedPatchService struct {
	*countingK8s
	t *testing.T
}

func (s *isolatedPatchService) PatchResource(_ context.Context, ref k8s.PatchRef) (*k8s.ResourceResult, error) {
	if ref.ApprovedResourceVersion != ref.Name {
		s.t.Errorf("cross-call target: name=%s version=%s", ref.Name, ref.ApprovedResourceVersion)
	}
	return &k8s.ResourceResult{Object: map[string]any{}}, nil
}

func TestPatchApprovalContextIsPerCall(t *testing.T) {
	h, _ := approvingHandler(t)
	h.k8s = &isolatedPatchService{&countingK8s{}, t}
	var wg sync.WaitGroup
	for _, name := range []string{"one", "two", "three", "four"} {
		wg.Add(1)
		go func(name string) {
			defer wg.Done()
			ctx := context.WithValue(context.Background(), approvedTargetKey{}, k8s.TargetState{ResourceVersion: name, UID: "uid-" + name})
			raw := strings.Replace(conditionalPatchArgs, `"name":"api"`, `"name":"`+name+`"`, 1)
			if _, err := h.resourcePatch(ctx, json.RawMessage(raw)); err != nil {
				t.Error(err)
			}
		}(name)
	}
	wg.Wait()
}
