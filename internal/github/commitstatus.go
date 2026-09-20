package github

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strings"
	"unicode/utf8"
)

const (
	contextPrefixesEnv    = "GITHUB_COMMIT_STATUS_CONTEXT_PREFIXES"
	maxDescriptionRunes   = 140
	commitStatusPermLevel = "write"
)

// fullSHARe is the only accepted spelling of a commit: the 40-hex form. A short
// SHA or a ref name would resolve on GitHub's side, which is the problem — the
// status would land on whatever that name points at when the request arrives,
// not on the commit the caller meant.
var fullSHARe = regexp.MustCompile(`^[0-9a-fA-F]{40}$`)

var commitStatusStates = []string{"error", "failure", "pending", "success"}

// CommitStatusInput is one commit status request.
type CommitStatusInput struct {
	Repository  string
	SHA         string
	State       string
	Context     string
	Description string
	TargetURL   string
}

// CommitStatus is the created status as it goes back to the caller.
type CommitStatus struct {
	ID          int64  `json:"id"`
	State       string `json:"state"`
	Context     string `json:"context"`
	SHA         string `json:"sha"`
	Description string `json:"description,omitempty"`
	TargetURL   string `json:"target_url,omitempty"`
	CreatedAt   string `json:"created_at"`
}

func invalid(msg string) *Error { return &Error{kind: kindInvalid, msg: msg} }

func (u *Unavailable) CreateCommitStatus(context.Context, CommitStatusInput) (*CommitStatus, error) {
	return nil, unavailable(u.reason)
}

func contextPrefixesFromEnv() []string {
	var prefixes []string
	for _, raw := range strings.Split(os.Getenv(contextPrefixesEnv), ",") {
		if trimmed := strings.TrimSpace(raw); trimmed != "" {
			prefixes = append(prefixes, trimmed)
		}
	}
	return prefixes
}

func validateCommitStatusInput(in CommitStatusInput) error {
	if strings.TrimSpace(in.Repository) == "" {
		return invalid("repository is required")
	}
	// The owner half is the installation account, resolved server-side. A
	// caller-supplied "owner/repo" would not be rejected by GitHub — it would
	// be pasted into the path and address a different repository than the one
	// the minted token is scoped to.
	if strings.ContainsAny(in.Repository, "/ ") {
		return invalid(fmt.Sprintf("repository %q must be the repo name without its owner", in.Repository))
	}
	if !fullSHARe.MatchString(in.SHA) {
		return invalid(fmt.Sprintf("sha %q is not a 40-character hex commit sha", in.SHA))
	}
	if !slicesContains(commitStatusStates, in.State) {
		return invalid(fmt.Sprintf("state %q must be one of %s", in.State, strings.Join(commitStatusStates, ", ")))
	}
	if in.Context == "" {
		return invalid("context is required")
	}
	if n := utf8.RuneCountInString(in.Description); n > maxDescriptionRunes {
		return invalid(fmt.Sprintf("description is %d characters, above the %d limit", n, maxDescriptionRunes))
	}
	if in.TargetURL != "" {
		parsed, err := url.Parse(in.TargetURL)
		if err != nil || !parsed.IsAbs() || (parsed.Scheme != "http" && parsed.Scheme != "https") {
			return invalid(fmt.Sprintf("target_url %q must be an absolute http(s) URL", in.TargetURL))
		}
	}
	return nil
}

func slicesContains(haystack []string, needle string) bool {
	for _, item := range haystack {
		if item == needle {
			return true
		}
	}
	return false
}

func (c *Client) checkContextPrefix(statusContext string) error {
	if len(c.statusContextPrefixes) == 0 {
		return invalid(fmt.Sprintf(
			"no commit status context prefix is configured, so every context is refused; set %s to the namespaces this server may write",
			contextPrefixesEnv))
	}
	for _, prefix := range c.statusContextPrefixes {
		if strings.HasPrefix(statusContext, prefix) {
			return nil
		}
	}
	return invalid(fmt.Sprintf("context %q is outside the allowed prefixes: %s",
		statusContext, strings.Join(c.statusContextPrefixes, ", ")))
}

func (c *Client) CreateCommitStatus(ctx context.Context, in CommitStatusInput) (*CommitStatus, error) {
	if err := validateCommitStatusInput(in); err != nil {
		return nil, err
	}
	if err := c.checkContextPrefix(in.Context); err != nil {
		return nil, err
	}

	jwtToken, err := c.appJWT()
	if err != nil {
		return nil, err
	}

	owner, err := c.installationOwner(ctx, jwtToken)
	if err != nil {
		return nil, err
	}

	// Below CreateInstallationToken's AC5 guard on purpose — see
	// mintInstallationToken.
	token, err := c.mintInstallationToken(ctx, jwtToken, map[string]any{
		"repositories": []string{in.Repository},
		"permissions":  map[string]any{statusesPermission: commitStatusPermLevel},
	})
	if err != nil {
		return nil, err
	}
	defer func() {
		// Best effort: the token expires on its own, and a revoke failure must
		// not turn a written status into a reported failure.
		_ = c.revokeToken(ctx, token.Token)
	}()

	return c.postCommitStatus(ctx, token.Token, owner, in)
}

func (c *Client) postCommitStatus(ctx context.Context, token, owner string, in CommitStatusInput) (*CommitStatus, error) {
	endpoint := fmt.Sprintf("%s/repos/%s/%s/statuses/%s", c.apiBase, owner, in.Repository, in.SHA)

	body := map[string]any{
		"state":   in.State,
		"context": in.Context,
	}
	if in.Description != "" {
		body["description"] = in.Description
	}
	if in.TargetURL != "" {
		body["target_url"] = in.TargetURL
	}
	bodyBytes, err := json.Marshal(body)
	if err != nil {
		return nil, apiError(fmt.Sprintf("encode request body: %v", err))
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, apiError(fmt.Sprintf("build request: %v", err))
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", githubAPIVersion)
	req.Header.Set("User-Agent", c.userAgent)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, apiError(fmt.Sprintf("post %s: %v", endpoint, err))
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, apiError(fmt.Sprintf("%s returned %s: %s", endpoint, resp.Status, string(respBody)))
	}

	var status CommitStatus
	if err := json.Unmarshal(respBody, &status); err != nil {
		return nil, apiError(fmt.Sprintf("parse commit status: %v", err))
	}
	return &status, nil
}
