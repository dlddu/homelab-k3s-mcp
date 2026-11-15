package server_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/dlddu/homelab-k3s-mcp/internal/mcp"
	"github.com/dlddu/homelab-k3s-mcp/internal/server"
	"github.com/dlddu/homelab-k3s-mcp/internal/sessionplatform"
)

func TestToolsListAdvertisesSessionRestore(t *testing.T) {
	app := server.App(nil, unavailableK8s(), unavailableGitHub(), unavailableAWS(), unavailableGrafana(), unavailableOpenSearch(), unavailableSessionPlatform())
	rec := findTool(t, toolsList(t, app), "session_restore")

	required, _ := at(t, rec, "inputSchema", "required").([]any)
	if len(required) != 1 || required[0] != "id" {
		t.Fatalf("required = %v, want [id]", required)
	}
	if at(t, rec, "annotations", "title") != "Restore Session" ||
		at(t, rec, "annotations", "readOnlyHint") != false ||
		at(t, rec, "annotations", "destructiveHint") != false ||
		at(t, rec, "annotations", "idempotentHint") != true ||
		at(t, rec, "annotations", "openWorldHint") != true {
		t.Fatalf("annotations = %v", rec["annotations"])
	}
}

func TestSessionRestoreReturnsTheActiveSession(t *testing.T) {
	fake := &fakeSessionPlatform{}
	app := server.App(nil, unavailableK8s(), unavailableGitHub(), unavailableAWS(), unavailableGrafana(), unavailableOpenSearch(), fake)

	body := callTool(t, app, 320, "session_restore", map[string]any{"id": "s-1"})
	if at(t, body, "result", "isError") != false ||
		at(t, body, "result", "structuredContent", "session", "id") != "s-1" ||
		at(t, body, "result", "structuredContent", "session", "state") != "active" ||
		at(t, body, "result", "structuredContent", "session", "pod") != "session-s-1-r1" {
		t.Fatalf("result = %v", at(t, body, "result"))
	}
	if len(fake.restoreCalls) != 1 || fake.restoreCalls[0] != "s-1" {
		t.Fatalf("restoreCalls = %v, want [s-1]", fake.restoreCalls)
	}
	if len(fake.readCalls) != 0 || len(fake.writeCalls) != 0 || len(fake.archiveCalls) != 0 {
		t.Fatalf("restore touched other endpoints: read=%v write=%v archive=%v",
			fake.readCalls, fake.writeCalls, fake.archiveCalls)
	}
}

func TestSessionRestoreAnswersPendingWhenTheRestoreIsSlow(t *testing.T) {
	release := make(chan struct{})
	finished := make(chan struct{})
	fake := &fakeSessionPlatform{restoreResponse: func(id string) (*sessionplatform.Session, error) {
		defer close(finished)
		<-release
		return &sessionplatform.Session{ID: id, State: "active", Pod: "session-" + id + "-r1"}, nil
	}}
	app := server.App(nil, unavailableK8s(), unavailableGitHub(), unavailableAWS(), unavailableGrafana(), unavailableOpenSearch(), fake,
		mcp.WithSessionRestoreWait(50*time.Millisecond))

	started := time.Now()
	body := callTool(t, app, 321, "session_restore", map[string]any{"id": "s-slow"})
	if elapsed := time.Since(started); elapsed > 5*time.Second {
		t.Fatalf("session_restore held its response for %s, want it bounded by the wait", elapsed)
	}
	if at(t, body, "result", "isError") != false ||
		at(t, body, "result", "structuredContent", "pending") != true ||
		at(t, body, "result", "structuredContent", "session", "id") != "s-slow" ||
		at(t, body, "result", "structuredContent", "session", "state") != "restoring" {
		t.Fatalf("result = %v", at(t, body, "result"))
	}
	message, _ := at(t, body, "result", "structuredContent", "message").(string)
	if !strings.Contains(message, "session_list") || !strings.Contains(message, "'active'") {
		t.Fatalf("message = %q, want it to point at session_list and the active state", message)
	}

	// The restore outlives the answer: it is not cancelled with the request.
	close(release)
	select {
	case <-finished:
	case <-time.After(5 * time.Second):
		t.Fatal("the restore did not run to completion after the tool answered")
	}
	if len(fake.restoreCalls) != 1 {
		t.Fatalf("restoreCalls = %v, want exactly one", fake.restoreCalls)
	}
}

func TestSessionRestoreRejectsBadArguments(t *testing.T) {
	for _, args := range []map[string]any{{}, {"id": ""}, {"id": 9}} {
		fake := &fakeSessionPlatform{}
		app := server.App(nil, unavailableK8s(), unavailableGitHub(), unavailableAWS(), unavailableGrafana(), unavailableOpenSearch(), fake)

		body := callTool(t, app, 322, "session_restore", args)
		message, _ := at(t, body, "error", "message").(string)
		if !strings.Contains(message, "id is required") {
			t.Fatalf("args %v: error message = %q", args, message)
		}
		if len(fake.restoreCalls) != 0 {
			t.Fatalf("args %v: restoreCalls = %v, want none", args, fake.restoreCalls)
		}
	}
}

