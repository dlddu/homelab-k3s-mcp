package pocketaide

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strings"
	"unicode/utf8"
)

const (
	contextPrefixesEnv       = "GITHUB_COMMIT_STATUS_CONTEXT_PREFIXES"
	maxContextRunes          = 255
	maxDescriptionRunes      = 140
	maxExplanationRunes      = 2000
	commitStatusRequestsPath = "/api/external/commit-status-requests"
)

var (
	fullSHARe     = regexp.MustCompile(`^[0-9a-fA-F]{40}$`)
	githubNameRe  = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)
	statusStates  = []string{"error", "failure", "pending", "success"}
	statusStateOK = map[string]bool{"error": true, "failure": true, "pending": true, "success": true}
)

// CommitStatusRequest is one commit status the caller asks the owner to approve; it carries everything PocketAide needs to write the status later.
type CommitStatusRequest struct {
	Repository  string
	SHA         string
	State       string
	Context     string
	Description string
	TargetURL   string
	Requester   string
	Explanation string
	SessionID   string
}

// CommitStatusTicket is PocketAide's receipt for a stored request.
type CommitStatusTicket struct {
	ID        string `json:"id"`
	Status    string `json:"status"`
	CreatedAt int64  `json:"createdAt"`
}

// ContextPrefixes is the namespace this server may ask statuses for, read from GITHUB_COMMIT_STATUS_CONTEXT_PREFIXES on every call.
func ContextPrefixes() []string {
	var prefixes []string
	for _, raw := range strings.Split(os.Getenv(contextPrefixesEnv), ",") {
		if trimmed := strings.TrimSpace(raw); trimmed != "" {
			prefixes = append(prefixes, trimmed)
		}
	}
	return prefixes
}

// ValidateCommitStatusRequest applies the input rules and the context prefix allowlist; the tool calls it before RequestCommitStatus so a bad request is refused the same way whether or not PocketAide is configured.
func ValidateCommitStatusRequest(in CommitStatusRequest) error {
	owner, repo, ok := strings.Cut(in.Repository, "/")
	switch {
	case strings.TrimSpace(in.Repository) == "":
		return invalidStatus("repository is required")
	case !ok || !githubNameRe.MatchString(owner) || !githubNameRe.MatchString(repo):
		return invalidStatus(fmt.Sprintf("repository %q must be owner/name", in.Repository))
	case !fullSHARe.MatchString(in.SHA):
		return invalidStatus(fmt.Sprintf("sha %q is not a 40-character hex commit sha", in.SHA))
	case !statusStateOK[in.State]:
		return invalidStatus(fmt.Sprintf("state %q must be one of %s", in.State, strings.Join(statusStates, ", ")))
	case in.Context == "":
		return invalidStatus("context is required")
	case utf8.RuneCountInString(in.Context) > maxContextRunes:
		return invalidStatus(fmt.Sprintf("context must be %d characters or fewer", maxContextRunes))
	case utf8.RuneCountInString(in.Description) > maxDescriptionRunes:
		return invalidStatus(fmt.Sprintf("description is %d characters, above the %d limit",
			utf8.RuneCountInString(in.Description), maxDescriptionRunes))
	case utf8.RuneCountInString(in.Explanation) > maxExplanationRunes:
		return invalidStatus(fmt.Sprintf("explanation must be %d characters or fewer", maxExplanationRunes))
	}
	if in.TargetURL != "" {
		parsed, err := url.Parse(in.TargetURL)
		if err != nil || !parsed.IsAbs() || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
			return invalidStatus(fmt.Sprintf("target_url %q must be an absolute http(s) URL", in.TargetURL))
		}
	}
	if err := ValidateRequester(in.Requester); err != nil {
		return invalidStatus(strings.TrimPrefix(err.Error(), "invalid notification: "))
	}
	return checkContextPrefix(in.Context, ContextPrefixes())
}

func checkContextPrefix(statusContext string, prefixes []string) error {
	if len(prefixes) == 0 {
		return invalidStatus(fmt.Sprintf(
			"no commit status context prefix is configured, so every context is refused; set %s to the namespaces this server may request",
			contextPrefixesEnv))
	}
	for _, prefix := range prefixes {
		if strings.HasPrefix(statusContext, prefix) {
			return nil
		}
	}
	return invalidStatus(fmt.Sprintf("context %q is outside the allowed prefixes: %s",
		statusContext, strings.Join(prefixes, ", ")))
}

func invalidStatus(msg string) *Error { return &Error{kind: kindInvalidStatus, msg: msg} }

type commitStatusBody struct {
	Owner       string `json:"owner"`
	Repo        string `json:"repo"`
	SHA         string `json:"sha"`
	State       string `json:"state"`
	Context     string `json:"context"`
	Description string `json:"description,omitempty"`
	TargetURL   string `json:"targetUrl,omitempty"`
	Requester   string `json:"requester"`
	Reason      string `json:"reason,omitempty"`
	SessionID   string `json:"sessionId,omitempty"`
}

// RequestCommitStatus stores the request in PocketAide and returns as soon as PocketAide has it; nothing is written to GitHub until the owner approves.
func (c *Client) RequestCommitStatus(ctx context.Context, in CommitStatusRequest) (*CommitStatusTicket, error) {
	if err := ValidateCommitStatusRequest(in); err != nil {
		return nil, err
	}
	owner, repo, _ := strings.Cut(in.Repository, "/")
	raw, err := json.Marshal(commitStatusBody{
		Owner:       owner,
		Repo:        repo,
		SHA:         strings.ToLower(in.SHA),
		State:       in.State,
		Context:     in.Context,
		Description: in.Description,
		TargetURL:   in.TargetURL,
		Requester:   strings.TrimSpace(in.Requester),
		Reason:      strings.TrimSpace(in.Explanation),
		SessionID:   in.SessionID,
	})
	if err != nil {
		return nil, apiError(err.Error())
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+commitStatusRequestsPath, bytes.NewReader(raw))
	if err != nil {
		return nil, apiError(err.Error())
	}
	req.Header.Set("content-type", "application/json")
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
	case resp.StatusCode == http.StatusCreated:
	case resp.StatusCode == http.StatusUnauthorized:
		return nil, apiError("PocketAide did not accept this server's service account (401) — is it registered as a caller in PocketAide?")
	case resp.StatusCode == http.StatusNotFound:
		return nil, apiError("PocketAide has no commit status request endpoint (404) — deploy a PocketAide that has it (dlddu/pocket-aide PRD-12 AC18)")
	case resp.StatusCode == http.StatusBadRequest:
		return nil, apiError("PocketAide rejected the commit status request (400): " + serverMessage(resp.Body))
	default:
		return nil, apiError(fmt.Sprintf("PocketAide returned %d", resp.StatusCode))
	}

	var out CommitStatusTicket
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, apiError("unreadable response: " + err.Error())
	}
	return &out, nil
}

// RequestCommitStatus always fails with the configured reason.
func (u *Unavailable) RequestCommitStatus(context.Context, CommitStatusRequest) (*CommitStatusTicket, error) {
	return nil, unavailable(u.reason)
}
