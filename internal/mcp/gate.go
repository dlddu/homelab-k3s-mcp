package mcp

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"time"

	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/dlddu/homelab-k3s-mcp/internal/gatekeeper"
	"github.com/dlddu/homelab-k3s-mcp/internal/k8s"
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

	// resolve derives the pair from one call's own arguments, for the generic
	// tools whose resource is an argument rather than a constant. A static table
	// cannot describe them: it would have to name every kind in the cluster, or
	// name none and lie. What it may not do is consult discovery — the gate has
	// to reach its verdict before the apiserver is touched, so the resource name
	// comes from the kind by apimachinery's own guess.
	resolve func(json.RawMessage) ([]gatekeeper.Pair, error)

	// target derives the object this call addresses, so the gate can read it
	// before describing it (AC3's verb detail, AC6's precondition). It is
	// separate from resolve because the two answer different questions at
	// different times: resolve names a permission and must not touch the
	// cluster, target names an object and is read from only once the gate knows
	// it has someone to ask.
	//
	// Existing-object writes require this at authorize. The create batch uses
	// the absent-target exception in prd-approval-gate AC6 (callCreateBatch).
	target func(json.RawMessage) (*k8s.TargetRef, error)

	// collectionTarget is target's plural: it derives the selection a
	// deletecollection call addresses. A second field rather than a shape
	// target could return, because the two are two permissions — `get on
	// ⟨kind⟩` and `list on ⟨kind⟩` — and a declaration sets one, never both.
	collectionTarget func(json.RawMessage) (*k8s.CollectionTargetRef, error)

	// outsideGate, when non-empty, is the documented reason this tool stays
	// outside the gate even though its pairs would otherwise select it. It is
	// never a judgement made here: each value cites the document that made it.
	// An undocumented exemption is the bypass AC1 forbids, so this field is
	// deliberately a sentence rather than a bool.
	outsideGate string

	// alwaysGated is outsideGate's mirror: the documented reason every pair of
	// this tool needs approval even when the verb alone would not select it.
	// gatedVerbs judges by verb because for almost every tool the verb is what
	// the call does — but AC15 makes resource_proxy's verb follow the HTTP
	// method, so a GET there is not a read of an object, it is an arbitrary
	// request to whatever the object serves. A sentence rather than a bool for
	// the same reason outsideGate is one: widening the gate is a decision the
	// documents make, and this field is where the code cites them.
	alwaysGated string
}

