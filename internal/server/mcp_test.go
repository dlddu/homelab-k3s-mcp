package server_test

import (
	"net/http"
	"strings"
	"testing"

	"github.com/dlddu/homelab-k3s-mcp/internal/awsconfig"
	"github.com/dlddu/homelab-k3s-mcp/internal/github"
	"github.com/dlddu/homelab-k3s-mcp/internal/grafana"
	"github.com/dlddu/homelab-k3s-mcp/internal/k8s"
	"github.com/dlddu/homelab-k3s-mcp/internal/server"
)

func rpc(t *testing.T, handler http.Handler, payload any) map[string]any {
	t.Helper()
	return bodyJSON(t, serve(handler, jsonRequest("/mcp", payload)))
}

func callTool(t *testing.T, handler http.Handler, id int, name string, args any) map[string]any {
	t.Helper()
	return rpc(t, handler, map[string]any{
		"jsonrpc": "2.0",
		"id":      id,
		"method":  "tools/call",
		"params":  map[string]any{"name": name, "arguments": args},
	})
}

func toolsList(t *testing.T, handler http.Handler) []any {
	t.Helper()
	body := rpc(t, handler, map[string]any{"jsonrpc": "2.0", "id": 1, "method": "tools/list"})
	tools, ok := at(t, body, "result", "tools").([]any)
	if !ok {
		t.Fatalf("tools is not an array")
	}
	return tools
}

func findTool(t *testing.T, tools []any, name string) map[string]any {
	t.Helper()
	for _, x := range tools {
		m := x.(map[string]any)
		if m["name"] == name {
			return m
		}
	}
	t.Fatalf("tool %s not found", name)
	return nil
}

func enumStrings(t *testing.T, v any) []string {
	t.Helper()
	arr, ok := v.([]any)
	if !ok {
		t.Fatalf("expected array, got %T", v)
	}
	out := make([]string, 0, len(arr))
	for _, x := range arr {
		out = append(out, x.(string))
	}
	return out
}

func wantStrSlice(t *testing.T, got []string, want ...string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("slice = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("slice = %v, want %v", got, want)
		}
	}
}

func TestInitializeReturnsServerInfo(t *testing.T) {
	app := server.App(nil, unavailableK8s(), unavailableGitHub(), unavailableAWS(), unavailableGrafana())
	body := rpc(t, app, map[string]any{"jsonrpc": "2.0", "id": 1, "method": "initialize"})

	if body["jsonrpc"] != "2.0" {
		t.Fatalf("jsonrpc = %v", body["jsonrpc"])
	}
	if at(t, body, "id") != float64(1) {
		t.Fatalf("id = %v", at(t, body, "id"))
	}
	if at(t, body, "result", "serverInfo", "name") != "homelab-k3s-mcp" {
		t.Fatalf("serverInfo.name = %v", at(t, body, "result", "serverInfo", "name"))
	}
	if _, ok := at(t, body, "result", "capabilities", "tools").(map[string]any); !ok {
		t.Fatalf("capabilities.tools is not an object")
	}
}

func TestToolsListIncludesAllTools(t *testing.T) {
	app := server.App(nil, unavailableK8s(), unavailableGitHub(), unavailableAWS(), unavailableGrafana())
	tools := toolsList(t, app)
	if len(tools) != 11 {
		t.Fatalf("len(tools) = %d, want 11", len(tools))
	}
	for _, name := range []string{
		"ping", "namespace_list", "workload_list", "workload_restart",
		"workload_scale", "workload_logs", "pod_describe",
		"dear_baby_reset_onboarding", "github_app_installation_token",
		"aws_config_get", "grafana_token",
	} {
		findTool(t, tools, name)
	}
}

func TestToolsListAdvertisesAnnotations(t *testing.T) {
	app := server.App(nil, unavailableK8s(), unavailableGitHub(), unavailableAWS(), unavailableGrafana())
	tools := toolsList(t, app)

	ping := findTool(t, tools, "ping")
	if at(t, ping, "annotations", "title") != "Ping" ||
		at(t, ping, "annotations", "readOnlyHint") != true ||
		at(t, ping, "annotations", "idempotentHint") != true ||
		at(t, ping, "annotations", "openWorldHint") != false {
		t.Fatalf("ping annotations = %v", ping["annotations"])
	}

	list := findTool(t, tools, "workload_list")
	if at(t, list, "annotations", "title") != "List Workloads" ||
		at(t, list, "annotations", "readOnlyHint") != true {
		t.Fatalf("workload_list annotations = %v", list["annotations"])
	}

	restart := findTool(t, tools, "workload_restart")
	if at(t, restart, "annotations", "title") != "Restart Workload" ||
		at(t, restart, "annotations", "readOnlyHint") != false ||
		at(t, restart, "annotations", "destructiveHint") != true ||
		at(t, restart, "annotations", "idempotentHint") != false {
		t.Fatalf("workload_restart annotations = %v", restart["annotations"])
	}
}

func TestPingToolReturnsPong(t *testing.T) {
	app := server.App(nil, unavailableK8s(), unavailableGitHub(), unavailableAWS(), unavailableGrafana())
	body := callTool(t, app, 3, "ping", map[string]any{})
	if at(t, body, "result", "content", 0, "text") != "pong" {
		t.Fatalf("text = %v", at(t, body, "result", "content", 0, "text"))
	}
	if at(t, body, "result", "isError") != false {
		t.Fatalf("isError = %v", at(t, body, "result", "isError"))
	}
}

func TestUnknownMethodReturnsError(t *testing.T) {
	app := server.App(nil, unavailableK8s(), unavailableGitHub(), unavailableAWS(), unavailableGrafana())
	body := rpc(t, app, map[string]any{"jsonrpc": "2.0", "id": 4, "method": "does/not/exist"})
	if at(t, body, "error", "code") != float64(-32601) {
		t.Fatalf("error.code = %v", at(t, body, "error", "code"))
	}
}

func TestUnknownToolReturnsError(t *testing.T) {
	app := server.App(nil, unavailableK8s(), unavailableGitHub(), unavailableAWS(), unavailableGrafana())
	body := rpc(t, app, map[string]any{
		"jsonrpc": "2.0", "id": 5, "method": "tools/call",
		"params": map[string]any{"name": "nonexistent"},
	})
	if at(t, body, "error", "code") != float64(-32602) {
		t.Fatalf("error.code = %v", at(t, body, "error", "code"))
	}
}

