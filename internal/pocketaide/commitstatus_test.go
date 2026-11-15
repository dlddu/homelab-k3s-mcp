package pocketaide_test

import (
	"context"
	"net/http"
	"reflect"
	"strings"
	"testing"

	"github.com/dlddu/homelab-k3s-mcp/internal/pocketaide"
)

func validStatusRequest() pocketaide.CommitStatusRequest {
	return pocketaide.CommitStatusRequest{
		Repository:  "dlddu/pocket-aide",
		SHA:         "0123456789ABCDEF0123456789abcdef01234567",
		State:       "success",
		Context:     "review/manual-approval",
		Description: "수동 승인",
		TargetURL:   "https://github.com/dlddu/pocket-aide/pull/405",
		Requester:   "  Claude Code 세션 — PR #405  ",
		Explanation: " 검토를 마쳤다. ",
		SessionID:   "s-1",
	}
}

func TestRequestCommitStatusFilesEverythingPocketAideNeedsAndReturnsAtOnce(t *testing.T) {
	t.Setenv("GITHUB_COMMIT_STATUS_CONTEXT_PREFIXES", " homelab-k3s-mcp/, review/ ")
	fake := &fakePocketAide{status: http.StatusCreated, reply: `{"id":"req-1","status":"PENDING","createdAt":1800000000}`}
	client, _ := newClient(t, fake)

	ticket, err := client.RequestCommitStatus(context.Background(), validStatusRequest())
	if err != nil {
		t.Fatalf("RequestCommitStatus: %v", err)
	}
	if *ticket != (pocketaide.CommitStatusTicket{ID: "req-1", Status: "PENDING", CreatedAt: 1_800_000_000}) {
		t.Fatalf("ticket = %+v", ticket)
	}
	calls := fake.all()
	if len(calls) != 1 || calls[0].path != "/api/external/commit-status-requests" || calls[0].auth != "Bearer sa-token-1" ||
		calls[0].contentType != "application/json" {
		t.Fatalf("calls = %+v", calls)
	}
	want := map[string]any{
		"owner": "dlddu", "repo": "pocket-aide", "sha": "0123456789abcdef0123456789abcdef01234567",
		"state": "success", "context": "review/manual-approval", "description": "수동 승인",
		"targetUrl": "https://github.com/dlddu/pocket-aide/pull/405", "requester": "Claude Code 세션 — PR #405",
		"reason": "검토를 마쳤다.", "sessionId": "s-1",
	}
	if !reflect.DeepEqual(calls[0].body, want) {
		t.Fatalf("body = %v\nwant  %v", calls[0].body, want)
	}
}

func TestRequestCommitStatusOmitsEmptyOptionalFields(t *testing.T) {
	t.Setenv("GITHUB_COMMIT_STATUS_CONTEXT_PREFIXES", "review/")
	fake := &fakePocketAide{status: http.StatusCreated, reply: `{"id":"req-2","status":"PENDING","createdAt":1}`}
	client, _ := newClient(t, fake)
	in := pocketaide.CommitStatusRequest{
		Repository: "dlddu/x", SHA: strings.Repeat("a", 40), State: "pending", Context: "review/x", Requester: "r",
	}
	if _, err := client.RequestCommitStatus(context.Background(), in); err != nil {
		t.Fatal(err)
	}
	body := fake.all()[0].body
	for _, key := range []string{"description", "targetUrl", "reason", "sessionId"} {
		if _, present := body[key]; present {
			t.Errorf("empty %s was sent: %v", key, body)
		}
	}
}

