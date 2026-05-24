// Package mcp implements the JSON-RPC MCP endpoint and its tools.
package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"sort"
	"strings"

	"github.com/dlddu/homelab-k3s-mcp/internal/awsconfig"
	"github.com/dlddu/homelab-k3s-mcp/internal/github"
	"github.com/dlddu/homelab-k3s-mcp/internal/grafana"
	"github.com/dlddu/homelab-k3s-mcp/internal/k8s"
	"github.com/dlddu/homelab-k3s-mcp/internal/version"
)

const (
	protocolVersion = "2025-06-18"

	logsDefaultTailLines int64 = 200
	logsMaxTailLines     int64 = 5000

	dearBabyDefaultSelector  = "app=dear-baby"
	dearBabyDefaultContainer = "backend"
	dearBabyResetBin         = "/reset-onboarding"
)

// Handler serves the MCP JSON-RPC endpoint.
type Handler struct {
	k8s     k8s.Service
	github  github.Service
	aws     awsconfig.Service
	grafana grafana.Service
}

// NewHandler builds an MCP handler backed by the given services.
func NewHandler(k8sSvc k8s.Service, ghSvc github.Service, awsSvc awsconfig.Service, grafanaSvc grafana.Service) *Handler {
	return &Handler{k8s: k8sSvc, github: ghSvc, aws: awsSvc, grafana: grafanaSvc}
}

type rpcRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params"`
}

type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *rpcErrorBody   `json:"error,omitempty"`
}

type rpcErrorBody struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// rpcErr is an internal carrier for a JSON-RPC error (code + message).
type rpcErr struct {
	code    int
	message string
}

func errf(code int, format string, args ...any) *rpcErr {
	return &rpcErr{code: code, message: fmt.Sprintf(format, args...)}
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	var req rpcRequest
	dec := json.NewDecoder(r.Body)
	if err := dec.Decode(&req); err != nil {
		writeResponse(w, errorResponse(nullID(), -32700, "parse error"))
		return
	}
	if req.JSONRPC == "" {
		req.JSONRPC = "2.0"
	}

	id := req.ID
	if len(bytes.TrimSpace(id)) == 0 {
		id = nullID()
	}

	if req.JSONRPC != "2.0" {
		writeResponse(w, errorResponse(id, -32600, "invalid jsonrpc version"))
		return
	}

	result, rerr := h.dispatch(r.Context(), req.Method, req.Params)
	if rerr != nil {
		writeResponse(w, errorResponse(id, rerr.code, rerr.message))
		return
	}
	writeResponse(w, successResponse(id, result))
}

func (h *Handler) dispatch(ctx context.Context, method string, params json.RawMessage) (any, *rpcErr) {
	switch method {
	case "initialize":
		return initializeResult(), nil
	case "tools/list":
		return json.RawMessage(toolsListJSON), nil
	case "tools/call":
		return h.toolsCall(ctx, params)
	case "ping":
		return map[string]any{}, nil
	default:
		return nil, errf(-32601, "method not found: %s", method)
	}
}

func initializeResult() any {
	return map[string]any{
		"protocolVersion": protocolVersion,
		"capabilities":    map[string]any{"tools": map[string]any{"listChanged": false}},
		"serverInfo":      map[string]any{"name": version.Name, "version": version.Version},
	}
}

func (h *Handler) toolsCall(ctx context.Context, params json.RawMessage) (any, *rpcErr) {
	obj, ok := decodeObject(params)
	if !ok {
		return nil, errf(-32602, "missing tool name")
	}
	nameVal, ok := obj["name"]
	if !ok {
		return nil, errf(-32602, "missing tool name")
	}
	name, ok := nameVal.(string)
	if !ok {
		return nil, errf(-32602, "missing tool name")
	}

	args := params // arguments are re-decoded per tool from the raw params
	rawArgs := extractArguments(args)

	switch name {
	case "ping":
		return toolText("pong", false), nil
	case "namespace_list":
		return h.namespaceList(ctx)
	case "workload_list":
		return h.workloadList(ctx, rawArgs)
	case "workload_restart":
		return h.workloadRestart(ctx, rawArgs)
	case "workload_scale":
		return h.workloadScale(ctx, rawArgs)
	case "workload_logs":
		return h.workloadLogs(ctx, rawArgs)
	case "pod_describe":
		return h.podDescribe(ctx, rawArgs)
	case "dear_baby_reset_onboarding":
		return h.dearBabyResetOnboarding(ctx, rawArgs)
	case "github_app_installation_token":
		return h.githubAppInstallationToken(ctx, rawArgs)
	case "aws_config_get":
		return h.awsConfigGet(ctx)
	case "grafana_cloud_token":
		return h.grafanaCloudToken(ctx)
	default:
		return nil, errf(-32602, "unknown tool: %s", name)
	}
}

