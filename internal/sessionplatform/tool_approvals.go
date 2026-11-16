// A claude-code turn's tool calls, waiting on the session owner (session-platform
// AC-E2, 「턴 중 도구 승인」). A write that asked for them (WriteOptions.ToolApprovals)
// queues a turn whose tool calls stop for a decision; this file is how the owner
// reads what is waiting and decides it.
//
// The decision names the exact call it was taken on — the request id, the hash
// of the arguments the model asked for, and the epoch of the run that put it up.
// The control plane and the session's agent bind it to all three, so a decision
// taken on arguments that have since changed, or on an earlier run of the
// session, is refused rather than applied to whatever is waiting now.
package sessionplatform

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
)

func toolApprovalsPath(id string) string {
	return sessionsPath + "/" + url.PathEscape(id) + "/tool-approvals"
}

func toolApprovalDecidePath(id, approvalID string) string {
	return toolApprovalsPath(id) + "/" + url.PathEscape(approvalID) + "/decide"
}

// Tool approval decisions. Nothing else is a decision: the platform policy's
// own denials never reach a person, so there is no decision that turns one
// into an allow.
const (
	ToolApprovalApprove = "approve"
	ToolApprovalDeny    = "deny"
)

// PendingToolApproval is one tool call the running turn waits on.
type PendingToolApproval struct {
	ID string `json:"id"`
	// Tool is the tool the model asked to use and Input the arguments it asked
	// for — what the owner reads to decide. The arguments are the model's own
	// and an approval runs them unchanged.
	Tool  string          `json:"tool"`
	Input json.RawMessage `json:"input"`
	// ArgsHash names this exact call: a sha256 over the tool and its
	// arguments. A decision carries it back.
	ArgsHash string `json:"argsHash"`
	// ToolUseID is the SDK's own id for the call, for matching it against the
	// turn's output. It may be empty.
	ToolUseID string `json:"toolUseId"`
	// Epoch names the run of the session that put the call up.
	Epoch     string `json:"epoch"`
	AskedAt   string `json:"askedAt"`
	ExpiresAt string `json:"expiresAt"`
}

// ToolApprovalPage is what the owner reads: the run's epoch and the calls
// waiting in it, oldest first.
type ToolApprovalPage struct {
	Epoch     string                `json:"epoch"`
	Approvals []PendingToolApproval `json:"approvals"`
}

// ToolApprovalDecision is one decision on one call. Every field but Reason is
// required — a decision that does not name the call it was taken on is refused
// before it is sent.
type ToolApprovalDecision struct {
	Decision string `json:"decision"`
	ArgsHash string `json:"argsHash"`
	Epoch    string `json:"epoch"`
	Reason   string `json:"reason,omitempty"`
}

// ToolApprovals lists the tool calls a claude-code session's running turn put
// to its owner. It is a passive read, like ListSessions: it never promotes an
// idle session or restores a snapshot, and a session without a live pod has no
// running turn, so nothing waits.
func (c *Client) ToolApprovals(ctx context.Context, id string) (*ToolApprovalPage, error) {
	if id == "" {
		return nil, invalidArgument("session id is required")
	}

	endpoint := c.endpoint + toolApprovalsPath(id)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, apiError(fmt.Sprintf("build request: %v", err))
	}

	resp, err := c.send(c.http, req)
	if err != nil {
		return nil, sendError("get", endpoint, err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	message := controlPlaneMessage(respBody, resp.Status)
	switch {
	case resp.StatusCode == http.StatusNotFound:
		return nil, notFound(fmt.Sprintf("session %q: %s", id, message))
	case resp.StatusCode == http.StatusUnprocessableEntity:
		return nil, invalidArgument(message)
	case resp.StatusCode < 200 || resp.StatusCode >= 300:
		return nil, apiError(fmt.Sprintf("%s returned %s: %s", endpoint, resp.Status, string(respBody)))
	}

	var parsed ToolApprovalPage
	if err := json.Unmarshal(respBody, &parsed); err != nil {
		return nil, apiError(fmt.Sprintf("parse tool approvals: %v", err))
	}
	if parsed.Approvals == nil {
		parsed.Approvals = []PendingToolApproval{}
	}
	return &parsed, nil
}

// DecideToolApproval relays the owner's decision to the turn waiting on it.
// Deciding is an access, like a write. A refusal arrives distinguished: not
// found when the call is no longer waiting (decided, closed with its turn, or
// past its window — nothing to retry), and conflict when the decision names
// another call or an earlier run, which means reading the list again rather
// than sending the same decision.
func (c *Client) DecideToolApproval(ctx context.Context, id, approvalID string, decision ToolApprovalDecision) error {
	switch {
	case id == "":
		return invalidArgument("session id is required")
	case approvalID == "":
		return invalidArgument("approval id is required")
	case decision.Decision != ToolApprovalApprove && decision.Decision != ToolApprovalDeny:
		return invalidArgument(fmt.Sprintf("decision must be %q or %q", ToolApprovalApprove, ToolApprovalDeny))
	case decision.ArgsHash == "":
		return invalidArgument("argsHash is required — it names the call the decision was taken on")
	case decision.Epoch == "":
		return invalidArgument("epoch is required — it names the run the call was put up in")
	}

	endpoint := c.endpoint + toolApprovalDecidePath(id, approvalID)
	body, err := json.Marshal(decision)
	if err != nil {
		return apiError(fmt.Sprintf("build request: %v", err))
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return apiError(fmt.Sprintf("build request: %v", err))
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.send(c.http, req)
	if err != nil {
		return sendError("post", endpoint, err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	message := controlPlaneMessage(respBody, resp.Status)
	switch {
	case resp.StatusCode == http.StatusNotFound:
		return notFound(fmt.Sprintf("tool call %q of session %q: %s", approvalID, id, message))
	case resp.StatusCode == http.StatusConflict:
		return conflict(message)
	case resp.StatusCode == http.StatusBadRequest, resp.StatusCode == http.StatusUnprocessableEntity:
		return invalidArgument(message)
	case resp.StatusCode < 200 || resp.StatusCode >= 300:
		return apiError(fmt.Sprintf("%s returned %s: %s", endpoint, resp.Status, string(respBody)))
	}
	return nil
}
