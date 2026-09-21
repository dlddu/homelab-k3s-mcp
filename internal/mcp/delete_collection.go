package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/dlddu/homelab-k3s-mcp/internal/gatekeeper"
	"github.com/dlddu/homelab-k3s-mcp/internal/k8s"
)

// contextNameLimit is how many target names the approval screen carries in
// full (prd-approval-gate AC3). Past it the count does the work the names were
// there for: an operator judging whether 900 is the number they meant is not
// reading 900 lines to decide.
const contextNameLimit = 20

// deleteCollectionSelection is what AC11's arguments amount to: a namespace
// and two selectors.
type deleteCollectionSelection struct {
	namespace     string
	labelSelector string
	fieldSelector string
	grace         *int64
}

// parseDeleteCollectionTarget reads AC11's one shape and refuses every
// argument that would make it something else.
//
// Both sides run it, for the reason parseDeleteTarget sets out.
//
// The namespace check is the one that matters, because a missing namespace
// does not fail safe: the apiserver reads an empty one on a collection delete
// as every namespace, so the permissive reading is the widest deletion there
// is.
func parseDeleteCollectionTarget(obj map[string]any) (deleteCollectionSelection, *rpcErr) {
	if _, present := obj["name"]; present {
		return deleteCollectionSelection{}, errf(-32602,
			"name does not apply to resource_delete_collection, which removes a selection; "+
				"deleting one named object is the delete verb and resource_delete holds it")
	}
	if _, present := obj["subresource"]; present {
		return deleteCollectionSelection{}, errf(-32602,
			"subresource does not apply to resource_delete_collection; this tool removes the objects themselves")
	}

	namespace := optionalString(obj, "namespace")
	if namespace == nil {
		return deleteCollectionSelection{}, errf(-32602,
			"namespace is required; a collection delete is confined to one namespace and "+
				"this tool has no all-namespaces path (prd-resource-generic AC11)")
	}

	sel := deleteCollectionSelection{namespace: *namespace}
	if v := optionalString(obj, "labelSelector"); v != nil {
		sel.labelSelector = *v
	}
	if v := optionalString(obj, "fieldSelector"); v != nil {
		sel.fieldSelector = *v
	}

	if v, present := obj["gracePeriodSeconds"]; present {
		n, ok := intValue(v)
		if !ok {
			return deleteCollectionSelection{}, errf(-32602, "gracePeriodSeconds must be an integer")
		}
		if n < 0 {
			return deleteCollectionSelection{}, errf(-32602, "gracePeriodSeconds must be >= 0; 0 deletes without waiting")
		}
		sel.grace = &n
	}
	return sel, nil
}