func TestWorkloadListDispatchesToService(t *testing.T) {
	fake := &fakeK8s{items: []any{map[string]any{"name": "api", "namespace": "default", "replicas": 3}}}
	app := server.App(nil, fake, unavailableGitHub(), unavailableAWS(), unavailableGrafana())

	body := callTool(t, app, 10, "workload_list", map[string]any{"kind": "Deployment", "namespace": "default"})

	if at(t, body, "result", "isError") != false {
		t.Fatalf("isError = %v", at(t, body, "result", "isError"))
	}
	if at(t, body, "result", "structuredContent", "kind") != "Deployment" {
		t.Fatalf("kind = %v", at(t, body, "result", "structuredContent", "kind"))
	}
	if at(t, body, "result", "structuredContent", "namespace") != "default" {
		t.Fatalf("namespace = %v", at(t, body, "result", "structuredContent", "namespace"))
	}
	if at(t, body, "result", "structuredContent", "items", 0, "name") != "api" {
		t.Fatalf("items[0].name = %v", at(t, body, "result", "structuredContent", "items", 0, "name"))
	}
	if fake.lastList == nil || fake.lastList.kind != k8s.Deployment || fake.lastList.namespace == nil || *fake.lastList.namespace != "default" {
		t.Fatalf("lastList = %+v", fake.lastList)
	}
}

func TestWorkloadListWithoutNamespaceListsAll(t *testing.T) {
	fake := &fakeK8s{}
	app := server.App(nil, fake, unavailableGitHub(), unavailableAWS(), unavailableGrafana())

	body := callTool(t, app, 11, "workload_list", map[string]any{"kind": "StatefulSet"})
	if at(t, body, "result", "isError") != false {
		t.Fatalf("isError = %v", at(t, body, "result", "isError"))
	}
	if at(t, body, "result", "structuredContent", "namespace") != nil {
		t.Fatalf("namespace should be null, got %v", at(t, body, "result", "structuredContent", "namespace"))
	}
	if fake.lastList == nil || fake.lastList.kind != k8s.StatefulSet || fake.lastList.namespace != nil {
		t.Fatalf("lastList = %+v", fake.lastList)
	}
}

func TestToolsListAdvertisesNamespaceList(t *testing.T) {
	app := server.App(nil, unavailableK8s(), unavailableGitHub(), unavailableAWS(), unavailableGrafana())
	tools := toolsList(t, app)
	ns := findTool(t, tools, "namespace_list")
	if at(t, ns, "annotations", "title") != "List Namespaces" {
		t.Fatalf("title = %v", at(t, ns, "annotations", "title"))
	}
	props := at(t, ns, "inputSchema", "properties").(map[string]any)
	if len(props) != 0 {
		t.Fatalf("properties should be empty, got %v", props)
	}
}

func TestNamespaceListDispatchesToService(t *testing.T) {
	fake := &fakeK8s{namespaces: []any{
		map[string]any{"name": "default", "phase": "Active"},
		map[string]any{"name": "kube-system", "phase": "Active"},
	}}
	app := server.App(nil, fake, unavailableGitHub(), unavailableAWS(), unavailableGrafana())

	body := callTool(t, app, 13, "namespace_list", map[string]any{})
	if at(t, body, "result", "isError") != false {
		t.Fatalf("isError = %v", at(t, body, "result", "isError"))
	}
	items := at(t, body, "result", "structuredContent", "items").([]any)
	if len(items) != 2 {
		t.Fatalf("len(items) = %d, want 2", len(items))
	}
	if at(t, body, "result", "structuredContent", "items", 0, "name") != "default" {
		t.Fatalf("items[0].name = %v", at(t, body, "result", "structuredContent", "items", 0, "name"))
	}
	if fake.namespaceCalls != 1 {
		t.Fatalf("namespaceCalls = %d, want 1", fake.namespaceCalls)
	}
}

func TestNamespaceListUnavailableIsToolError(t *testing.T) {
	app := server.App(nil, unavailableK8s(), unavailableGitHub(), unavailableAWS(), unavailableGrafana())
	body := callTool(t, app, 14, "namespace_list", map[string]any{})
	if at(t, body, "result", "isError") != true {
		t.Fatalf("isError = %v", at(t, body, "result", "isError"))
	}
	text, _ := at(t, body, "result", "content", 0, "text").(string)
	if !strings.Contains(text, "kubernetes") {
		t.Fatalf("text = %q", text)
	}
}

func TestWorkloadRestartDispatchesToService(t *testing.T) {
	fake := &fakeK8s{}
	app := server.App(nil, fake, unavailableGitHub(), unavailableAWS(), unavailableGrafana())

	body := callTool(t, app, 20, "workload_restart", map[string]any{
		"kind": "DaemonSet", "namespace": "kube-system", "name": "kindnet",
	})
	if at(t, body, "result", "isError") != false {
		t.Fatalf("isError = %v", at(t, body, "result", "isError"))
	}
	if at(t, body, "result", "structuredContent", "kind") != "DaemonSet" {
		t.Fatalf("kind = %v", at(t, body, "result", "structuredContent", "kind"))
	}
	if _, ok := at(t, body, "result", "structuredContent", "restartedAt").(string); !ok {
		t.Fatalf("restartedAt should be a string")
	}
	if len(fake.restarts) != 1 || fake.restarts[0].kind != k8s.DaemonSet ||
		fake.restarts[0].namespace != "kube-system" || fake.restarts[0].name != "kindnet" {
		t.Fatalf("restarts = %+v", fake.restarts)
	}
}

func TestWorkloadRestartRequiresNamespaceAndName(t *testing.T) {
	app := server.App(nil, unavailableK8s(), unavailableGitHub(), unavailableAWS(), unavailableGrafana())
	body := callTool(t, app, 30, "workload_restart", map[string]any{"kind": "Deployment", "namespace": "default"})
	if at(t, body, "error", "code") != float64(-32602) {
		t.Fatalf("error.code = %v", at(t, body, "error", "code"))
	}
}

