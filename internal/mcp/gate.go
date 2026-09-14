package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"time"

	"github.com/dlddu/homelab-k3s-mcp/internal/gatekeeper"
)

// gatedVerbs are the RBAC verbs that change cluster state, gated without
// exception and judged by the verb alone (prd-approval-gate AC1).
var gatedVerbs = map[string]bool{
	"create":           true,
	"update":           true,
	"patch":            true,
	"delete":           true,
	"deletecollection": true,
}

// toolDeclaration is one row of the (verb, resource[/subresource]) table AC1
// requires every tool to publish.
type toolDeclaration struct {
	// pairs is what this tool exercises against the apiserver. Empty means the
	// tool touches no kubernetes resource at all (the platform integrations).
	pairs []gatekeeper.Pair

	// outsideGate, when non-empty, is the documented reason this tool stays
	// outside the gate even though its pairs would otherwise select it. It is
	// never a judgement made here: each value cites the document that made it.
	// An undocumented exemption is the bypass AC1 forbids, so this field is
	// deliberately a sentence rather than a bool.
	outsideGate string
}

// The two reasons any tool is currently exempt. Both come from the documents,
// not from this package.
const (
	// docs/prd-approval-gate.md, "남은 예외 1건" — the reason and its open state
	// live there; this code only cites them.
	exemptPendingOwnerDecision = "prd-approval-gate 「남은 예외 1건」 — 소유자 판단 대기 (doc-tracker.md 미결)"

	// docs/prd-approval-gate.md's gate table lists resource_* tools only; the
	// six V1 workload tools were retired from the documentation entirely by
	// PR #72 and survive in code alone, pending removal. Gating them would mean
	// inventing a rule the documents do not state, so instead they are named
	// here and reported at startup, which is what doc-tracker.md already tracks
	// as "문서 없는 실행 코드 ⬜ 구현 제거 선행 대기".
	exemptRetiredPendingRemoval = "문서에서 폐기됨(PR #72) — doc-tracker.md 「문서 없는 실행 코드」 구현 제거 대기"
)

// toolRegistry binds each tool's handler to the pairs it exercises. Handler and
// declaration are registered together on purpose: AC1 asks that a tool
// exercising an undeclared pair be refused registration, and the cheapest way
// to guarantee that is to make it impossible to register one without the other.
var toolRegistry = map[string]toolEntry{
	"ping": {
		handle: func(*Handler, context.Context, json.RawMessage) (any, *rpcErr) {
			return toolText("pong", false), nil
		},
	},

	"namespace_list": {
		decl: toolDeclaration{pairs: []gatekeeper.Pair{{Verb: "list", Resource: "namespaces"}}},
		handle: func(h *Handler, ctx context.Context, _ json.RawMessage) (any, *rpcErr) {
			return h.namespaceList(ctx)
		},
	},

	"workload_list": {
		decl: toolDeclaration{pairs: []gatekeeper.Pair{
			{Verb: "list", Resource: "deployments"},
			{Verb: "list", Resource: "statefulsets"},
			{Verb: "list", Resource: "daemonsets"},
		}},
		handle: (*Handler).workloadList,
	},

	"workload_restart": {
		decl: toolDeclaration{
			pairs: []gatekeeper.Pair{
				{Verb: "patch", Resource: "deployments"},
				{Verb: "patch", Resource: "statefulsets"},
				{Verb: "patch", Resource: "daemonsets"},
			},
			outsideGate: exemptRetiredPendingRemoval,
		},
		handle: (*Handler).workloadRestart,
	},

	"workload_scale": {
		decl: toolDeclaration{
			// The scale subresource is not used: the implementation patches
			// spec.replicas directly, so the pair really is patch on the
			// workload. DaemonSet is rejected before any call is made.
			pairs: []gatekeeper.Pair{
				{Verb: "patch", Resource: "deployments"},
				{Verb: "patch", Resource: "statefulsets"},
			},
			outsideGate: exemptRetiredPendingRemoval,
		},
		handle: (*Handler).workloadScale,
	},

	"workload_logs": {
		decl: toolDeclaration{pairs: []gatekeeper.Pair{
			// The workload is read to resolve its pod selector before the log
			// stream is opened, so those gets belong to this tool too.
			{Verb: "get", Resource: "deployments"},
			{Verb: "get", Resource: "statefulsets"},
			{Verb: "get", Resource: "daemonsets"},
			{Verb: "list", Resource: "pods"},
			{Verb: "get", Resource: "pods/log"},
		}},
		handle: (*Handler).workloadLogs,
	},

	"pod_describe": {
		decl: toolDeclaration{pairs: []gatekeeper.Pair{
			{Verb: "get", Resource: "pods"},
			{Verb: "list", Resource: "pods"},
			{Verb: "list", Resource: "events"},
		}},
		handle: (*Handler).podDescribe,
	},

	"dear_baby_reset_user": {
		decl: toolDeclaration{
			pairs: []gatekeeper.Pair{
				{Verb: "list", Resource: "pods"},
				{Verb: "create", Resource: "pods/exec"},
			},
			outsideGate: exemptPendingOwnerDecision,
		},
		handle: (*Handler).dearBabyResetUser,
	},

	"github_app_installation_token": {handle: (*Handler).githubAppInstallationToken},
	"opensearch_search":             {handle: (*Handler).opensearchSearch},
	"opensearch_document_put":       {handle: (*Handler).opensearchDocumentPut},
	"opensearch_document_delete":    {handle: (*Handler).opensearchDocumentDelete},
	"session_read":                  {handle: (*Handler).sessionRead},
	"session_write":                 {handle: (*Handler).sessionWrite},

	"aws_config_get": {
		handle: func(h *Handler, ctx context.Context, _ json.RawMessage) (any, *rpcErr) {
			return h.awsConfigGet(ctx)
		},
	},
	"grafana_token": {
		handle: func(h *Handler, ctx context.Context, _ json.RawMessage) (any, *rpcErr) {
			return h.grafanaToken(ctx)
		},
	},
	"session_list": {
		handle: func(h *Handler, ctx context.Context, _ json.RawMessage) (any, *rpcErr) {
			return h.sessionList(ctx)
		},
	},
}