func TestCommitStatusRequestValidationComesFirst(t *testing.T) {
	t.Setenv("GITHUB_COMMIT_STATUS_CONTEXT_PREFIXES", "review/")
	fake := &fakePocketAide{status: http.StatusCreated, reply: `{}`}
	client, _ := newClient(t, fake)

	cases := []struct {
		name   string
		mutate func(*pocketaide.CommitStatusRequest)
		want   string
	}{
		{"no repository", func(r *pocketaide.CommitStatusRequest) { r.Repository = "" }, "repository is required"},
		{"repository without owner", func(r *pocketaide.CommitStatusRequest) { r.Repository = "pocket-aide" }, "must be owner/name"},
		{"repository with extra path", func(r *pocketaide.CommitStatusRequest) { r.Repository = "dlddu/pocket-aide/x" }, "must be owner/name"},
		{"short sha", func(r *pocketaide.CommitStatusRequest) { r.SHA = "abc1234" }, "40-character hex commit sha"},
		{"unknown state", func(r *pocketaide.CommitStatusRequest) { r.State = "ok" }, `state "ok" must be one of`},
		{"no context", func(r *pocketaide.CommitStatusRequest) { r.Context = "" }, "context is required"},
		{"long context", func(r *pocketaide.CommitStatusRequest) { r.Context = "review/" + strings.Repeat("c", 250) }, "255 characters or fewer"},
		{"long description", func(r *pocketaide.CommitStatusRequest) { r.Description = strings.Repeat("d", 141) }, "above the 140 limit"},
		{"ftp url", func(r *pocketaide.CommitStatusRequest) { r.TargetURL = "ftp://x" }, "absolute http(s) URL"},
		{"no requester", func(r *pocketaide.CommitStatusRequest) { r.Requester = "  " }, "requester is required"},
		{"long requester", func(r *pocketaide.CommitStatusRequest) { r.Requester = strings.Repeat("가", 201) }, "requester must be 200 characters or fewer"},
		{"long explanation", func(r *pocketaide.CommitStatusRequest) { r.Explanation = strings.Repeat("e", 2001) }, "explanation must be 2000 characters or fewer"},
		{"foreign context", func(r *pocketaide.CommitStatusRequest) { r.Context = "ci/build" }, `context "ci/build" is outside the allowed prefixes: review/`},
	}
	for _, c := range cases {
		in := validStatusRequest()
		c.mutate(&in)
		_, err := client.RequestCommitStatus(context.Background(), in)
		if err == nil || !strings.HasPrefix(err.Error(), "invalid commit status request: ") || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: err = %v, want an invalid commit status request containing %q", c.name, err, c.want)
		}
	}
	if calls := fake.all(); len(calls) != 0 {
		t.Fatalf("refused requests reached PocketAide: %+v", calls)
	}
}

func TestCommitStatusRequestRefusesEveryContextWithoutPrefixes(t *testing.T) {
	t.Setenv("GITHUB_COMMIT_STATUS_CONTEXT_PREFIXES", " , ")
	err := pocketaide.ValidateCommitStatusRequest(validStatusRequest())
	if err == nil || !strings.Contains(err.Error(), "no commit status context prefix is configured") ||
		!strings.Contains(err.Error(), "GITHUB_COMMIT_STATUS_CONTEXT_PREFIXES") {
		t.Fatalf("err = %v", err)
	}
}

func TestRequestCommitStatusExplainsEachRefusal(t *testing.T) {
	t.Setenv("GITHUB_COMMIT_STATUS_CONTEXT_PREFIXES", "review/")
	for _, c := range []struct {
		status int
		reply  string
		want   string
	}{
		{http.StatusUnauthorized, `{"error":"invalid caller key"}`, "registered as a caller"},
		{http.StatusNotFound, `404 page not found`, "no commit status request endpoint"},
		{http.StatusBadRequest, `{"error":"sha must be a 40-character hex commit sha"}`, "(400): sha must be"},
		{http.StatusInternalServerError, `{}`, "PocketAide returned 500"},
	} {
		client, _ := newClient(t, &fakePocketAide{status: c.status, reply: c.reply})
		_, err := client.RequestCommitStatus(context.Background(), validStatusRequest())
		if err == nil || !strings.HasPrefix(err.Error(), "pocket-aide error: ") || !strings.Contains(err.Error(), c.want) {
			t.Errorf("status %d: err = %v, want %q", c.status, err, c.want)
		}
	}
}

func TestUnavailableRefusesCommitStatusRequests(t *testing.T) {
	_, err := pocketaide.NewUnavailable("").RequestCommitStatus(context.Background(), validStatusRequest())
	if err == nil || err.Error() != "pocket-aide unavailable: PocketAide is not configured (APPROVAL_GATE_URL)" {
		t.Fatalf("err = %v", err)
	}
}