func TestWorkloadScaleDispatchesToService(t *testing.T) {
	fake := &fakeK8s{}
	app := server.App(nil, fake, unavailableGitHub(), unavailableAWS(), unavailableGrafana())

	body := callTool(t, app, 70, "workload_scale", map[string]any{
		"kind": "Deployment", "namespace": "default", "name": "api", "replicas": 3,
	})
	if at(t, body, "result", "isError") != false {
		t.Fatalf("isError = %v", at(t, body, "result", "isError"))
	}
	if at(t, body, "result", "structuredContent", "replicas") != float64(3) {
		t.Fatalf("replicas = %v", at(t, body, "result", "structuredContent", "replicas"))
	}
	if len(fake.scales) != 1 || fake.scales[0].kind != k8s.Deployment ||
		fake.scales[0].namespace != "default" || fake.scales[0].name != "api" || fake.scales[0].replicas != 3 {
		t.Fatalf("scales = %+v", fake.scales)
	}
}

func TestWorkloadScaleSupportsZeroReplicas(t *testing.T) {
	fake := &fakeK8s{}
	app := server.App(nil, fake, unavailableGitHub(), unavailableAWS(), unavailableGrafana())

	body := callTool(t, app, 71, "workload_scale", map[string]any{
		"kind": "StatefulSet", "namespace": "data", "name": "redis", "replicas": 0,
	})
	if at(t, body, "result", "isError") != false {
		t.Fatalf("isError = %v", at(t, body, "result", "isError"))
	}
	if at(t, body, "result", "structuredContent", "replicas") != float64(0) {
		t.Fatalf("replicas = %v", at(t, body, "result", "structuredContent", "replicas"))
	}
	if fake.scales[0].kind != k8s.StatefulSet || fake.scales[0].replicas != 0 {
		t.Fatalf("scales[0] = %+v", fake.scales[0])
	}
}

func TestWorkloadScaleRejectsNegativeReplicas(t *testing.T) {
	app := server.App(nil, unavailableK8s(), unavailableGitHub(), unavailableAWS(), unavailableGrafana())
	body := callTool(t, app, 72, "workload_scale", map[string]any{
		"kind": "Deployment", "namespace": "default", "name": "api", "replicas": -1,
	})
	if at(t, body, "error", "code") != float64(-32602) {
		t.Fatalf("error.code = %v", at(t, body, "error", "code"))
	}
}

func TestWorkloadScaleRequiresReplicas(t *testing.T) {
	app := server.App(nil, unavailableK8s(), unavailableGitHub(), unavailableAWS(), unavailableGrafana())
	body := callTool(t, app, 73, "workload_scale", map[string]any{
		"kind": "Deployment", "namespace": "default", "name": "api",
	})
	if at(t, body, "error", "code") != float64(-32602) {
		t.Fatalf("error.code = %v", at(t, body, "error", "code"))
	}
}

func TestToolsListAdvertisesWorkloadScale(t *testing.T) {
	app := server.App(nil, unavailableK8s(), unavailableGitHub(), unavailableAWS(), unavailableGrafana())
	tools := toolsList(t, app)
	scale := findTool(t, tools, "workload_scale")
	if at(t, scale, "annotations", "title") != "Scale Workload" ||
		at(t, scale, "annotations", "destructiveHint") != true ||
		at(t, scale, "annotations", "idempotentHint") != true {
		t.Fatalf("scale annotations = %v", scale["annotations"])
	}
	kinds := enumStrings(t, at(t, scale, "inputSchema", "properties", "kind", "enum"))
	wantStrSlice(t, kinds, "Deployment", "StatefulSet")
}

func TestWorkloadRejectsUnknownKind(t *testing.T) {
	app := server.App(nil, unavailableK8s(), unavailableGitHub(), unavailableAWS(), unavailableGrafana())
	body := callTool(t, app, 31, "workload_list", map[string]any{"kind": "Pod"})
	if at(t, body, "error", "code") != float64(-32602) {
		t.Fatalf("error.code = %v", at(t, body, "error", "code"))
	}
}

func TestUnavailableK8sReturnsToolError(t *testing.T) {
	app := server.App(nil, unavailableK8s(), unavailableGitHub(), unavailableAWS(), unavailableGrafana())
	body := callTool(t, app, 40, "workload_list", map[string]any{"kind": "Deployment"})
	if at(t, body, "result", "isError") != true {
		t.Fatalf("isError = %v", at(t, body, "result", "isError"))
	}
	text, _ := at(t, body, "result", "content", 0, "text").(string)
	if !strings.Contains(text, "kubernetes") {
		t.Fatalf("text = %q", text)
	}
}

func TestToolsListAdvertisesDearBabyReset(t *testing.T) {
	app := server.App(nil, unavailableK8s(), unavailableGitHub(), unavailableAWS(), unavailableGrafana())
	tools := toolsList(t, app)
	reset := findTool(t, tools, "dear_baby_reset_onboarding")
	required := enumStrings(t, at(t, reset, "inputSchema", "required"))
	if !contains(required, "namespace") || !contains(required, "email") {
		t.Fatalf("required = %v", required)
	}
	if at(t, reset, "annotations", "title") != "Reset dear-baby Onboarding" ||
		at(t, reset, "annotations", "destructiveHint") != true ||
		at(t, reset, "annotations", "idempotentHint") != true {
		t.Fatalf("annotations = %v", reset["annotations"])
	}
}

func TestDearBabyResetDispatchesWithDefaults(t *testing.T) {
	fake := &fakeK8s{execResponse: func() (*k8s.ExecOutcome, error) {
		code := int32(0)
		return &k8s.ExecOutcome{
			Pod:      "dear-baby-7d9c9f6b8b-xyz",
			Stdout:   "reset onboarding for user@example.com\n",
			ExitCode: &code,
			Success:  true,
		}, nil
	}}
	app := server.App(nil, fake, unavailableGitHub(), unavailableAWS(), unavailableGrafana())

	body := callTool(t, app, 60, "dear_baby_reset_onboarding", map[string]any{
		"namespace": "dear-baby", "email": "user@example.com",
	})
	if at(t, body, "result", "isError") != false {
		t.Fatalf("isError = %v", at(t, body, "result", "isError"))
	}
	sc := at(t, body, "result", "structuredContent").(map[string]any)
	if sc["selector"] != "app=dear-baby" || sc["container"] != "backend" ||
		sc["pod"] != "dear-baby-7d9c9f6b8b-xyz" || sc["exitCode"] != float64(0) || sc["success"] != true {
		t.Fatalf("structuredContent = %v", sc)
	}
	if !strings.Contains(sc["stdout"].(string), "reset onboarding") {
		t.Fatalf("stdout = %v", sc["stdout"])
	}
	if len(fake.execCalls) != 1 {
		t.Fatalf("execCalls = %+v", fake.execCalls)
	}
	c := fake.execCalls[0]
	if c.namespace != "dear-baby" || c.selector != "app=dear-baby" || c.container == nil || *c.container != "backend" {
		t.Fatalf("execCall = %+v", c)
	}
	if len(c.command) != 2 || c.command[0] != "/reset-onboarding" || c.command[1] != "user@example.com" {
		t.Fatalf("command = %v", c.command)
	}
}

