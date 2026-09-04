package sessionplatform

import (
	"context"
	"encoding/json"
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

// --- session_read ---

// readStub models the control plane's read endpoint closely enough to exercise
// the cursor contract: it holds an append-only output buffer, serves the span
// after the requested offset, and issues the resulting cursor. Every request it
// sees is recorded, which is how the AC3 cases prove a rejected cursor never
// reaches the wire.
type readStub struct {
	output   []byte
	sessions map[string]Session
	path     string
	seen     []recordedRequest
}

func newReadStub(t *testing.T, output string) (*Client, *readStub) {
	t.Helper()

	st := &readStub{
		output: []byte(output),
		path:   "active",
		sessions: map[string]Session{
			"s-1": {
				ID: "s-1", Name: "live shell", WorkloadType: "shell", State: "active",
				Pod: "session-s-1", CreatedAt: "2026-09-01T00:00:00Z",
				LastAccess: "2026-09-03T11:00:00Z",
			},
		},
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		st.seen = append(st.seen, recordedRequest{method: r.Method, path: r.URL.Path})

		id, ok := strings.CutSuffix(strings.TrimPrefix(r.URL.Path, sessionsPath+"/"), "/read")
		if r.Method != http.MethodPost || !ok {
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"error":"no such route"}`))
			return
		}
		sess, known := st.sessions[id]
		if !known {
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"error":"session not found"}`))
			return
		}

		var req struct {
			Offset int64 `json:"offset"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		if req.Offset < 0 {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"error":"invalid offset"}`))
			return
		}
		if req.Offset > int64(len(st.output)) {
			req.Offset = int64(len(st.output))
		}

		// Reading activates the target, so the stub reports it active
		// afterwards, exactly as the control plane does.
		sess.State = "active"
		w.Header().Set("Content-Type", "application/json")
		body, _ := json.Marshal(ReadResult{
			Session:    sess,
			Path:       st.path,
			Payload:    string(st.output[req.Offset:]),
			NextOffset: int64(len(st.output)),
		})
		_, _ = w.Write(body)
	}))
	t.Cleanup(srv.Close)

	return &Client{
		endpoint:  srv.URL,
		userAgent: "homelab-k3s-mcp/test",
		http:      srv.Client(),
	}, st
}

// TestReadFullThenIncremental is AC1's cursor contract: offset 0 is everything,
// the issued cursor yields nothing new, and once more output accumulates the
// same cursor yields exactly the increment.
func TestReadFullThenIncremental(t *testing.T) {
	c, st := newReadStub(t, "hello\n")

	full, err := c.ReadSession(context.Background(), "s-1", 0)
	if err != nil {
		t.Fatalf("ReadSession(0): %v", err)
	}
	if full.Payload != "hello\n" {
		t.Fatalf("payload = %q, want the whole output", full.Payload)
	}
	if full.NextOffset != 6 {
		t.Fatalf("nextOffset = %d, want 6", full.NextOffset)
	}

	// Nothing new yet: an empty payload at the same cursor, not an error.
	empty, err := c.ReadSession(context.Background(), "s-1", full.NextOffset)
	if err != nil {
		t.Fatalf("ReadSession(nextOffset): %v", err)
	}
	if empty.Payload != "" {
		t.Fatalf("payload = %q, want empty", empty.Payload)
	}
	if empty.NextOffset != full.NextOffset {
		t.Fatalf("nextOffset = %d, want the cursor to stand still at %d", empty.NextOffset, full.NextOffset)
	}

	st.output = append(st.output, []byte("world\n")...)
	incr, err := c.ReadSession(context.Background(), "s-1", full.NextOffset)
	if err != nil {
		t.Fatalf("ReadSession after new output: %v", err)
	}
	if incr.Payload != "world\n" {
		t.Fatalf("payload = %q, want only the increment", incr.Payload)
	}
	if incr.NextOffset != 12 {
		t.Fatalf("nextOffset = %d, want 12", incr.NextOffset)
	}
}

