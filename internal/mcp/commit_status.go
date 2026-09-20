package mcp

import (
	"context"
	"encoding/json"

	"github.com/dlddu/homelab-k3s-mcp/internal/github"
)

func (h *Handler) githubCommitStatusCreate(ctx context.Context, raw json.RawMessage) (any, *rpcErr) {
	obj, isObject := decodeObject(raw)
	if !isObject {
		return nil, errf(-32602, "arguments must be an object")
	}

	in := github.CommitStatusInput{}
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

	status, err := h.github.CreateCommitStatus(ctx, in)
	if err != nil {
		return toolError(err), nil
	}

	payload := map[string]any{
		"id":          status.ID,
		"state":       status.State,
		"context":     status.Context,
		"sha":         status.SHA,
		"description": status.Description,
		"target_url":  status.TargetURL,
		"created_at":  status.CreatedAt,
	}
	return map[string]any{
		"content":           []any{map[string]any{"type": "text", "text": prettyJSON(payload)}},
		"structuredContent": payload,
		"isError":           false,
	}, nil
}
