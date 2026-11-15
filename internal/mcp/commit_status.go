package mcp

import (
	"context"
	"encoding/json"

	"github.com/dlddu/homelab-k3s-mcp/internal/pocketaide"
)

func (h *Handler) githubCommitStatusRequest(ctx context.Context, raw json.RawMessage) (any, *rpcErr) {
	obj, isObject := decodeObject(raw)
	if !isObject {
		return nil, errf(-32602, "arguments must be an object")
	}

	in := pocketaide.CommitStatusRequest{SessionID: sessionIDFrom(ctx)}
	for _, field := range []struct {
		name string
		dest *string
	}{
		{"repository", &in.Repository},
		{"sha", &in.SHA},
		{"state", &in.State},
		{"context", &in.Context},
		{"description", &in.Description},
		{"target_url", &in.TargetURL},
		{"requester", &in.Requester},
		{"explanation", &in.Explanation},
	} {
		value, present := obj[field.name]
		if !present || value == nil {
			continue
		}
		s, ok := value.(string)
		if !ok {
			return nil, errf(-32602, "%s must be a string", field.name)
		}
		*field.dest = s
	}

	if err := pocketaide.ValidateCommitStatusRequest(in); err != nil {
		return toolError(ctx, err), nil
	}
	ticket, err := h.pocketAide.RequestCommitStatus(ctx, in)
	if err != nil {
		return toolError(ctx, err), nil
	}
	return successResult(map[string]any{
		"id":         ticket.ID,
		"status":     ticket.Status,
		"created_at": ticket.CreatedAt,
		"message":    "Approval requested in PocketAide. Nothing is written to GitHub until the owner approves; watch the commit's statuses to see the result.",
	}), nil
}
