// The session owner's side of a claude-code turn's tool approvals
// (session-platform AC-E2, 「턴 중 도구 승인」): session_tool_approvals reads
// what a running turn is waiting on, session_tool_approval_decide decides one
// of those calls.
//
// Both are thin — the control plane and the session's own agent hold the calls
// and bind each decision to the call it was taken on (id, argument hash, run
// epoch). What these handlers add is the refusal of a decision that does not
// name all three, so a half-specified decision never travels as if it were a
// decision on whatever happens to be waiting.
package mcp

import (
	"context"
	"encoding/json"

	"github.com/dlddu/homelab-k3s-mcp/internal/sessionplatform"
)

func (h *Handler) sessionToolApprovals(ctx context.Context, raw json.RawMessage) (any, *rpcErr) {
	obj, ok := decodeObject(raw)
	if !ok {
		return nil, errf(-32602, "arguments must be an object")
	}
	id := optionalString(obj, "id")
	if id == nil {
		return nil, errf(-32602, "id is required")
	}

	page, err := h.sessionPlatform.ToolApprovals(ctx, *id)
	if err != nil {
		return toolError(ctx, err), nil
	}

	approvals := make([]map[string]any, 0, len(page.Approvals))
	for _, call := range page.Approvals {
		approvals = append(approvals, map[string]any{
			"id":        call.ID,
			"tool":      call.Tool,
			"input":     call.Input,
			"argsHash":  call.ArgsHash,
			"toolUseId": call.ToolUseID,
			"epoch":     call.Epoch,
			"askedAt":   call.AskedAt,
			"expiresAt": call.ExpiresAt,
		})
	}
	return successResult(map[string]any{
		"epoch":     page.Epoch,
		"approvals": approvals,
	}), nil
}

func (h *Handler) sessionToolApprovalDecide(ctx context.Context, raw json.RawMessage) (any, *rpcErr) {
	obj, ok := decodeObject(raw)
	if !ok {
		return nil, errf(-32602, "arguments must be an object")
	}
	id := optionalString(obj, "id")
	if id == nil {
		return nil, errf(-32602, "id is required")
	}
	approvalID := optionalString(obj, "approvalId")
	if approvalID == nil {
		return nil, errf(-32602, "approvalId is required")
	}
	decision := optionalString(obj, "decision")
	if decision == nil {
		return nil, errf(-32602, "decision is required")
	}
	if *decision != sessionplatform.ToolApprovalApprove && *decision != sessionplatform.ToolApprovalDeny {
		return nil, errf(-32602, "decision must be \"approve\" or \"deny\"")
	}
	// The hash and the epoch are what bind the decision to the call that was
	// read. Missing either is a mistake, not a decision on whatever is waiting.
	argsHash := optionalString(obj, "argsHash")
	if argsHash == nil {
		return nil, errf(-32602, "argsHash is required — it names the call the decision was taken on")
	}
	epoch := optionalString(obj, "epoch")
	if epoch == nil {
		return nil, errf(-32602, "epoch is required — it names the run the call was put up in")
	}
	// The control plane's field is "reason"; the tool argument is "note"
	// because this server reserves "reason" for the approval gate (provenance.go).
	note := ""
	if v := optionalString(obj, "note"); v != nil {
		note = *v
	}

	err := h.sessionPlatform.DecideToolApproval(ctx, *id, *approvalID, sessionplatform.ToolApprovalDecision{
		Decision: *decision,
		ArgsHash: *argsHash,
		Epoch:    *epoch,
		Reason:   note,
	})
	if err != nil {
		return toolError(ctx, err), nil
	}

	return successResult(map[string]any{
		"id":       *approvalID,
		"decision": *decision,
	}), nil
}
