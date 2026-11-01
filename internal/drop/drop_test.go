package drop

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
)

type received struct {
	path, auth, contentType string
	body                    map[string]any
}

type fakeDrop struct {
	mu        sync.Mutex
	calls     []received
	status    int
	reply     string
	redirects int
}

func (f *fakeDrop) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	raw, _ := io.ReadAll(r.Body)
	var body map[string]any
	_ = json.Unmarshal(raw, &body)
	f.mu.Lock()
	f.calls = append(f.calls, received{r.URL.Path, r.Header.Get("Authorization"), r.Header.Get("Content-Type"), body})
	redirect := f.redirects > 0
	if redirect {
		f.redirects--
	}
	f.mu.Unlock()
	if redirect {
		http.Redirect(w, r, r.URL.String(), http.StatusTemporaryRedirect)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(f.status)
	_, _ = io.WriteString(w, f.reply)
}

func (f *fakeDrop) all() []received {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]received(nil), f.calls...)
}

const issued = `{"token":"drk_abc","expires_at":"2026-11-01T13:00:00Z","max_days":30,"created_by":"sa:homelab-k3s-mcp/homelab-k3s-mcp","base_url":"https://drop.llkm.nl/"}`

func writeToken(t *testing.T, path, token string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(token+"\n"), 0o600); err != nil {
		t.Fatalf("write token: %v", err)
	}
}

func newClient(t *testing.T, fake *fakeDrop) (*Client, string) {
	t.Helper()
	srv := httptest.NewServer(fake)
	t.Cleanup(srv.Close)
	path := filepath.Join(t.TempDir(), "token")
	writeToken(t, path, "sa-token-1")
	return New(srv.URL+"/", path, srv.Client()), path
}

func TestIssueTokenExchangesTheServiceAccountToken(t *testing.T) {
	fake := &fakeDrop{status: http.StatusCreated, reply: issued}
	client, _ := newClient(t, fake)

	tok, err := client.IssueToken(context.Background(), Request{MaxDays: 7, Label: "  build logs  "})
	if err != nil {
		t.Fatalf("issue: %v", err)
	}
	want := Token{Token: "drk_abc", ExpiresAt: "2026-11-01T13:00:00Z", MaxDays: 30, CreatedBy: "sa:homelab-k3s-mcp/homelab-k3s-mcp", BaseURL: "https://drop.llkm.nl"}
	if *tok != want {
		t.Fatalf("token = %+v, want %+v (base_url trailing slash trimmed)", *tok, want)
	}
	calls := fake.all()
	if len(calls) != 1 {
		t.Fatalf("calls = %d", len(calls))
	}
	c := calls[0]
	if c.path != "/api/v1/sa/token" || c.auth != "Bearer sa-token-1" || c.contentType != "application/json" {
		t.Fatalf("request = %+v", c)
	}
	if c.body["valid_minutes"] != float64(DefaultValidMinutes) || c.body["max_days"] != float64(7) || c.body["label"] != "build logs" {
		t.Fatalf("body = %v, want the 60-minute default, max_days 7 and the trimmed label", c.body)
	}
}

func TestIssueTokenOmitsUnsetOptionalFields(t *testing.T) {
	fake := &fakeDrop{status: http.StatusCreated, reply: issued}
	client, _ := newClient(t, fake)

	if _, err := client.IssueToken(context.Background(), Request{ValidMinutes: 5}); err != nil {
		t.Fatalf("issue: %v", err)
	}
	body := fake.all()[0].body
	if _, ok := body["max_days"]; ok {
		t.Fatalf("body = %v, want no max_days", body)
	}
	if _, ok := body["label"]; ok {
		t.Fatalf("body = %v, want no label", body)
	}
	if body["valid_minutes"] != float64(5) {
		t.Fatalf("body = %v", body)
	}
}

func TestIssueTokenReadsTheTokenOnEveryCall(t *testing.T) {
	fake := &fakeDrop{status: http.StatusCreated, reply: issued}
	client, path := newClient(t, fake)

	if _, err := client.IssueToken(context.Background(), Request{}); err != nil {
		t.Fatal(err)
	}
	writeToken(t, path, "sa-token-2")
	if _, err := client.IssueToken(context.Background(), Request{}); err != nil {
		t.Fatal(err)
	}
	calls := fake.all()
	if calls[0].auth != "Bearer sa-token-1" || calls[1].auth != "Bearer sa-token-2" {
		t.Fatalf("auth = %q then %q, want the rotated token on the second call", calls[0].auth, calls[1].auth)
	}
}

// Sablier 의 blocking 전략은 잠든 drop 을 깨우다 시간이 넘으면 같은 URL 로 307 을 준다.
func TestIssueTokenFollowsSablierRedirectWithBody(t *testing.T) {
	fake := &fakeDrop{status: http.StatusCreated, reply: issued, redirects: 1}
	client, _ := newClient(t, fake)

	if _, err := client.IssueToken(context.Background(), Request{ValidMinutes: 15, Label: "x"}); err != nil {
		t.Fatalf("issue: %v", err)
	}
	calls := fake.all()
	if len(calls) != 2 {
		t.Fatalf("calls = %d, want the 307 followed", len(calls))
	}
	if calls[1].body["valid_minutes"] != float64(15) || calls[1].auth != "Bearer sa-token-1" {
		t.Fatalf("replayed request = %+v, want the body and token carried over", calls[1])
	}
}

