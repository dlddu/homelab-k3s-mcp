package pocketaide_test

import (
	"context"
	"errors"
	"net/http"
	"os"
	"strings"
	"testing"

	"github.com/dlddu/homelab-k3s-mcp/internal/pocketaide"
)

// AC1: the read goes to PocketAide's caller route with the projected token and
// carries nothing else — the subject is the registration, not a parameter.
func TestChatPresenceGetsWithTheServiceAccountToken(t *testing.T) {
	fake := &fakePocketAide{status: http.StatusOK, reply: `{"present":true,"last_seen_at":1800000000,"ttl_seconds":45}`}
	client, _ := newClient(t, fake)

	res, err := client.ChatPresence(context.Background())
	if err != nil {
		t.Fatalf("chat presence: %v", err)
	}
	if !res.Present || res.LastSeenAt == nil || *res.LastSeenAt != 1_800_000_000 || res.TTLSeconds != 45 {
		t.Fatalf("result = %+v", *res)
	}
	calls := fake.all()
	if len(calls) != 1 {
		t.Fatalf("calls = %d", len(calls))
	}
	got := calls[0]
	if got.path != "/api/external/chat-presence" || got.auth != "Bearer sa-token-1" {
		t.Fatalf("request = %+v", got)
	}
	if len(got.body) != 0 {
		t.Fatalf("body = %v, want none", got.body)
	}
}

// AC1: the kubelet replaces the projected token in place, so it is read per call.
func TestChatPresenceReadsTheTokenOnEveryCall(t *testing.T) {
	fake := &fakePocketAide{status: http.StatusOK, reply: `{"present":false,"last_seen_at":null,"ttl_seconds":45}`}
	client, path := newClient(t, fake)

	if _, err := client.ChatPresence(context.Background()); err != nil {
		t.Fatalf("first call: %v", err)
	}
	if err := os.WriteFile(path, []byte("sa-token-2\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := client.ChatPresence(context.Background()); err != nil {
		t.Fatalf("second call: %v", err)
	}

	calls := fake.all()
	if len(calls) != 2 || calls[0].auth != "Bearer sa-token-1" || calls[1].auth != "Bearer sa-token-2" {
		t.Fatalf("authorizations = %q, %q", calls[0].auth, calls[1].auth)
	}
}

// AC2: a null last read must stay nil rather than decoding to zero — "unknown"
// and "was here at the epoch" are not the same claim.
func TestChatPresenceKeepsNullLastSeenDistinctFromZero(t *testing.T) {
	unknown := &fakePocketAide{status: http.StatusOK, reply: `{"present":false,"last_seen_at":null,"ttl_seconds":45}`}
	client, _ := newClient(t, unknown)
	res, err := client.ChatPresence(context.Background())
	if err != nil {
		t.Fatalf("chat presence: %v", err)
	}
	if res.Present || res.LastSeenAt != nil {
		t.Fatalf("never seen = %+v, want absent with a nil last seen", *res)
	}

	away := &fakePocketAide{status: http.StatusOK, reply: `{"present":false,"last_seen_at":1799999000,"ttl_seconds":45}`}
	client, _ = newClient(t, away)
	res, err = client.ChatPresence(context.Background())
	if err != nil {
		t.Fatalf("chat presence: %v", err)
	}
	if res.Present || res.LastSeenAt == nil || *res.LastSeenAt != 1_799_999_000 {
		t.Fatalf("away = %+v, want absent with the last read kept", *res)
	}
}

// AC4: each refusal is its own sentence, and none of them is reported as a
// presence of false — an error must not read as "they are not there".
func TestChatPresenceExplainsEachRefusal(t *testing.T) {
	for _, c := range []struct {
		status int
		want   string
	}{
		{http.StatusUnauthorized, "registered as a caller"},
		{http.StatusNotFound, "AC22"},
		{http.StatusInternalServerError, "returned 500"},
	} {
		fake := &fakePocketAide{status: c.status}
		client, _ := newClient(t, fake)

		res, err := client.ChatPresence(context.Background())
		if err == nil {
			t.Fatalf("status %d: got %+v, want an error", c.status, res)
		}
		if res != nil {
			t.Fatalf("status %d: result = %+v, want none", c.status, res)
		}
		if !strings.Contains(err.Error(), c.want) {
			t.Fatalf("status %d: error = %q, want it to mention %q", c.status, err, c.want)
		}
		var pErr *pocketaide.Error
		if errors.As(err, &pErr) && pErr.Unavailable() {
			t.Fatalf("status %d: refusal reported as unconfigured", c.status)
		}
	}

	// An OK with an unreadable body is an error too, not a false presence.
	fake := &fakePocketAide{status: http.StatusOK, reply: `not json`}
	client, _ := newClient(t, fake)
	if _, err := client.ChatPresence(context.Background()); err == nil || !strings.Contains(err.Error(), "unreadable response") {
		t.Fatalf("unreadable body: err = %v, want an unreadable-response error", err)
	}
}

// AC5: with PocketAide unconfigured the tool refuses with the reason instead of
// claiming the owner is away.
func TestUnavailableChatPresenceRefusesWithTheReason(t *testing.T) {
	res, err := pocketaide.NewUnavailable("").ChatPresence(context.Background())
	if res != nil {
		t.Fatalf("result = %+v, want none", res)
	}
	var pErr *pocketaide.Error
	if !errors.As(err, &pErr) || !pErr.Unavailable() {
		t.Fatalf("err = %v, want an unavailable error", err)
	}
	if !strings.Contains(err.Error(), "APPROVAL_GATE_URL") {
		t.Fatalf("err = %v, want it to name APPROVAL_GATE_URL", err)
	}
}