func TestDearBabyResetHonoursOverrides(t *testing.T) {
	fake := &fakeK8s{}
	app := server.App(nil, fake, unavailableGitHub(), unavailableAWS(), unavailableGrafana())

	body := callTool(t, app, 61, "dear_baby_reset_onboarding", map[string]any{
		"namespace": "staging", "email": "qa@example.com",
		"selector": "app=dear-baby,track=canary", "container": "api",
	})
	if at(t, body, "result", "isError") != false {
		t.Fatalf("isError = %v", at(t, body, "result", "isError"))
	}
	c := fake.execCalls[0]
	if c.namespace != "staging" || c.selector != "app=dear-baby,track=canary" || *c.container != "api" {
		t.Fatalf("execCall = %+v", c)
	}
}

func TestDearBabyResetReportsNonZeroExit(t *testing.T) {
	fake := &fakeK8s{execResponse: func() (*k8s.ExecOutcome, error) {
		code := int32(1)
		return &k8s.ExecOutcome{
			Pod:      "dear-baby-1",
			Stderr:   "no user found with email \"missing@example.com\"\n",
			ExitCode: &code,
			Success:  false,
		}, nil
	}}
	app := server.App(nil, fake, unavailableGitHub(), unavailableAWS(), unavailableGrafana())

	body := callTool(t, app, 62, "dear_baby_reset_onboarding", map[string]any{
		"namespace": "dear-baby", "email": "missing@example.com",
	})
	if at(t, body, "result", "isError") != true {
		t.Fatalf("isError = %v", at(t, body, "result", "isError"))
	}
	sc := at(t, body, "result", "structuredContent").(map[string]any)
	if sc["success"] != false || sc["exitCode"] != float64(1) {
		t.Fatalf("structuredContent = %v", sc)
	}
	if !strings.Contains(sc["stderr"].(string), "no user found") {
		t.Fatalf("stderr = %v", sc["stderr"])
	}
}

func TestDearBabyResetRequiresNamespaceAndEmail(t *testing.T) {
	app := server.App(nil, unavailableK8s(), unavailableGitHub(), unavailableAWS(), unavailableGrafana())
	body := callTool(t, app, 63, "dear_baby_reset_onboarding", map[string]any{"email": "user@example.com"})
	if at(t, body, "error", "code") != float64(-32602) {
		t.Fatalf("error.code = %v", at(t, body, "error", "code"))
	}
}

func TestToolsListAdvertisesWorkloadLogs(t *testing.T) {
	app := server.App(nil, unavailableK8s(), unavailableGitHub(), unavailableAWS(), unavailableGrafana())
	tools := toolsList(t, app)
	logs := findTool(t, tools, "workload_logs")
	if at(t, logs, "annotations", "title") != "View Workload Logs" {
		t.Fatalf("title = %v", at(t, logs, "annotations", "title"))
	}
	required := enumStrings(t, at(t, logs, "inputSchema", "required"))
	wantStrSlice(t, required, "kind", "namespace", "name")
	kinds := enumStrings(t, at(t, logs, "inputSchema", "properties", "kind", "enum"))
	wantStrSlice(t, kinds, "Deployment", "StatefulSet", "DaemonSet")
	if at(t, logs, "inputSchema", "properties", "tail_lines", "maximum") != float64(5000) {
		t.Fatalf("tail_lines.maximum = %v", at(t, logs, "inputSchema", "properties", "tail_lines", "maximum"))
	}
}

func TestWorkloadLogsDispatchesWithDefaults(t *testing.T) {
	fake := &fakeK8s{logResponse: func() (*k8s.LogResult, error) {
		return &k8s.LogResult{Pod: "api-7d9c9f6b8b-xyz", Logs: "line one\nline two\n"}, nil
	}}
	app := server.App(nil, fake, unavailableGitHub(), unavailableAWS(), unavailableGrafana())

	body := callTool(t, app, 81, "workload_logs", map[string]any{
		"kind": "Deployment", "namespace": "default", "name": "api",
	})
	if at(t, body, "result", "isError") != false {
		t.Fatalf("isError = %v", at(t, body, "result", "isError"))
	}
	sc := at(t, body, "result", "structuredContent").(map[string]any)
	if sc["pod"] != "api-7d9c9f6b8b-xyz" || sc["tailLines"] != float64(200) ||
		sc["previous"] != false || sc["timestamps"] != false || sc["sinceSeconds"] != nil ||
		sc["logs"] != "line one\nline two\n" {
		t.Fatalf("structuredContent = %v", sc)
	}
	if at(t, body, "result", "content", 0, "text") != "line one\nline two\n" {
		t.Fatalf("text = %v", at(t, body, "result", "content", 0, "text"))
	}
	c := fake.logCalls[0]
	if c.kind != k8s.Deployment || c.namespace != "default" || c.name != "api" {
		t.Fatalf("logCall = %+v", c)
	}
	if c.options.TailLines == nil || *c.options.TailLines != 200 || c.options.Container != nil ||
		c.options.Previous || c.options.Timestamps || c.options.SinceSeconds != nil {
		t.Fatalf("options = %+v", c.options)
	}
}