// TestReadIsNonDestructive is the other half of AC1: reading consumes nothing,
// so the same cursor always returns the same span.
func TestReadIsNonDestructive(t *testing.T) {
	c, _ := newReadStub(t, "line one\nline two\n")

	first, err := c.ReadSession(context.Background(), "s-1", 0)
	if err != nil {
		t.Fatalf("ReadSession: %v", err)
	}
	for i := 0; i < 3; i++ {
		again, err := c.ReadSession(context.Background(), "s-1", 0)
		if err != nil {
			t.Fatalf("ReadSession #%d: %v", i+2, err)
		}
		if again.Payload != first.Payload || again.NextOffset != first.NextOffset {
			t.Fatalf("re-read #%d = (%q, %d), want (%q, %d)",
				i+2, again.Payload, again.NextOffset, first.Payload, first.NextOffset)
		}
	}
}

// TestReadRejectsNegativeOffset is AC3's cursor half. The assertion that
// matters is not the error type alone but that the stub saw *no request*: a
// rejected cursor cannot have touched the session's state or lastAccess,
// because nothing was ever sent.
func TestReadRejectsNegativeOffset(t *testing.T) {
	c, st := newReadStub(t, "hello\n")

	_, err := c.ReadSession(context.Background(), "s-1", -1)
	if err == nil {
		t.Fatalf("ReadSession(-1) succeeded, want an invalid argument error")
	}
	var e *Error
	if !errors.As(err, &e) || e.kind != kindInvalidArgument {
		t.Fatalf("error = %v, want a kindInvalidArgument *Error", err)
	}
	if !strings.Contains(err.Error(), "session platform invalid argument") {
		t.Fatalf("error = %q", err.Error())
	}
	if len(st.seen) != 0 {
		t.Fatalf("requests = %+v, want none: a rejected cursor must not reach the control plane", st.seen)
	}

	// An empty id is refused the same way, and equally without a request.
	if _, err := c.ReadSession(context.Background(), "", 0); err == nil {
		t.Fatalf("ReadSession with an empty id succeeded")
	}
	if len(st.seen) != 0 {
		t.Fatalf("requests = %+v, want none", st.seen)
	}
}

// TestReadNotFound is AC3's target half: an unknown id is its own error kind, so
// a caller can tell it from a malformed cursor and from a broken control plane.
func TestReadNotFound(t *testing.T) {
	c, st := newReadStub(t, "hello\n")

	_, err := c.ReadSession(context.Background(), "s-missing", 0)
	if err == nil {
		t.Fatalf("ReadSession on an unknown id succeeded, want a not found error")
	}
	var e *Error
	if !errors.As(err, &e) || e.kind != kindNotFound {
		t.Fatalf("error = %v, want a kindNotFound *Error", err)
	}
	if !strings.Contains(err.Error(), "session platform not found") ||
		!strings.Contains(err.Error(), "session not found") {
		t.Fatalf("error = %q, want the control plane's reason carried through", err.Error())
	}

	// It is distinguishable from the two neighbouring failures.
	_, invalidErr := c.ReadSession(context.Background(), "s-1", -1)
	if invalidErr.Error() == err.Error() {
		t.Fatalf("a bad cursor and a missing session produced the same error %q", err.Error())
	}
	if len(st.seen) != 1 {
		t.Fatalf("requests = %+v, want exactly the one lookup", st.seen)
	}
}

// TestReadDisclosesStateBranch is AC2 at the tool's own layer: whichever branch
// the control plane took to serve the read is carried out verbatim, together
// with the session as it stands afterwards. A read that restored a snapshot
// must not look like a read of an already-running session.
func TestReadDisclosesStateBranch(t *testing.T) {
	for _, tc := range []struct {
		name  string
		path  string
		start string
	}{
		{name: "active", path: "active", start: "active"},
		{name: "idle", path: "idle->active->read", start: "idle"},
		{name: "snapshot", path: "snapshot->restore->read", start: "snapshot"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, st := newReadStub(t, "output\n")
			st.path = tc.path
			sess := st.sessions["s-1"]
			sess.State = tc.start
			st.sessions["s-1"] = sess

			got, err := c.ReadSession(context.Background(), "s-1", 0)
			if err != nil {
				t.Fatalf("ReadSession: %v", err)
			}
			if got.Path != tc.path {
				t.Fatalf("path = %q, want %q", got.Path, tc.path)
			}
			if got.Session.State != "active" {
				t.Fatalf("post-read state = %q, want active", got.Session.State)
			}
			if got.Session.ID != "s-1" {
				t.Fatalf("session = %+v, want the target session", got.Session)
			}
		})
	}
}

