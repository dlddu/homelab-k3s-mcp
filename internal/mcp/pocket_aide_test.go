package mcp

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/dlddu/homelab-k3s-mcp/internal/eventlog"
	"github.com/dlddu/homelab-k3s-mcp/internal/pocketaide"
)

type requesterRecorder struct{ requesters []string }

func (r *requesterRecorder) Notify(_ context.Context, _, _, requester string) (*pocketaide.Result, error) {
	r.requesters = append(r.requesters, requester)
	return &pocketaide.Result{}, nil
}

func TestPocketAideNotificationSendPassesThePrincipalAsRequester(t *testing.T) {
	h, _ := testHandler(t, &scriptedGate{}, toolRegistry)
	rec := &requesterRecorder{}
	h.pocketAide = rec

	params := json.RawMessage(`{"name":"pocket_aide_notification_send","arguments":{"title":"x","requester":"spoofed"}}`)
	for _, p := range []eventlog.Principal{
		{Method: "jwt", ID: "operator@example.test"},
		{Method: "github_oidc", ID: "dlddu/reconciler/.github/workflows/reconcile.yml"},
	} {
		if _, rerr := h.toolsCall(eventlog.WithPrincipal(context.Background(), p), params); rerr != nil {
			t.Fatalf("%s: %v", p, rerr)
		}
	}
	if _, rerr := h.toolsCall(context.Background(), params); rerr != nil {
		t.Fatalf("anonymous: %v", rerr)
	}

	want := []string{"jwt:operator@example.test", "github_oidc:dlddu/reconciler/.github/workflows/reconcile.yml", "anonymous"}
	if len(rec.requesters) != len(want) {
		t.Fatalf("requesters = %q, want %q", rec.requesters, want)
	}
	for i := range want {
		if rec.requesters[i] != want[i] {
			t.Fatalf("requesters = %q, want %q", rec.requesters, want)
		}
	}
}
