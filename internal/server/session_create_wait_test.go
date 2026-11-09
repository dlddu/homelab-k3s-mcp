package server_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/dlddu/homelab-k3s-mcp/internal/mcp"
	"github.com/dlddu/homelab-k3s-mcp/internal/server"
	"github.com/dlddu/homelab-k3s-mcp/internal/sessionplatform"
)

// AC7: a create the control plane has not finished within the wait is answered
// as pending, not as an error, and names the session to look for.
func TestSessionCreateAnswersPendingWhenControlPlaneIsSlow(t *testing.T) {
	release := make(chan struct{})
	fake := &fakeSessionPlatform{createResponse: func(req sessionplatform.CreateRequest) (*sessionplatform.Session, error) {
		<-release
		return &sessionplatform.Session{ID: "s-late", Name: req.Name, State: "active", Pod: "session-s-late"}, nil
	}}
	app := server.App(nil, unavailableK8s(), unavailableGitHub(), unavailableAWS(), unavailableGrafana(), unavailableOpenSearch(), fake,
		mcp.WithSessionCreateWait(50*time.Millisecond))
	defer close(release)

	started := time.Now()
	body := callTool(t, app, 195, "session_create", map[string]any{"name": "  worker  ", "workloadType": "claude-code"})
	if elapsed := time.Since(started); elapsed > 5*time.Second {
		t.Fatalf("session_create held its response for %s, want it bounded by the wait", elapsed)
	}
	if at(t, body, "result", "isError") != false {
		t.Fatalf("isError = %v, want a pending (non-error) answer", at(t, body, "result", "isError"))
	}
	if at(t, body, "result", "structuredContent", "pending") != true ||
		at(t, body, "result", "structuredContent", "session", "state") != "starting" ||
		at(t, body, "result", "structuredContent", "session", "name") != "worker" {
		t.Fatalf("structuredContent = %v", at(t, body, "result", "structuredContent"))
	}
	message, _ := at(t, body, "result", "structuredContent", "message").(string)
	if !strings.Contains(message, "session_list") || !strings.Contains(message, `"worker"`) {
		t.Fatalf("message = %q, want it to point at session_list for the named session", message)
	}
}

// AC7: a create that finishes within the wait is answered exactly as before.
func TestSessionCreateWithinTheWaitReturnsTheSession(t *testing.T) {
	fake := &fakeSessionPlatform{}
	app := server.App(nil, unavailableK8s(), unavailableGitHub(), unavailableAWS(), unavailableGrafana(), unavailableOpenSearch(), fake,
		mcp.WithSessionCreateWait(5*time.Second))

	body := callTool(t, app, 197, "session_create", map[string]any{"name": "quick"})
	if at(t, body, "result", "structuredContent", "session", "state") != "active" {
		t.Fatalf("structuredContent = %v, want the active session", at(t, body, "result", "structuredContent"))
	}
	if _, pending := at(t, body, "result", "structuredContent").(map[string]any)["pending"]; pending {
		t.Fatalf("structuredContent = %v, want no pending marker", at(t, body, "result", "structuredContent"))
	}
}

// AC7: once the tool has answered pending, the control-plane request it started
// stays open until the control plane answers — the caller leaving must not
// cancel it, or the control plane reclaims the half-made pod.
func TestSessionCreateKeepsTheControlPlaneRequestAfterAnswering(t *testing.T) {
	release := make(chan struct{})
	canceled := make(chan bool, 1)
	control := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.NotFound(w, r)
			return
		}
		// net/http only watches for the client going away once the body has
		// been read, so read it before waiting.
		_, _ = io.Copy(io.Discard, r.Body)
		select {
		case <-release:
			canceled <- false
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"id":"s-late","name":"worker","state":"active","pod":"session-s-late"}`))
		case <-r.Context().Done():
			canceled <- true
		}
	}))
	defer control.Close()

	t.Setenv("SESSION_PLATFORM_ENDPOINT", control.URL)
	client, err := sessionplatform.FromEnv()
	if err != nil || client == nil {
		t.Fatalf("FromEnv() = (%v, %v), want a client", client, err)
	}
	app := server.App(nil, unavailableK8s(), unavailableGitHub(), unavailableAWS(), unavailableGrafana(), unavailableOpenSearch(), client,
		mcp.WithSessionCreateWait(50*time.Millisecond))

	// net/http cancels a request's context once its handler returns; do the
	// same here, so the test fails if the create still rides on that context.
	ctx, cancel := context.WithCancel(context.Background())
	req := jsonRequest("/mcp", map[string]any{
		"jsonrpc": "2.0", "id": 196, "method": "tools/call",
		"params": map[string]any{"name": "session_create", "arguments": map[string]any{"name": "worker"}},
	}).WithContext(ctx)
	body := bodyJSON(t, serve(app, req))
	cancel()
	if at(t, body, "result", "structuredContent", "pending") != true {
		t.Fatalf("result = %v, want a pending answer", at(t, body, "result"))
	}

	select {
	case <-canceled:
		t.Fatal("the control-plane create was canceled when the tool answered")
	case <-time.After(200 * time.Millisecond):
	}
	close(release)
	select {
	case wasCanceled := <-canceled:
		if wasCanceled {
			t.Fatal("the control-plane create was canceled instead of completing")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the control-plane create never completed")
	}
}
