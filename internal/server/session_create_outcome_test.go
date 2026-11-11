package server_test

import (
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

// pendingCreates reads session_list's report of creates answered pending.
func pendingCreates(t *testing.T, app http.Handler, id int) []any {
	t.Helper()
	body := callTool(t, app, id, "session_list", map[string]any{})
	content, _ := at(t, body, "result", "structuredContent").(map[string]any)
	list, _ := content["pendingCreates"].([]any)
	return list
}

// AC7: after session_create answered pending, session_list tells whether that
// create is still starting, was created, or failed — so a failure the control
// plane reports after the caller left is not silent.
func TestSessionListReportsHowAPendingCreateEnded(t *testing.T) {
	for _, tc := range []struct {
		name      string
		status    int
		body      string
		wantState string
		wantField string
		wantValue string
	}{
		{"failed", http.StatusInternalServerError, `{"error":"pod sessions/sess-x not Ready: context deadline exceeded"}`,
			"failed", "error", "not Ready"},
		{"created", http.StatusCreated, `{"id":"s-late","name":"worker","state":"active","pod":"sess-s-late"}`,
			"created", "id", "s-late"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			release := make(chan struct{})
			finished := make(chan struct{})
			control := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				if r.Method == http.MethodGet {
					_, _ = w.Write([]byte(`{"sessions":[]}`))
					return
				}
				_, _ = io.Copy(io.Discard, r.Body)
				<-release
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
				close(finished)
			}))
			defer control.Close()

			t.Setenv("SESSION_PLATFORM_ENDPOINT", control.URL)
			client, err := sessionplatform.FromEnv()
			if err != nil || client == nil {
				t.Fatalf("FromEnv() = (%v, %v), want a client", client, err)
			}
			app := server.App(nil, unavailableK8s(), unavailableGitHub(), unavailableAWS(), unavailableGrafana(), unavailableOpenSearch(), client,
				mcp.WithSessionCreateWait(50*time.Millisecond))

			body := callTool(t, app, 300, "session_create", map[string]any{"name": " worker ", "workloadType": "claude-code"})
			if at(t, body, "result", "structuredContent", "pending") != true {
				t.Fatalf("result = %v, want a pending answer", at(t, body, "result"))
			}
			message, _ := at(t, body, "result", "structuredContent", "message").(string)
			if !strings.Contains(message, "pendingCreates") {
				t.Fatalf("message = %q, want it to point at pendingCreates", message)
			}

			list := pendingCreates(t, app, 301)
			if len(list) != 1 {
				t.Fatalf("pendingCreates while provisioning = %v, want one entry", list)
			}
			entry := list[0].(map[string]any)
			if entry["name"] != "worker" || entry["workloadType"] != "claude-code" || entry["state"] != "starting" {
				t.Fatalf("pendingCreates entry while provisioning = %v", entry)
			}

			close(release)
			<-finished
			deadline := time.Now().Add(5 * time.Second)
			for {
				list = pendingCreates(t, app, 302)
				if len(list) == 1 && list[0].(map[string]any)["state"] != "starting" {
					break
				}
				if time.Now().After(deadline) {
					t.Fatalf("pendingCreates never left starting: %v", list)
				}
				time.Sleep(10 * time.Millisecond)
			}
			entry = list[0].(map[string]any)
			if entry["state"] != tc.wantState {
				t.Fatalf("state = %v, want %s (entry %v)", entry["state"], tc.wantState, entry)
			}
			if got, _ := entry[tc.wantField].(string); !strings.Contains(got, tc.wantValue) {
				t.Fatalf("%s = %q, want it to contain %q", tc.wantField, got, tc.wantValue)
			}
		})
	}
}

// AC7: a create answered within the wait is not tracked — session_list
// carries no pendingCreates field.
func TestSessionListOmitsPendingCreatesWhenNoneWerePending(t *testing.T) {
	fake := &fakeSessionPlatform{}
	app := server.App(nil, unavailableK8s(), unavailableGitHub(), unavailableAWS(), unavailableGrafana(), unavailableOpenSearch(), fake,
		mcp.WithSessionCreateWait(5*time.Second))
	callTool(t, app, 310, "session_create", map[string]any{"name": "quick"})
	body := callTool(t, app, 311, "session_list", map[string]any{})
	if _, present := at(t, body, "result", "structuredContent").(map[string]any)["pendingCreates"]; present {
		t.Fatalf("structuredContent = %v, want no pendingCreates", at(t, body, "result", "structuredContent"))
	}
}