func TestSessionRestoreSurfacesRefusalsDistinctly(t *testing.T) {
	var status int
	control := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || !strings.HasSuffix(r.URL.Path, "/switch") {
			t.Errorf("request = %s %s, want POST …/switch", r.Method, r.URL.Path)
		}
		w.WriteHeader(status)
		_, _ = w.Write([]byte(`{"error":"refused"}`))
	}))
	defer control.Close()

	t.Setenv("SESSION_PLATFORM_ENDPOINT", control.URL)
	client, err := sessionplatform.FromEnv()
	if err != nil || client == nil {
		t.Fatalf("FromEnv() = (%v, %v), want a client", client, err)
	}
	app := server.App(nil, unavailableK8s(), unavailableGitHub(), unavailableAWS(), unavailableGrafana(), unavailableOpenSearch(), client)

	texts := map[int]string{}
	for _, code := range []int{http.StatusNotFound, http.StatusConflict, http.StatusServiceUnavailable} {
		status = code
		body := callTool(t, app, 323, "session_restore", map[string]any{"id": "s-1"})
		if at(t, body, "result", "isError") != true {
			t.Fatalf("status %d: isError = %v", code, at(t, body, "result", "isError"))
		}
		texts[code], _ = at(t, body, "result", "content", 0, "text").(string)
	}
	if !strings.Contains(texts[http.StatusNotFound], "not found") ||
		strings.Contains(texts[http.StatusNotFound], "retry") {
		t.Fatalf("not found text = %q", texts[http.StatusNotFound])
	}
	if !strings.Contains(texts[http.StatusConflict], "conflict") ||
		!strings.Contains(texts[http.StatusConflict], "retry") {
		t.Fatalf("conflict text = %q", texts[http.StatusConflict])
	}
	if !strings.Contains(texts[http.StatusServiceUnavailable], "unavailable") {
		t.Fatalf("unavailable text = %q", texts[http.StatusServiceUnavailable])
	}
}

func TestSessionRestoreUnavailableReturnsToolError(t *testing.T) {
	app := server.App(nil, unavailableK8s(), unavailableGitHub(), unavailableAWS(), unavailableGrafana(), unavailableOpenSearch(), unavailableSessionPlatform())

	body := callTool(t, app, 324, "session_restore", map[string]any{"id": "s-1"})
	text, _ := at(t, body, "result", "content", 0, "text").(string)
	if at(t, body, "result", "isError") != true || !strings.Contains(text, "session platform unavailable") {
		t.Fatalf("result = %v", at(t, body, "result"))
	}

	survivor := callTool(t, app, 325, "ping", map[string]any{})
	if at(t, survivor, "result", "content", 0, "text") != "pong" {
		t.Fatalf("ping did not survive the refusal: %v", survivor)
	}
}

// prd-session-read AC2 · prd-session-write AC2: an archived session is
// refused, nothing reaches its read or write endpoint, and the refusal names
// session_restore.
func TestSessionReadAndWriteRefuseAnArchivedSession(t *testing.T) {
	for _, tc := range []struct {
		tool string
		args map[string]any
	}{
		{"session_read", map[string]any{"id": "s-frozen", "offset": 0}},
		{"session_write", map[string]any{"id": "s-frozen", "payload": "ls\n"}},
	} {
		t.Run(tc.tool, func(t *testing.T) {
			fake := &fakeSessionPlatform{getResponse: func(id string) (*sessionplatform.Session, error) {
				return &sessionplatform.Session{ID: id, State: "snapshot"}, nil
			}}
			app := server.App(nil, unavailableK8s(), unavailableGitHub(), unavailableAWS(), unavailableGrafana(), unavailableOpenSearch(), fake)

			body := callTool(t, app, 326, tc.tool, tc.args)
			if at(t, body, "result", "isError") != true {
				t.Fatalf("isError = %v, want a refusal", at(t, body, "result", "isError"))
			}
			text, _ := at(t, body, "result", "content", 0, "text").(string)
			if !strings.Contains(text, "session_restore") || !strings.Contains(text, "s-frozen") ||
				!strings.Contains(text, "'snapshot'") {
				t.Fatalf("text = %q, want it to name the session, its state and session_restore", text)
			}
			if len(fake.readCalls) != 0 || len(fake.writeCalls) != 0 || len(fake.restoreCalls) != 0 {
				t.Fatalf("a refused call reached the control plane: read=%v write=%v restore=%v",
					fake.readCalls, fake.writeCalls, fake.restoreCalls)
			}
		})
	}
}

// The guard is a passive look: idle and active sessions pass it untouched,
// and a missing session is the read's own not-found.
func TestSessionReadPassesTheGuardForLiveSessionsAndReportsMissingOnes(t *testing.T) {
	for _, state := range []string{"active", "idle"} {
		fake := &fakeSessionPlatform{getResponse: func(id string) (*sessionplatform.Session, error) {
			return &sessionplatform.Session{ID: id, State: state}, nil
		}}
		app := server.App(nil, unavailableK8s(), unavailableGitHub(), unavailableAWS(), unavailableGrafana(), unavailableOpenSearch(), fake)
		body := callTool(t, app, 327, "session_read", map[string]any{"id": "s-1"})
		if at(t, body, "result", "isError") != false || len(fake.readCalls) != 1 {
			t.Fatalf("%s: result = %v, readCalls = %v", state, at(t, body, "result"), fake.readCalls)
		}
	}

	control := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("a missing session got a %s %s after its lookup failed", r.Method, r.URL.Path)
		}
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"error":"session not found"}`))
	}))
	defer control.Close()
	t.Setenv("SESSION_PLATFORM_ENDPOINT", control.URL)
	client, err := sessionplatform.FromEnv()
	if err != nil || client == nil {
		t.Fatalf("FromEnv() = (%v, %v), want a client", client, err)
	}
	app := server.App(nil, unavailableK8s(), unavailableGitHub(), unavailableAWS(), unavailableGrafana(), unavailableOpenSearch(), client)
	body := callTool(t, app, 328, "session_read", map[string]any{"id": "s-gone"})
	text, _ := at(t, body, "result", "content", 0, "text").(string)
	if at(t, body, "result", "isError") != true || !strings.Contains(text, "not found") {
		t.Fatalf("missing session: result = %v", at(t, body, "result"))
	}
}