func TestWorkloadLogsHonoursOverrides(t *testing.T) {
	fake := &fakeK8s{}
	app := server.App(nil, fake, unavailableGitHub(), unavailableAWS(), unavailableGrafana())

	body := callTool(t, app, 82, "workload_logs", map[string]any{
		"kind": "StatefulSet", "namespace": "data", "name": "redis",
		"container": "redis", "tail_lines": 500, "previous": true,
		"timestamps": true, "since_seconds": 3600,
	})
	if at(t, body, "result", "isError") != false {
		t.Fatalf("isError = %v", at(t, body, "result", "isError"))
	}
	c := fake.logCalls[0]
	if c.kind != k8s.StatefulSet || c.namespace != "data" || c.name != "redis" {
		t.Fatalf("logCall = %+v", c)
	}
	if c.options.Container == nil || *c.options.Container != "redis" ||
		c.options.TailLines == nil || *c.options.TailLines != 500 ||
		!c.options.Previous || !c.options.Timestamps ||
		c.options.SinceSeconds == nil || *c.options.SinceSeconds != 3600 {
		t.Fatalf("options = %+v", c.options)
	}
}

func TestWorkloadLogsRejectsTailLinesOverMax(t *testing.T) {
	app := server.App(nil, unavailableK8s(), unavailableGitHub(), unavailableAWS(), unavailableGrafana())
	body := callTool(t, app, 83, "workload_logs", map[string]any{
		"kind": "Deployment", "namespace": "default", "name": "api", "tail_lines": 100000,
	})
	if at(t, body, "error", "code") != float64(-32602) {
		t.Fatalf("error.code = %v", at(t, body, "error", "code"))
	}
	msg, _ := at(t, body, "error", "message").(string)
	if !strings.Contains(msg, "tail_lines") {
		t.Fatalf("message = %q", msg)
	}
}

func TestWorkloadLogsRequiresNamespaceAndName(t *testing.T) {
	app := server.App(nil, unavailableK8s(), unavailableGitHub(), unavailableAWS(), unavailableGrafana())
	body := callTool(t, app, 84, "workload_logs", map[string]any{"kind": "Deployment"})
	if at(t, body, "error", "code") != float64(-32602) {
		t.Fatalf("error.code = %v", at(t, body, "error", "code"))
	}
}

func TestWorkloadLogsEmptyOutputPlaceholder(t *testing.T) {
	fake := &fakeK8s{logResponse: func() (*k8s.LogResult, error) {
		return &k8s.LogResult{Pod: "api-1", Logs: ""}, nil
	}}
	app := server.App(nil, fake, unavailableGitHub(), unavailableAWS(), unavailableGrafana())

	body := callTool(t, app, 85, "workload_logs", map[string]any{
		"kind": "Deployment", "namespace": "default", "name": "api",
	})
	if at(t, body, "result", "isError") != false {
		t.Fatalf("isError = %v", at(t, body, "result", "isError"))
	}
	if at(t, body, "result", "content", 0, "text") != "(no log output)" {
		t.Fatalf("text = %v", at(t, body, "result", "content", 0, "text"))
	}
	if at(t, body, "result", "structuredContent", "logs") != "" {
		t.Fatalf("logs = %v", at(t, body, "result", "structuredContent", "logs"))
	}
}

func TestToolsListAdvertisesPodDescribe(t *testing.T) {
	app := server.App(nil, unavailableK8s(), unavailableGitHub(), unavailableAWS(), unavailableGrafana())
	tools := toolsList(t, app)
	describe := findTool(t, tools, "pod_describe")
	if at(t, describe, "annotations", "title") != "Describe Pod" {
		t.Fatalf("title = %v", at(t, describe, "annotations", "title"))
	}
	required := enumStrings(t, at(t, describe, "inputSchema", "required"))
	wantStrSlice(t, required, "namespace")
	props := at(t, describe, "inputSchema", "properties").(map[string]any)
	for _, key := range []string{"name", "selector", "workload_kind", "workload_name"} {
		if _, ok := props[key]; !ok {
			t.Fatalf("missing property %s", key)
		}
	}
	kinds := enumStrings(t, at(t, describe, "inputSchema", "properties", "workload_kind", "enum"))
	wantStrSlice(t, kinds, "Deployment", "StatefulSet", "DaemonSet")
}

func TestPodDescribeRendersStructuredPayload(t *testing.T) {
	fake := &fakeK8s{describeResponse: func() (*k8s.PodDescription, error) {
		return &k8s.PodDescription{
			Name:            "api-7d9c9f6b8b-xyz",
			Namespace:       "default",
			Node:            strptr("k3s-node-1"),
			Phase:           strptr("Running"),
			PodIP:           strptr("10.0.0.42"),
			HostIP:          strptr("192.168.1.10"),
			ServiceAccount:  strptr("default"),
			Priority:        int32Ptr(0),
			QOSClass:        strptr("BestEffort"),
			StartTime:       strptr("2026-05-10T12:00:00Z"),
			Labels:          map[string]string{"app": "api"},
			Annotations:     map[string]string{},
			NodeSelector:    map[string]string{},
			OwnerReferences: []any{},
			Conditions:      []k8s.PodConditionInfo{{Type: "Ready", Status: "True"}},
			InitContainers:  []k8s.ContainerInfo{},
			Containers: []k8s.ContainerInfo{{
				Name:         "api",
				Image:        "ghcr.io/example/api:1.2.3",
				Ready:        true,
				Started:      boolPtr(true),
				RestartCount: 2,
				State:        strptr("running"),
				StartedAt:    strptr("2026-05-10T12:00:01Z"),
				LastState:    strptr("terminated"),
				LastReason:   strptr("Error"),
				LastExitCode: int32Ptr(137),
			}},
			Events: []k8s.PodEventInfo{{
				Type:           "Warning",
				Reason:         "BackOff",
				Message:        "Back-off restarting failed container",
				Count:          5,
				FirstTimestamp: strptr("2026-05-10T11:00:00Z"),
				LastTimestamp:  strptr("2026-05-10T11:55:00Z"),
				Source:         strptr("kubelet"),
			}},
		}, nil
	}}
	app := server.App(nil, fake, unavailableGitHub(), unavailableAWS(), unavailableGrafana())

	body := callTool(t, app, 91, "pod_describe", map[string]any{
		"namespace": "default", "name": "api-7d9c9f6b8b-xyz",
	})
	if at(t, body, "result", "isError") != false {
		t.Fatalf("isError = %v", at(t, body, "result", "isError"))
	}
	sc := at(t, body, "result", "structuredContent").(map[string]any)
	if sc["name"] != "api-7d9c9f6b8b-xyz" || sc["node"] != "k3s-node-1" ||
		sc["phase"] != "Running" || sc["pod_ip"] != "10.0.0.42" {
		t.Fatalf("structuredContent = %v", sc)
	}
	if at(t, sc, "containers", 0, "image") != "ghcr.io/example/api:1.2.3" ||
		at(t, sc, "containers", 0, "state") != "running" ||
		at(t, sc, "containers", 0, "restart_count") != float64(2) ||
		at(t, sc, "containers", 0, "last_state") != "terminated" ||
		at(t, sc, "containers", 0, "last_exit_code") != float64(137) {
		t.Fatalf("containers = %v", sc["containers"])
	}
	if at(t, sc, "conditions", 0, "type") != "Ready" || at(t, sc, "events", 0, "type") != "Warning" {
		t.Fatalf("conditions/events = %v / %v", sc["conditions"], sc["events"])
	}

	text := at(t, body, "result", "content", 0, "text").(string)
	for _, want := range []string{
		"Name:         api-7d9c9f6b8b-xyz", "Namespace:    default",
		"Node:         k3s-node-1", "ghcr.io/example/api:1.2.3", "BackOff",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("text missing %q:\n%s", want, text)
		}
	}

	if len(fake.describeCalls) != 1 || fake.describeCalls[0].namespace != "default" ||
		fake.describeCalls[0].target != (k8s.PodTarget{Mode: k8s.TargetName, Name: "api-7d9c9f6b8b-xyz"}) {
		t.Fatalf("describeCalls = %+v", fake.describeCalls)
	}
}

