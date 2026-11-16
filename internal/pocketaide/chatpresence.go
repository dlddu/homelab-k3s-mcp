package pocketaide

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
)

const chatPresencePath = "/api/external/chat-presence"

// ChatPresence is what PocketAide reports about the owner's chat screen
// (dlddu/pocket-aide PRD-1 AC8 · PRD-12 AC22).
//
// LastSeenAt is nil when PocketAide has not seen that screen since its own
// process started, which is not the same as the owner having left: absent with
// a time says "was here, left", absent without one says "unknown".
type ChatPresence struct {
	Present    bool   `json:"present"`
	LastSeenAt *int64 `json:"last_seen_at"`
	TTLSeconds int    `json:"ttl_seconds"`
}

// ChatPresence asks whether the person who registered this server is looking
// at PocketAide's chat screen right now. It reads; nothing is created, sent or
// changed, and the owner is not interrupted.
func (c *Client) ChatPresence(ctx context.Context) (*ChatPresence, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+chatPresencePath, nil)
	if err != nil {
		return nil, apiError(err.Error())
	}
	token, err := readToken(c.tokenFile)
	if err != nil {
		return nil, apiError(err.Error())
	}
	req.Header.Set("authorization", "Bearer "+token)

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, apiError("request failed: " + err.Error())
	}
	defer func() { _ = resp.Body.Close() }()

	switch {
	case resp.StatusCode == http.StatusOK:
	case resp.StatusCode == http.StatusUnauthorized:
		return nil, apiError("PocketAide did not accept this server's service account (401) — is it registered as a caller in PocketAide?")
	case resp.StatusCode == http.StatusNotFound:
		return nil, apiError("PocketAide has no chat presence endpoint (404) — deploy a PocketAide that has it (dlddu/pocket-aide PRD-12 AC22)")
	default:
		return nil, apiError(fmt.Sprintf("PocketAide returned %d", resp.StatusCode))
	}

	var out ChatPresence
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, apiError("unreadable response: " + err.Error())
	}
	return &out, nil
}

// ChatPresence always fails with the configured reason.
func (u *Unavailable) ChatPresence(context.Context) (*ChatPresence, error) {
	return nil, unavailable(u.reason)
}
