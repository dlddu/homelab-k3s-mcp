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

func TestToolsListAdvertisesSessionArchive(t *testing.T) {
	app := server.App(nil, unavailableK8s(), unavailableGitHub(), unavailableAWS(), unavailableGrafana(), unavailableOpenSearch(), unavailableSessionPlatform())
	rec := findTool(t, toolsList(t, app), "session_archive")

	required, _ := at(t, rec, "inputSchema", "required").([]any)
	if len(required) != 1 || required[0] != "id" {
		t.Fatalf("required = %v, want [id]", required)
	}
	if at(t, rec, "annotations", "title") != "Archive Session" ||
		at(t, rec, "annotations", "readOnlyHint") != false ||
		at(t, rec, "annotations", "destructiveHint") != false ||
		at(t, rec, "annotations", "idempotentHint") != true ||
		at(t, rec, "annotations", "openWorldHint") != true {
		t.Fatalf("annotations = %v", rec["annotations"])
	}
}

func TestSessionArchiveReturnsTheFrozenSession(t *testing.T) {
	fake := &fakeSessionPlatform{}
	app := server.App(nil, unavailableK8s(), unavailableGitHub(), unavailableAWS(), unavailableGrafana(), unavailableOpenSearch(), fake)

	body := callTool(t, app, 310, "session_archive", map[string]any{"id": "s-1"})
	if at(t, body, "result", "isError") != false ||
		at(t, body, "result", "structuredContent", "session", "id") != "s-1" ||
		at(t, body, "result", "structuredContent", "session", "state") != "snapshot" {
		t.Fatalf("result = %v", at(t, body, "result"))
	}
	if _, hasPod := at(t, body, "result", "structuredContent", "session").(map[string]any)["pod"]; hasPod {
		t.Fatalf("session = %v, want no pod once archived", at(t, body, "result", "structuredContent", "session"))
	}
	if len(fake.archiveCalls) != 1 || fake.archiveCalls[0] != "s-1" {
		t.Fatalf("archiveCalls = %v, want [s-1]", fake.archiveCalls)
	}
	if len(fake.createCalls) != 0 || len(fake.deleteCalls) != 0 || fake.listCalls != 0 ||
		len(fake.readCalls) != 0 || len(fake.writeCalls) != 0 {
		t.Fatalf("archive touched other endpoints: create=%v delete=%v list=%d read=%v write=%v",
			fake.createCalls, fake.deleteCalls, fake.listCalls, fake.readCalls, fake.writeCalls)
	}
}

func TestSessionArchiveAnswersPendingWhenTheFreezeIsSlow(t *testing.T) {
	release := make(chan struct{})
	finished := make(chan struct{})
	fake := &fakeSessionPlatform{archiveResponse: func(id string) (*sessionplatform.Session, error) {
		defer close(finished)
		<-release
		return &sessionplatform.Session{ID: id, State: "snapshot"}, nil
	}}
	app := server.App(nil, unavailableK8s(), unavailableGitHub(), unavailableAWS(), unavailableGrafana(), unavailableOpenSearch(), fake,
		mcp.WithSessionArchiveWait(50*time.Millisecond))

	started := time.Now()
	body := callTool(t, app, 311, "session_archive", map[string]any{"id": "s-slow"})
	if elapsed := time.Since(started); elapsed > 5*time.Second {
		t.Fatalf("session_archive held its response for %s, want it bounded by the wait", elapsed)
	}
	if at(t, body, "result", "isError") != false ||
		at(t, body, "result", "structuredContent", "pending") != true ||
		at(t, body, "result", "structuredContent", "session", "id") != "s-slow" ||
		at(t, body, "result", "structuredContent", "session", "state") != "archiving" {
		t.Fatalf("result = %v", at(t, body, "result"))
	}
	message, _ := at(t, body, "result", "structuredContent", "message").(string)
	if !strings.Contains(message, "session_list") || !strings.Contains(message, "'snapshot'") {
		t.Fatalf("message = %q, want it to point at session_list and the snapshot state", message)
	}

	// The freeze outlives the answer: it is not cancelled with the request.
	close(release)
	select {
	case <-finished:
	case <-time.After(5 * time.Second):
		t.Fatal("the archive did not run to completion after the tool answered")
	}
	if len(fake.archiveCalls) != 1 {
		t.Fatalf("archiveCalls = %v, want exactly one", fake.archiveCalls)
	}
}

func TestSessionArchiveRejectsBadArguments(t *testing.T) {
	for _, args := range []map[string]any{{}, {"id": ""}, {"id": 9}} {
		fake := &fakeSessionPlatform{}
		app := server.App(nil, unavailableK8s(), unavailableGitHub(), unavailableAWS(), unavailableGrafana(), unavailableOpenSearch(), fake)

		body := callTool(t, app, 312, "session_archive", args)
		message, _ := at(t, body, "error", "message").(string)
		if !strings.Contains(message, "id is required") {
			t.Fatalf("args %v: error message = %q", args, message)
		}
		if len(fake.archiveCalls) != 0 {
			t.Fatalf("args %v: archiveCalls = %v, want none", args, fake.archiveCalls)
		}
	}
}

func TestSessionArchiveSurfacesRefusalsDistinctly(t *testing.T) {
	var status int
	control := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
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
		body := callTool(t, app, 313, "session_archive", map[string]any{"id": "s-1"})
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

func TestSessionArchiveUnavailableReturnsToolError(t *testing.T) {
	app := server.App(nil, unavailableK8s(), unavailableGitHub(), unavailableAWS(), unavailableGrafana(), unavailableOpenSearch(), unavailableSessionPlatform())

	body := callTool(t, app, 314, "session_archive", map[string]any{"id": "s-1"})
	text, _ := at(t, body, "result", "content", 0, "text").(string)
	if at(t, body, "result", "isError") != true || !strings.Contains(text, "session platform unavailable") {
		t.Fatalf("result = %v", at(t, body, "result"))
	}

	survivor := callTool(t, app, 315, "ping", map[string]any{})
	if at(t, survivor, "result", "content", 0, "text") != "pong" {
		t.Fatalf("ping did not survive the refusal: %v", survivor)
	}
}
