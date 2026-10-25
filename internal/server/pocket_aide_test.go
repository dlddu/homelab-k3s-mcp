package server_test

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/dlddu/homelab-k3s-mcp/internal/mcp"
	"github.com/dlddu/homelab-k3s-mcp/internal/pocketaide"
	"github.com/dlddu/homelab-k3s-mcp/internal/server"
)

type notifyCall struct{ title, body string }

type fakePocketAide struct {
	calls  []notifyCall
	result *pocketaide.Result
	err    error
}

func (f *fakePocketAide) Notify(_ context.Context, title, body string) (*pocketaide.Result, error) {
	f.calls = append(f.calls, notifyCall{title, body})
	return f.result, f.err
}

func pocketAideApp(svc pocketaide.Service) http.Handler {
	return server.App(nil, unavailableK8s(), unavailableGitHub(), unavailableAWS(), unavailableGrafana(), unavailableOpenSearch(), unavailableSessionPlatform(),
		mcp.WithPocketAide(svc))
}

func TestToolsListAdvertisesPocketAideNotificationSend(t *testing.T) {
	tool := findTool(t, toolsList(t, pocketAideApp(nil)), "pocket_aide_notification_send")

	required, _ := at(t, tool, "inputSchema", "required").([]any)
	if len(required) != 1 || required[0] != "title" {
		t.Fatalf("required = %v, want [title]", required)
	}
	props := at(t, tool, "inputSchema", "properties").(map[string]any)
	if len(props) != 2 || props["title"] == nil || props["body"] == nil {
		t.Fatalf("properties = %v, want title and body", props)
	}
	if at(t, tool, "annotations", "readOnlyHint") != false ||
		at(t, tool, "annotations", "destructiveHint") != false ||
		at(t, tool, "annotations", "idempotentHint") != false ||
		at(t, tool, "annotations", "openWorldHint") != true {
		t.Fatalf("annotations = %v", at(t, tool, "annotations"))
	}
}

func TestPocketAideNotificationSendReturnsDeliveryCounts(t *testing.T) {
	fake := &fakePocketAide{result: &pocketaide.Result{Devices: 2, Sent: 1}}
	body := callTool(t, pocketAideApp(fake), 401, "pocket_aide_notification_send",
		map[string]any{"title": "백업 완료", "body": "스냅샷 3개"})

	if at(t, body, "result", "isError") != false ||
		at(t, body, "result", "structuredContent", "devices") != float64(2) ||
		at(t, body, "result", "structuredContent", "sent") != float64(1) {
		t.Fatalf("result = %v", at(t, body, "result"))
	}
	if len(fake.calls) != 1 || fake.calls[0] != (notifyCall{"백업 완료", "스냅샷 3개"}) {
		t.Fatalf("calls = %v", fake.calls)
	}
}

func TestPocketAideNotificationSendBodyIsOptional(t *testing.T) {
	fake := &fakePocketAide{result: &pocketaide.Result{}}
	body := callTool(t, pocketAideApp(fake), 402, "pocket_aide_notification_send", map[string]any{"title": "ping"})

	if at(t, body, "result", "isError") != false {
		t.Fatalf("result = %v", at(t, body, "result"))
	}
	if len(fake.calls) != 1 || fake.calls[0] != (notifyCall{"ping", ""}) {
		t.Fatalf("calls = %v", fake.calls)
	}
}

func TestPocketAideNotificationSendRefusesBadTitlesBeforeSending(t *testing.T) {
	fake := &fakePocketAide{result: &pocketaide.Result{}}
	app := pocketAideApp(fake)
	for _, args := range []map[string]any{
		{},
		{"title": "   "},
		{"title": strings.Repeat("가", pocketaide.MaxTitleRunes+1)},
	} {
		body := callTool(t, app, 403, "pocket_aide_notification_send", args)
		text, _ := at(t, body, "result", "content", 0, "text").(string)
		if at(t, body, "result", "isError") != true || !strings.Contains(text, "invalid notification") {
			t.Fatalf("args %v: result = %v", args, at(t, body, "result"))
		}
	}
	for _, args := range []map[string]any{{"title": 7}, {"title": "x", "body": 7}} {
		body := callTool(t, app, 404, "pocket_aide_notification_send", args)
		if message, _ := at(t, body, "error", "message").(string); !strings.Contains(message, "must be a string") {
			t.Fatalf("args %v: error = %v", args, body["error"])
		}
	}
	if len(fake.calls) != 0 {
		t.Fatalf("calls = %v, want none", fake.calls)
	}
}

func TestPocketAideNotificationSendUnconfiguredRefusesEvenABadTitleTheSameWay(t *testing.T) {
	app := pocketAideApp(nil)

	body := callTool(t, app, 405, "pocket_aide_notification_send", map[string]any{"title": "x"})
	text, _ := at(t, body, "result", "content", 0, "text").(string)
	if at(t, body, "result", "isError") != true || !strings.Contains(text, "pocket-aide unavailable") || !strings.Contains(text, "APPROVAL_GATE_URL") {
		t.Fatalf("unconfigured: result = %v", at(t, body, "result"))
	}

	body = callTool(t, app, 406, "pocket_aide_notification_send", map[string]any{"title": ""})
	text, _ = at(t, body, "result", "content", 0, "text").(string)
	if !strings.Contains(text, "invalid notification") {
		t.Fatalf("bad title on an unconfigured server: text = %q, want the validation refusal", text)
	}
}

func TestPocketAideNotificationSendSurfacesPocketAideRefusal(t *testing.T) {
	fake := &fakePocketAide{err: errors.New("pocket-aide error: PocketAide cannot send push notifications right now (503)")}
	body := callTool(t, pocketAideApp(fake), 407, "pocket_aide_notification_send", map[string]any{"title": "x"})

	text, _ := at(t, body, "result", "content", 0, "text").(string)
	if at(t, body, "result", "isError") != true || !strings.Contains(text, "503") {
		t.Fatalf("result = %v", at(t, body, "result"))
	}
}