// toolEntry is a registered tool: what it does and what it is allowed to do.
type toolEntry struct {
	decl   toolDeclaration
	handle func(*Handler, context.Context, json.RawMessage) (any, *rpcErr)
}

// gatedPairs returns the declared pairs that require approval, or nil when the
// tool may run unattended. A documented exemption short-circuits the answer but
// does not erase the pairs — Exemptions still names what is passing through
// ungated.
func (d toolDeclaration) gatedPairs(sensitiveKinds []string) []gatekeeper.Pair {
	if d.outsideGate != "" {
		return nil
	}
	var gated []gatekeeper.Pair
	for _, p := range d.pairs {
		if gatedVerbs[p.Verb] || readIsSensitive(p, sensitiveKinds) {
			gated = append(gated, p)
		}
	}
	return gated
}

// readIsSensitive decides the read half of the gate: get and watch are gated
// only when the target is a sensitive kind, because a stream hands over the
// whole object exactly as a get does.
func readIsSensitive(p gatekeeper.Pair, sensitiveKinds []string) bool {
	if p.Verb != "get" && p.Verb != "watch" {
		return false
	}
	for _, kind := range sensitiveKinds {
		// RESOURCE_GATED_KINDS is written as group/version/Kind ("v1/Secret");
		// a declaration names the RBAC resource ("secrets"). Compare on the
		// trailing kind, lowercased and pluralised the way RBAC spells it.
		idx := strings.LastIndex(kind, "/")
		name := strings.ToLower(kind[idx+1:])
		if p.Resource == name+"s" || p.Resource == name {
			return true
		}
	}
	return false
}

// validateRegistry checks that the dispatchable tools and the advertised tools
// are the same set. tools/list is what a client believes it may call, so a name
// in one and not the other is either a tool nobody can reach or a tool nobody
// declared — and an undeclared tool is exactly the hole AC1 closes.
func validateRegistry(registered map[string]toolEntry, advertised []string) error {
	adv := make(map[string]bool, len(advertised))
	for _, name := range advertised {
		adv[name] = true
	}

	var undeclared, unreachable []string
	for _, name := range advertised {
		if _, ok := registered[name]; !ok {
			undeclared = append(undeclared, name)
		}
	}
	for name := range registered {
		if !adv[name] {
			unreachable = append(unreachable, name)
		}
	}
	sort.Strings(undeclared)
	sort.Strings(unreachable)

	var problems []string
	if len(undeclared) > 0 {
		problems = append(problems, fmt.Sprintf("advertised but not registered (no declaration): %s", strings.Join(undeclared, ", ")))
	}
	if len(unreachable) > 0 {
		problems = append(problems, fmt.Sprintf("registered but not advertised: %s", strings.Join(unreachable, ", ")))
	}
	for name, entry := range registered {
		if entry.handle == nil {
			problems = append(problems, fmt.Sprintf("%s has no handler", name))
		}
	}
	if len(problems) == 0 {
		return nil
	}
	sort.Strings(problems)
	return fmt.Errorf("tool registry is inconsistent: %s", strings.Join(problems, "; "))
}

