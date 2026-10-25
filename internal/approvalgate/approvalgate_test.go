package approvalgate

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeBackend is a PocketAide stand-in. Verdicts are scripted per poll so a
// test can make a request sit in PENDING before it settles, which is the only
// way to observe that polling actually happens (AC4).
type fakeBackend struct {
	mu sync.Mutex

	createStatus int
	createBody   string

	verdicts []requestResponse
	polls    int

	createdBodies []createRequestBody
	createdRaw    []map[string]any
	createdAuth   []string
	polledAuth    []string
	pollDelay     time.Duration
}

func (f *fakeBackend) handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/external/approvals", func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		f.createdAuth = append(f.createdAuth, r.Header.Get("authorization"))

		raw, _ := io.ReadAll(r.Body)
		var body createRequestBody
		_ = json.Unmarshal(raw, &body)
		f.createdBodies = append(f.createdBodies, body)
		var keys map[string]any
		_ = json.Unmarshal(raw, &keys)
		f.createdRaw = append(f.createdRaw, keys)

		if f.createStatus != 0 && f.createStatus != http.StatusCreated {
			w.WriteHeader(f.createStatus)
			_, _ = w.Write([]byte(`{}`))
			return
		}
		if f.createBody != "" {
			_, _ = w.Write([]byte(f.createBody))
			return
		}
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"id":"req-1","status":"PENDING","createdAt":1,"expiresAt":301}`))
	})
	mux.HandleFunc("GET /api/external/approvals/{id}", func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		delay := f.pollDelay
		f.polledAuth = append(f.polledAuth, r.Header.Get("authorization"))
		idx := f.polls
		f.polls++
		var verdict requestResponse
		if idx < len(f.verdicts) {
			verdict = f.verdicts[idx]
		} else if len(f.verdicts) > 0 {
			verdict = f.verdicts[len(f.verdicts)-1]
		}
		f.mu.Unlock()

		if delay > 0 {
			time.Sleep(delay)
		}
		_ = json.NewEncoder(w).Encode(verdict)
	})
	return mux
}

func (f *fakeBackend) pollCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.polls
}

// tokenFile writes a projected-token stand-in and returns its path.
func tokenFile(t *testing.T, token string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "token")
	if err := os.WriteFile(path, []byte(token+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func newTestClient(t *testing.T, backend *fakeBackend, tune func(*Client)) (*Client, *httptest.Server) {
	t.Helper()
	srv := httptest.NewServer(backend.handler())
	t.Cleanup(srv.Close)

	c := New(Config{
		BaseURL:      srv.URL,
		TokenFile:    tokenFile(t, "sa-token"),
		Timeout:      2 * time.Second,
		PollInterval: time.Millisecond,
		GatedKinds:   []string{"v1/Secret"},
	}, "homelab-k3s-mcp")
	if tune != nil {
		tune(c)
	}
	return c, srv
}

func sampleCall() Call {
	return describedCall(func(context.Context) (string, error) {
		return "tool: resource_patch\npatch: {\"spec\":{\"replicas\":3}}", nil
	})
}

// describedCall is sampleCall with the rendering swapped, for the cases that are
// about when Describe runs rather than what it returns.
func describedCall(describe func(context.Context) (string, error)) Call {
	return Call{
		Tool:     "resource_patch",
		Pair:     Pair{Verb: "patch", Resource: "deployments"},
		Describe: describe,
	}
}

// AC2: the approval request carries every field, authenticated with the
// service account token.
func TestAuthorizeSendsTheDocumentedRequestBody(t *testing.T) {
	backend := &fakeBackend{verdicts: []requestResponse{{ID: "req-1", Status: StatusApproved}}}
	client, _ := newTestClient(t, backend, nil)

	if _, err := client.Authorize(context.Background(), sampleCall()); err != nil {
		t.Fatalf("Authorize() = %v, want approval", err)
	}

	if len(backend.createdBodies) != 1 {
		t.Fatalf("create calls = %d, want 1", len(backend.createdBodies))
	}
	body := backend.createdBodies[0]
	if body.ExternalID == "" {
		t.Error("externalId is empty")
	}
	if body.Context == "" {
		t.Error("context is empty")
	}
	if body.RequesterName != "homelab-k3s-mcp" {
		t.Errorf("requesterName = %q, want homelab-k3s-mcp", body.RequesterName)
	}
	if body.TimeoutSeconds != 2 {
		t.Errorf("timeoutSeconds = %d, want 2", body.TimeoutSeconds)
	}
	if backend.createdAuth[0] != "Bearer sa-token" {
		t.Errorf("create authorization = %q, want the service account token", backend.createdAuth[0])
	}
	if backend.polledAuth[0] != "Bearer sa-token" {
		t.Errorf("poll authorization = %q, want the service account token", backend.polledAuth[0])
	}
}

func TestRequestBodyKeysWithAndWithoutTheOptionalFields(t *testing.T) {
	cases := []struct {
		name      string
		sessionID string
		reason    string
		want      []string
	}{
		{"neither", "", "", []string{"context", "externalId", "requesterName", "timeoutSeconds"}},
		{"session only", "sess-1", "", []string{"context", "externalId", "requesterName", "sessionId", "timeoutSeconds"}},
		{"reason only", "", "롤아웃이 멈춰 재시작한다.", []string{"context", "externalId", "reason", "requesterName", "timeoutSeconds"}},
		{"both", "sess-1", "롤아웃이 멈춰 재시작한다.", []string{"context", "externalId", "reason", "requesterName", "sessionId", "timeoutSeconds"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			backend := &fakeBackend{verdicts: []requestResponse{{ID: "req-1", Status: StatusApproved}}}
			client, _ := newTestClient(t, backend, nil)
			call := sampleCall()
			call.SessionID = tc.sessionID
			call.Reason = tc.reason

			if _, err := client.Authorize(context.Background(), call); err != nil {
				t.Fatalf("Authorize() = %v", err)
			}
			raw := backend.createdRaw[0]
			got := make([]string, 0, len(raw))
			for k := range raw {
				got = append(got, k)
			}
			sort.Strings(got)
			if strings.Join(got, ",") != strings.Join(tc.want, ",") {
				t.Errorf("body keys = %v, want %v", got, tc.want)
			}
			if tc.sessionID != "" && raw["sessionId"] != tc.sessionID {
				t.Errorf("sessionId = %v, want %q", raw["sessionId"], tc.sessionID)
			}
			if tc.reason != "" && raw["reason"] != tc.reason {
				t.Errorf("reason = %v, want %q", raw["reason"], tc.reason)
			}
			if tc.reason != "" && strings.Contains(raw["context"].(string), tc.reason) {
				t.Error("the reason was folded into context; it is its own field")
			}
		})
	}
}

// The kubelet rotates the projected token in place; a token read once at
// startup would stop working about an hour later.
func TestTokenIsReadPerRequest(t *testing.T) {
	backend := &fakeBackend{verdicts: []requestResponse{{ID: "req-1", Status: StatusApproved}}}
	client, _ := newTestClient(t, backend, nil)

	if _, err := client.Authorize(context.Background(), sampleCall()); err != nil {
		t.Fatalf("Authorize() = %v", err)
	}
	if err := os.WriteFile(client.cfg.TokenFile, []byte("rotated"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := client.Authorize(context.Background(), sampleCall()); err != nil {
		t.Fatalf("Authorize() = %v", err)
	}
	if got := backend.createdAuth[1]; got != "Bearer rotated" {
		t.Errorf("authorization after rotation = %q, want the rotated token", got)
	}
}

// AC2: two calls are two approvals.
func TestExternalIDDiffersPerCall(t *testing.T) {
	backend := &fakeBackend{verdicts: []requestResponse{{ID: "req-1", Status: StatusApproved}}}
	client, _ := newTestClient(t, backend, nil)

	for i := 0; i < 2; i++ {
		if _, err := client.Authorize(context.Background(), sampleCall()); err != nil {
			t.Fatalf("Authorize() #%d = %v", i, err)
		}
	}
	if backend.createdBodies[0].ExternalID == backend.createdBodies[1].ExternalID {
		t.Fatalf("externalId was reused across calls: %q", backend.createdBodies[0].ExternalID)
	}
}

// AC4: a verdict that arrives after the request was created.
func TestPendingBecomesApprovedThroughPolling(t *testing.T) {
	backend := &fakeBackend{verdicts: []requestResponse{
		{ID: "req-1", Status: StatusPending},
		{ID: "req-1", Status: StatusPending},
		{ID: "req-1", Status: StatusApproved},
	}}
	client, _ := newTestClient(t, backend, nil)

	decision, err := client.Authorize(context.Background(), sampleCall())
	if err != nil {
		t.Fatalf("Authorize() = %v, want approval", err)
	}
	if backend.pollCount() < 3 {
		t.Errorf("polls = %d, want at least 3 (the verdict only appears on the third)", backend.pollCount())
	}
	if decision.AutoApproved {
		t.Error("AutoApproved = true for a verdict observed by polling; only a create-time verdict is automatic")
	}
}

// AC4/AC5: an EXPIRED verdict is a refusal.
func TestExpiredVerdictIsRefused(t *testing.T) {
	backend := &fakeBackend{verdicts: []requestResponse{{ID: "req-1", Status: StatusExpired}}}
	client, _ := newTestClient(t, backend, nil)

	if _, err := client.Authorize(context.Background(), sampleCall()); err == nil {
		t.Fatal("Authorize() = nil, want refusal on EXPIRED")
	}
}

// AC5: the table is the list the AC itself enumerates. Each row also names the
// verdict the refusal carries for the record (prd-event-log AC2): a human reads
// the prose, the record reads the type, and the two must not disagree.
func TestEveryFailurePathRefuses(t *testing.T) {
	cases := []struct {
		name      string
		backend   *fakeBackend
		tune      func(*Client)
		want      string
		verdict   Verdict
		requestID string
	}{
		{
			name:      "rejected",
			backend:   &fakeBackend{verdicts: []requestResponse{{ID: "req-1", Status: StatusRejected}}},
			want:      "rejected",
			verdict:   VerdictRejected,
			requestID: "req-1",
		},
		{
			name:      "expired",
			backend:   &fakeBackend{verdicts: []requestResponse{{ID: "req-1", Status: StatusExpired}}},
			want:      "expired",
			verdict:   VerdictExpired,
			requestID: "req-1",
		},
		{
			name:      "no verdict before the timeout",
			backend:   &fakeBackend{verdicts: []requestResponse{{ID: "req-1", Status: StatusPending}}},
			tune:      func(c *Client) { c.cfg.Timeout = 20 * time.Millisecond },
			want:      "no approval within",
			verdict:   VerdictTimeout,
			requestID: "req-1",
		},
		{
			name:    "external id collision",
			backend: &fakeBackend{createStatus: http.StatusConflict},
			want:    "409",
			verdict: VerdictUnreachable,
		},
		{
			name:    "service account not accepted",
			backend: &fakeBackend{createStatus: http.StatusUnauthorized},
			want:    "401",
			verdict: VerdictUnreachable,
		},
		{
			name:    "service account verification unavailable",
			backend: &fakeBackend{createStatus: http.StatusServiceUnavailable},
			want:    "returned 503",
			verdict: VerdictUnreachable,
		},
		{
			name:    "token file vanished",
			backend: &fakeBackend{},
			tune:    func(c *Client) { c.cfg.TokenFile = filepath.Join(os.TempDir(), "no-such-approval-token") },
			want:    "service account token",
			verdict: VerdictUnreachable,
		},
		{
			name:    "backend 5xx",
			backend: &fakeBackend{createStatus: http.StatusInternalServerError},
			want:    "returned 500",
			verdict: VerdictUnreachable,
		},
		{
			name:    "unreadable response",
			backend: &fakeBackend{createBody: "not json"},
			want:    "unreadable",
			verdict: VerdictUnreachable,
		},
		{
			name:    "response without a request id",
			backend: &fakeBackend{createBody: `{"status":"PENDING"}`},
			want:    "no request id",
			verdict: VerdictUnreachable,
		},
		{
			name:      "unknown status",
			backend:   &fakeBackend{verdicts: []requestResponse{{ID: "req-1", Status: "MAYBE"}}},
			want:      "unknown approval status",
			verdict:   VerdictUnreachable,
			requestID: "req-1",
		},
		{
			name:      "auto rejected",
			backend:   &fakeBackend{createBody: `{"id":"req-1","status":"REJECTED"}`},
			want:      "auto-rejected",
			verdict:   VerdictRejected,
			requestID: "req-1",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			client, _ := newTestClient(t, tc.backend, tc.tune)
			_, err := client.Authorize(context.Background(), sampleCall())
			if err == nil {
				t.Fatal("Authorize() = nil, want refusal")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error = %q, want it to mention %q", err, tc.want)
			}
			var refusal *Refusal
			if !errors.As(err, &refusal) {
				t.Fatalf("error %T is not a *Refusal; the record cannot classify it", err)
			}
			if refusal.Verdict != tc.verdict || refusal.RequestID != tc.requestID {
				t.Errorf("refusal = {%s %q}, want {%s %q}", refusal.Verdict, refusal.RequestID, tc.verdict, tc.requestID)
			}
		})
	}
}

// AC2 (prd-event-log): the refusals that never reach the backend carry their
// own verdict.
func TestRefusalsBeforeAVerdictAreTypedToo(t *testing.T) {
	unreachable := New(Config{BaseURL: "http://127.0.0.1:1", TokenFile: tokenFile(t, "t"), Timeout: time.Second, PollInterval: time.Millisecond}, "homelab-k3s-mcp")
	cases := map[string]struct {
		gate    Gate
		call    Call
		verdict Verdict
	}{
		"unreachable":  {gate: unreachable, call: sampleCall(), verdict: VerdictUnreachable},
		"unconfigured": {gate: NewUnavailable(nil), call: sampleCall(), verdict: VerdictUnconfigured},
		"undescribed":  {gate: unreachable, call: describedCall(func(context.Context) (string, error) { return "", errors.New("target could not be read") }), verdict: ""},
		"reader unconfigured": {gate: unreachable, call: describedCall(func(context.Context) (string, error) {
			return "", unavailableReader{}
		}), verdict: VerdictUnconfigured},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := tc.gate.Authorize(context.Background(), tc.call)
			var refusal *Refusal
			if !errors.As(err, &refusal) {
				t.Fatalf("Authorize() = %v (%T), want a *Refusal", err, err)
			}
			if refusal.Verdict != tc.verdict {
				t.Errorf("verdict = %q, want %q", refusal.Verdict, tc.verdict)
			}
		})
	}
}

// unavailableReader stands in for a gate reader that was never configured:
// what internal/k8s's "not configured" error says about itself.
type unavailableReader struct{}

func (unavailableReader) Error() string     { return "kubernetes client unavailable: not configured" }
func (unavailableReader) Unavailable() bool { return true }

// AC5: a network that never answers is a refusal, not a hang that eventually
// runs the call.
func TestUnreachableBackendRefuses(t *testing.T) {
	client := New(Config{
		BaseURL:      "http://127.0.0.1:1",
		TokenFile:    tokenFile(t, "t"),
		Timeout:      time.Second,
		PollInterval: time.Millisecond,
	}, "homelab-k3s-mcp")

	if _, err := client.Authorize(context.Background(), sampleCall()); err == nil {
		t.Fatal("Authorize() = nil, want refusal when the backend is unreachable")
	}
}

// AC5: this is the state a deployment without APPROVAL_GATE_URL is in.
//
// The second assertion is the property Call.Describe exists for: a gate with
// nobody to ask must not reach the renderer, because that is where the
// pre-approval cluster read lives.
func TestUnavailableGateRefuses(t *testing.T) {
	described := false
	gate := NewUnavailable(nil)
	_, err := gate.Authorize(context.Background(), describedCall(func(context.Context) (string, error) {
		described = true
		return "tool: resource_patch", nil
	}))
	if !errors.Is(err, ErrNotConfigured) {
		t.Fatalf("Authorize() = %v, want ErrNotConfigured", err)
	}
	if described {
		t.Error("an unconfigured gate rendered the context, which means it read the target it had no verdict for")
	}
}

// AC3: an approval request nobody can judge is not sent at all.
func TestEmptyContextIsRefusedBeforeTheRequestIsMade(t *testing.T) {
	backend := &fakeBackend{verdicts: []requestResponse{{ID: "req-1", Status: StatusApproved}}}
	client, _ := newTestClient(t, backend, nil)

	// Three ways a context can fail to say what it is approving.
	cases := map[string]Call{
		"blank":   describedCall(func(context.Context) (string, error) { return "   ", nil }),
		"errored": describedCall(func(context.Context) (string, error) { return "", errors.New("target could not be read") }),
		"absent":  describedCall(nil),
	}
	for name, call := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := client.Authorize(context.Background(), call); err == nil {
				t.Fatal("Authorize() = nil, want refusal for an unjudgeable context")
			}
		})
	}
	if len(backend.createdBodies) != 0 {
		t.Errorf("create calls = %d, want 0 — nothing should have been sent", len(backend.createdBodies))
	}
}

// AC7 at the client layer: Consume is the half the dispatcher spends (mcp).
func TestApprovalIsSpentOnce(t *testing.T) {
	backend := &fakeBackend{verdicts: []requestResponse{{ID: "req-1", Status: StatusApproved}}}
	client, _ := newTestClient(t, backend, nil)

	decision, err := client.Authorize(context.Background(), sampleCall())
	if err != nil {
		t.Fatalf("Authorize() = %v", err)
	}
	if err := decision.Consume(); err != nil {
		t.Fatalf("first Consume() = %v, want nil", err)
	}
	if err := decision.Consume(); !errors.Is(err, ErrConsumed) {
		t.Fatalf("second Consume() = %v, want ErrConsumed", err)
	}
}

// AC9: an approval nobody looked at is still an approval.
func TestAutoApprovalIsReported(t *testing.T) {
	backend := &fakeBackend{createBody: `{"id":"req-1","status":"APPROVED"}`}
	client, _ := newTestClient(t, backend, nil)

	decision, err := client.Authorize(context.Background(), sampleCall())
	if err != nil {
		t.Fatalf("Authorize() = %v, want approval", err)
	}
	if !decision.AutoApproved {
		t.Error("AutoApproved = false, want true so the caller can say so in its answer")
	}
	if backend.pollCount() != 0 {
		t.Errorf("polls = %d, want 0 — an auto-approved request is already settled", backend.pollCount())
	}
}

func TestFromEnv(t *testing.T) {
	t.Run("unset is not an error", func(t *testing.T) {
		t.Setenv("APPROVAL_GATE_URL", "")
		t.Setenv("APPROVAL_GATE_TOKEN_FILE", "")
		cfg, err := FromEnv()
		if err != nil || cfg != nil {
			t.Fatalf("FromEnv() = (%v, %v), want (nil, nil)", cfg, err)
		}
	})

	t.Run("token file without a URL is an error", func(t *testing.T) {
		t.Setenv("APPROVAL_GATE_URL", "")
		t.Setenv("APPROVAL_GATE_TOKEN_FILE", tokenFile(t, "t"))
		if _, err := FromEnv(); err == nil {
			t.Fatal("FromEnv() = nil error, want a complaint about the missing URL")
		}
	})

	t.Run("unreadable token is an error", func(t *testing.T) {
		t.Setenv("APPROVAL_GATE_URL", "http://pocket-aide")
		t.Setenv("APPROVAL_GATE_TOKEN_FILE", filepath.Join(t.TempDir(), "missing"))
		if _, err := FromEnv(); err == nil {
			t.Fatal("FromEnv() = nil error, want a complaint about the unreadable token")
		}
		t.Setenv("APPROVAL_GATE_TOKEN_FILE", tokenFile(t, " "))
		if _, err := FromEnv(); err == nil {
			t.Fatal("FromEnv() = nil error, want a complaint about the empty token")
		}
	})

	t.Run("defaults and overrides", func(t *testing.T) {
		path := tokenFile(t, "t")
		t.Setenv("APPROVAL_GATE_URL", "http://pocket-aide/")
		t.Setenv("APPROVAL_GATE_TOKEN_FILE", path)
		t.Setenv("APPROVAL_GATE_TIMEOUT_SECONDS", "")
		t.Setenv("APPROVAL_GATE_POLL_INTERVAL_SECONDS", "")
		t.Setenv("RESOURCE_GATED_KINDS", "")

		cfg, err := FromEnv()
		if err != nil {
			t.Fatalf("FromEnv() = %v", err)
		}
		if cfg.BaseURL != "http://pocket-aide" {
			t.Errorf("BaseURL = %q, want the trailing slash trimmed", cfg.BaseURL)
		}
		if cfg.TokenFile != path {
			t.Errorf("TokenFile = %q, want %q", cfg.TokenFile, path)
		}
		if cfg.Timeout != defaultTimeout || cfg.PollInterval != defaultPollInterval {
			t.Errorf("timeouts = (%v, %v), want the documented defaults", cfg.Timeout, cfg.PollInterval)
		}
		if len(cfg.GatedKinds) != 1 || cfg.GatedKinds[0] != defaultGatedKind {
			t.Errorf("GatedKinds = %v, want [%s]", cfg.GatedKinds, defaultGatedKind)
		}

		t.Setenv("APPROVAL_GATE_TIMEOUT_SECONDS", "0")
		if _, err := FromEnv(); err == nil {
			t.Error("FromEnv() accepted a zero timeout, which would mean 'never wait for approval'")
		}

		t.Setenv("APPROVAL_GATE_TIMEOUT_SECONDS", "30")
		t.Setenv("RESOURCE_GATED_KINDS", "v1/Secret, example.com/v1/Credential")
		cfg, err = FromEnv()
		if err != nil {
			t.Fatalf("FromEnv() = %v", err)
		}
		if cfg.Timeout != 30*time.Second {
			t.Errorf("Timeout = %v, want 30s", cfg.Timeout)
		}
		if len(cfg.GatedKinds) != 2 {
			t.Errorf("GatedKinds = %v, want both kinds", cfg.GatedKinds)
		}
	})

	t.Run("token file defaults to the projected volume", func(t *testing.T) {
		t.Setenv("APPROVAL_GATE_URL", "http://pocket-aide")
		t.Setenv("APPROVAL_GATE_TOKEN_FILE", "")
		_, err := FromEnv()
		if err == nil || !strings.Contains(err.Error(), DefaultTokenFile) {
			t.Fatalf("FromEnv() = %v, want it to look for %s", err, DefaultTokenFile)
		}
	})
}
