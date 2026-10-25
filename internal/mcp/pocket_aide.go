package mcp

import (
	"context"
	"encoding/json"

	"github.com/dlddu/homelab-k3s-mcp/internal/pocketaide"
)

func (h *Handler) pocketAideNotificationSend(ctx context.Context, raw json.RawMessage) (any, *rpcErr) {
	obj, ok := decodeObject(raw)
	if !ok {
		return nil, errf(-32602, "arguments must be an object")
	}
	var title string
	if v, present := obj["title"]; present && v != nil {
		s, ok := v.(string)
		if !ok {
			return nil, errf(-32602, "title must be a string")
		}
		title = s
	}
	var body string
	if v, present := obj["body"]; present && v != nil {
		s, ok := v.(string)
		if !ok {
			return nil, errf(-32602, "body must be a string")
		}
		body = s
	}

	if err := pocketaide.ValidateTitle(title); err != nil {
		return toolError(ctx, err), nil
	}
	res, err := h.pocketAide.Notify(ctx, title, body)
	if err != nil {
		return toolError(ctx, err), nil
	}
	return successResult(map[string]any{"devices": res.Devices, "sent": res.Sent}), nil
}
