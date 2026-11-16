package server_test

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dlddu/homelab-k3s-mcp/internal/pocketaide"
)

func presenceUpstream(t *testing.T, status int, body string) (http.Handler, *[]*http.Request) {
	t.Helper()
	var seen []*http.Request
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = append(seen, r)
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(upstream.Close)
	tokenPath := filepath.Join(t.TempDir(), "token")
	if err := os.WriteFile(tokenPath, []byte("sa-token"), 0o600); err != nil {
		t.Fatal(err)
	}
	return pocketAideApp(pocketaide.New(upstream.URL, tokenPath, upstream.Client())), &seen
}

// The tool is advertised as a read with no arguments at all — a caller cannot
// ask about anyone but the owner, because there is nothing to ask with.
func TestToolsListAdvertisesPocketAideChatPresenceGet(t *testing.T) {
	tool := findTool(t, toolsList(t, pocketAideApp(nil)), "pocket_aide_chat_presence_get")

	props := at(t, tool, "inputSchema", "properties").(map[string]any)
	if len(props) != 0 {
		t.Fatalf("properties = %v, want none", props)
	}
	if at(t, tool, "inputSchema", "additionalProperties") != false {
		t.Fatalf("additionalProperties = %v, want false", at(t, tool, "inputSchema", "additionalProperties"))
	}
	if at(t, tool, "annotations", "readOnlyHint") != true ||
		at(t, tool, "annotations", "destructiveHint") != false ||
		at(t, tool, "annotations", "idempotentHint") != true ||
		at(t, tool, "annotations", "openWorldHint") != true {
		t.Fatalf("annotations = %v, want a read-only idempotent tool", at(t, tool, "annotations"))
	}
}

// PRD-12 AC22: present with a last read, and the GET goes to PocketAide's
// caller route carrying the projected service account token.
func TestPocketAideChatPresenceGetReportsPresent(t *testing.T) {
	app, seen := presenceUpstream(t, http.StatusOK, `{"present":true,"last_seen_at":1800000000,"ttl_seconds":45}`)

	body := callTool(t, app, 420, "pocket_aide_chat_presence_get", map[string]any{})
	if at(t, body, "result", "isError") != false ||
		at(t, body, "result", "structuredContent", "present") != true ||
		at(t, body, "result", "structuredContent", "last_seen_at") != float64(1_800_000_000) ||
		at(t, body, "result", "structuredContent", "ttl_seconds") != float64(45) {
		t.Fatalf("result = %v", at(t, body, "result"))
	}
	if len(*seen) != 1 {
		t.Fatalf("upstream calls = %d, want 1", len(*seen))
	}
	req := (*seen)[0]
	if req.Method != http.MethodGet || req.URL.Path != "/api/external/chat-presence" {
		t.Fatalf("upstream got %s %s, want GET /api/external/chat-presence", req.Method, req.URL.Path)
	}
	if req.Header.Get("authorization") != "Bearer sa-token" {
		t.Fatalf("authorization = %q, want the service account token", req.Header.Get("authorization"))
	}
}

// "Never seen" and "was here, left" must not collapse into one answer: the
// first is null, the second keeps the time it last read.
func TestPocketAideChatPresenceGetDistinguishesUnknownFromAway(t *testing.T) {
	unknown, _ := presenceUpstream(t, http.StatusOK, `{"present":false,"last_seen_at":null,"ttl_seconds":45}`)
	body := callTool(t, unknown, 421, "pocket_aide_chat_presence_get", map[string]any{})
	got := at(t, body, "result", "structuredContent").(map[string]any)
	seenAt, present := got["last_seen_at"]
	if got["present"] != false || !present || seenAt != nil {
		t.Fatalf("never seen = %v, want present false with last_seen_at present and null", got)
	}

	away, _ := presenceUpstream(t, http.StatusOK, `{"present":false,"last_seen_at":1799999000,"ttl_seconds":45}`)
	body = callTool(t, away, 422, "pocket_aide_chat_presence_get", map[string]any{})
	got = at(t, body, "result", "structuredContent").(map[string]any)
	if got["present"] != false || got["last_seen_at"] != float64(1_799_999_000) {
		t.Fatalf("away = %v, want present false with the last read kept", got)
	}
}

// Arguments are refused rather than ignored — a caller passing a user is
// asking for something this tool cannot do.
func TestPocketAideChatPresenceGetTakesNoArguments(t *testing.T) {
	fake := &fakePocketAide{presence: &pocketaide.ChatPresence{Present: true, TTLSeconds: 45}}
	app := pocketAideApp(fake)

	for _, args := range []map[string]any{{"user": "alice"}, {"userId": 1}} {
		body := callTool(t, app, 423, "pocket_aide_chat_presence_get", args)
		if message, _ := at(t, body, "error", "message").(string); !strings.Contains(message, "takes no arguments") {
			t.Fatalf("args %v: error = %v, want a refusal", args, body["error"])
		}
	}
	if fake.presenceCalls != 0 {
		t.Fatalf("presence calls = %d, want none", fake.presenceCalls)
	}

	body := callTool(t, app, 424, "pocket_aide_chat_presence_get", map[string]any{})
	if at(t, body, "result", "isError") != false || fake.presenceCalls != 1 {
		t.Fatalf("empty arguments: result = %v, calls = %d", at(t, body, "result"), fake.presenceCalls)
	}
}

// PocketAide's refusals arrive distinguished: an unregistered caller (401) and
// a PocketAide too old to have the route (404) say so, and a server without
// PocketAide configured refuses with the reason rather than reporting absent —
// "unknown" must not be answered as "not there".
func TestPocketAideChatPresenceGetSurfacesRefusals(t *testing.T) {
	unauthorized, _ := presenceUpstream(t, http.StatusUnauthorized, ``)
	body := callTool(t, unauthorized, 425, "pocket_aide_chat_presence_get", map[string]any{})
	text, _ := at(t, body, "result", "content", 0, "text").(string)
	if at(t, body, "result", "isError") != true || !strings.Contains(text, "registered as a caller") {
		t.Fatalf("401: result = %v", at(t, body, "result"))
	}

	missing, _ := presenceUpstream(t, http.StatusNotFound, ``)
	body = callTool(t, missing, 426, "pocket_aide_chat_presence_get", map[string]any{})
	text, _ = at(t, body, "result", "content", 0, "text").(string)
	if at(t, body, "result", "isError") != true || !strings.Contains(text, "AC22") {
		t.Fatalf("404: result = %v, want the deploy-a-newer-PocketAide hint", at(t, body, "result"))
	}

	body = callTool(t, pocketAideApp(nil), 427, "pocket_aide_chat_presence_get", map[string]any{})
	text, _ = at(t, body, "result", "content", 0, "text").(string)
	if at(t, body, "result", "isError") != true ||
		!strings.Contains(text, "pocket-aide unavailable") || !strings.Contains(text, "APPROVAL_GATE_URL") {
		t.Fatalf("unconfigured: result = %v", at(t, body, "result"))
	}
}
