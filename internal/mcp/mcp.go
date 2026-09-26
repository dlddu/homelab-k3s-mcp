// Package mcp implements the JSON-RPC MCP endpoint and its tools.
package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/dlddu/homelab-k3s-mcp/internal/awsconfig"
	"github.com/dlddu/homelab-k3s-mcp/internal/eventlog"
	"github.com/dlddu/homelab-k3s-mcp/internal/gatekeeper"
	"github.com/dlddu/homelab-k3s-mcp/internal/github"
	"github.com/dlddu/homelab-k3s-mcp/internal/grafana"
	"github.com/dlddu/homelab-k3s-mcp/internal/k8s"
	"github.com/dlddu/homelab-k3s-mcp/internal/opensearch"
	"github.com/dlddu/homelab-k3s-mcp/internal/sessionplatform"
	"github.com/dlddu/homelab-k3s-mcp/internal/version"
)

const (
	protocolVersion = "2025-06-18"

	dearBabyDefaultSelector  = "app=dear-baby"
	dearBabyDefaultContainer = "backend"
	dearBabyResetBin         = "/reset-user"

	// defaultSensitiveKind mirrors RESOURCE_GATED_KINDS' default: reading a
	// Secret is gated even though reads otherwise are not.
	defaultSensitiveKind = "v1/Secret"
)

// Handler serves the MCP JSON-RPC endpoint.
type Handler struct {
	k8s             k8s.Service
	github          github.Service
	aws             awsconfig.Service
	grafana         grafana.Service
	opensearch      opensearch.Service
	sessionPlatform sessionplatform.Service

	// gate holds gated calls until a human decides. It is never nil: an
	// unconfigured deployment gets a gate that refuses, because "no approval
	// backend" has to mean "no state changes", not "no approvals needed".
	gate gatekeeper.Gate

	// gateReader is how the gate reads a target on its own behalf before the
	// verdict exists (prd-approval-gate AC11). A separate field from k8s above
	// because it is a separate permission; k8s.TargetReader holds the reason.
	// Never nil — a deployment without a cluster client gets one that refuses.
	gateReader k8s.TargetReader

	// gateCollectionReader is the plural of gateReader: the `list on ⟨kind⟩`
	// half of AC11's table. A separate field for the same reason gateReader is
	// one; k8s.CollectionTargetReader holds it. Never nil.
	gateCollectionReader k8s.CollectionTargetReader

	sensitiveKinds []string

	// registry is the dispatchable tool set. Held on the Handler rather than
	// read from the package global so a test can dispatch against a tool that
	// production does not ship.
	registry map[string]toolEntry

	// events receives one record per tools/call (prd-event-log AC1). nil
	// means eventlog.Log — the field exists so a test can read records as
	// values instead of scraping the log.
	events eventlog.Sink
}

// Option customises a Handler at construction.
type Option func(*Handler)

// WithGate installs the approval gate. Without it a Handler refuses every gated
// call (prd-approval-gate AC5).
func WithGate(gate gatekeeper.Gate, sensitiveKinds []string) Option {
	return func(h *Handler) {
		if gate != nil {
			h.gate = gate
		}
		if len(sensitiveKinds) > 0 {
			h.sensitiveKinds = sensitiveKinds
		}
	}
}

// WithGateReader installs the reader the gate uses for AC11's two pairs. It is
// a separate option from WithGate because the gate and its reader fail
// independently: an approval backend can be up while the cluster client is not,
// and that combination has to refuse gated calls rather than describe them from
// nothing.
func WithGateReader(reader k8s.TargetReader) Option {
	return func(h *Handler) {
		if reader != nil {
			h.gateReader = reader
		}
	}
}