// Validate reports whether the tool registry and the advertised tools/list
// agree. main calls it before listening so a mismatch stops the process rather
// than shipping a tool whose pairs nobody declared (AC1).
func Validate() error {
	advertised, err := advertisedToolNames()
	if err != nil {
		return err
	}
	return validateRegistry(toolRegistry, advertised)
}

func advertisedToolNames() ([]string, error) {
	var doc struct {
		Tools []struct {
			Name string `json:"name"`
		} `json:"tools"`
	}
	if err := json.Unmarshal([]byte(toolsListJSON), &doc); err != nil {
		return nil, fmt.Errorf("tools/list payload is not valid JSON: %w", err)
	}
	names := make([]string, 0, len(doc.Tools))
	for _, t := range doc.Tools {
		names = append(names, t.Name)
	}
	return names, nil
}

// Exemptions lists the tools that exercise a gated pair yet run ungated, with
// the document that holds them out. Nothing here is silent: main logs it at
// startup so "which state-changing calls are not behind the gate right now" has
// an answer that does not require reading this file.
func Exemptions() map[string]string {
	out := map[string]string{}
	for name, entry := range toolRegistry {
		if entry.decl.outsideGate == "" {
			continue
		}
		for _, p := range entry.decl.pairs {
			if gatedVerbs[p.Verb] {
				out[name] = entry.decl.outsideGate
				break
			}
		}
	}
	return out
}

// authorize runs the gate for one tool call. It is called by the dispatcher
// before the handler, never by a handler (AC1).
func (h *Handler) authorize(ctx context.Context, name string, entry toolEntry, rawArgs json.RawMessage) (*gatekeeper.Decision, *rpcErr) {
	gated := entry.decl.gatedPairs(h.sensitiveKinds)
	if len(gated) == 0 {
		return nil, nil
	}

	call := gatekeeper.Call{
		Tool:    name,
		Pair:    gated[0],
		Context: approvalContext(name, gated, rawArgs),
	}
	decision, err := h.gate.Authorize(ctx, call)
	if err != nil {
		slog.Warn("approval refused",
			"tool", name,
			"pairs", pairsText(gated),
			"error", err.Error(),
		)
		return nil, errf(-32603, "%s", err.Error())
	}
	if err := decision.Consume(); err != nil {
		slog.Warn("approval refused", "tool", name, "request_id", decision.RequestID, "error", err.Error())
		return nil, errf(-32603, "%s", err.Error())
	}

	// AC8: an executed gated call always leaves this record.
	slog.Info("approval granted",
		"tool", name,
		"pairs", pairsText(gated),
		"request_id", decision.RequestID,
		"external_id", decision.ExternalID,
		"processed_by_id", decision.ProcessedByID,
		"auto_approved", decision.AutoApproved,
	)
	return decision, nil
}

// approvalContext renders what the operator sees. Arguments go in verbatim
// rather than summarised (AC3).
func approvalContext(name string, gated []gatekeeper.Pair, rawArgs json.RawMessage) string {
	var b strings.Builder
	fmt.Fprintf(&b, "tool: %s\n", name)
	fmt.Fprintf(&b, "rbac: %s\n", pairsText(gated))
	fmt.Fprintf(&b, "requested at: %s\n", time.Now().UTC().Format(time.RFC3339))

	args := strings.TrimSpace(string(rawArgs))
	if args == "" || args == "null" {
		args = "(none)"
	}
	fmt.Fprintf(&b, "arguments:\n%s", args)
	return b.String()
}

func pairsText(pairs []gatekeeper.Pair) string {
	parts := make([]string, 0, len(pairs))
	for _, p := range pairs {
		parts = append(parts, p.String())
	}
	return strings.Join(parts, ", ")
}

// annotateAutoApproval marks a result that reached kubernetes without a human
// looking at it (AC9).
func annotateAutoApproval(result any, decision *gatekeeper.Decision) any {
	if decision == nil || !decision.AutoApproved {
		return result
	}
	m, ok := result.(map[string]any)
	if !ok {
		return result
	}
	notice := fmt.Sprintf("[auto-approved by gatekeeper without human review — request %s]", decision.RequestID)
	if content, ok := m["content"].([]any); ok {
		m["content"] = append(content, map[string]any{"type": "text", "text": notice})
	}
	return m
}