func TestReadSurfacesControlPlaneError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error":"boom"}`))
	}))
	defer srv.Close()

	c := &Client{endpoint: srv.URL, userAgent: "homelab-k3s-mcp/test", http: srv.Client()}
	_, err := c.ReadSession(context.Background(), "s-1", 0)
	var e *Error
	if !errors.As(err, &e) || e.kind != kindAPI {
		t.Fatalf("error = %v, want a kindAPI *Error", err)
	}
}

// TestReadUsesThePerSessionEndpoint pins the wire contract the control plane's
// OpenAPI document declares: POST /api/v1/sessions/{id}/read.
func TestReadUsesThePerSessionEndpoint(t *testing.T) {
	c, st := newReadStub(t, "hello\n")

	if _, err := c.ReadSession(context.Background(), "s-1", 0); err != nil {
		t.Fatalf("ReadSession: %v", err)
	}
	if len(st.seen) != 1 {
		t.Fatalf("requests = %+v, want one", st.seen)
	}
	if st.seen[0].method != http.MethodPost || st.seen[0].path != "/api/v1/sessions/s-1/read" {
		t.Fatalf("request = %+v, want POST /api/v1/sessions/s-1/read", st.seen[0])
	}
}

// TestUnavailableRefusesRead is AC4 at the service layer: with no endpoint
// configured the read fails as unavailable rather than panicking on a nil
// client.
func TestUnavailableRefusesRead(t *testing.T) {
	result, err := NewUnavailable("").ReadSession(context.Background(), "s-1", 0)
	if result != nil {
		t.Fatalf("result = %+v, want nil", result)
	}
	var e *Error
	if !errors.As(err, &e) || e.kind != kindUnavailable {
		t.Fatalf("error = %v, want a kindUnavailable *Error", err)
	}
	if err.Error() != "session platform unavailable: session platform endpoint is not configured" {
		t.Fatalf("error = %q", err.Error())
	}
}

// --- session_write ---

// writeStub serves the control plane's write endpoint *and* its read endpoint
// from one server, because session_write/AC1's stated verification is a round
// trip: write a payload, then read the output back. A shell workload is the
// smallest thing that closes that loop, so a write here appends to the same
// append-only buffer the read endpoint serves. refuseWrite forces a refusal on
// the write endpoint only, which is how the AC4 cases show that a refused write
// leaves existing output readable.
type writeStub struct {
	output   []byte
	sessions map[string]Session
	path     string
	seen     []recordedRequest
	payloads []string

	refuseWriteStatus int
	refuseWriteBody   string
}