// WithGateCollectionReader installs the reader the gate uses for the `list`
// half of AC11's table. Its own option rather than a second argument to
// WithGateReader because the failure is silent: a gate missing only this one
// still describes a collection delete, from arguments that do not say what
// will be deleted.
func WithGateCollectionReader(reader k8s.CollectionTargetReader) Option {
	return func(h *Handler) {
		if reader != nil {
			h.gateCollectionReader = reader
		}
	}
}

// WithEventSink routes tool-call records somewhere other than the default
// log.
func WithEventSink(sink eventlog.Sink) Option {
	return func(h *Handler) {
		if sink != nil {
			h.events = sink
		}
	}
}

// NewHandler builds an MCP handler backed by the given services.
func NewHandler(k8sSvc k8s.Service, ghSvc github.Service, awsSvc awsconfig.Service, grafanaSvc grafana.Service, osSvc opensearch.Service, sessionSvc sessionplatform.Service, opts ...Option) *Handler {
	h := &Handler{
		k8s:             k8sSvc,
		github:          ghSvc,
		aws:             awsSvc,
		grafana:         grafanaSvc,
		opensearch:      osSvc,
		sessionPlatform: sessionSvc,
		gate:            gatekeeper.NewUnavailable(nil),
		gateReader:      k8s.NewUnavailableTargetReader("the approval gate has no kubernetes client to read a target with"),
		sensitiveKinds:  []string{defaultSensitiveKind},
		registry:        toolRegistry,
	}
	h.gateCollectionReader = k8s.NewUnavailableTargetReader(
		"the approval gate has no kubernetes client to read a selection with")
	for _, opt := range opts {
		opt(h)
	}
	return h
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

// rpcErr is an internal carrier for a JSON-RPC error (code + message), and
// of what the record says about the refusal it is (prd-event-log AC2): a
// JSON-RPC error never reaches a handler, so every one of them is a refused
// call that needs a reason.
type rpcErr struct {
	code    int
	message string

	// reason is the refusal's reason when the site that produced the error
	// knows it; reasonOf fills the rest in from the code.
	reason eventlog.Reason
	// gate is what the gate said, on the errors produced after it answered.
	gate eventlog.Gate
}

func errf(code int, format string, args ...any) *rpcErr {
	return &rpcErr{code: code, message: fmt.Sprintf(format, args...)}
}

// reasonOf is the reason a JSON-RPC error records. -32602 is the arguments
// (a missing or unknown name, a coordinate that does not parse, a gate pair
// that cannot be named); every other code this dispatcher produces is the
// gate layer refusing before it could ask — a declaration the gate cannot
// work from, an execution step that found no approval on the context.
func (e *rpcErr) reasonOf() eventlog.Reason {
	if e.reason != "" {
		return e.reason
	}
	if e.code == -32602 {
		return eventlog.ReasonInvalidInput
	}
	return eventlog.ReasonGateUnconfigured
}

// afterVerdict marks a refusal that came after the gate had approved: the
// approval was spent already (AC7), or the target moved under it (AC6). The
// record keeps the backend's verdict and says the gate layer withheld the
// approval anyway — that is the case Reason and Gate.Decision differ for.
func (e *rpcErr) afterVerdict(decision *gatekeeper.Decision) *rpcErr {
	e.reason = eventlog.ReasonGateRejected
	e.gate = gateOf(decision)
	return e
}

// gateRefusal turns the gate's answer into the JSON-RPC error the caller has
// always received, carrying the verdict for the record. A gate that answers
// with an untyped error gave no verdict, which is what unreachable means; a
// typed refusal without a verdict is a call whose approval context could not
// be built, and that is the call's own fault: its arguments named something
// that could not be read.
func gateRefusal(err error) *rpcErr {
	e := errf(-32603, "%s", err.Error())
	var refusal *gatekeeper.Refusal
	if !errors.As(err, &refusal) {
		e.reason = eventlog.ReasonGateUnreachable
		return e
	}
	e.gate = eventlog.Gate{RequestID: refusal.RequestID, Decision: string(refusal.Verdict)}
	switch refusal.Verdict {
	case gatekeeper.VerdictRejected:
		e.reason = eventlog.ReasonGateRejected
	case gatekeeper.VerdictExpired:
		e.reason = eventlog.ReasonGateExpired
	case gatekeeper.VerdictTimeout:
		e.reason = eventlog.ReasonGateTimeout
	case gatekeeper.VerdictUnconfigured:
		e.reason = eventlog.ReasonGateUnconfigured
	case "":
		e.reason = eventlog.ReasonInvalidInput
	default:
		e.reason = eventlog.ReasonGateUnreachable
	}
	return e
}

// gateOf renders an approval for the record (AC2's request id and AC9's
// auto-approval notice, as fields).
func gateOf(decision *gatekeeper.Decision) eventlog.Gate {
	if decision == nil {
		return eventlog.Gate{}
	}
	return eventlog.Gate{
		RequestID:    decision.RequestID,
		Decision:     string(gatekeeper.VerdictApproved),
		AutoApproved: decision.AutoApproved,
	}
}

// callNote is what a call learns about itself on the way that the result
// alone does not say: the gate's answer on a gated path that returns a
// result, and a handler's failure being an integration that was never
// configured. It rides on the context because the handlers and the gated
// paths return the same (result, error) pair they always have — the record
// is the dispatcher's, and this is how the layers below tell it what they
// saw without writing records of their own.
type callNote struct {
	reason eventlog.Reason
	gate   eventlog.Gate

	// gateWait is the time the gate held this call, summed over every
	// Authorize a batch makes (prd-metrics AC3). askGate adds to it; the
	// dispatcher subtracts it from the call's own duration so a human's
	// deliberation never reads as tool latency.
	gateWait time.Duration
}

type callNoteKey struct{}

func noteFrom(ctx context.Context) *callNote {
	note, _ := ctx.Value(callNoteKey{}).(*callNote)
	return note
}

// noteGate records the approval a gated path is about to execute under.
func noteGate(ctx context.Context, decision *gatekeeper.Decision) {
	if note := noteFrom(ctx); note != nil {
		g := gateOf(decision)
		if note.gate.RequestID != "" {
			// A batch spends one approval per document; the record names
			// them all, in order.
			g.RequestID = note.gate.RequestID + "," + g.RequestID
			g.AutoApproved = note.gate.AutoApproved && g.AutoApproved
		}
		note.gate = g
	}
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

// toolsCall is the one place a tool call is recorded (prd-event-log AC1).
// The record is written here, after the outcome and around every exit, rather
// than by the handlers: a handler that writes its own record is a handler
// that can forget to, and a refusal that happens before any handler runs
// (an unknown name, a bad coordinate, the gate) has no handler to write one.
func (h *Handler) toolsCall(ctx context.Context, params json.RawMessage) (result any, rerr *rpcErr) {
	start := time.Now()
	record := eventlog.Record{Principal: eventlog.PrincipalFrom(ctx)}
	note := &callNote{}
	ctx = context.WithValue(ctx, callNoteKey{}, note)
	defer func() {
		record.Result, record.Reason, record.Gate = outcome(result, rerr, note)
		record.GateWait = note.gateWait
		record.Duration = time.Since(start) - note.gateWait
		h.sink().Emit(ctx, record)
	}()

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
	record.Tool = name

	args := params
	rawArgs := extractArguments(args)

	entry, ok := h.registry[name]
	if !ok {
		return nil, errf(-32602, "unknown tool: %s", name)
	}
	if entry.decl.resolve != nil {
		record.Target = recordTarget(rawArgs)
	}

	// The gate runs here rather than inside the handlers. A handler that calls
	// the gate itself is a handler that can forget to (prd-approval-gate AC1).
	if entry.createBatch {
		return h.callCreateBatch(ctx, name, entry, rawArgs)
	}
	if entry.collectionGate {
		return h.callDeleteCollection(ctx, name, entry, rawArgs)
	}
	decision, approved, rerr := h.authorize(ctx, name, entry, rawArgs)
	if rerr != nil {
		return nil, rerr
	}
	if decision != nil {
		noteGate(ctx, decision)
	}
	if approved != nil {
		ctx = context.WithValue(ctx, approvedTargetKey{}, *approved)
	}

	result, rerr = h.invoke(ctx, name, entry, rawArgs)
	if rerr != nil {
		return nil, rerr
	}
	return annotateAutoApproval(result, decision), nil
}

// askGate is the one way a call reaches the gate, so the time the gate held
// it is measured once, here, whichever of the three gated paths asked
// (prd-metrics AC3). The gate's own answer is untouched — this only notes
// how long it took.
func (h *Handler) askGate(ctx context.Context, call gatekeeper.Call) (*gatekeeper.Decision, error) {
	begin := time.Now()
	decision, err := h.gate.Authorize(ctx, call)
	if note := noteFrom(ctx); note != nil {
		note.gateWait += time.Since(begin)
	}
	return decision, err
}

func (h *Handler) sink() eventlog.Sink {
	if h.events != nil {
		return h.events
	}
	return eventlog.Log{}
}

// outcome classifies a call's end for the record (AC1's three results, AC2's
// reason and gate). A JSON-RPC error is always a refusal: every path that
// produces one — argument validation, the gate, an unknown name — returns
// before the handler runs, so nothing reached the cluster or an external
// system. A handler that ran and set isError reports an error — unless what
// failed was an integration nobody configured, which is a refusal too (AC4):
// nothing was sent there either. Anything else is success.
func outcome(result any, rerr *rpcErr, note *callNote) (eventlog.Result, eventlog.Reason, eventlog.Gate) {
	if rerr != nil {
		gate := rerr.gate
		if gate == (eventlog.Gate{}) {
			gate = note.gate
		}
		return eventlog.ResultRefused, rerr.reasonOf(), gate
	}
	if m, ok := result.(map[string]any); ok {
		if isErr, _ := m["isError"].(bool); isErr {
			if note.reason != "" {
				return eventlog.ResultRefused, note.reason, note.gate
			}
			return eventlog.ResultError, "", note.gate
		}
	}
	return eventlog.ResultSuccess, "", note.gate
}

// recordTarget reads the four coordinate fields off a generic tool's
// arguments, and only those four (AC3): the rest of the arguments — a
// manifest, a patch, an exec command, a proxy body — never reach the record.
func recordTarget(rawArgs json.RawMessage) eventlog.Target {
	obj, ok := decodeObject(rawArgs)
	if !ok {
		return eventlog.Target{}
	}
	field := func(key string) string {
		s, _ := obj[key].(string)
		return s
	}
	return eventlog.Target{
		APIVersion: field("apiVersion"),
		Kind:       field("kind"),
		Namespace:  field("namespace"),
		Name:       field("name"),
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

func (h *Handler) dearBabyResetUser(ctx context.Context, raw json.RawMessage) (any, *rpcErr) {
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
		return toolError(ctx, err), nil
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
		return toolError(ctx, err), nil
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
		return toolError(ctx, err), nil
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

// sessionList enumerates the control plane's sessions. An empty inventory is a
// successful empty list, not an error, so a caller can tell "no sessions" from
// "the control plane is unreachable".
func (h *Handler) sessionList(ctx context.Context) (any, *rpcErr) {
	sessions, err := h.sessionPlatform.ListSessions(ctx)
	if err != nil {
		return toolError(ctx, err), nil
	}
	items := make([]any, 0, len(sessions))
	for _, s := range sessions {
		items = append(items, sessionFields(s))
	}
	return successResult(map[string]any{"sessions": items}), nil
}

// sessionFields renders one session for a tool result. A snapshotted session has
// had its pods reclaimed, so the control plane omits `pod`; keep it omitted
// rather than reporting an empty name as if a pod existed. All three session
// tools report a session the same way, so they share this.
func sessionFields(s sessionplatform.Session) map[string]any {
	fields := map[string]any{
		"id":           s.ID,
		"name":         s.Name,
		"workloadType": s.WorkloadType,
		"state":        s.State,
		"createdAt":    s.CreatedAt,
		"lastAccess":   s.LastAccess,
	}
	if s.Pod != "" {
		fields["pod"] = s.Pod
	}
	return fields
}

// sessionRead reads one session's accumulated output from a byte cursor. Unlike
// sessionList this is not a passive call: the control plane activates the target
// first, so the result carries both the branch it took and the session as it
// stands afterwards. A caller that reads a snapshotted session has just brought
// its pod back, and must be able to see that from the response alone.
func (h *Handler) sessionRead(ctx context.Context, raw json.RawMessage) (any, *rpcErr) {
	obj, ok := decodeObject(raw)
	if !ok {
		return nil, errf(-32602, "arguments must be an object")
	}
	id := optionalString(obj, "id")
	if id == nil {
		return nil, errf(-32602, "id is required")
	}

	// offset is optional and defaults to 0, "everything since session start".
	var offset int64
	if ov, present := obj["offset"]; present && ov != nil {
		oi, ok := intValue(ov)
		if !ok {
			return nil, errf(-32602, "offset must be an integer")
		}
		if oi < 0 {
			return nil, errf(-32602, "offset must be >= 0")
		}
		offset = oi
	}

	result, err := h.sessionPlatform.ReadSession(ctx, *id, offset)
	if err != nil {
		return toolError(ctx, err), nil
	}

	return successResult(map[string]any{
		"payload":    result.Payload,
		"nextOffset": result.NextOffset,
		"path":       result.Path,
		"session":    sessionFields(result.Session),
	}), nil
}

// sessionWrite injects input into one session's workload. Both arguments are
// validated here so that a malformed call is refused before any request is
// issued — the same structural guarantee sessionRead makes for a bad cursor:
// nothing was sent, so the session cannot have been touched. That matters more
// for a write, whose mere arrival activates the target and can restore a
// snapshot. The result deliberately carries no output: the control plane returns
// on acceptance, and whatever the workload produces is recovered with
// session_read.
func (h *Handler) sessionWrite(ctx context.Context, raw json.RawMessage) (any, *rpcErr) {
	obj, ok := decodeObject(raw)
	if !ok {
		return nil, errf(-32602, "arguments must be an object")
	}
	id := optionalString(obj, "id")
	if id == nil {
		return nil, errf(-32602, "id is required")
	}
	pv, present := obj["payload"]
	if !present || pv == nil {
		return nil, errf(-32602, "payload is required")
	}
	payload, ok := pv.(string)
	if !ok {
		return nil, errf(-32602, "payload must be a string")
	}

	result, err := h.sessionPlatform.WriteSession(ctx, *id, payload)
	if err != nil {
		return toolError(ctx, err), nil
	}

	return successResult(map[string]any{
		"path":    result.Path,
		"session": sessionFields(result.Session),
	}), nil
}

func (h *Handler) grafanaToken(ctx context.Context) (any, *rpcErr) {
	creds, err := h.grafana.CreateToken(ctx)
	if err != nil {
		return toolError(ctx, err), nil
	}
	return grafanaTokenResult(creds), nil
}

func grafanaTokenResult(creds *grafana.Credentials) any {
	return map[string]any{
		"content": []any{
			map[string]any{
				"type": "resource",
				"resource": map[string]any{
					"uri":      "file:///grafana-token.env",
					"mimeType": "text/plain",
					"text":     grafanaTokenEnv(creds),
				},
			},
		},
		"isError": false,
	}
}

func grafanaTokenEnv(creds *grafana.Credentials) string {
	var b strings.Builder
	if creds.ExpiresAt != "" {
		fmt.Fprintf(&b, "# token expires %s\n", creds.ExpiresAt)
	}
	fmt.Fprintf(&b, "GRAFANA_METRICS_URL=%s\n", creds.MetricsURL)
	fmt.Fprintf(&b, "GRAFANA_METRICS_USER=%s\n", creds.MetricsUser)
	fmt.Fprintf(&b, "GRAFANA_LOGS_URL=%s\n", creds.LogsURL)
	fmt.Fprintf(&b, "GRAFANA_LOGS_USER=%s\n", creds.LogsUser)
	fmt.Fprintf(&b, "# GRAFANA_TOKEN is the shared Basic-auth password for both *_USER values above\n")
	fmt.Fprintf(&b, "GRAFANA_TOKEN=%s\n", creds.Token)
	return b.String()
}

func (h *Handler) opensearchSearch(ctx context.Context, raw json.RawMessage) (any, *rpcErr) {
	obj, ok := decodeObject(raw)
	if !ok {
		return nil, errf(-32602, "arguments must be an object")
	}
	query := optionalString(obj, "query")
	if query == nil {
		return nil, errf(-32602, "query is required")
	}
	index := optionalString(obj, "index")

	var size *int64
	if v, present := obj["size"]; present {
		n, ok := intValue(v)
		if !ok {
			return nil, errf(-32602, "size must be an integer")
		}
		size = &n
	}

	result, err := h.opensearch.Search(ctx, *query, index, size)
	if err != nil {
		return toolError(ctx, err), nil
	}
	payload := map[string]any{
		"query": *query,
		"index": index,
		"total": result.Total,
		"hits":  result.Hits,
	}
	return successResult(payload), nil
}

func (h *Handler) opensearchDocumentPut(ctx context.Context, raw json.RawMessage) (any, *rpcErr) {
	obj, ok := decodeObject(raw)
	if !ok {
		return nil, errf(-32602, "arguments must be an object")
	}
	index := optionalString(obj, "index")
	if index == nil {
		return nil, errf(-32602, "index is required")
	}
	dv, present := obj["document"]
	if !present {
		return nil, errf(-32602, "document is required")
	}
	document, ok := dv.(map[string]any)
	if !ok {
		return nil, errf(-32602, "document must be an object")
	}
	id := optionalString(obj, "id")

	result, err := h.opensearch.PutDocument(ctx, *index, id, document)
	if err != nil {
		return toolError(ctx, err), nil
	}
	return successResult(map[string]any{
		"index":  result.Index,
		"id":     result.ID,
		"result": result.Result,
	}), nil
}

func (h *Handler) opensearchDocumentDelete(ctx context.Context, raw json.RawMessage) (any, *rpcErr) {
	obj, ok := decodeObject(raw)
	if !ok {
		return nil, errf(-32602, "arguments must be an object")
	}
	index := optionalString(obj, "index")
	if index == nil {
		return nil, errf(-32602, "index is required")
	}
	id := optionalString(obj, "id")
	if id == nil {
		return nil, errf(-32602, "id is required")
	}

	result, err := h.opensearch.DeleteDocument(ctx, *index, *id)
	if err != nil {
		return toolError(ctx, err), nil
	}
	return successResult(map[string]any{
		"index":  result.Index,
		"id":     result.ID,
		"result": result.Result,
	}), nil
}

// --- shared helpers ---

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

// toolError renders a handler's failure for the client, and tells the record
// when that failure was an integration that is not configured: the response
// looks the same either way (the caller has always read the message), the
// record does not (AC4 counts an unconfigured refusal, not an error).
func toolError(ctx context.Context, err error) any {
	var u interface{ Unavailable() bool }
	if errors.As(err, &u) && u.Unavailable() {
		if note := noteFrom(ctx); note != nil {
			note.reason = eventlog.ReasonUnconfigured
		}
	}
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