func TestIssueTokenExplainsEachRefusal(t *testing.T) {
	cases := []struct {
		status int
		reply  string
		want   string
	}{
		{http.StatusBadRequest, `{"error":"valid_minutes 는 1~1440 입니다"}`, "(400): valid_minutes 는 1~1440 입니다"},
		{http.StatusUnauthorized, `{"error":"유효하지 않은 서비스 계정 토큰입니다(audience drop)"}`, `audience "drop"`},
		{http.StatusForbidden, `{"error":"허용되지 않은 서비스 계정입니다: x/y"}`, "SA_ALLOW"},
		{http.StatusBadGateway, `{"error":"서비스 계정을 확인하지 못했습니다"}`, "system:auth-delegator"},
		{http.StatusServiceUnavailable, `{"error":"서비스 계정 교환이 설정되지 않았습니다"}`, "(503)"},
		{http.StatusInternalServerError, strings.Repeat("x", 400), "drop returned 500: " + strings.Repeat("x", 300) + "..."},
	}
	seen := map[string]bool{}
	for _, tc := range cases {
		client, _ := newClient(t, &fakeDrop{status: tc.status, reply: tc.reply})
		_, err := client.IssueToken(context.Background(), Request{})
		var de *Error
		if !errors.As(err, &de) || de.Unavailable() {
			t.Fatalf("%d: err = %v, want a non-unavailable drop error", tc.status, err)
		}
		if !strings.Contains(err.Error(), tc.want) {
			t.Fatalf("%d: err = %q, want it to contain %q", tc.status, err, tc.want)
		}
		if seen[err.Error()] {
			t.Fatalf("%d: message %q is not distinct", tc.status, err)
		}
		seen[err.Error()] = true
	}
}

func TestIssueTokenRejectsA201WithoutAToken(t *testing.T) {
	client, _ := newClient(t, &fakeDrop{status: http.StatusCreated, reply: `{"expires_at":"x"}`})
	if _, err := client.IssueToken(context.Background(), Request{}); err == nil || !strings.Contains(err.Error(), "without a token") {
		t.Fatalf("err = %v", err)
	}
}

func TestIssueTokenRefusesBadArgumentsWithoutARequest(t *testing.T) {
	fake := &fakeDrop{status: http.StatusCreated, reply: issued}
	client, _ := newClient(t, fake)
	for _, req := range []Request{
		{ValidMinutes: -1},
		{ValidMinutes: MaxValidMinutes + 1},
		{MaxDays: -1},
		{MaxDays: MaxDays + 1},
		{Label: strings.Repeat("가", MaxLabelRunes+1)},
	} {
		_, err := client.IssueToken(context.Background(), req)
		if err == nil || !strings.Contains(err.Error(), "invalid drop token request") {
			t.Fatalf("%+v: err = %v", req, err)
		}
	}
	if len(fake.all()) != 0 {
		t.Fatalf("calls = %d, want none", len(fake.all()))
	}
	if err := Validate(Request{ValidMinutes: MaxValidMinutes, MaxDays: MaxDays, Label: strings.Repeat("가", MaxLabelRunes)}); err != nil {
		t.Fatalf("limits themselves must pass: %v", err)
	}
}

func TestUnavailableIsTheUnconfiguredKind(t *testing.T) {
	_, err := NewUnavailable("").IssueToken(context.Background(), Request{})
	var de *Error
	if !errors.As(err, &de) || !de.Unavailable() || err.Error() != "drop unavailable: drop is not configured (DROP_URL)" {
		t.Fatalf("err = %v", err)
	}
}

func TestFromEnv(t *testing.T) {
	t.Setenv("DROP_URL", "")
	if c, err := FromEnv(); c != nil || err != nil {
		t.Fatalf("unset: client = %v, err = %v", c, err)
	}

	path := filepath.Join(t.TempDir(), "token")
	writeToken(t, path, "sa")
	t.Setenv("DROP_URL", "http://drop-external.drop.svc.cluster.local/")
	t.Setenv("DROP_TOKEN_FILE", path)
	c, err := FromEnv()
	if err != nil || c == nil || c.baseURL != "http://drop-external.drop.svc.cluster.local" || c.tokenFile != path {
		t.Fatalf("client = %+v, err = %v", c, err)
	}

	t.Setenv("DROP_TOKEN_FILE", filepath.Join(t.TempDir(), "missing"))
	if _, err := FromEnv(); err == nil || !strings.Contains(err.Error(), "drop service account token") {
		t.Fatalf("missing token file: err = %v, want a fail-fast error", err)
	}

	t.Setenv("DROP_TOKEN_FILE", path)
	t.Setenv("DROP_URL", "drop.svc")
	if _, err := FromEnv(); err == nil || !strings.Contains(err.Error(), "DROP_URL") {
		t.Fatalf("relative URL: err = %v", err)
	}
}

func TestFromEnvDefaultsTheTokenFile(t *testing.T) {
	t.Setenv("DROP_URL", "http://drop.example")
	t.Setenv("DROP_TOKEN_FILE", "")
	_, err := FromEnv()
	if _, statErr := os.Stat(DefaultTokenFile); statErr == nil {
		t.Skip("running where the projected token exists")
	}
	if err == nil || !strings.Contains(err.Error(), DefaultTokenFile) {
		t.Fatalf("err = %v, want it to name %s", err, DefaultTokenFile)
	}
}