func TestPodDescribeAcceptsSelectorTarget(t *testing.T) {
	fake := &fakeK8s{}
	app := server.App(nil, fake, unavailableGitHub(), unavailableAWS(), unavailableGrafana())
	body := callTool(t, app, 95, "pod_describe", map[string]any{"namespace": "default", "selector": "app=api"})
	if at(t, body, "result", "isError") != false {
		t.Fatalf("isError = %v", at(t, body, "result", "isError"))
	}
	if fake.describeCalls[0].target != (k8s.PodTarget{Mode: k8s.TargetSelector, Selector: "app=api"}) {
		t.Fatalf("target = %+v", fake.describeCalls[0].target)
	}
}

func TestPodDescribeAcceptsWorkloadTarget(t *testing.T) {
	fake := &fakeK8s{}
	app := server.App(nil, fake, unavailableGitHub(), unavailableAWS(), unavailableGrafana())
	body := callTool(t, app, 96, "pod_describe", map[string]any{
		"namespace": "default", "workload_kind": "Deployment", "workload_name": "api",
	})
	if at(t, body, "result", "isError") != false {
		t.Fatalf("isError = %v", at(t, body, "result", "isError"))
	}
	want := k8s.PodTarget{Mode: k8s.TargetWorkload, Kind: k8s.Deployment, WorkloadName: "api"}
	if fake.describeCalls[0].target != want {
		t.Fatalf("target = %+v", fake.describeCalls[0].target)
	}
}

func TestPodDescribeRejectsMutuallyExclusiveTargets(t *testing.T) {
	app := server.App(nil, unavailableK8s(), unavailableGitHub(), unavailableAWS(), unavailableGrafana())
	body := callTool(t, app, 97, "pod_describe", map[string]any{
		"namespace": "default", "name": "api-0", "selector": "app=api",
	})
	if at(t, body, "error", "code") != float64(-32602) {
		t.Fatalf("error.code = %v", at(t, body, "error", "code"))
	}
	msg, _ := at(t, body, "error", "message").(string)
	if !strings.Contains(msg, "mutually exclusive") {
		t.Fatalf("message = %q", msg)
	}
}

func TestPodDescribeRejectsPartialWorkloadTarget(t *testing.T) {
	app := server.App(nil, unavailableK8s(), unavailableGitHub(), unavailableAWS(), unavailableGrafana())
	body := callTool(t, app, 98, "pod_describe", map[string]any{
		"namespace": "default", "workload_kind": "Deployment",
	})
	if at(t, body, "error", "code") != float64(-32602) {
		t.Fatalf("error.code = %v", at(t, body, "error", "code"))
	}
}

func TestPodDescribeNoEventsPlaceholder(t *testing.T) {
	fake := &fakeK8s{}
	app := server.App(nil, fake, unavailableGitHub(), unavailableAWS(), unavailableGrafana())
	body := callTool(t, app, 92, "pod_describe", map[string]any{"namespace": "default", "name": "api-0"})
	if at(t, body, "result", "isError") != false {
		t.Fatalf("isError = %v", at(t, body, "result", "isError"))
	}
	text := at(t, body, "result", "content", 0, "text").(string)
	if !strings.Contains(text, "Events:       <none>") {
		t.Fatalf("text = %q", text)
	}
}

func TestPodDescribeRequiresTarget(t *testing.T) {
	app := server.App(nil, unavailableK8s(), unavailableGitHub(), unavailableAWS(), unavailableGrafana())
	body := callTool(t, app, 93, "pod_describe", map[string]any{"namespace": "default"})
	if at(t, body, "error", "code") != float64(-32602) {
		t.Fatalf("error.code = %v", at(t, body, "error", "code"))
	}
	msg, _ := at(t, body, "error", "message").(string)
	if !strings.Contains(msg, "name") || !strings.Contains(msg, "selector") {
		t.Fatalf("message = %q", msg)
	}
}

func TestPodDescribeSurfacesK8sErrorAsToolError(t *testing.T) {
	fake := &fakeK8s{describeResponse: func() (*k8s.PodDescription, error) {
		return nil, k8s.APIError("pods \"missing\" not found")
	}}
	app := server.App(nil, fake, unavailableGitHub(), unavailableAWS(), unavailableGrafana())
	body := callTool(t, app, 94, "pod_describe", map[string]any{"namespace": "default", "name": "missing"})
	if at(t, body, "result", "isError") != true {
		t.Fatalf("isError = %v", at(t, body, "result", "isError"))
	}
	text, _ := at(t, body, "result", "content", 0, "text").(string)
	if !strings.Contains(text, "not found") {
		t.Fatalf("text = %q", text)
	}
}

