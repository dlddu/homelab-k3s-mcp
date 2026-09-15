package gatekeeper

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeBackend is a gatekeeper stand-in. Verdicts are scripted per poll so a
// test can make a request sit in PENDING before it settles, which is the only
// way to observe that polling actually happens (AC4).
type fakeBackend struct {
	mu sync.Mutex

	createStatus int
	createBody   string

	verdicts []requestResponse
	polls    int

	createdBodies []createRequestBody
	createdKeys   []string
	polledKeys    []string
	pollDelay     time.Duration
}

func (f *fakeBackend) handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/requests", func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		f.createdKeys = append(f.createdKeys, r.Header.Get("x-api-key"))

		var body createRequestBody
		_ = json.NewDecoder(r.Body).Decode(&body)
		f.createdBodies = append(f.createdBodies, body)

		if f.createStatus != 0 && f.createStatus != http.StatusOK {
			w.WriteHeader(f.createStatus)
			_, _ = w.Write([]byte(`{}`))
			return
		}
		if f.createBody != "" {
			_, _ = w.Write([]byte(f.createBody))
			return
		}
		_, _ = w.Write([]byte(`{"id":"req-1","externalId":"` + body.ExternalID + `","status":"PENDING"}`))
	})
	mux.HandleFunc("GET /api/requests/{id}", func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		delay := f.pollDelay
		f.polledKeys = append(f.polledKeys, r.Header.Get("x-api-key"))
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

func newTestClient(t *testing.T, backend *fakeBackend, tune func(*Client)) (*Client, *httptest.Server) {
	t.Helper()
	srv := httptest.NewServer(backend.handler())
	t.Cleanup(srv.Close)

	c := New(Config{
		BaseURL:      srv.URL,
		APIKey:       "test-key",
		UserID:       "user-7",
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

// AC2: the approval request carries every field the backend needs to show a
// human what they are being asked about, authenticated with the API key.
func TestAuthorizeSendsTheDocumentedRequestBody(t *testing.T) {
	backend := &fakeBackend{verdicts: []requestResponse{{ID: "req-1", Status: StatusApproved, ProcessedByID: "op-1"}}}
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
	if body.UserID != "user-7" {
		t.Errorf("userId = %q, want user-7 so the push notification is sent", body.UserID)
	}
	if backend.createdKeys[0] != "test-key" {
		t.Errorf("x-api-key = %q, want test-key", backend.createdKeys[0])
	}
	if backend.polledKeys[0] != "test-key" {
		t.Errorf("poll x-api-key = %q, want test-key", backend.polledKeys[0])
	}
}

// AC2: two calls are two approvals. Reusing an id would let one approval stand
// in for a second call.
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

// AC4: a verdict that arrives after the request was created is only visible to
// a client that keeps asking.
func TestPendingBecomesApprovedThroughPolling(t *testing.T) {
	backend := &fakeBackend{verdicts: []requestResponse{
		{ID: "req-1", Status: StatusPending},
		{ID: "req-1", Status: StatusPending},
		{ID: "req-1", Status: StatusApproved, ProcessedByID: "op-9"},
	}}
	client, _ := newTestClient(t, backend, nil)

	decision, err := client.Authorize(context.Background(), sampleCall())
	if err != nil {
		t.Fatalf("Authorize() = %v, want approval", err)
	}
	if backend.pollCount() < 3 {
		t.Errorf("polls = %d, want at least 3 (the verdict only appears on the third)", backend.pollCount())
	}
	if decision.ProcessedByID != "op-9" {
		t.Errorf("processedById = %q, want op-9 recorded for the audit log", decision.ProcessedByID)
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

// AC5: every path that is not an observed approval refuses. The table is the
// list the AC itself enumerates.
func TestEveryFailurePathRefuses(t *testing.T) {
	cases := []struct {
		name    string
		backend *fakeBackend
		tune    func(*Client)
		want    string
	}{
		{
			name:    "rejected",
			backend: &fakeBackend{verdicts: []requestResponse{{ID: "req-1", Status: StatusRejected}}},
			want:    "rejected",
		},
		{
			name:    "expired",
			backend: &fakeBackend{verdicts: []requestResponse{{ID: "req-1", Status: StatusExpired}}},
			want:    "expired",
		},
		{
			name:    "no verdict before the timeout",
			backend: &fakeBackend{verdicts: []requestResponse{{ID: "req-1", Status: StatusPending}}},
			tune:    func(c *Client) { c.cfg.Timeout = 20 * time.Millisecond },
			want:    "no approval within",
		},
		{
			name:    "external id collision",
			backend: &fakeBackend{createStatus: http.StatusConflict},
			want:    "409",
		},
		{
			name:    "backend 5xx",
			backend: &fakeBackend{createStatus: http.StatusInternalServerError},
			want:    "returned 500",
		},
		{
			name:    "unreadable response",
			backend: &fakeBackend{createBody: "not json"},
			want:    "unreadable",
		},
		{
			name:    "response without a request id",
			backend: &fakeBackend{createBody: `{"status":"PENDING"}`},
			want:    "no request id",
		},
		{
			name:    "auto rejected",
			backend: &fakeBackend{createBody: `{"id":"req-1","status":"REJECTED","autoRejected":true}`},
			want:    "auto-rejected",
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
		})
	}
}

// AC5: a network that never answers is a refusal, not a hang that eventually
// runs the call.
func TestUnreachableBackendRefuses(t *testing.T) {
	client := New(Config{
		BaseURL:      "http://127.0.0.1:1",
		APIKey:       "k",
		Timeout:      time.Second,
		PollInterval: time.Millisecond,
	}, "homelab-k3s-mcp")

	if _, err := client.Authorize(context.Background(), sampleCall()); err == nil {
		t.Fatal("Authorize() = nil, want refusal when the backend is unreachable")
	}
}

// AC5: an unconfigured gate refuses rather than waving calls through. This is
// the state a deployment without GATEKEEPER_* env is in.
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

	// Three ways a context can fail to say what it is approving. AC3 ends by
	// asking for the same outcome from each: no request, not a vague one.
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

// AC9: an approval nobody looked at is still an approval, but it must not look
// like one that a human granted.
func TestAutoApprovalIsReported(t *testing.T) {
	backend := &fakeBackend{createBody: `{"id":"req-1","status":"APPROVED","autoApproved":true,"processedById":"auto"}`}
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
		t.Setenv("GATEKEEPER_BASE_URL", "")
		t.Setenv("GATEKEEPER_API_KEY", "")
		cfg, err := FromEnv()
		if err != nil || cfg != nil {
			t.Fatalf("FromEnv() = (%v, %v), want (nil, nil)", cfg, err)
		}
	})

	t.Run("half configured is an error", func(t *testing.T) {
		t.Setenv("GATEKEEPER_BASE_URL", "http://gatekeeper")
		t.Setenv("GATEKEEPER_API_KEY", "")
		if _, err := FromEnv(); err == nil {
			t.Fatal("FromEnv() = nil error, want a complaint about the missing API key")
		}
	})

	t.Run("defaults and overrides", func(t *testing.T) {
		t.Setenv("GATEKEEPER_BASE_URL", "http://gatekeeper/")
		t.Setenv("GATEKEEPER_API_KEY", "k")
		t.Setenv("GATEKEEPER_TIMEOUT_SECONDS", "")
		t.Setenv("GATEKEEPER_POLL_INTERVAL_SECONDS", "")
		t.Setenv("RESOURCE_GATED_KINDS", "")

		cfg, err := FromEnv()
		if err != nil {
			t.Fatalf("FromEnv() = %v", err)
		}
		if cfg.BaseURL != "http://gatekeeper" {
			t.Errorf("BaseURL = %q, want the trailing slash trimmed", cfg.BaseURL)
		}
		if cfg.Timeout != defaultTimeout || cfg.PollInterval != defaultPollInterval {
			t.Errorf("timeouts = (%v, %v), want the documented defaults", cfg.Timeout, cfg.PollInterval)
		}
		if len(cfg.GatedKinds) != 1 || cfg.GatedKinds[0] != defaultGatedKind {
			t.Errorf("GatedKinds = %v, want [%s]", cfg.GatedKinds, defaultGatedKind)
		}

		t.Setenv("GATEKEEPER_TIMEOUT_SECONDS", "0")
		if _, err := FromEnv(); err == nil {
			t.Error("FromEnv() accepted a zero timeout, which would mean 'never wait for approval'")
		}

		t.Setenv("GATEKEEPER_TIMEOUT_SECONDS", "30")
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
}
