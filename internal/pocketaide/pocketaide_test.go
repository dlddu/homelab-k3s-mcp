package pocketaide_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/dlddu/homelab-k3s-mcp/internal/pocketaide"
)

type received struct {
	path, auth, contentType string
	body                    map[string]any
}

type fakePocketAide struct {
	mu     sync.Mutex
	calls  []received
	status int
	reply  string
}

func (f *fakePocketAide) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	raw, _ := io.ReadAll(r.Body)
	var body map[string]any
	_ = json.Unmarshal(raw, &body)
	f.mu.Lock()
	f.calls = append(f.calls, received{r.URL.Path, r.Header.Get("Authorization"), r.Header.Get("Content-Type"), body})
	f.mu.Unlock()
	w.WriteHeader(f.status)
	_, _ = w.Write([]byte(f.reply))
}

func (f *fakePocketAide) all() []received {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]received(nil), f.calls...)
}

func tokenFile(t *testing.T, token string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "token")
	if err := os.WriteFile(path, []byte(token+"\n"), 0o600); err != nil {
		t.Fatalf("write token: %v", err)
	}
	return path
}

func newClient(t *testing.T, fake *fakePocketAide) (*pocketaide.Client, string) {
	t.Helper()
	srv := httptest.NewServer(fake)
	t.Cleanup(srv.Close)
	path := tokenFile(t, "sa-token-1")
	return pocketaide.New(srv.URL+"/", path, srv.Client()), path
}

func TestNotifyPostsTitleAndBodyWithTheServiceAccountToken(t *testing.T) {
	fake := &fakePocketAide{status: http.StatusOK, reply: `{"devices":2,"sent":2}`}
	client, _ := newClient(t, fake)

	res, err := client.Notify(context.Background(), "  백업 완료 ", " 스냅샷 3개 ")
	if err != nil {
		t.Fatalf("notify: %v", err)
	}
	if *res != (pocketaide.Result{Devices: 2, Sent: 2}) {
		t.Fatalf("result = %+v", *res)
	}
	calls := fake.all()
	if len(calls) != 1 {
		t.Fatalf("calls = %d", len(calls))
	}
	got := calls[0]
	if got.path != "/api/external/notifications" || got.auth != "Bearer sa-token-1" || got.contentType != "application/json" {
		t.Fatalf("request = %+v", got)
	}
	if got.body["title"] != "백업 완료" || got.body["body"] != "스냅샷 3개" || len(got.body) != 2 {
		t.Fatalf("body = %v, want trimmed title and body only", got.body)
	}
}

func TestNotifyOmitsAnEmptyBody(t *testing.T) {
	fake := &fakePocketAide{status: http.StatusOK, reply: `{"devices":0,"sent":0}`}
	client, _ := newClient(t, fake)

	res, err := client.Notify(context.Background(), "ping", "")
	if err != nil {
		t.Fatalf("notify: %v", err)
	}
	if res.Devices != 0 || res.Sent != 0 {
		t.Fatalf("result = %+v", *res)
	}
	if body := fake.all()[0].body; len(body) != 1 || body["title"] != "ping" {
		t.Fatalf("body = %v, want title only", body)
	}
}

func TestNotifyReadsTheTokenOnEveryCall(t *testing.T) {
	fake := &fakePocketAide{status: http.StatusOK, reply: `{"devices":1,"sent":1}`}
	client, path := newClient(t, fake)

	if _, err := client.Notify(context.Background(), "one", ""); err != nil {
		t.Fatalf("notify: %v", err)
	}
	if err := os.WriteFile(path, []byte("sa-token-2"), 0o600); err != nil {
		t.Fatalf("rotate: %v", err)
	}
	if _, err := client.Notify(context.Background(), "two", ""); err != nil {
		t.Fatalf("notify: %v", err)
	}
	calls := fake.all()
	if calls[0].auth != "Bearer sa-token-1" || calls[1].auth != "Bearer sa-token-2" {
		t.Fatalf("auth headers = %q, %q, want the rotated token on the second call", calls[0].auth, calls[1].auth)
	}
}

func TestNotifyRefusesBadTitlesWithoutARequest(t *testing.T) {
	fake := &fakePocketAide{status: http.StatusOK, reply: `{}`}
	client, _ := newClient(t, fake)

	for _, title := range []string{"", "   ", strings.Repeat("가", pocketaide.MaxTitleRunes+1)} {
		_, err := client.Notify(context.Background(), title, "")
		var e *pocketaide.Error
		if !errors.As(err, &e) || e.Unavailable() || !strings.Contains(err.Error(), "invalid notification") {
			t.Fatalf("title %q: err = %v, want an invalid-notification error", title, err)
		}
	}
	if n := len(fake.all()); n != 0 {
		t.Fatalf("requests = %d, want none", n)
	}
}

func TestNotifyExplainsEachRefusal(t *testing.T) {
	cases := []struct {
		status int
		reply  string
		want   string
	}{
		{http.StatusUnauthorized, `{"error":"invalid caller key"}`, "registered as a caller"},
		{http.StatusServiceUnavailable, `{"error":"push notifications unavailable"}`, "APNs is not configured"},
		{http.StatusBadRequest, `{"error":"title is too long"}`, "title is too long"},
		{http.StatusInternalServerError, `{"error":"send failed"}`, "returned 500"},
	}
	for _, c := range cases {
		fake := &fakePocketAide{status: c.status, reply: c.reply}
		client, _ := newClient(t, fake)
		_, err := client.Notify(context.Background(), "x", "")
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Fatalf("status %d: err = %v, want it to mention %q", c.status, err, c.want)
		}
		var e *pocketaide.Error
		if !errors.As(err, &e) || e.Unavailable() {
			t.Fatalf("status %d: a reachable PocketAide's refusal must not read as unconfigured", c.status)
		}
	}
}

func TestUnavailableIsTheUnconfiguredKind(t *testing.T) {
	_, err := pocketaide.NewUnavailable("").Notify(context.Background(), "x", "")
	var e *pocketaide.Error
	if !errors.As(err, &e) || !e.Unavailable() || !strings.Contains(err.Error(), "APPROVAL_GATE_URL") {
		t.Fatalf("err = %v, want the unconfigured kind naming APPROVAL_GATE_URL", err)
	}
}

func TestFromEnvFollowsTheApprovalGateSettings(t *testing.T) {
	t.Setenv("APPROVAL_GATE_URL", "")
	t.Setenv("APPROVAL_GATE_TOKEN_FILE", "")
	if c, err := pocketaide.FromEnv(); c != nil || err != nil {
		t.Fatalf("unset URL: client=%v err=%v, want (nil, nil)", c, err)
	}

	t.Setenv("APPROVAL_GATE_URL", "http://pocket-aide.pocket-aide.svc.cluster.local:8080")
	t.Setenv("APPROVAL_GATE_TOKEN_FILE", filepath.Join(t.TempDir(), "missing"))
	if _, err := pocketaide.FromEnv(); err == nil {
		t.Fatal("missing token file: want an error")
	}

	t.Setenv("APPROVAL_GATE_TOKEN_FILE", tokenFile(t, "sa"))
	if c, err := pocketaide.FromEnv(); c == nil || err != nil {
		t.Fatalf("configured: client=%v err=%v", c, err)
	}
}
