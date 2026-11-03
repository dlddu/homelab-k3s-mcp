package server_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dlddu/homelab-k3s-mcp/internal/drop"
	"github.com/dlddu/homelab-k3s-mcp/internal/mcp"
	"github.com/dlddu/homelab-k3s-mcp/internal/server"
)

type fakeDrop struct {
	calls []drop.Request
	token *drop.Token
	err   error
}

func (f *fakeDrop) IssueToken(_ context.Context, req drop.Request) (*drop.Token, error) {
	f.calls = append(f.calls, req)
	return f.token, f.err
}

func dropApp(svc drop.Service) http.Handler {
	return server.App(nil, unavailableK8s(), unavailableGitHub(), unavailableAWS(), unavailableGrafana(), unavailableOpenSearch(), unavailableSessionPlatform(),
		mcp.WithDrop(svc))
}

var sampleDropToken = &drop.Token{
	Token:     "drk_secret",
	ExpiresAt: "2026-11-01T13:00:00Z",
	MaxDays:   3,
	CreatedBy: "sa:homelab-k3s-mcp/homelab-k3s-mcp",
	BaseURL:   "https://drop.llkm.nl",
}

func TestToolsListAdvertisesDropTokenIssue(t *testing.T) {
	tool := findTool(t, toolsList(t, dropApp(nil)), "drop_token_issue")

	props := at(t, tool, "inputSchema", "properties").(map[string]any)
	if len(props) != 3 || props["valid_minutes"] == nil || props["max_days"] == nil || props["label"] == nil {
		t.Fatalf("properties = %v, want exactly valid_minutes, max_days and label", props)
	}
	for name := range props {
		lower := strings.ToLower(name)
		for _, banned := range []string{"file", "content", "data", "body", "path", "reason"} {
			if strings.Contains(lower, banned) {
				t.Fatalf("property %q looks like a file or content parameter", name)
			}
		}
	}
	if at(t, tool, "inputSchema", "additionalProperties") != false {
		t.Fatalf("additionalProperties = %v", at(t, tool, "inputSchema", "additionalProperties"))
	}
	if req, ok := at(t, tool, "inputSchema").(map[string]any)["required"]; ok {
		t.Fatalf("required = %v, want every argument optional", req)
	}
	if at(t, tool, "inputSchema", "properties", "valid_minutes", "default") != float64(60) ||
		at(t, tool, "inputSchema", "properties", "valid_minutes", "maximum") != float64(1440) ||
		at(t, tool, "inputSchema", "properties", "max_days", "maximum") != float64(30) ||
		at(t, tool, "inputSchema", "properties", "label", "maxLength") != float64(drop.MaxLabelRunes) {
		t.Fatalf("schema bounds = %v", props)
	}
	if at(t, tool, "annotations", "readOnlyHint") != false ||
		at(t, tool, "annotations", "destructiveHint") != false ||
		at(t, tool, "annotations", "idempotentHint") != false ||
		at(t, tool, "annotations", "openWorldHint") != true {
		t.Fatalf("annotations = %v", at(t, tool, "annotations"))
	}
	desc, _ := tool["description"].(string)
	for _, want := range []string{"never sends or receives file contents", "curl", "-o", "context"} {
		if !strings.Contains(desc, want) {
			t.Fatalf("description does not say %q: %s", want, desc)
		}
	}
}

func TestDropTokenIssueReturnsTokenAndCurlTemplates(t *testing.T) {
	fake := &fakeDrop{token: sampleDropToken}
	body := callTool(t, dropApp(fake), 501, "drop_token_issue", map[string]any{"max_days": 3, "label": "빌드 로그"})

	if at(t, body, "result", "isError") != false {
		t.Fatalf("result = %v", at(t, body, "result"))
	}
	sc := at(t, body, "result", "structuredContent").(map[string]any)
	wantKeys := []string{"base_url", "created_by", "expires_at", "max_days", "token", "usage"}
	if len(sc) != len(wantKeys) {
		t.Fatalf("structuredContent keys = %v, want %v", sc, wantKeys)
	}
	for _, k := range wantKeys {
		if _, ok := sc[k]; !ok {
			t.Fatalf("structuredContent lacks %q: %v", k, sc)
		}
	}
	if sc["token"] != "drk_secret" || sc["expires_at"] != "2026-11-01T13:00:00Z" || sc["max_days"] != float64(3) ||
		sc["base_url"] != "https://drop.llkm.nl" || sc["created_by"] != "sa:homelab-k3s-mcp/homelab-k3s-mcp" {
		t.Fatalf("structuredContent = %v", sc)
	}
	usage := sc["usage"].(map[string]any)
	if usage["setup"] != "export DROP_TOKEN='drk_secret'" {
		t.Fatalf("setup = %v", usage["setup"])
	}
	raw, _ := json.Marshal(usage)
	if strings.Count(string(raw), "drk_secret") != 1 {
		t.Fatalf("usage carries the token %d times, want once (setup only): %s", strings.Count(string(raw), "drk_secret"), raw)
	}
	upload, _ := usage["upload"].(string)
	if !strings.Contains(upload, `-H "Authorization: Bearer $DROP_TOKEN"`) || !strings.Contains(upload, "https://drop.llkm.nl/api/v1/drops") || !strings.Contains(upload, "-F days=3") {
		t.Fatalf("upload = %q, want the base_url, the $DROP_TOKEN placeholder and days lowered to max_days", upload)
	}
	for _, k := range []string{"download_file", "download_zip"} {
		if s, _ := usage[k].(string); !strings.Contains(s, " -o ") {
			t.Fatalf("%s = %q, want it to save to disk with -o", k, s)
		}
	}
	if len(fake.calls) != 1 || fake.calls[0] != (drop.Request{MaxDays: 3, Label: "빌드 로그"}) {
		t.Fatalf("calls = %+v", fake.calls)
	}
}