// callDeleteCollection is the gate path for a selection rather than an object.
func (h *Handler) callDeleteCollection(ctx context.Context, name string, entry toolEntry, raw json.RawMessage) (any, *rpcErr) {
	gated, err := entry.decl.gatedPairs(h.sensitiveKinds, raw)
	if err != nil {
		return nil, errf(-32602, "%s", err.Error())
	}
	if len(gated) != 1 || gated[0].Verb != "deletecollection" || entry.decl.collectionTarget == nil {
		return nil, errf(-32603, "refusing %s: a collection delete must resolve to exactly its deletecollection permission and declare a collection target", name)
	}

	ref, err := entry.decl.collectionTarget(raw)
	if err != nil {
		// AC3's closing clause: no approval request is made for a call whose
		// target cannot be named.
		return nil, errf(-32602, "%s", err.Error())
	}

	approved, err := h.gateCollectionReader.ListTargets(ctx, *ref)
	if err != nil {
		slog.Warn("approval refused", "tool", name, "pairs", pairsText(gated), "error", err.Error())
		return nil, errf(-32603, "refusing %s: the selection could not be read before approval: %s", name, err.Error())
	}

	if len(approved.Targets) == 0 {
		slog.Info("collection delete matched nothing; no approval requested",
			"tool", name, "pairs", pairsText(gated), "namespace", ref.Namespace)
		return successResult(map[string]any{
			"apiVersion":    ref.APIVersion,
			"kind":          ref.Kind,
			"namespace":     ref.Namespace,
			"labelSelector": ref.LabelSelector,
			"fieldSelector": ref.FieldSelector,
			"targets":       0,
			"accepted":      false,
			"detail":        "the selector matched no objects, so nothing was deleted and no approval was requested",
		}), nil
	}

	decision, err := h.gate.Authorize(ctx, gatekeeper.Call{
		Tool: name,
		Pair: gated[0],
		Describe: func(context.Context) (string, error) {
			return collectionApprovalContext(name, gated, raw, h.sensitiveKinds, ref, approved), nil
		},
	})
	if err != nil {
		slog.Warn("approval refused", "tool", name, "pairs", pairsText(gated), "error", err.Error())
		return nil, errf(-32603, "%s", err.Error())
	}
	if err := decision.Consume(); err != nil {
		slog.Warn("approval refused", "tool", name, "request_id", decision.RequestID, "error", err.Error())
		return nil, errf(-32603, "%s", err.Error())
	}

	if rerr := h.confirmCollectionUnchanged(ctx, name, decision, gated, ref, approved); rerr != nil {
		return nil, rerr
	}

	slog.Info("approval granted",
		"tool", name,
		"pairs", pairsText(gated),
		"targets", len(approved.Targets),
		"request_id", decision.RequestID,
		"external_id", decision.ExternalID,
		"processed_by_id", decision.ProcessedByID,
		"auto_approved", decision.AutoApproved,
	)

	result, rerr := entry.handle(h, ctx, raw)
	if rerr != nil {
		return nil, rerr
	}
	return annotateAutoApproval(result, decision), nil
}

// confirmCollectionUnchanged re-reads the selection and refuses when it is not
// the one that was approved (prd-approval-gate AC6).
//
// Growth is the case AC11 names, but shrinkage and replacement are refused on
// the same footing: a list with different names on it is a different approval.
func (h *Handler) confirmCollectionUnchanged(
	ctx context.Context,
	name string,
	decision *gatekeeper.Decision,
	gated []gatekeeper.Pair,
	ref *k8s.CollectionTargetRef,
	approved *k8s.CollectionTargetState,
) *rpcErr {
	current, err := h.gateCollectionReader.ListTargets(ctx, *ref)
	if err != nil {
		slog.Warn("approval refused", "tool", name, "request_id", decision.RequestID, "error", err.Error())
		return errf(-32603, "refusing %s: the approved selection could not be re-read before execution: %s", name, err.Error())
	}
	if sameTargets(approved.Targets, current.Targets) {
		return nil
	}

	reason := fmt.Sprintf(
		"refusing %s: the selection changed after it was approved (%d target(s) → %d); "+
			"the approval was for the earlier set, so this needs a new one",
		name, len(approved.Targets), len(current.Targets),
	)
	slog.Warn("approval refused",
		"tool", name,
		"pairs", pairsText(gated),
		"request_id", decision.RequestID,
		"error", reason,
	)
	return errf(-32603, "%s", reason)
}

// sameTargets reports whether two snapshots name the same objects in the same
// states. ListTargets sorts by name and refuses duplicates, so comparing
// position by position is comparing the sets.
func sameTargets(approved, current []k8s.CollectionTarget) bool {
	if len(approved) != len(current) {
		return false
	}
	for i := range approved {
		if approved[i] != current[i] {
			return false
		}
	}
	return true
}

