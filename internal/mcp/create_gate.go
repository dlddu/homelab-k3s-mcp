package mcp

import (
	"context"
	"encoding/json"
	"log/slog"

	"github.com/dlddu/homelab-k3s-mcp/internal/gatekeeper"
	"github.com/dlddu/homelab-k3s-mcp/internal/k8s"
)

func (h *Handler) callCreateBatch(ctx context.Context, name string, entry toolEntry, raw json.RawMessage) (any, *rpcErr) {
	docs, rerr := parseCreateDocuments(raw)
	if rerr != nil {
		return nil, rerr
	}
	decisions := make([]*gatekeeper.Decision, 0, len(docs))
	seenRequests := map[string]bool{}
	seenExternal := map[string]bool{}
	for _, doc := range docs {
		pairs, err := entry.decl.gatedPairs(h.sensitiveKinds, doc.raw)
		if err != nil || len(pairs) != 1 || pairs[0] != doc.pair || pairs[0].Verb != "create" || entry.decl.target != nil {
			return nil, errf(-32603, "create must declare exactly its document's create permission and no existing target")
		}
		decision, err := h.askGate(ctx, gatekeeper.Call{
			Tool: name,
			Pair: doc.pair,
			Describe: func(context.Context) (string, error) {
				ref := &k8s.TargetRef{APIVersion: doc.ref.APIVersion, Kind: doc.ref.Kind, Namespace: doc.ref.Namespace, Name: doc.ref.Name}
				return approvalContext(name, pairs, doc.raw, h.sensitiveKinds, ref, nil), nil
			},
		})
		if err != nil {
			slog.Warn("approval refused", "tool", name, "pairs", pairsText(pairs), "error", err.Error())
			return nil, gateRefusal(err)
		}
		if decision == nil || decision.RequestID == "" || seenRequests[decision.RequestID] ||
			(decision.ExternalID != "" && seenExternal[decision.ExternalID]) {
			return nil, errf(-32603, "create requires a distinct approval for every document")
		}
		seenRequests[decision.RequestID] = true
		seenExternal[decision.ExternalID] = true
		decisions = append(decisions, decision)
	}

	created := []any{}
	var failed any
	unattempted := []any{}
	executed := []*gatekeeper.Decision{}
	for i, doc := range docs {
		var failure string
		if err := ctx.Err(); err != nil {
			failure = "create execution cancelled before this document was attempted"
		} else if err := decisions[i].Consume(); err != nil {
			failure = err.Error()
		} else {
			d := decisions[i]
			slog.Info("approval granted", "tool", name, "pairs", pairsText([]gatekeeper.Pair{doc.pair}),
				"request_id", d.RequestID, "external_id", d.ExternalID,
				"processed_by_id", d.ProcessedByID, "auto_approved", d.AutoApproved)
			executed = append(executed, d)
			noteGate(ctx, d)
			result, callErr := h.invoke(ctx, name, entry, doc.raw)
			if callErr != nil {
				failure = callErr.message
			} else if m, ok := result.(map[string]any); !ok {
				failure = "create returned an invalid result; inspect the target before retrying"
			} else if m["isError"] == true {
				failure = "create failed"
				if content, ok := m["content"].([]any); ok && len(content) > 0 {
					if item, ok := content[0].(map[string]any); ok {
						if message, ok := item["text"].(string); ok {
							failure = message
						}
					}
				}
			}
		}
		if failure != "" {
			coordinate := createCoordinate(doc.ref)
			coordinate["error"] = failure
			failed = coordinate
			for _, remaining := range docs[i+1:] {
				unattempted = append(unattempted, createCoordinate(remaining.ref))
			}
			break
		}
		created = append(created, createCoordinate(doc.ref))
	}
	result := successResult(map[string]any{"created": created, "failed": failed, "unattempted": unattempted}).(map[string]any)
	result["isError"] = failed != nil
	for _, decision := range executed {
		annotateAutoApproval(result, decision)
	}
	return result, nil
}