func TestDropTokenIssueDefaultsAndUnlimitedDaysKeepTheExampleAtSeven(t *testing.T) {
	tok := *sampleDropToken
	tok.MaxDays = 30
	fake := &fakeDrop{token: &tok}
	body := callTool(t, dropApp(fake), 502, "drop_token_issue", nil)

	if at(t, body, "result", "isError") != false {
		t.Fatalf("result = %v", at(t, body, "result"))
	}
	if s, _ := at(t, body, "result", "structuredContent", "usage", "upload").(string); !strings.Contains(s, "-F days=7") {
		t.Fatalf("upload = %q", s)
	}
	if len(fake.calls) != 1 || fake.calls[0] != (drop.Request{}) {
		t.Fatalf("calls = %+v, want an empty request (drop's 60-minute default)", fake.calls)
	}
}

func TestDropTokenIssueRefusesBadArgumentsBeforeCallingDrop(t *testing.T) {
	fake := &fakeDrop{token: sampleDropToken}
	app := dropApp(fake)
	cases := []struct {
		args map[string]any
		want string
	}{
		{map[string]any{"valid_minutes": 0}, "valid_minutes must be between 1 and 1440"},
		{map[string]any{"valid_minutes": 1441}, "valid_minutes must be between 1 and 1440"},
		{map[string]any{"valid_minutes": 1.5}, "valid_minutes must be an integer"},
		{map[string]any{"valid_minutes": "60"}, "valid_minutes must be an integer"},
		{map[string]any{"max_days": 0}, "max_days must be between 1 and 30"},
		{map[string]any{"max_days": 31}, "max_days must be between 1 and 30"},
		{map[string]any{"label": 7}, "label must be a string"},
		{map[string]any{"label": strings.Repeat("가", drop.MaxLabelRunes+1)}, "label must be 40 characters or fewer"},
		{map[string]any{"file": "aGVsbG8="}, "unknown argument(s): file"},
		{map[string]any{"content": "hello", "valid_minutes": 5}, "unknown argument(s): content"},
		{map[string]any{"reason": "업로드"}, "unknown argument(s): reason"},
	}
	for i, tc := range cases {
		body := callTool(t, app, 510+i, "drop_token_issue", tc.args)
		message, _ := at(t, body, "error", "message").(string)
		if at(t, body, "error", "code") != float64(-32602) || !strings.Contains(message, tc.want) {
			t.Fatalf("args %v: error = %v, want -32602 containing %q", tc.args, body["error"], tc.want)
		}
	}
	if len(fake.calls) != 0 {
		t.Fatalf("calls = %+v, want none", fake.calls)
	}

	body := callTool(t, app, 530, "drop_token_issue", map[string]any{"valid_minutes": 1440, "max_days": 30, "label": strings.Repeat("가", drop.MaxLabelRunes)})
	if at(t, body, "result", "isError") != false {
		t.Fatalf("the limits themselves must pass: %v", body)
	}
}

func TestDropTokenIssueUnconfiguredRefusesGracefully(t *testing.T) {
	app := dropApp(nil)
	body := callTool(t, app, 540, "drop_token_issue", map[string]any{})
	text, _ := at(t, body, "result", "content", 0, "text").(string)
	if at(t, body, "result", "isError") != true || text != "drop unavailable: drop is not configured (DROP_URL)" {
		t.Fatalf("result = %v", at(t, body, "result"))
	}
	body = callTool(t, app, 541, "drop_token_issue", map[string]any{"valid_minutes": 0})
	if message, _ := at(t, body, "error", "message").(string); !strings.Contains(message, "valid_minutes") {
		t.Fatalf("bad argument on an unconfigured server: %v, want the validation refusal", body)
	}
	if at(t, callTool(t, app, 542, "ping", map[string]any{}), "result", "content", 0, "text") != "pong" {
		t.Fatal("ping broke after the refusal")
	}
}

func TestDropTokenIssueSurfacesDropRefusal(t *testing.T) {
	fake := &fakeDrop{err: errors.New("drop error: drop does not allow this server's service account (403) — add it to drop's SA_ALLOW")}
	body := callTool(t, dropApp(fake), 550, "drop_token_issue", map[string]any{})
	text, _ := at(t, body, "result", "content", 0, "text").(string)
	if at(t, body, "result", "isError") != true || !strings.Contains(text, "403") {
		t.Fatalf("result = %v", at(t, body, "result"))
	}
}

func TestDropTokenIssueSendsOnlyTokenParametersToDrop(t *testing.T) {
	var bodies []map[string]any
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		bodies = append(bodies, body)
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"token":"drk_live","expires_at":"2026-11-01T13:00:00Z","max_days":30,"created_by":"sa:a/b","base_url":"http://drop.drop.svc.cluster.local"}`))
	}))
	t.Cleanup(upstream.Close)
	tokenPath := filepath.Join(t.TempDir(), "token")
	if err := os.WriteFile(tokenPath, []byte("sa-token"), 0o600); err != nil {
		t.Fatal(err)
	}
	app := dropApp(drop.New(upstream.URL, tokenPath, upstream.Client()))

	body := callTool(t, app, 560, "drop_token_issue", map[string]any{"valid_minutes": 10})
	if at(t, body, "result", "structuredContent", "token") != "drk_live" {
		t.Fatalf("result = %v", at(t, body, "result"))
	}
	if len(bodies) != 1 || len(bodies[0]) != 1 || bodies[0]["valid_minutes"] != float64(10) {
		t.Fatalf("drop received %v, want only valid_minutes", bodies)
	}
}
