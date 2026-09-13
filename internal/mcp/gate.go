package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"time"

	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/dlddu/homelab-k3s-mcp/internal/gatekeeper"
)

// gatedVerbs are the RBAC verbs that change cluster state. prd-approval-gate
// AC1 gates them without exception and judges by the verb alone — never by the
// tool's name, the patch body or the path. Looking at content would mean
// writing a classifier ("this patch is only a restart, let it through"), and
// that classifier immediately becomes the way around the gate.
var gatedVerbs = map[string]bool{
	"create":           true,
	"update":           true,
	"patch":            true,
	"delete":           true,
	"deletecollection": true,
}

// toolDeclaration is one row of the (verb, resource[/subresource]) table AC1
// requires every tool to publish. The gate reads this table, so a tool whose
// declaration disagrees with what it actually exercises makes the table lie —
// and that lie passes silently at runtime.
type toolDeclaration struct {
	// pairs is what this tool exercises against the apiserver. Empty means the
	// tool touches no kubernetes resource at all (the platform integrations).
	pairs []gatekeeper.Pair

	// resolve derives the pair from one call's own arguments, for the generic
	// tools whose resource is an argument rather than a constant. A static table
	// cannot describe them: it would have to name every kind in the cluster, or
	// name none and lie. What it may not do is consult discovery — the gate has
	// to reach its verdict before the apiserver is touched, so the resource name
	// comes from the kind by apimachinery's own guess.
	resolve func(json.RawMessage) ([]gatekeeper.Pair, error)

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
	// docs/prd-approval-gate.md, "남은 예외 1건": dear_baby_reset_user exercises
	// create on pods/exec and so meets the write-gate definition, but it is
	// held out because it addresses its target by application meaning rather
	// than by resource coordinates, and folding it in means rewriting
	// prd-dear-baby-reset-user's ACs. doc-tracker.md carries it as an open item
	// awaiting the owner's decision — this code does not decide it.
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

	"api_resources": {
		// Discovery is not a resource permission, so this tool declares no pair
		// (prd-resource-generic AC20).
		handle: func(h *Handler, ctx context.Context, _ json.RawMessage) (any, *rpcErr) {
			return h.apiResources(ctx)
		},
	},

	"resource_list": {
		decl:   toolDeclaration{resolve: genericPairs("list")},
		handle: (*Handler).resourceList,
	},

	"resource_get": {
		decl:   toolDeclaration{resolve: genericPairs("get")},
		handle: (*Handler).resourceGet,
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

// callPairs is what this call exercises: the declared constants, or the pair
// its own arguments name.
func (d toolDeclaration) callPairs(rawArgs json.RawMessage) ([]gatekeeper.Pair, error) {
	if d.resolve == nil {
		return d.pairs, nil
	}
	return d.resolve(rawArgs)
}

// gatedPairs returns the pairs of this call that require approval, or nil when
// it may run unattended. A documented exemption short-circuits the answer but
// does not erase the pairs — Exemptions still names what is passing through
// ungated.
func (d toolDeclaration) gatedPairs(sensitiveKinds []string, rawArgs json.RawMessage) ([]gatekeeper.Pair, error) {
	if d.outsideGate != "" {
		return nil, nil
	}
	pairs, err := d.callPairs(rawArgs)
	if err != nil {
		return nil, err
	}
	var gated []gatekeeper.Pair
	for _, p := range pairs {
		if gatedVerbs[p.Verb] || readIsSensitive(p, sensitiveKinds) {
			gated = append(gated, p)
		}
	}
	return gated, nil
}

// genericPairs reads one call's coordinate and reports the single pair it
// exercises. The resource name is apimachinery's guess from the kind rather
// than discovery's answer, because a sensitive read has to be refused with the
// kubernetes call count still at zero (prd-resource-generic AC16) — asking the
// cluster what "Secret" is called would already be one.
func genericPairs(verb string) func(json.RawMessage) ([]gatekeeper.Pair, error) {
	return func(rawArgs json.RawMessage) ([]gatekeeper.Pair, error) {
		obj, ok := decodeObject(rawArgs)
		if !ok {
			return nil, fmt.Errorf("arguments must be an object")
		}
		apiVersion, _ := obj["apiVersion"].(string)
		kind, _ := obj["kind"].(string)
		if apiVersion == "" || kind == "" {
			return nil, fmt.Errorf("apiVersion and kind are required before this call can be judged")
		}
		gv, err := schema.ParseGroupVersion(apiVersion)
		if err != nil {
			return nil, fmt.Errorf("apiVersion %q is not a group/version", apiVersion)
		}
		plural, _ := meta.UnsafeGuessKindToResource(gv.WithKind(kind))
		resource := plural.Resource
		if sub, _ := obj["subresource"].(string); sub != "" {
			resource += "/" + sub
		}
		return []gatekeeper.Pair{{Verb: verb, Resource: resource}}, nil
	}
}

// readIsSensitive decides the read half of the gate: get and watch are gated
// only when the target is a sensitive kind, because a stream hands over the
// whole object exactly as a get does.
func readIsSensitive(p gatekeeper.Pair, sensitiveKinds []string) bool {
	if p.Verb != "get" && p.Verb != "watch" {
		return false
	}
	// A subresource is judged by the object it hangs off: reading
	// secrets/<anything> is still reading a Secret, and matching the whole
	// string would let a subresource name walk straight past this.
	resource := p.Resource
	if idx := strings.Index(resource, "/"); idx >= 0 {
		resource = resource[:idx]
	}
	for _, kind := range sensitiveKinds {
		// RESOURCE_GATED_KINDS is written as group/version/Kind ("v1/Secret");
		// a declaration names the RBAC resource ("secrets"). Compare on the
		// trailing kind, lowercased and pluralised the way RBAC spells it.
		idx := strings.LastIndex(kind, "/")
		name := strings.ToLower(kind[idx+1:])
		if resource == name+"s" || resource == name {
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
// before the handler, never by a handler: AC1 requires that there be no code
// path in which a gated tool decides for itself whether to ask.
func (h *Handler) authorize(ctx context.Context, name string, entry toolEntry, rawArgs json.RawMessage) (*gatekeeper.Decision, *rpcErr) {
	gated, err := entry.decl.gatedPairs(h.sensitiveKinds, rawArgs)
	if err != nil {
		// A call whose pair cannot be named is a call whose gating cannot be
		// decided, and AC5 makes every undecided path a refusal.
		return nil, errf(-32602, "%s", err.Error())
	}
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

	// AC8: an executed gated call always leaves this record, so "ran without an
	// approval" cannot exist in the log.
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

// approvalContext renders what the operator sees. AC3 asks for the target and
// the verb-specific detail in full, so the arguments go in verbatim rather than
// summarised — deciding which part of a patch matters is the operator's job,
// and summarising makes the server do it for them.
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
// looking at it. AC9 keeps this out of the log alone: an operator who turned on
// AUTO_APPROVE has disabled the gate, and the tool's own answer should say so.
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
