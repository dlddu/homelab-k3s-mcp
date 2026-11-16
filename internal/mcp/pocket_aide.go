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
	var requester string
	if v, present := obj["requester"]; present && v != nil {
		s, ok := v.(string)
		if !ok {
			return nil, errf(-32602, "requester must be a string")
		}
		requester = s
	}

	if err := pocketaide.ValidateTitle(title); err != nil {
		return toolError(ctx, err), nil
	}
	if err := pocketaide.ValidateRequester(requester); err != nil {
		return toolError(ctx, err), nil
	}
	res, err := h.pocketAide.Notify(ctx, title, body, requester)
	if err != nil {
		return toolError(ctx, err), nil
	}
	return successResult(map[string]any{"devices": res.Devices, "sent": res.Sent}), nil
}

func (h *Handler) pocketAideChatPresenceGet(ctx context.Context, raw json.RawMessage) (any, *rpcErr) {
	if obj, ok := decodeObject(raw); !ok {
		return nil, errf(-32602, "arguments must be an object")
	} else if len(obj) > 0 {
		// The subject is PocketAide's caller registration, so there is nothing
		// to pass; a caller naming a user is asking for something this tool
		// cannot do, and silently ignoring it would read as if it had.
		return nil, errf(-32602, "pocket_aide_chat_presence_get takes no arguments")
	}

	res, err := h.pocketAide.ChatPresence(ctx)
	if err != nil {
		return toolError(ctx, err), nil
	}
	out := map[string]any{"present": res.Present, "ttl_seconds": res.TTLSeconds}
	if res.LastSeenAt != nil {
		out["last_seen_at"] = *res.LastSeenAt
	} else {
		out["last_seen_at"] = nil
	}
	return successResult(out), nil
}