func newWriteStub(t *testing.T, output string) (*Client, *writeStub) {
	t.Helper()

	st := &writeStub{
		output: []byte(output),
		path:   "active",
		sessions: map[string]Session{
			"s-1": {
				ID: "s-1", Name: "live shell", WorkloadType: "shell", State: "active",
				Pod: "pod-7f3c", CreatedAt: "2026-09-01T00:00:00Z",
				LastAccess: "2026-09-03T11:00:00Z",
			},
		},
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		st.seen = append(st.seen, recordedRequest{method: r.Method, path: r.URL.Path})

		trimmed := strings.TrimPrefix(r.URL.Path, sessionsPath+"/")
		id, isWrite := strings.CutSuffix(trimmed, "/write")
		if !isWrite {
			id, _ = strings.CutSuffix(trimmed, "/read")
		}
		sess, known := st.sessions[id]
		if r.Method != http.MethodPost || !known {
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"error":"session not found"}`))
			return
		}

		w.Header().Set("Content-Type", "application/json")
		if !isWrite {
			var req struct {
				Offset int64 `json:"offset"`
			}
			_ = json.NewDecoder(r.Body).Decode(&req)
			if req.Offset > int64(len(st.output)) {
				req.Offset = int64(len(st.output))
			}
			sess.State = "active"
			body, _ := json.Marshal(ReadResult{
				Session:    sess,
				Path:       "active",
				Payload:    string(st.output[req.Offset:]),
				NextOffset: int64(len(st.output)),
			})
			_, _ = w.Write(body)
			return
		}

		if st.refuseWriteStatus != 0 {
			w.WriteHeader(st.refuseWriteStatus)
			_, _ = w.Write([]byte(st.refuseWriteBody))
			return
		}

		var req struct {
			Payload string `json:"payload"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		st.payloads = append(st.payloads, req.Payload)
		// A shell workload echoes what it is given, so the accepted payload
		// becomes part of the output a later read recovers.
		st.output = append(st.output, req.Payload...)

		// Writing activates the target, so the stub reports it active
		// afterwards, exactly as the control plane does.
		sess.State = "active"
		body, _ := json.Marshal(WriteResult{Session: sess, Path: st.path})
		_, _ = w.Write(body)
	}))
	t.Cleanup(srv.Close)

	return &Client{
		endpoint:  srv.URL,
		userAgent: "homelab-k3s-mcp/test",
		http:      srv.Client(),
	}, st
}

// TestWriteThenReadRecoversOutput is AC1 at the tool layer. Two things are
// asserted that a looser test would miss: the payload arrives byte-for-byte
// (a multi-byte prompt must not be re-encoded or trimmed), and the output it
// produces is recovered by a *subsequent read* rather than by the write's own
// response — which is what "the call does not wait for execution" means in a
// contract with no completion signal.
func TestWriteThenReadRecoversOutput(t *testing.T) {
	c, st := newWriteStub(t, "banner\n")

	before, err := c.ReadSession(context.Background(), "s-1", 0)
	if err != nil {
		t.Fatalf("ReadSession before write: %v", err)
	}
	if before.Payload != "banner\n" {
		t.Fatalf("payload before write = %q", before.Payload)
	}

	// Deliberately multi-byte: byte length (10) and rune count (8) differ, so a
	// stub or client that measured the wrong one would show up here.
	const payload = "echo 안녕\n"
	written, err := c.WriteSession(context.Background(), "s-1", payload)
	if err != nil {
		t.Fatalf("WriteSession: %v", err)
	}
	if written.Path != "active" || written.Session.ID != "s-1" {
		t.Fatalf("write result = %+v", written)
	}
	if len(st.payloads) != 1 || st.payloads[0] != payload {
		t.Fatalf("payloads = %q, want exactly %q delivered verbatim", st.payloads, payload)
	}

	// The write response said nothing about output; the increment is only
	// visible once the caller reads from the cursor it held beforehand.
	after, err := c.ReadSession(context.Background(), "s-1", before.NextOffset)
	if err != nil {
		t.Fatalf("ReadSession after write: %v", err)
	}
	if after.Payload != payload {
		t.Fatalf("payload after write = %q, want exactly the increment %q", after.Payload, payload)
	}
	if after.NextOffset != before.NextOffset+int64(len(payload)) {
		t.Fatalf("nextOffset = %d, want the cursor advanced by the %d payload bytes",
			after.NextOffset, len(payload))
	}
}