func TestToolsListAdvertisesGitHubToken(t *testing.T) {
	app := server.App(nil, unavailableK8s(), unavailableGitHub(), unavailableAWS(), unavailableGrafana())
	tools := toolsList(t, app)
	token := findTool(t, tools, "github_app_installation_token")
	props := at(t, token, "inputSchema", "properties").(map[string]any)
	if _, ok := props["installation_id"]; ok {
		t.Fatalf("should not expose installation_id")
	}
	if _, ok := props["repositories"]; !ok {
		t.Fatalf("missing repositories")
	}
	if _, ok := props["permissions"]; !ok {
		t.Fatalf("missing permissions")
	}
	if at(t, token, "annotations", "title") != "GitHub App Installation Token" ||
		at(t, token, "annotations", "openWorldHint") != true {
		t.Fatalf("annotations = %v", token["annotations"])
	}
}

func TestGitHubTokenDispatchesWithDefaults(t *testing.T) {
	fake := &fakeGitHub{response: func() (*github.InstallationToken, error) {
		return &github.InstallationToken{
			Token:               "ghs_short_lived",
			ExpiresAt:           "2026-05-07T01:00:00Z",
			Permissions:         map[string]any{"contents": "read", "metadata": "read"},
			RepositorySelection: "all",
		}, nil
	}}
	app := server.App(nil, unavailableK8s(), fake, unavailableAWS(), unavailableGrafana())

	body := callTool(t, app, 71, "github_app_installation_token", map[string]any{})
	if at(t, body, "result", "isError") != false {
		t.Fatalf("isError = %v", at(t, body, "result", "isError"))
	}
	if at(t, body, "result", "structuredContent") != nil {
		t.Fatalf("structuredContent should be null")
	}
	resource := at(t, body, "result", "content", 0).(map[string]any)
	if resource["type"] != "resource" {
		t.Fatalf("type = %v", resource["type"])
	}
	if at(t, resource, "resource", "mimeType") != "text/plain" {
		t.Fatalf("mimeType = %v", at(t, resource, "resource", "mimeType"))
	}
	uri := at(t, resource, "resource", "uri").(string)
	if !strings.HasSuffix(uri, ".env") {
		t.Fatalf("uri = %q", uri)
	}
	text := at(t, resource, "resource", "text").(string)
	for _, want := range []string{
		"GITHUB_TOKEN=ghs_short_lived", "# Expires at: 2026-05-07T01:00:00Z",
		"# Repository selection: all", "contents=read", "metadata=read",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("text missing %q:\n%s", want, text)
		}
	}
	if len(fake.calls) != 1 || fake.calls[0].repositories != nil || fake.calls[0].permissions != nil {
		t.Fatalf("calls = %+v", fake.calls)
	}
}

func TestGitHubTokenPassesThroughScope(t *testing.T) {
	fake := &fakeGitHub{}
	app := server.App(nil, unavailableK8s(), fake, unavailableAWS(), unavailableGrafana())

	body := callTool(t, app, 72, "github_app_installation_token", map[string]any{
		"repositories": []any{"homelab-k3s-mcp", "infra"},
		"permissions":  map[string]any{"contents": "read", "pull_requests": "write"},
	})
	if at(t, body, "result", "isError") != false {
		t.Fatalf("isError = %v", at(t, body, "result", "isError"))
	}
	c := fake.calls[0]
	if len(c.repositories) != 2 || c.repositories[0] != "homelab-k3s-mcp" || c.repositories[1] != "infra" {
		t.Fatalf("repositories = %v", c.repositories)
	}
	if c.permissions["contents"] != "read" || c.permissions["pull_requests"] != "write" {
		t.Fatalf("permissions = %v", c.permissions)
	}
}

func TestGitHubTokenWithoutArgumentsField(t *testing.T) {
	fake := &fakeGitHub{}
	app := server.App(nil, unavailableK8s(), fake, unavailableAWS(), unavailableGrafana())
	body := rpc(t, app, map[string]any{
		"jsonrpc": "2.0", "id": 73, "method": "tools/call",
		"params": map[string]any{"name": "github_app_installation_token"},
	})
	if at(t, body, "result", "isError") != false {
		t.Fatalf("isError = %v", at(t, body, "result", "isError"))
	}
	if len(fake.calls) != 1 {
		t.Fatalf("calls = %d", len(fake.calls))
	}
}

func TestGitHubTokenUnavailableReturnsToolError(t *testing.T) {
	app := server.App(nil, unavailableK8s(), unavailableGitHub(), unavailableAWS(), unavailableGrafana())
	body := callTool(t, app, 74, "github_app_installation_token", map[string]any{})
	if at(t, body, "result", "isError") != true {
		t.Fatalf("isError = %v", at(t, body, "result", "isError"))
	}
	text, _ := at(t, body, "result", "content", 0, "text").(string)
	if !strings.Contains(text, "github app") {
		t.Fatalf("text = %q", text)
	}
}

func TestGitHubTokenRejectsNonArrayRepositories(t *testing.T) {
	app := server.App(nil, unavailableK8s(), unavailableGitHub(), unavailableAWS(), unavailableGrafana())
	body := callTool(t, app, 75, "github_app_installation_token", map[string]any{"repositories": "not-a-list"})
	if at(t, body, "error", "code") != float64(-32602) {
		t.Fatalf("error.code = %v", at(t, body, "error", "code"))
	}
}

func TestToolsListAdvertisesAWSConfig(t *testing.T) {
	app := server.App(nil, unavailableK8s(), unavailableGitHub(), unavailableAWS(), unavailableGrafana())
	tools := toolsList(t, app)
	cfg := findTool(t, tools, "aws_config_get")
	props := at(t, cfg, "inputSchema", "properties").(map[string]any)
	if len(props) != 0 {
		t.Fatalf("properties should be empty, got %v", props)
	}
	if at(t, cfg, "annotations", "title") != "Get AWS Config File" ||
		at(t, cfg, "annotations", "readOnlyHint") != true ||
		at(t, cfg, "annotations", "idempotentHint") != true ||
		at(t, cfg, "annotations", "openWorldHint") != true {
		t.Fatalf("aws_config_get annotations = %v", cfg["annotations"])
	}
}

