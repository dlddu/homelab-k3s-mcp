package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/dlddu/homelab-k3s-mcp/internal/approvalgate"
)

func (h *Handler) callExternalWrite(ctx context.Context, name string, entry toolEntry, rawArgs json.RawMessage) (any, *rpcErr) {
	w := entry.decl.externalWrite
	if result, rerr := w.precheck(h, ctx, rawArgs); result != nil || rerr != nil {
		return result, rerr
	}

	decision, err := h.askGate(ctx, approvalgate.Call{
		Tool: name,
		Describe: func(context.Context) (string, error) {
			return externalWriteContext(name, w.action, rawArgs), nil
		},
	})
	if err != nil {
		slog.Warn("approval refused", "tool", name, "action", w.action, "error", err.Error())
		return nil, gateRefusal(err)
	}
	if err := decision.Consume(); err != nil {
		slog.Warn("approval refused", "tool", name, "request_id", decision.RequestID, "error", err.Error())
		return nil, errf(-32603, "%s", err.Error()).afterVerdict(decision)
	}
	slog.Info("approval granted",
		"tool", name,
		"action", w.action,
		"request_id", decision.RequestID,
		"external_id", decision.ExternalID,
		"auto_approved", decision.AutoApproved,
	)
	noteGate(ctx, decision)

	result, rerr := h.invoke(ctx, name, entry, rawArgs)
	if rerr != nil {
		return nil, rerr
	}
	return annotateAutoApproval(result, decision), nil
}

func externalWriteContext(name, action string, rawArgs json.RawMessage) string {
	var b strings.Builder
	fmt.Fprintf(&b, "tool: %s\n", name)
	fmt.Fprintf(&b, "action: %s\n", action)
	fmt.Fprintf(&b, "requested at: %s\n", time.Now().UTC().Format(time.RFC3339))
	b.WriteString(argumentsBlock(nil, rawArgs, nil))
	return b.String()
}
