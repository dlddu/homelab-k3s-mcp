package mcp

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/dlddu/homelab-k3s-mcp/internal/gatekeeper"
	"github.com/dlddu/homelab-k3s-mcp/internal/k8s"
)

func TestUpdateReceivesTheGateApprovedVersion(t *testing.T) {
	for _, args := range []string{
		`{"apiVersion":"apps/v1","kind":"Deployment","namespace":"ops","name":"api","subresource":"scale","replicas":3,"resourceVersion":"999"}`,
		`{"apiVersion":"apps/v1","kind":"Deployment","namespace":"ops","name":"api","manifest":{"apiVersion":"apps/v1","kind":"Deployment","metadata":{"name":"api","namespace":"ops","resourceVersion":"100"},"spec":{"replicas":3}}}`,
	} {
		t.Run(args, func(t *testing.T) {
			reader := newStateReader()
			gate := &scriptedGate{decision: &gatekeeper.Decision{RequestID: "req-1"}}
			h, service, _ := testHandlerReading(t, gate, toolRegistry, reader)
			_, rerr := callTool(t, h, "resource_update", args)
			if rerr != nil {
				t.Fatalf("tools/call = %v", rerr)
			}
			if service.count() != 1 || service.update().ApprovedResourceVersion != "100" {
				t.Fatalf("update calls = %d, approved version = %q", service.count(), service.update().ApprovedResourceVersion)
			}
			if !strings.Contains(gate.context(0), "target resourceVersion: 100") {
				t.Errorf("approval did not describe the version sent to the service")
			}
			if len(reader.observed()) != 2 {
				t.Errorf("gate reads = %d, want describe + recheck", len(reader.observed()))
			}
		})
	}
}

func TestUpdateRequiresApprovalStateInTheRequest(t *testing.T) {
	gate := &scriptedGate{decision: &gatekeeper.Decision{RequestID: "req-1"}}
	h, service := testHandler(t, gate, toolRegistry)
	args := json.RawMessage(`{"apiVersion":"apps/v1","kind":"Deployment","namespace":"ops","name":"api","subresource":"scale","replicas":3}`)
	for _, ctx := range []context.Context{
		context.Background(),
		context.WithValue(context.Background(), approvedTargetKey{}, k8s.TargetState{}),
	} {
		_, rerr := h.resourceUpdate(ctx, args)
		if rerr == nil || !strings.Contains(rerr.message, "new approval") {
			t.Fatalf("resourceUpdate = %v, want missing approval state refusal", rerr)
		}
	}
	if service.count() != 0 {
		t.Errorf("service calls = %d, want zero", service.count())
	}
}

func TestUpdateDoesNotReuseAnEarlierCallsApprovalState(t *testing.T) {
	reader := newStateReader()
	gate := &scriptedGate{decision: &gatekeeper.Decision{RequestID: "req-1"}}
	h, service, _ := testHandlerReading(t, gate, toolRegistry, reader)
	args := `{"apiVersion":"apps/v1","kind":"Deployment","namespace":"ops","name":"api","subresource":"scale","replicas":3}`
	if _, rerr := callTool(t, h, "resource_update", args); rerr != nil {
		t.Fatal(rerr)
	}
	reader.state.ResourceVersion = "200"
	gate.decision = &gatekeeper.Decision{RequestID: "req-2"}
	if _, rerr := callTool(t, h, "resource_update", args); rerr != nil {
		t.Fatal(rerr)
	}
	if service.count() != 2 || service.update().ApprovedResourceVersion != "200" {
		t.Fatalf("second call used %q, want its own approved version 200", service.update().ApprovedResourceVersion)
	}
}