// TestWriteDisclosesStateBranch is AC2 at the tool's own layer: a write to a
// parked session is not refused, it revives it, and the branch that did so is
// carried out verbatim. A write that restored a snapshot must not look like a
// write to an already-running session.
func TestWriteDisclosesStateBranch(t *testing.T) {
	for _, tc := range []struct {
		name  string
		path  string
		start string
	}{
		{name: "active", path: "active", start: "active"},
		{name: "idle", path: "idle->active->write", start: "idle"},
		{name: "snapshot", path: "snapshot->restore->write", start: "snapshot"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, st := newWriteStub(t, "banner\n")
			st.path = tc.path
			sess := st.sessions["s-1"]
			sess.State = tc.start
			st.sessions["s-1"] = sess

			got, err := c.WriteSession(context.Background(), "s-1", "ls\n")
			if err != nil {
				t.Fatalf("WriteSession: %v", err)
			}
			if got.Path != tc.path {
				t.Fatalf("path = %q, want %q", got.Path, tc.path)
			}
			if got.Session.State != "active" {
				t.Fatalf("post-write state = %q, want active", got.Session.State)
			}
			if got.Session.ID != "s-1" {
				t.Fatalf("session = %+v, want the target session", got.Session)
			}
		})
	}
}

// writeRefusal is one of the four refusals session_write/AC4 requires a caller
// to be able to tell apart.
type writeRefusal struct {
	name    string
	status  int
	reason  string
	kind    errKind
	retries bool
}

func writeRefusals() []writeRefusal {
	return []writeRefusal{
		{name: "missing session", status: http.StatusNotFound, reason: "session not found", kind: kindNotFound},
		{name: "payload over the limit", status: http.StatusRequestEntityTooLarge, reason: "workload prompt exceeds size limit", kind: kindTooLarge},
		{name: "prompt queue full", status: http.StatusTooManyRequests, reason: "workload prompt queue is full", kind: kindBusy, retries: true},
		{name: "output quota exhausted", status: http.StatusInsufficientStorage, reason: "workload output quota is full", kind: kindQuotaExhausted},
	}
}

// TestWriteMapsControlPlaneRefusals is AC4's first half: each refusal gets its
// own error kind, carries the control plane's reason through, and — the part
// AC4 names outright — the one refusal worth retrying says so while the
// permanent ones say the opposite. It also checks AC4's tail: a refused write
// leaves what the session already produced readable.
func TestWriteMapsControlPlaneRefusals(t *testing.T) {
	for _, tc := range writeRefusals() {
		t.Run(tc.name, func(t *testing.T) {
			c, st := newWriteStub(t, "banner\n")
			st.refuseWriteStatus = tc.status
			st.refuseWriteBody = `{"error":"` + tc.reason + `"}`

			_, err := c.WriteSession(context.Background(), "s-1", "ls\n")
			if err == nil {
				t.Fatalf("WriteSession succeeded, want a %d refusal", tc.status)
			}
			var e *Error
			if !errors.As(err, &e) || e.kind != tc.kind {
				t.Fatalf("error = %v, want kind %d", err, tc.kind)
			}
			if !strings.Contains(err.Error(), tc.reason) {
				t.Fatalf("error = %q, want the control plane's reason carried through", err.Error())
			}
			// A caller that cannot inspect the kind still has to know whether
			// trying again is pointless.
			if tc.retries {
				if !strings.Contains(err.Error(), "retry after") ||
					strings.Contains(err.Error(), "retrying will not help") {
					t.Fatalf("error = %q, want a full queue to read as retryable", err.Error())
				}
			} else if tc.kind != kindNotFound && !strings.Contains(err.Error(), "retrying will not help") {
				t.Fatalf("error = %q, want a permanent refusal to say so", err.Error())
			}

			back, readErr := c.ReadSession(context.Background(), "s-1", 0)
			if readErr != nil {
				t.Fatalf("ReadSession after a refused write: %v", readErr)
			}
			if back.Payload != "banner\n" {
				t.Fatalf("payload after a refused write = %q, want the existing output intact", back.Payload)
			}
		})
	}
}