func TestAWSConfigGetDispatchesToService(t *testing.T) {
	fake := &fakeAWS{response: func() (*awsconfig.Object, error) {
		return &awsconfig.Object{
			Bucket:       "homelab-config",
			Key:          "aws/config",
			Content:      "[default]\nregion = ap-northeast-2\n",
			ContentType:  "text/plain",
			ETag:         "abc123",
			LastModified: "2026-05-10T12:00:00Z",
			Size:         32,
		}, nil
	}}
	app := server.App(nil, unavailableK8s(), unavailableGitHub(), fake, unavailableGrafana())

	body := callTool(t, app, 100, "aws_config_get", map[string]any{})
	if at(t, body, "result", "isError") != false {
		t.Fatalf("isError = %v", at(t, body, "result", "isError"))
	}
	sc := at(t, body, "result", "structuredContent").(map[string]any)
	if sc["bucket"] != "homelab-config" || sc["key"] != "aws/config" ||
		sc["contentType"] != "text/plain" || sc["etag"] != "abc123" ||
		sc["lastModified"] != "2026-05-10T12:00:00Z" || sc["size"] != float64(32) {
		t.Fatalf("structuredContent = %v", sc)
	}
	if at(t, body, "result", "content", 0, "text") != "[default]\nregion = ap-northeast-2\n" {
		t.Fatalf("text = %v", at(t, body, "result", "content", 0, "text"))
	}
	if fake.calls != 1 {
		t.Fatalf("calls = %d, want 1", fake.calls)
	}
}

func TestAWSConfigGetEmptyObjectPlaceholder(t *testing.T) {
	fake := &fakeAWS{response: func() (*awsconfig.Object, error) {
		return &awsconfig.Object{Bucket: "homelab-config", Key: "aws/config", Content: ""}, nil
	}}
	app := server.App(nil, unavailableK8s(), unavailableGitHub(), fake, unavailableGrafana())

	body := callTool(t, app, 101, "aws_config_get", map[string]any{})
	if at(t, body, "result", "isError") != false {
		t.Fatalf("isError = %v", at(t, body, "result", "isError"))
	}
	if at(t, body, "result", "content", 0, "text") != "(empty object)" {
		t.Fatalf("text = %v", at(t, body, "result", "content", 0, "text"))
	}
	if at(t, body, "result", "structuredContent", "content") != "" {
		t.Fatalf("content = %v", at(t, body, "result", "structuredContent", "content"))
	}
}

func TestAWSConfigGetUnavailableReturnsToolError(t *testing.T) {
	app := server.App(nil, unavailableK8s(), unavailableGitHub(), unavailableAWS(), unavailableGrafana())
	body := callTool(t, app, 102, "aws_config_get", map[string]any{})
	if at(t, body, "result", "isError") != true {
		t.Fatalf("isError = %v", at(t, body, "result", "isError"))
	}
	text, _ := at(t, body, "result", "content", 0, "text").(string)
	if !strings.Contains(text, "aws config") {
		t.Fatalf("text = %q", text)
	}
}

func TestToolsListAdvertisesGrafanaToken(t *testing.T) {
	app := server.App(nil, unavailableK8s(), unavailableGitHub(), unavailableAWS(), unavailableGrafana())
	tools := toolsList(t, app)
	token := findTool(t, tools, "grafana_token")
	props := at(t, token, "inputSchema", "properties").(map[string]any)
	if len(props) != 0 {
		t.Fatalf("properties should be empty, got %v", props)
	}
	if at(t, token, "annotations", "title") != "Grafana Cloud Read Token" ||
		at(t, token, "annotations", "readOnlyHint") != false ||
		at(t, token, "annotations", "openWorldHint") != true {
		t.Fatalf("annotations = %v", token["annotations"])
	}
}

func TestGrafanaTokenDispatchesEnvResource(t *testing.T) {
	fake := &fakeGrafana{response: func() (*grafana.Token, error) {
		return &grafana.Token{
			Token:     "glc_short_lived",
			Name:      "homelab-k3s-mcp-1",
			ExpiresAt: "2026-05-27T01:00:00Z",
		}, nil
	}}
	app := server.App(nil, unavailableK8s(), unavailableGitHub(), unavailableAWS(), fake)

	body := callTool(t, app, 110, "grafana_token", map[string]any{})
	if at(t, body, "result", "isError") != false {
		t.Fatalf("isError = %v", at(t, body, "result", "isError"))
	}
	if at(t, body, "result", "structuredContent") != nil {
		t.Fatalf("structuredContent should be null")
	}
	resource := at(t, body, "result", "content", 0).(map[string]any)
	if resource["type"] != "resource" {
		t.Fatalf("type = %v", resource["type"])
	}
	if at(t, resource, "resource", "mimeType") != "text/plain" {
		t.Fatalf("mimeType = %v", at(t, resource, "resource", "mimeType"))
	}
	uri := at(t, resource, "resource", "uri").(string)
	if !strings.HasSuffix(uri, ".env") {
		t.Fatalf("uri = %q", uri)
	}
	text := at(t, resource, "resource", "text").(string)
	for _, want := range []string{
		"GRAFANA_TOKEN=glc_short_lived", "# Expires at: 2026-05-27T01:00:00Z",
		"# Scope: metrics:read, logs:read",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("text missing %q:\n%s", want, text)
		}
	}
	if fake.calls != 1 {
		t.Fatalf("calls = %d, want 1", fake.calls)
	}
}

func TestGrafanaTokenWithoutArgumentsField(t *testing.T) {
	fake := &fakeGrafana{}
	app := server.App(nil, unavailableK8s(), unavailableGitHub(), unavailableAWS(), fake)
	body := rpc(t, app, map[string]any{
		"jsonrpc": "2.0", "id": 111, "method": "tools/call",
		"params": map[string]any{"name": "grafana_token"},
	})
	if at(t, body, "result", "isError") != false {
		t.Fatalf("isError = %v", at(t, body, "result", "isError"))
	}
	if fake.calls != 1 {
		t.Fatalf("calls = %d", fake.calls)
	}
}

func TestGrafanaTokenUnavailableReturnsToolError(t *testing.T) {
	app := server.App(nil, unavailableK8s(), unavailableGitHub(), unavailableAWS(), unavailableGrafana())
	body := callTool(t, app, 112, "grafana_token", map[string]any{})
	if at(t, body, "result", "isError") != true {
		t.Fatalf("isError = %v", at(t, body, "result", "isError"))
	}
	text, _ := at(t, body, "result", "content", 0, "text").(string)
	if !strings.Contains(text, "grafana") {
		t.Fatalf("text = %q", text)
	}
}

func contains(s []string, want string) bool {
	for _, x := range s {
		if x == want {
			return true
		}
	}
	return false
}

func strptr(s string) *string { return &s }
func boolPtr(b bool) *bool    { return &b }
