package mcp

import (
	"context"
	"encoding/json"
	"errors"
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

func TestUpdateConflictEndsTheCallWithoutAutomaticReapproval(t *testing.T) {
	for _, shape := range []struct {
		name string
		args string
	}{
		{"object", `{"apiVersion":"apps/v1","kind":"Deployment","namespace":"ops","name":"api","manifest":{"apiVersion":"apps/v1","kind":"Deployment","metadata":{"name":"api","namespace":"ops"},"spec":{"replicas":3}}}`},
		{"scale", `{"apiVersion":"apps/v1","kind":"Deployment","namespace":"ops","name":"api","subresource":"scale","replicas":3}`},
	} {
		for _, phase := range []string{"gate-recheck", "conditional-write"} {
			t.Run(shape.name+"/"+phase, func(t *testing.T) {
				reader := newStateReader()
				gate := &scriptedGate{decision: &gatekeeper.Decision{RequestID: "req-1"}}
				h, service, _ := testHandlerReading(t, gate, toolRegistry, reader)
				wantUpdates := 0
				if phase == "gate-recheck" {
					reader.next = &k8s.TargetState{ResourceVersion: "101", UID: "uid-1"}
				} else {
					wantUpdates = 1
					service.updateErr = k8s.APIError("resource_update automatically refused: resourceVersion conflict; this call has ended")
				}

				result, rerr := callTool(t, h, "resource_update", shape.args)
				if phase == "gate-recheck" {
					if result != nil || rerr == nil || !strings.Contains(rerr.message, "refusing resource_update") {
						t.Fatalf("tools/call = (%v, %v), want automatic pre-write refusal", result, rerr)
					}
				} else {
					if rerr != nil {
						t.Fatalf("tools/call RPC error = %v, want tool error", rerr)
					}
					payload, ok := result.(map[string]any)
					if !ok || payload["isError"] != true || payload["structuredContent"] != nil {
						t.Fatalf("tools/call = %v, want tool error without success payload", result)
					}
					encoded, err := json.Marshal(payload)
					if err != nil || !strings.Contains(string(encoded), service.updateErr.Error()) {
						t.Fatalf("tools/call lost the terminal refusal: %s, %v", encoded, err)
					}
					if service.update().ApprovedResourceVersion != "100" {
						t.Fatalf("update used version %q, want approved 100", service.update().ApprovedResourceVersion)
					}
				}
				if service.count() != wantUpdates {
					t.Errorf("update calls = %d, want %d", service.count(), wantUpdates)
				}
				if len(gate.calls) != 1 {
					t.Errorf("approval requests = %d, want one without automatic reapproval", len(gate.calls))
				}
				if len(reader.observed()) != 2 {
					t.Errorf("target reads = %d, want describe + recheck without automatic refresh", len(reader.observed()))
				}
				if err := gate.decision.Consume(); !errors.Is(err, gatekeeper.ErrConsumed) {
					t.Errorf("approval was not consumed: %v", err)
				}
			})
		}
	}
}