// collectionApprovalContext renders what the operator sees for a collection
// delete: AC3's deletecollection row.
func collectionApprovalContext(
	name string,
	gated []gatekeeper.Pair,
	rawArgs json.RawMessage,
	sensitiveKinds []string,
	ref *k8s.CollectionTargetRef,
	state *k8s.CollectionTargetState,
) string {
	var b strings.Builder
	fmt.Fprintf(&b, "tool: %s\n", name)
	fmt.Fprintf(&b, "rbac: %s\n", pairsText(gated))
	fmt.Fprintf(&b, "requested at: %s\n", time.Now().UTC().Format(time.RFC3339))
	fmt.Fprintf(&b, "target: %s %s in namespace %s\n", ref.APIVersion, ref.Kind, ref.Namespace)
	fmt.Fprintf(&b, "labelSelector: %s\n", selectorText(ref.LabelSelector))
	fmt.Fprintf(&b, "fieldSelector: %s\n", selectorText(ref.FieldSelector))
	fmt.Fprintf(&b, "targets: %d\n", len(state.Targets))
	for i, target := range state.Targets {
		if i == contextNameLimit {
			fmt.Fprintf(&b, "  … and %d more (%d total)\n", len(state.Targets)-contextNameLimit, len(state.Targets))
			break
		}
		fmt.Fprintf(&b, "  %s/%s\n", target.Namespace, target.Name)
	}
	b.WriteString(argumentsBlock(gated, rawArgs, sensitiveKinds))
	return b.String()
}

// selectorText spells an absent selector out rather than leaving the line
// blank: "labelSelector:" with nothing after it reads as a rendering fault,
// when the fact worth conveying is that this half is unconstrained.
func selectorText(selector string) string {
	if selector == "" {
		return "(none — this half of the selection is unconstrained)"
	}
	return selector
}

// resourceDeleteCollection removes one namespace's selection
// (prd-resource-generic AC11). By the time it runs, callDeleteCollection has
// read the selection, shown it, and confirmed it had not moved.
func (h *Handler) resourceDeleteCollection(ctx context.Context, raw json.RawMessage) (any, *rpcErr) {
	obj, ok := decodeObject(raw)
	if !ok {
		return nil, errf(-32602, "arguments must be an object")
	}
	coord, rerr := parseCoordinate(obj)
	if rerr != nil {
		return nil, rerr
	}
	sel, rerr := parseDeleteCollectionTarget(obj)
	if rerr != nil {
		return nil, rerr
	}

	result, err := h.k8s.DeleteCollection(ctx, k8s.DeleteCollectionRef{
		APIVersion:         coord.apiVersion,
		Kind:               coord.kind,
		Namespace:          sel.namespace,
		LabelSelector:      sel.labelSelector,
		FieldSelector:      sel.fieldSelector,
		GracePeriodSeconds: sel.grace,
	})
	if err != nil {
		return toolError(err), nil
	}

	payload := map[string]any{
		"apiVersion":    coord.apiVersion,
		"kind":          coord.kind,
		"namespace":     result.Namespace,
		"labelSelector": result.LabelSelector,
		"fieldSelector": result.FieldSelector,
		"resource":      result.Resource,
		"accepted":      true,
	}
	if sel.grace != nil {
		payload["gracePeriodSeconds"] = *sel.grace
	}
	return map[string]any{
		"content":           []any{map[string]any{"type": "text", "text": collectionDeletionText(result, sel.grace)}},
		"structuredContent": payload,
		"isError":           false,
	}, nil
}

// collectionDeletionText says what was asked for rather than what happened,
// for the reason deletionText gives and one more: the apiserver does not
// report how many objects went, so a number here would be the gate's
// pre-approval count passed off as an observation.
func collectionDeletionText(result *k8s.DeleteCollectionResult, grace *int64) string {
	var b strings.Builder
	fmt.Fprintf(&b, "accepted for deletion: the %s selection in namespace %s", result.Resource, result.Namespace)
	if grace != nil {
		fmt.Fprintf(&b, " (gracePeriodSeconds=%d)", *grace)
	}
	b.WriteString("\nThe approval screen listed the objects this selection named at approval time.")
	b.WriteString("\nEach is removed once its grace period and finalizers are done.")
	return b.String()
}