// TestWriteRefusalsAreDistinctWithoutTheControlPlanesProse is AC4's second half,
// and the reason the two permanent refusals get separate kinds instead of one
// shared "not retryable". Every status here is served with the *same* error
// body, so any difference between the four messages is something this package
// added. Vary the bodies instead and the assertion becomes vacuous: the four
// would differ because the control plane worded them differently, and folding
// 413 and 507 onto one kind would still pass.
func TestWriteRefusalsAreDistinctWithoutTheControlPlanesProse(t *testing.T) {
	const sameReason = "refused"

	messages := map[string]string{}
	for _, tc := range writeRefusals() {
		c, st := newWriteStub(t, "banner\n")
		st.refuseWriteStatus = tc.status
		st.refuseWriteBody = `{"error":"` + sameReason + `"}`

		_, err := c.WriteSession(context.Background(), "s-1", "ls\n")
		if err == nil {
			t.Fatalf("%s: WriteSession succeeded, want a %d refusal", tc.name, tc.status)
		}
		for prior, priorMsg := range messages {
			if priorMsg == err.Error() {
				t.Fatalf("%s and %s are indistinguishable once the control plane's wording is held constant: %q",
					prior, tc.name, err.Error())
			}
		}
		messages[tc.name] = err.Error()
	}
	if len(messages) != 4 {
		t.Fatalf("collected %d refusals, want 4", len(messages))
	}
}

// TestWriteRejectsEmptyIDBeforeTheWire mirrors the read side's guarantee, and it
// matters more here: a write's mere arrival activates its target and can restore
// a snapshot, so a malformed call must not reach the control plane at all. The
// assertion is that the stub saw no request, not merely that an error came back.
func TestWriteRejectsEmptyIDBeforeTheWire(t *testing.T) {
	c, st := newWriteStub(t, "banner\n")

	_, err := c.WriteSession(context.Background(), "", "ls\n")
	if err == nil {
		t.Fatalf("WriteSession with an empty id succeeded, want an invalid argument error")
	}
	var e *Error
	if !errors.As(err, &e) || e.kind != kindInvalidArgument {
		t.Fatalf("error = %v, want a kindInvalidArgument *Error", err)
	}
	if len(st.seen) != 0 {
		t.Fatalf("requests = %+v, want none: a rejected write must not touch the session", st.seen)
	}
	if len(st.payloads) != 0 {
		t.Fatalf("payloads = %q, want none", st.payloads)
	}
}

// TestWriteUsesThePerSessionEndpoint pins the wire contract the control plane's
// OpenAPI document declares: POST /api/v1/sessions/{id}/write.
func TestWriteUsesThePerSessionEndpoint(t *testing.T) {
	c, st := newWriteStub(t, "banner\n")

	if _, err := c.WriteSession(context.Background(), "s-1", "ls\n"); err != nil {
		t.Fatalf("WriteSession: %v", err)
	}
	if len(st.seen) != 1 {
		t.Fatalf("requests = %+v, want one", st.seen)
	}
	if st.seen[0].method != http.MethodPost || st.seen[0].path != "/api/v1/sessions/s-1/write" {
		t.Fatalf("request = %+v, want POST /api/v1/sessions/s-1/write", st.seen[0])
	}
}

func TestWriteSurfacesControlPlaneError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error":"boom"}`))
	}))
	defer srv.Close()

	c := &Client{endpoint: srv.URL, userAgent: "homelab-k3s-mcp/test", http: srv.Client()}
	_, err := c.WriteSession(context.Background(), "s-1", "ls\n")
	var e *Error
	if !errors.As(err, &e) || e.kind != kindAPI {
		t.Fatalf("error = %v, want a kindAPI *Error", err)
	}
}

// TestUnavailableRefusesWrite is AC5 at the service layer: with no endpoint
// configured the write fails as unavailable rather than panicking on a nil
// client.
func TestUnavailableRefusesWrite(t *testing.T) {
	result, err := NewUnavailable("").WriteSession(context.Background(), "s-1", "ls\n")
	if result != nil {
		t.Fatalf("result = %+v, want nil", result)
	}
	var e *Error
	if !errors.As(err, &e) || e.kind != kindUnavailable {
		t.Fatalf("error = %v, want a kindUnavailable *Error", err)
	}
	if err.Error() != "session platform unavailable: session platform endpoint is not configured" {
		t.Fatalf("error = %q", err.Error())
	}
}