// The one reason any tool is currently exempt. It comes from the documents,
// not from this package.
const (
	// docs/prd-approval-gate.md, "남은 예외 1건" — the reason and its open state
	// live there; this code only cites them.
	exemptPendingOwnerDecision = "prd-approval-gate 「남은 예외 1건」 — 소유자 판단 대기 (doc-tracker.md 미결)"

	// docs/prd-resource-generic.md AC15, and the same sentence in
	// prd-approval-gate's write-gate table: "메서드와 무관하게 전부 게이트
	// 대상이다". The pair a proxy call spends does not say what it does — only
	// the path does — so there is no method whose approval can be skipped.
	gatedEveryMethodByAC15 = "prd-resource-generic AC15 — 경로가 호출을 한정하므로 메서드와 무관하게 전부 게이트 대상"
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
		decl:   toolDeclaration{resolve: genericPairs("get"), target: genericTarget()},
		handle: (*Handler).resourceGet,
	},

	"resource_watch": {
		decl:   toolDeclaration{resolve: genericPairs("watch")},
		handle: (*Handler).resourceWatch,
	},

	"resource_update": {
		decl:   toolDeclaration{resolve: updatePairs(), target: genericTarget()},
		handle: (*Handler).resourceUpdate,
	},

	"resource_create": {
		decl:        toolDeclaration{resolve: createPairs},
		handle:      (*Handler).resourceCreate,
		createBatch: true,
	},

	"resource_patch": {
		decl:   toolDeclaration{resolve: genericPairs("patch"), target: genericTarget()},
		handle: (*Handler).resourcePatch,
	},

	"resource_delete": {
		decl:   toolDeclaration{resolve: deletePairs(), target: genericTarget()},
		handle: (*Handler).resourceDelete,
	},

	"resource_delete_collection": {
		decl: toolDeclaration{
			resolve:          deleteCollectionPairs(),
			collectionTarget: genericCollectionTarget(),
		},
		handle:         (*Handler).resourceDeleteCollection,
		collectionGate: true,
	},

	"resource_exec": {
		decl:   toolDeclaration{resolve: execPairs(), target: genericTarget()},
		handle: (*Handler).resourceExec,
	},

	"resource_attach": {
		decl:   toolDeclaration{resolve: attachPairs(), target: genericTarget()},
		handle: (*Handler).resourceAttach,
	},

	"resource_port_forward": {
		decl:   toolDeclaration{resolve: portForwardPairs(), target: genericTarget()},
		handle: (*Handler).resourcePortForward,
	},

	"resource_proxy": {
		decl: toolDeclaration{
			resolve:     proxyPairs(),
			target:      genericTarget(),
			alwaysGated: gatedEveryMethodByAC15,
		},
		handle: (*Handler).resourceProxy,
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
	"github_commit_status_create":   {handle: (*Handler).githubCommitStatusCreate},
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
	decl        toolDeclaration
	handle      func(*Handler, context.Context, json.RawMessage) (any, *rpcErr)
	createBatch bool

	// collectionGate routes this tool through callDeleteCollection instead of
	// authorize. Like createBatch it marks an approval that does not fit the
	// one-object-one-read shape authorize is built around: this one reads
	// before deciding whether to ask at all.
	collectionGate bool
}

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
	if d.alwaysGated != "" {
		return pairs, nil
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

// updatePairs is genericPairs("update") with AC8's own argument checks run
// first. They cannot wait for the handler: authorize runs before it, so a call
// the handler would reject has already cost an approval request by then, and
// scenario 8 asks for a negative or missing replicas to be refused without one
// ever being made. Pair resolution is the only thing that runs earlier, and a
// resolve error is already a refusal (authorize turns it into -32602).
func updatePairs() func(json.RawMessage) ([]gatekeeper.Pair, error) {
	generic := genericPairs("update")
	return func(rawArgs json.RawMessage) ([]gatekeeper.Pair, error) {
		obj, ok := decodeObject(rawArgs)
		if !ok {
			return nil, fmt.Errorf("arguments must be an object")
		}
		if _, _, rerr := parseUpdateTarget(obj); rerr != nil {
			return nil, errors.New(rerr.message)
		}
		return generic(rawArgs)
	}
}

// deletePairs is genericPairs("delete") with AC10's own argument checks run
// first, for the reason updatePairs states: authorize runs before the handler,
// so a call the handler would reject has already cost an approval request by
// then. Scenario 10 wants a selector-only call refused at argument validation,
// and pair resolution is the only thing that runs earlier (a resolve error is
// already a refusal — authorize turns it into -32602).
func deletePairs() func(json.RawMessage) ([]gatekeeper.Pair, error) {
	generic := genericPairs("delete")
	return func(rawArgs json.RawMessage) ([]gatekeeper.Pair, error) {
		obj, ok := decodeObject(rawArgs)
		if !ok {
			return nil, fmt.Errorf("arguments must be an object")
		}
		if _, _, rerr := parseDeleteTarget(obj); rerr != nil {
			return nil, errors.New(rerr.message)
		}
		return generic(rawArgs)
	}
}

// deleteCollectionPairs is genericPairs("deletecollection") with AC11's own
// argument checks run first, for the reason updatePairs states — and with more
// at stake: the argument this one rejects is a missing namespace, which is the
// widest deletion the tool could be asked to approve.
func deleteCollectionPairs() func(json.RawMessage) ([]gatekeeper.Pair, error) {
	generic := genericPairs("deletecollection")
	return func(rawArgs json.RawMessage) ([]gatekeeper.Pair, error) {
		obj, ok := decodeObject(rawArgs)
		if !ok {
			return nil, fmt.Errorf("arguments must be an object")
		}
		if _, rerr := parseDeleteCollectionTarget(obj); rerr != nil {
			return nil, errors.New(rerr.message)
		}
		return generic(rawArgs)
	}
}

// genericCollectionTarget reads the selection coordinate out of one call's
// arguments. It refuses the mirror image of what genericTarget refuses: there
// a call without a name cannot be described, here one without a namespace
// cannot be bounded.
func genericCollectionTarget() func(json.RawMessage) (*k8s.CollectionTargetRef, error) {
	return func(rawArgs json.RawMessage) (*k8s.CollectionTargetRef, error) {
		obj, ok := decodeObject(rawArgs)
		if !ok {
			return nil, fmt.Errorf("arguments must be an object")
		}
		sel, rerr := parseDeleteCollectionTarget(obj)
		if rerr != nil {
			return nil, errors.New(rerr.message)
		}
		apiVersion, _ := obj["apiVersion"].(string)
		kind, _ := obj["kind"].(string)
		if apiVersion == "" || kind == "" {
			return nil, fmt.Errorf("apiVersion and kind are required before this call can be described for approval")
		}
		return &k8s.CollectionTargetRef{
			APIVersion:    apiVersion,
			Kind:          kind,
			Namespace:     sel.namespace,
			LabelSelector: sel.labelSelector,
			FieldSelector: sel.fieldSelector,
		}, nil
	}
}

// readIsSensitive decides the read half of the gate: get and watch are gated
// only when the target is a sensitive kind, because a stream hands over the
// whole object exactly as a get does.
func readIsSensitive(p gatekeeper.Pair, sensitiveKinds []string) bool {
	if p.Verb != "get" && p.Verb != "watch" {
		return false
	}
	return resourceIsSensitive(p, sensitiveKinds)
}

// resourceIsSensitive answers the kind half of that question on its own, with
// no verb attached. Two callers need it and they need different verbs: the read
// gate asks about get and watch, and the context masker asks about writes.
// Splitting it keeps one normalisation rather than two that can drift.
func resourceIsSensitive(p gatekeeper.Pair, sensitiveKinds []string) bool {
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

// gateTargetKind is how AC11's table spells the resource of the gate's own two
// pairs. It stays a placeholder rather than a list of kinds for the reason
// genericPairs gives: the kind is whatever the gated call names, and a static
// list would either enumerate the cluster or lie about it.
const gateTargetKind = "⟨kind⟩"

// GatePairs reports the kubernetes permissions the gate exercises on its own
// behalf, before any verdict exists (prd-approval-gate AC11).
//
// AC19 of prd-resource-generic used to compare this against rbac.yaml and is
// now a결번, so nothing machine-checks the list any more. It is still published
// because the question it answers — what does the gate read before it asks
// anyone — is one a reviewer has to be able to answer without reading this
// file, and main prints it at startup beside Exemptions.
func GatePairs() []gatekeeper.Pair {
	return []gatekeeper.Pair{
		{Verb: "get", Resource: gateTargetKind},
		{Verb: "list", Resource: gateTargetKind},
	}
}

// authorize runs the gate for one tool call. It is called by the dispatcher
// before the handler, never by a handler (AC1).
func (h *Handler) authorize(ctx context.Context, name string, entry toolEntry, rawArgs json.RawMessage) (*gatekeeper.Decision, *k8s.TargetState, *rpcErr) {
	gated, err := entry.decl.gatedPairs(h.sensitiveKinds, rawArgs)
	if err != nil {
		// A call whose pair cannot be named is a call whose gating cannot be
		// decided, and AC5 makes every undecided path a refusal.
		return nil, nil, errf(-32602, "%s", err.Error())
	}
	if len(gated) == 0 {
		return nil, nil, nil
	}
	if entry.decl.resolve != nil && entry.decl.target == nil && changesState(gated) {
		// Fail closed rather than approve blind, over exactly the set where
		// "no target" means "a target was forgotten": a tool that resolves its
		// coordinate per call is addressing one named object, and a write to one
		// has no substitute for AC6's precondition.
		return nil, nil, errf(-32603, "refusing %s: it resolves a coordinate per call and changes state, but declares no target for the gate to read, so the approval could not be tied to a state (prd-approval-gate AC6)", name)
	}

	// approved holds what the gate read while the operator was deciding. AC6
	// compares against it once, just before the handler runs.
	var approved *k8s.TargetState
	var ref *k8s.TargetRef

	call := gatekeeper.Call{
		Tool: name,
		Pair: gated[0],
		Describe: func(ctx context.Context) (string, error) {
			var err error
			ref, approved, err = h.readGateTarget(ctx, entry.decl, gated, rawArgs)
			if err != nil {
				return "", err
			}
			return approvalContext(name, gated, rawArgs, h.sensitiveKinds, ref, approved), nil
		},
	}
	decision, err := h.gate.Authorize(ctx, call)
	if err != nil {
		slog.Warn("approval refused",
			"tool", name,
			"pairs", pairsText(gated),
			"error", err.Error(),
		)
		return nil, nil, errf(-32603, "%s", err.Error())
	}
	if err := decision.Consume(); err != nil {
		slog.Warn("approval refused", "tool", name, "request_id", decision.RequestID, "error", err.Error())
		return nil, nil, errf(-32603, "%s", err.Error())
	}

	// AC6: the approval was for that object in that state. This is the last
	// point before the handler runs, so it is where "still that state?" is
	// asked.
	if rerr := h.confirmTargetUnchanged(ctx, name, decision, gated, ref, approved); rerr != nil {
		return nil, nil, rerr
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
	return decision, approved, nil
}

type approvedTargetKey struct{}

// readGateTarget reads the object a gated call addresses, on the gate's own
// permission (AC11). It returns (nil, nil, nil) for a declaration with no
// target: the tools whose pairs are constants address no object this layer can
// name, and inventing one would be worse than describing the call from its
// arguments alone.
func (h *Handler) readGateTarget(ctx context.Context, decl toolDeclaration, gated []gatekeeper.Pair, rawArgs json.RawMessage) (*k8s.TargetRef, *k8s.TargetState, error) {
	if decl.target == nil {
		return nil, nil, nil
	}
	ref, err := decl.target(rawArgs)
	if err != nil {
		// AC3's closing clause: no approval request is made for a call whose
		// target cannot be named. Authorize turns this into a refusal before it
		// POSTs anything.
		return nil, nil, err
	}

	// AC11's exception. The kind decides this, not the verb: a Secret read
	// whole to learn its resourceVersion has leaked it whether the call that
	// prompted the read was a get or a patch.
	ref.MetadataOnly = resourceIsSensitive(gated[0], h.sensitiveKinds)

	state, err := h.gateReader.ReadTarget(ctx, *ref)
	if err != nil {
		return nil, nil, err
	}
	return ref, state, nil
}

// confirmTargetUnchanged re-reads the target and refuses when it has moved
// since the approval was written (AC6).
//
// A read cannot protect the interval after it returns. Update carries the
// approved version into its PUT as well; other verbs still need their own
// atomic conditions (doc-tracker.md).
func (h *Handler) confirmTargetUnchanged(ctx context.Context, name string, decision *gatekeeper.Decision, gated []gatekeeper.Pair, ref *k8s.TargetRef, approved *k8s.TargetState) *rpcErr {
	if ref == nil || approved == nil {
		return nil
	}
	current, err := h.gateReader.ReadTarget(ctx, *ref)
	if err != nil {
		slog.Warn("approval refused", "tool", name, "request_id", decision.RequestID, "error", err.Error())
		return errf(-32603, "refusing %s: the approved target could not be re-read before execution: %s", name, err.Error())
	}
	if current.ResourceVersion == approved.ResourceVersion && current.UID == approved.UID {
		return nil
	}

	reason := fmt.Sprintf(
		"refusing %s: %s changed after it was approved "+
			"(resourceVersion %s → %s); the approval was for the earlier state, so this needs a new one",
		name, targetText(ref), approved.ResourceVersion, current.ResourceVersion,
	)
	slog.Warn("approval refused",
		"tool", name,
		"pairs", pairsText(gated),
		"request_id", decision.RequestID,
		"error", reason,
	)
	return errf(-32603, "%s", reason)
}

// approvalContext renders what the operator sees. Arguments go in verbatim
// rather than summarised (AC3), with two exceptions the same AC names: a write
// to a sensitive kind has its values masked first, and a verb whose detail
// cannot be read off the arguments gets that detail from the target.
func approvalContext(name string, gated []gatekeeper.Pair, rawArgs json.RawMessage, sensitiveKinds []string, ref *k8s.TargetRef, state *k8s.TargetState) string {
	var b strings.Builder
	fmt.Fprintf(&b, "tool: %s\n", name)
	fmt.Fprintf(&b, "rbac: %s\n", pairsText(gated))
	fmt.Fprintf(&b, "requested at: %s\n", time.Now().UTC().Format(time.RFC3339))
	if ref != nil {
		fmt.Fprintf(&b, "target: %s\n", targetText(ref))
	}
	if state != nil {
		fmt.Fprintf(&b, "target resourceVersion: %s\n", state.ResourceVersion)
	}
	if detail := replicaChange(rawArgs, ref, state); detail != "" {
		// AC3 asks update(subresource=scale) for "현재 → 목표 레플리카". The
		// arguments carry only the target, so this line is the one piece of the
		// approval screen that cannot come from listing them verbatim: "3 → 10"
		// and "9 → 10" are the same request and different decisions.
		fmt.Fprintf(&b, "replicas: %s\n", detail)
	}
	if detail := proxyPathDetail(name, rawArgs); detail != "" {
		// AC3 asks proxy for "HTTP 메서드, 대상 종류·이름, 경로와 본문 전문".
		// The arguments below carry all four already; what this line adds is
		// the one thing listing them cannot — that this particular path is one
		// of kubelet's high-power endpoints. AC3 says to mark it, not to block
		// it, so this is a line on a screen and nothing else.
		fmt.Fprintf(&b, "proxy: %s\n", detail)
	}

	b.WriteString(argumentsBlock(gated, rawArgs, sensitiveKinds))
	return b.String()
}

// kubeletHighPowerPaths are the six prd-approval-gate AC3 names. They are not a
// denylist — nothing consults them to refuse a call — and they are not
// exhaustive of what a kubelet serves either. They are the endpoints an operator
// most needs to notice before pressing approve, and the marking exists because
// every one of them arrives wearing the same pair as /healthz.
var kubeletHighPowerPaths = []string{
	"/exec", "/attach", "/portForward", "/run", "/logs", "/containerLogs",
}

// proxyPathDetail renders the proxy line of an approval context, and nothing at
// all for every other tool. It reads the arguments rather than the target ref
// because the path is not part of any coordinate: the ref says which object is
// being reached and the path says what is being asked of it, and AC15's whole
// argument is that only the second one limits the call.
func proxyPathDetail(name string, rawArgs json.RawMessage) string {
	if name != "resource_proxy" {
		return ""
	}
	obj, ok := decodeObject(rawArgs)
	if !ok {
		return ""
	}
	args, rerr := parseProxyTarget(obj)
	if rerr != nil {
		// An unreadable call never reaches here — authorize resolves the pair
		// through the same parser first, and a refusal there is returned before
		// any approval is requested (AC3's "상세를 만들 수 없으면 거부").
		return ""
	}
	detail := fmt.Sprintf("%s %s", args.method, args.path)
	if kind, _ := obj["kind"].(string); strings.EqualFold(kind, "Node") {
		if endpoint := kubeletHighPowerEndpoint(args.path); endpoint != "" {
			detail += fmt.Sprintf(
				"  ⚠️ kubelet high-power endpoint (%s) — this reaches the node directly, not the apiserver's view of it",
				endpoint,
			)
		}
	}
	return detail
}

// kubeletHighPowerEndpoint reports which of AC3's six a path is, or "" for the
// rest. The match is on the first segment and is case-insensitive, because
// kubelet answers /portForward and /portforward alike and a marking that only
// fired on one spelling would be a marking an operator learns not to trust.
func kubeletHighPowerEndpoint(path string) string {
	head := path
	if idx := strings.IndexAny(strings.TrimPrefix(path, "/"), "/?"); idx >= 0 {
		head = "/" + strings.TrimPrefix(path, "/")[:idx]
	}
	for _, candidate := range kubeletHighPowerPaths {
		if strings.EqualFold(head, candidate) {
			return candidate
		}
	}
	return ""
}

// argumentsBlock renders the verbatim-arguments tail every approval context
// ends with. Shared rather than copied so the two context builders cannot
// drift on whether a Secret write gets masked.
func argumentsBlock(gated []gatekeeper.Pair, rawArgs json.RawMessage, sensitiveKinds []string) string {
	args := strings.TrimSpace(string(rawArgs))
	if args == "" || args == "null" {
		args = "(none)"
	} else if writesASensitiveKind(gated, sensitiveKinds) {
		args = maskCredentialValues(rawArgs)
	}
	return fmt.Sprintf("arguments:\n%s", args)
}

// targetText writes a coordinate the way AC3 asks for it — apiVersion, kind,
// namespace, name — in one line.
func targetText(ref *k8s.TargetRef) string {
	coordinate := ref.Kind
	if ref.Subresource != "" {
		coordinate += "/" + ref.Subresource
	}
	place := ref.Name
	if ref.Namespace != nil {
		place = *ref.Namespace + "/" + ref.Name
	}
	return fmt.Sprintf("%s %s %s", ref.APIVersion, coordinate, place)
}

// replicaChange renders "current → target" for a scale update, and nothing at
// all for every other call. An unreadable current count is reported as unknown
// rather than omitted: "the server could not tell you" and "this field does not
// apply here" are different facts, and only the first of them should give an
// operator pause.
func replicaChange(rawArgs json.RawMessage, ref *k8s.TargetRef, state *k8s.TargetState) string {
	if ref == nil || ref.Subresource != k8s.ScaleSubresource {
		return ""
	}
	obj, ok := decodeObject(rawArgs)
	if !ok {
		return ""
	}
	_, want, rerr := parseUpdateTarget(obj)
	if rerr != nil {
		return ""
	}
	if state == nil || state.Replicas == nil {
		return fmt.Sprintf("(current unknown) → %d", want)
	}
	return fmt.Sprintf("%d → %d", *state.Replicas, want)
}

// genericTarget reads the object coordinate out of one call's arguments. It
// shares decodeObject with genericPairs but not its tolerance: a pair can be
// named from apiVersion and kind alone, while an object cannot be read without
// a name, and AC3 would rather refuse than describe a call it cannot address.
func genericTarget() func(json.RawMessage) (*k8s.TargetRef, error) {
	return func(rawArgs json.RawMessage) (*k8s.TargetRef, error) {
		obj, ok := decodeObject(rawArgs)
		if !ok {
			return nil, fmt.Errorf("arguments must be an object")
		}
		apiVersion, _ := obj["apiVersion"].(string)
		kind, _ := obj["kind"].(string)
		nameValue, _ := obj["name"].(string)
		if apiVersion == "" || kind == "" {
			return nil, fmt.Errorf("apiVersion and kind are required before this call can be described for approval")
		}
		// Worded to match the handlers' own refusal. The gate now reaches this
		// check first for a gated call, and an operator should not get a
		// different sentence about the same missing argument depending on which
		// layer noticed. The handlers keep their copy: an ungated call never
		// passes through here at all.
		if nameValue == "" {
			return nil, fmt.Errorf("name is required; the approval screen has to name the object it asks about")
		}
		ref := &k8s.TargetRef{APIVersion: apiVersion, Kind: kind, Name: nameValue}
		if sub, _ := obj["subresource"].(string); sub != "" {
			ref.Subresource = sub
		}
		if ns, ok := obj["namespace"].(string); ok && ns != "" {
			ref.Namespace = &ns
		}
		return ref, nil
	}
}

// changesState reports whether any of this call's gated pairs is a write. The
// question is asked of the resolved pairs rather than the declaration because a
// generic tool's verb is the same for every call while its kind is not, and it
// is the verb that decides this.
func changesState(gated []gatekeeper.Pair) bool {
	for _, p := range gated {
		if gatedVerbs[p.Verb] {
			return true
		}
	}
	return false
}

// writesASensitiveKind reports whether this call carries credential values into
// the server. Reads of a sensitive kind carry only a coordinate, so the mask has
// nothing to do there; writes of an ordinary kind keep their body in full,
// because AC16 rests the distinction on whether the content is a credential
// rather than on what the call does.
func writesASensitiveKind(gated []gatekeeper.Pair, sensitiveKinds []string) bool {
	for _, p := range gated {
		if gatedVerbs[p.Verb] && resourceIsSensitive(p, sensitiveKinds) {
			return true
		}
	}
	return false
}

// credentialFields are the two places a kubernetes object carries values that
// must not reach the approval screen (prd-approval-gate AC3, AC10).
var credentialFields = map[string]bool{"data": true, "stringData": true}

// withheld replaces the whole argument blob when masking cannot be carried out.
// Refusing to render is the safe direction: a blob whose credential fields
// cannot be located is a blob that may be all credential.
const withheld = `"(withheld — arguments could not be parsed, so the credential fields in them could not be located)"`

// maskCredentialValues rewrites a sensitive-kind write so the operator sees
// which keys change and by how much without seeing the values themselves. AC3
// asks for "key name and byte count" because a value on the approval screen has
// already leaked whether or not the operator then clicks reject.
func maskCredentialValues(rawArgs json.RawMessage) string {
	dec := json.NewDecoder(strings.NewReader(string(rawArgs)))
	dec.UseNumber()
	var args map[string]any
	if err := dec.Decode(&args); err != nil {
		return withheld
	}

	// A JSON patch carries its value beside a path rather than under a "data"
	// key, so the tree walk below cannot see it. That shape is handled first,
	// from the declared patchType rather than by guessing at array contents.
	if patchType, _ := args["patchType"].(string); patchType == "json" {
		maskJSONPatchOps(args["patch"])
	}
	maskCredentialTree(args)

	out, err := json.MarshalIndent(args, "", "  ")
	if err != nil {
		return withheld
	}
	return string(out)
}

// maskCredentialTree walks the arguments and masks every data/stringData map it
// finds, wherever it sits. The field is looked for at any depth rather than at
// the one place a manifest puts it: the same argument shape reaches here from
// create, update and patch, and each nests it differently.
func maskCredentialTree(node any) {
	switch v := node.(type) {
	case map[string]any:
		for key, value := range v {
			if credentialFields[key] {
				if entries, ok := value.(map[string]any); ok {
					v[key] = maskedEntries(entries, key == "data")
					continue
				}
				v[key] = maskedValue(value, key == "data")
				continue
			}
			maskCredentialTree(value)
		}
	case []any:
		for _, item := range v {
			maskCredentialTree(item)
		}
	}
}

// maskJSONPatchOps masks the value of every RFC 6902 operation that addresses a
// credential field, including the whole-map form ("path": "/data").
func maskJSONPatchOps(node any) {
	ops, ok := node.([]any)
	if !ok {
		return
	}
	for _, item := range ops {
		op, ok := item.(map[string]any)
		if !ok {
			continue
		}
		path, _ := op["path"].(string)
		field, rest, found := credentialPath(path)
		if !found {
			continue
		}
		value, present := op["value"]
		if !present {
			continue
		}
		if rest == "" {
			if entries, ok := value.(map[string]any); ok {
				op["value"] = maskedEntries(entries, field == "data")
				continue
			}
		}
		op["value"] = maskedValue(value, field == "data")
	}
}

// credentialPath splits a JSON pointer into the credential field it addresses
// and whatever follows it, reporting whether it addresses one at all.
func credentialPath(path string) (field, rest string, found bool) {
	for name := range credentialFields {
		if path == "/"+name {
			return name, "", true
		}
		if strings.HasPrefix(path, "/"+name+"/") {
			return name, path[len(name)+2:], true
		}
	}
	return "", "", false
}

func maskedEntries(entries map[string]any, encoded bool) map[string]any {
	masked := make(map[string]any, len(entries))
	for key, value := range entries {
		masked[key] = maskedValue(value, encoded)
	}
	return masked
}

// maskedValue reports a value's size in place of the value. Secret.data is
// base64 on the wire, so its length is decoded first — the operator is judging
// "is that the size of the token I meant to rotate", and the encoded length
// answers a different question.
func maskedValue(value any, encoded bool) string {
	text, ok := value.(string)
	if !ok {
		return "(masked)"
	}
	size := len(text)
	if encoded {
		if raw, err := base64.StdEncoding.DecodeString(text); err == nil {
			size = len(raw)
		}
	}
	return fmt.Sprintf("(masked, %dB)", size)
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
