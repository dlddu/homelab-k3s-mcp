package sessionplatform

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// recordedRequest is one request the control plane stub saw. The AC2 case reads
// these to prove a listing never issues anything but the read-only GET.
type recordedRequest struct {
	method string
	path   string
}

// stub serves the given JSON body from the control plane's list endpoint and
// records every request it receives, whatever the path.
func stub(t *testing.T, body string) (*Client, *[]recordedRequest) {
	t.Helper()

	var seen []recordedRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = append(seen, recordedRequest{method: r.Method, path: r.URL.Path})
		if r.Method != http.MethodGet || r.URL.Path != sessionsPath {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)

	return &Client{
		endpoint:  srv.URL,
		userAgent: "homelab-k3s-mcp/test",
		http:      srv.Client(),
	}, &seen
}

func TestListSessionsReturnsAllSessions(t *testing.T) {
	// Two sessions in different states, shaped like the control plane's
	// `Session` schema: the snapshotted one has no `pod` because its pods are
	// reclaimed, and both carry fields (model, auxiliaryPods) the PRD does not
	// expose, so the decode must ignore them rather than fail.
	c, seen := stub(t, `{"sessions":[
	  {"id":"s-1","name":"live shell","workloadType":"shell","state":"active",
	   "pod":"session-s-1","auxiliaryPods":[],"createdAt":"2026-09-01T00:00:00Z",
	   "lastAccess":"2026-09-03T11:00:00Z"},
	  {"id":"s-2","name":"parked agent","workloadType":"claude-code","state":"snapshot",
	   "model":"platform-default","createdAt":"2026-08-30T09:30:00Z",
	   "lastAccess":"2026-09-02T18:45:00Z"}
	]}`)

	sessions, err := c.ListSessions(context.Background())
	if err != nil {
		t.Fatalf("ListSessions: %v", err)
	}
	if len(sessions) != 2 {
		t.Fatalf("len(sessions) = %d, want 2", len(sessions))
	}

	want := []Session{
		{
			ID: "s-1", Name: "live shell", WorkloadType: "shell", State: "active",
			Pod: "session-s-1", CreatedAt: "2026-09-01T00:00:00Z", LastAccess: "2026-09-03T11:00:00Z",
		},
		{
			ID: "s-2", Name: "parked agent", WorkloadType: "claude-code", State: "snapshot",
			Pod: "", CreatedAt: "2026-08-30T09:30:00Z", LastAccess: "2026-09-02T18:45:00Z",
		},
	}
	for i := range want {
		if sessions[i] != want[i] {
			t.Fatalf("sessions[%d] = %+v, want %+v", i, sessions[i], want[i])
		}
	}

	if len(*seen) != 1 || (*seen)[0].method != http.MethodGet || (*seen)[0].path != "/api/v1/sessions" {
		t.Fatalf("requests = %+v, want a single GET /api/v1/sessions", *seen)
	}
}

func TestListSessionsEmpty(t *testing.T) {
	c, _ := stub(t, `{"sessions":[]}`)

	sessions, err := c.ListSessions(context.Background())
	if err != nil {
		t.Fatalf("ListSessions: %v", err)
	}
	if sessions == nil {
		t.Fatalf("sessions = nil, want an empty slice")
	}
	if len(sessions) != 0 {
		t.Fatalf("len(sessions) = %d, want 0", len(sessions))
	}
}

// TestListSessionsIsPassive is the tool-level half of AC2: repeated listings
// must issue nothing but the read-only GET — no read/write call that would
// promote an idle session or restore a snapshot.
func TestListSessionsIsPassive(t *testing.T) {
	c, seen := stub(t, `{"sessions":[
	  {"id":"s-1","name":"idle shell","workloadType":"shell","state":"idle",
	   "pod":"session-s-1","createdAt":"2026-09-01T00:00:00Z",
	   "lastAccess":"2026-09-03T11:00:00Z"},
	  {"id":"s-2","name":"parked agent","workloadType":"claude-code","state":"snapshot",
	   "createdAt":"2026-08-30T09:30:00Z","lastAccess":"2026-09-02T18:45:00Z"}
	]}`)

	var first []Session
	for i := 0; i < 3; i++ {
		got, err := c.ListSessions(context.Background())
		if err != nil {
			t.Fatalf("ListSessions #%d: %v", i+1, err)
		}
		if first == nil {
			first = got
			continue
		}
		for j := range got {
			if got[j].State != first[j].State || got[j].LastAccess != first[j].LastAccess {
				t.Fatalf("call #%d changed session %d: %+v, want %+v", i+1, j, got[j], first[j])
			}
		}
	}

	if len(*seen) != 3 {
		t.Fatalf("len(requests) = %d, want 3", len(*seen))
	}
	for _, req := range *seen {
		if req.method != http.MethodGet || req.path != "/api/v1/sessions" {
			t.Fatalf("request = %+v, want GET /api/v1/sessions only", req)
		}
	}
}

func TestListSessionsSurfacesControlPlaneError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error":"boom"}`))
	}))
	defer srv.Close()

	c := &Client{endpoint: srv.URL, userAgent: "homelab-k3s-mcp/test", http: srv.Client()}
	_, err := c.ListSessions(context.Background())
	if err == nil {
		t.Fatalf("ListSessions succeeded, want an api error")
	}
	var e *Error
	if !errors.As(err, &e) || e.kind != kindAPI {
		t.Fatalf("error = %v, want a kindAPI *Error", err)
	}
	if !strings.Contains(err.Error(), "session platform api error") {
		t.Fatalf("error = %q", err.Error())
	}
}

func TestUnavailableFailsEveryCall(t *testing.T) {
	svc := NewUnavailable("")

	sessions, err := svc.ListSessions(context.Background())
	if sessions != nil {
		t.Fatalf("sessions = %+v, want nil", sessions)
	}
	var e *Error
	if !errors.As(err, &e) || e.kind != kindUnavailable {
		t.Fatalf("error = %v, want a kindUnavailable *Error", err)
	}
	if err.Error() != "session platform unavailable: session platform endpoint is not configured" {
		t.Fatalf("error = %q", err.Error())
	}

	if _, err := NewUnavailable("wired to nothing").ListSessions(context.Background()); err == nil ||
		!strings.Contains(err.Error(), "wired to nothing") {
		t.Fatalf("error = %v, want the configured reason", err)
	}
}

func TestFromEnv(t *testing.T) {
	t.Setenv("SESSION_PLATFORM_ENDPOINT", "")
	c, err := FromEnv()
	if err != nil {
		t.Fatalf("FromEnv with the variable unset: %v", err)
	}
	if c != nil {
		t.Fatalf("client = %+v, want nil when the endpoint is unset", c)
	}

	t.Setenv("SESSION_PLATFORM_ENDPOINT", "http://control-plane.session-platform.svc.cluster.local/")
	c, err = FromEnv()
	if err != nil {
		t.Fatalf("FromEnv: %v", err)
	}
	if c == nil || c.endpoint != "http://control-plane.session-platform.svc.cluster.local" {
		t.Fatalf("endpoint = %+v, want the trailing slash trimmed", c)
	}

	t.Setenv("SESSION_PLATFORM_ENDPOINT", "control-plane.session-platform.svc.cluster.local")
	if _, err = FromEnv(); err == nil {
		t.Fatalf("FromEnv accepted a non-absolute endpoint")
	}
}