// extractArguments pulls the "arguments" field out of the raw tools/call params.
// A missing field yields a null RawMessage.
func extractArguments(params json.RawMessage) json.RawMessage {
	var p struct {
		Arguments json.RawMessage `json:"arguments"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return nullID()
	}
	if len(bytes.TrimSpace(p.Arguments)) == 0 {
		return nullID()
	}
	return p.Arguments
}

// --- tool implementations ---

func (h *Handler) namespaceList(ctx context.Context) (any, *rpcErr) {
	items, err := h.k8s.ListNamespaces(ctx)
	if err != nil {
		return toolError(err), nil
	}
	return successResult(map[string]any{"items": items}), nil
}

func (h *Handler) workloadList(ctx context.Context, raw json.RawMessage) (any, *rpcErr) {
	obj, ok := decodeObject(raw)
	if !ok {
		return nil, errf(-32602, "arguments must be an object")
	}
	kind, rerr := parseKind(obj)
	if rerr != nil {
		return nil, rerr
	}
	namespace := optionalString(obj, "namespace")

	items, err := h.k8s.ListWorkloads(ctx, kind, namespace)
	if err != nil {
		return toolError(err), nil
	}
	return successResult(map[string]any{
		"kind":      kind.String(),
		"namespace": namespace,
		"items":     items,
	}), nil
}

func (h *Handler) workloadRestart(ctx context.Context, raw json.RawMessage) (any, *rpcErr) {
	obj, ok := decodeObject(raw)
	if !ok {
		return nil, errf(-32602, "arguments must be an object")
	}
	kind, rerr := parseKind(obj)
	if rerr != nil {
		return nil, rerr
	}
	namespace := optionalString(obj, "namespace")
	if namespace == nil {
		return nil, errf(-32602, "namespace is required")
	}
	name := optionalString(obj, "name")
	if name == nil {
		return nil, errf(-32602, "name is required")
	}

	restartedAt, err := h.k8s.RolloutRestart(ctx, kind, *namespace, *name)
	if err != nil {
		return toolError(err), nil
	}
	return successResult(map[string]any{
		"kind":        kind.String(),
		"namespace":   *namespace,
		"name":        *name,
		"restartedAt": restartedAt,
	}), nil
}

func (h *Handler) workloadScale(ctx context.Context, raw json.RawMessage) (any, *rpcErr) {
	obj, ok := decodeObject(raw)
	if !ok {
		return nil, errf(-32602, "arguments must be an object")
	}
	kind, rerr := parseKind(obj)
	if rerr != nil {
		return nil, rerr
	}
	namespace := optionalString(obj, "namespace")
	if namespace == nil {
		return nil, errf(-32602, "namespace is required")
	}
	name := optionalString(obj, "name")
	if name == nil {
		return nil, errf(-32602, "name is required")
	}
	rv, ok := obj["replicas"]
	if !ok {
		return nil, errf(-32602, "replicas is required")
	}
	ri, ok := intValue(rv)
	if !ok {
		return nil, errf(-32602, "replicas must be an integer")
	}
	if ri < 0 {
		return nil, errf(-32602, "replicas must be >= 0")
	}
	if ri > math.MaxInt32 {
		return nil, errf(-32602, "replicas is too large")
	}

	applied, err := h.k8s.ScaleWorkload(ctx, kind, *namespace, *name, int32(ri))
	if err != nil {
		return toolError(err), nil
	}
	return successResult(map[string]any{
		"kind":      kind.String(),
		"namespace": *namespace,
		"name":      *name,
		"replicas":  applied,
	}), nil
}

func (h *Handler) workloadLogs(ctx context.Context, raw json.RawMessage) (any, *rpcErr) {
	obj, ok := decodeObject(raw)
	if !ok {
		return nil, errf(-32602, "arguments must be an object")
	}
	kind, rerr := parseKind(obj)
	if rerr != nil {
		return nil, rerr
	}
	namespace := optionalString(obj, "namespace")
	if namespace == nil {
		return nil, errf(-32602, "namespace is required")
	}
	name := optionalString(obj, "name")
	if name == nil {
		return nil, errf(-32602, "name is required")
	}

	container := optionalString(obj, "container")

	previous, _, rerr := boolArg(obj, "previous")
	if rerr != nil {
		return nil, rerr
	}
	timestamps, _, rerr := boolArg(obj, "timestamps")
	if rerr != nil {
		return nil, rerr
	}

	tailLines := logsDefaultTailLines
	if v, present := obj["tail_lines"]; present {
		n, ok := intValue(v)
		if !ok {
			return nil, errf(-32602, "tail_lines must be an integer")
		}
		if n < 1 {
			return nil, errf(-32602, "tail_lines must be >= 1")
		}
		if n > logsMaxTailLines {
			return nil, errf(-32602, "tail_lines must be <= %d", logsMaxTailLines)
		}
		tailLines = n
	}

	var sinceSeconds *int64
	if v, present := obj["since_seconds"]; present {
		n, ok := intValue(v)
		if !ok {
			return nil, errf(-32602, "since_seconds must be an integer")
		}
		if n < 1 {
			return nil, errf(-32602, "since_seconds must be >= 1")
		}
		sinceSeconds = &n
	}

	tail := tailLines
	opts := k8s.LogOptions{
		Container:    container,
		TailLines:    &tail,
		Previous:     previous,
		Timestamps:   timestamps,
		SinceSeconds: sinceSeconds,
	}

	result, err := h.k8s.WorkloadLogs(ctx, kind, *namespace, *name, opts)
	if err != nil {
		return toolError(err), nil
	}

	payload := map[string]any{
		"kind":         kind.String(),
		"namespace":    *namespace,
		"name":         *name,
		"pod":          result.Pod,
		"container":    result.Container,
		"tailLines":    opts.TailLines,
		"previous":     opts.Previous,
		"timestamps":   opts.Timestamps,
		"sinceSeconds": opts.SinceSeconds,
		"logs":         result.Logs,
	}
	text := result.Logs
	if text == "" {
		text = "(no log output)"
	}
	return map[string]any{
		"content":           []any{map[string]any{"type": "text", "text": text}},
		"structuredContent": payload,
		"isError":           false,
	}, nil
}

func (h *Handler) podDescribe(ctx context.Context, raw json.RawMessage) (any, *rpcErr) {
	obj, ok := decodeObject(raw)
	if !ok {
		return nil, errf(-32602, "arguments must be an object")
	}
	namespace := optionalString(obj, "namespace")
	if namespace == nil {
		return nil, errf(-32602, "namespace is required")
	}
	target, rerr := parsePodTarget(obj)
	if rerr != nil {
		return nil, rerr
	}

	description, err := h.k8s.DescribePod(ctx, *namespace, target)
	if err != nil {
		return toolError(err), nil
	}
	return map[string]any{
		"content":           []any{map[string]any{"type": "text", "text": renderPodDescribeText(description)}},
		"structuredContent": description,
		"isError":           false,
	}, nil
}

func parsePodTarget(obj map[string]any) (k8s.PodTarget, *rpcErr) {
	name := optionalString(obj, "name")
	selector := optionalString(obj, "selector")
	workloadKind := optionalString(obj, "workload_kind")
	workloadName := optionalString(obj, "workload_name")

	workloadProvided := workloadKind != nil || workloadName != nil
	modes := 0
	if name != nil {
		modes++
	}
	if selector != nil {
		modes++
	}
	if workloadProvided {
		modes++
	}

	if modes == 0 {
		return k8s.PodTarget{}, errf(-32602, "one of 'name', 'selector', or 'workload_kind'+'workload_name' is required")
	}
	if modes > 1 {
		return k8s.PodTarget{}, errf(-32602, "'name', 'selector', and 'workload_kind'+'workload_name' are mutually exclusive")
	}

	if name != nil {
		return k8s.PodTarget{Mode: k8s.TargetName, Name: *name}, nil
	}
	if selector != nil {
		return k8s.PodTarget{Mode: k8s.TargetSelector, Selector: *selector}, nil
	}

	if workloadKind == nil {
		return k8s.PodTarget{}, errf(-32602, "workload_kind is required when workload_name is provided")
	}
	if workloadName == nil {
		return k8s.PodTarget{}, errf(-32602, "workload_name is required when workload_kind is provided")
	}
	kind, ok := k8s.ParseWorkloadKind(*workloadKind)
	if !ok {
		return k8s.PodTarget{}, errf(-32602, "unknown workload_kind: %s (expected Deployment, StatefulSet, or DaemonSet)", *workloadKind)
	}
	return k8s.PodTarget{Mode: k8s.TargetWorkload, Kind: kind, WorkloadName: *workloadName}, nil
}

func (h *Handler) dearBabyResetOnboarding(ctx context.Context, raw json.RawMessage) (any, *rpcErr) {
	obj, ok := decodeObject(raw)
	if !ok {
		return nil, errf(-32602, "arguments must be an object")
	}
	namespace := optionalString(obj, "namespace")
	if namespace == nil {
		return nil, errf(-32602, "namespace is required")
	}
	email := optionalString(obj, "email")
	if email == nil {
		return nil, errf(-32602, "email is required")
	}
	selector := dearBabyDefaultSelector
	if s := optionalString(obj, "selector"); s != nil {
		selector = *s
	}
	container := dearBabyDefaultContainer
	if c := optionalString(obj, "container"); c != nil {
		container = *c
	}

	command := []string{dearBabyResetBin, *email}
	outcome, err := h.k8s.ExecInPod(ctx, *namespace, selector, &container, command)
	if err != nil {
		return toolError(err), nil
	}

	payload := map[string]any{
		"namespace": *namespace,
		"email":     *email,
		"selector":  selector,
		"container": container,
		"pod":       outcome.Pod,
		"exitCode":  outcome.ExitCode,
		"stdout":    outcome.Stdout,
		"stderr":    outcome.Stderr,
		"success":   outcome.Success,
	}
	return map[string]any{
		"content":           []any{map[string]any{"type": "text", "text": prettyJSON(payload)}},
		"structuredContent": payload,
		"isError":           !outcome.Success,
	}, nil
}

func (h *Handler) githubAppInstallationToken(ctx context.Context, raw json.RawMessage) (any, *rpcErr) {
	obj, isObject := decodeObject(raw)
	if !isObject && !isNullArg(raw) {
		return nil, errf(-32602, "arguments must be an object")
	}

	var repositories []string
	if rv, present := obj["repositories"]; present && rv != nil {
		arr, ok := rv.([]any)
		if !ok {
			return nil, errf(-32602, "repositories must be an array of strings")
		}
		repositories = make([]string, 0, len(arr))
		for _, item := range arr {
			s, ok := item.(string)
			if !ok {
				return nil, errf(-32602, "repositories must be an array of strings")
			}
			repositories = append(repositories, s)
		}
	}

	var permissions map[string]any
	if pv, present := obj["permissions"]; present && pv != nil {
		m, ok := pv.(map[string]any)
		if !ok {
			return nil, errf(-32602, "permissions must be an object")
		}
		permissions = m
	}

	token, err := h.github.CreateInstallationToken(ctx, repositories, permissions)
	if err != nil {
		return toolError(err), nil
	}
	return installationTokenResult(token), nil
}

func installationTokenResult(token *github.InstallationToken) any {
	return map[string]any{
		"content": []any{
			map[string]any{
				"type": "resource",
				"resource": map[string]any{
					"uri":      "file:///github-token.env",
					"mimeType": "text/plain",
					"text":     installationTokenEnv(token),
				},
			},
		},
		"isError": false,
	}
}

func installationTokenEnv(token *github.InstallationToken) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# Expires at: %s\n", token.ExpiresAt)
	if token.RepositorySelection != "" {
		fmt.Fprintf(&b, "# Repository selection: %s\n", token.RepositorySelection)
	}
	if len(token.Permissions) > 0 {
		keys := make([]string, 0, len(token.Permissions))
		for k := range token.Permissions {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		parts := make([]string, 0, len(keys))
		for _, k := range keys {
			s, _ := token.Permissions[k].(string)
			parts = append(parts, k+"="+s)
		}
		fmt.Fprintf(&b, "# Permissions: %s\n", strings.Join(parts, ", "))
	}
	fmt.Fprintf(&b, "GITHUB_TOKEN=%s\n", token.Token)
	return b.String()
}

func (h *Handler) awsConfigGet(ctx context.Context) (any, *rpcErr) {
	obj, err := h.aws.GetConfig(ctx)
	if err != nil {
		return toolError(err), nil
	}

	payload := map[string]any{
		"bucket":       obj.Bucket,
		"key":          obj.Key,
		"content":      obj.Content,
		"contentType":  obj.ContentType,
		"etag":         obj.ETag,
		"lastModified": obj.LastModified,
		"size":         obj.Size,
	}
	text := obj.Content
	if text == "" {
		text = "(empty object)"
	}
	return map[string]any{
		"content":           []any{map[string]any{"type": "text", "text": text}},
		"structuredContent": payload,
		"isError":           false,
	}, nil
}

func (h *Handler) grafanaCloudToken(ctx context.Context) (any, *rpcErr) {
	token, err := h.grafana.CreateShortLivedToken(ctx)
	if err != nil {
		return toolError(err), nil
	}
	return grafanaTokenResult(token), nil
}

func grafanaTokenResult(token *grafana.Token) any {
	return map[string]any{
		"content": []any{
			map[string]any{
				"type": "resource",
				"resource": map[string]any{
					"uri":      "file:///grafana-token.env",
					"mimeType": "text/plain",
					"text":     grafanaTokenEnv(token),
				},
			},
		},
		"isError": false,
	}
}

func grafanaTokenEnv(token *grafana.Token) string {
	var b strings.Builder
	if token.ExpiresAt != "" {
		fmt.Fprintf(&b, "# Expires at: %s\n", token.ExpiresAt)
	}
	if token.AccessPolicyID != "" {
		fmt.Fprintf(&b, "# Access policy: %s\n", token.AccessPolicyID)
	}
	fmt.Fprintf(&b, "GRAFANA_CLOUD_TOKEN=%s\n", token.Token)
	return b.String()
}

// --- shared helpers ---

func parseKind(obj map[string]any) (k8s.WorkloadKind, *rpcErr) {
	v, ok := obj["kind"]
	if !ok {
		return 0, errf(-32602, "kind is required")
	}
	s, ok := v.(string)
	if !ok {
		return 0, errf(-32602, "kind is required")
	}
	kind, ok := k8s.ParseWorkloadKind(s)
	if !ok {
		return 0, errf(-32602, "unknown kind: %s (expected Deployment, StatefulSet, or DaemonSet)", s)
	}
	return kind, nil
}

func optionalString(obj map[string]any, key string) *string {
	v, ok := obj[key]
	if !ok {
		return nil
	}
	s, ok := v.(string)
	if !ok || s == "" {
		return nil
	}
	return &s
}

func boolArg(obj map[string]any, key string) (value bool, present bool, rerr *rpcErr) {
	v, ok := obj[key]
	if !ok {
		return false, false, nil
	}
	b, ok := v.(bool)
	if !ok {
		return false, true, errf(-32602, "%s must be a boolean", key)
	}
	return b, true, nil
}

func intValue(v any) (int64, bool) {
	n, ok := v.(json.Number)
	if !ok {
		return 0, false
	}
	i, err := n.Int64()
	if err != nil {
		return 0, false
	}
	return i, true
}

func isNullArg(raw json.RawMessage) bool {
	t := bytes.TrimSpace(raw)
	return len(t) == 0 || string(t) == "null"
}

func decodeObject(raw json.RawMessage) (map[string]any, bool) {
	if isNullArg(raw) {
		return nil, false
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var m map[string]any
	if err := dec.Decode(&m); err != nil {
		return nil, false
	}
	return m, true
}

func successResult(payload map[string]any) any {
	return map[string]any{
		"content":           []any{map[string]any{"type": "text", "text": prettyJSON(payload)}},
		"structuredContent": payload,
		"isError":           false,
	}
}

func toolText(text string, isError bool) any {
	return map[string]any{
		"content": []any{map[string]any{"type": "text", "text": text}},
		"isError": isError,
	}
}

func toolError(err error) any {
	return toolText(err.Error(), true)
}

func prettyJSON(v any) string {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		b2, _ := json.Marshal(v)
		return string(b2)
	}
	return string(b)
}

func nullID() json.RawMessage { return json.RawMessage("null") }

func successResponse(id json.RawMessage, result any) rpcResponse {
	b, err := json.Marshal(result)
	if err != nil {
		b = json.RawMessage("null")
	}
	return rpcResponse{JSONRPC: "2.0", ID: id, Result: b}
}

func errorResponse(id json.RawMessage, code int, message string) rpcResponse {
	return rpcResponse{JSONRPC: "2.0", ID: id, Error: &rpcErrorBody{Code: code, Message: message}}
}

func writeResponse(w http.ResponseWriter, resp rpcResponse) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(resp)
}
