package sessionplatform

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

type authRecorder struct {
	mu      sync.Mutex
	headers []string
	present []bool
}

func (a *authRecorder) seen() ([]string, []bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	return append([]string(nil), a.headers...), append([]bool(nil), a.present...)
}

func tenantStub(t *testing.T, status int) (*httptest.Server, *authRecorder) {
	t.Helper()
	rec := &authRecorder{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		values, ok := r.Header["Authorization"]
		rec.mu.Lock()
		rec.headers = append(rec.headers, strings.Join(values, ""))
		rec.present = append(rec.present, ok)
		rec.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		switch {
		case status != http.StatusOK:
			_, _ = w.Write([]byte(`{"error":"service account belongs to no tenant"}`))
		case r.URL.Path == configPath:
			_, _ = w.Write([]byte(`{"workloadTypes":[{"type":"shell"},{"type":"codex"}]}`))
		default:
			_, _ = w.Write([]byte(`{"sessions":[]}`))
		}
	}))
	t.Cleanup(srv.Close)
	return srv, rec
}

func writeTokenFile(t *testing.T, dir, token string) string {
	t.Helper()
	path := filepath.Join(dir, "token")
	if err := os.WriteFile(path, []byte(token+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestTenantTokenIsSentOnEveryControlPlaneCall(t *testing.T) {
	srv, rec := tenantStub(t, http.StatusOK)
	t.Setenv("SESSION_PLATFORM_ENDPOINT", srv.URL)
	t.Setenv("SESSION_PLATFORM_TOKEN_FILE", writeTokenFile(t, t.TempDir(), "tok-1"))
	c, err := FromEnv()
	if err != nil {
		t.Fatalf("FromEnv: %v", err)
	}

	ctx := context.Background()
	_, _ = c.ListSessions(ctx)
	_, _ = c.CreateSession(ctx, CreateRequest{Name: "n", WorkloadType: "codex"})
	_ = c.DeleteSession(ctx, "id")
	_, _ = c.ReadSession(ctx, "id", 0)
	_, _ = c.WriteSession(ctx, "id", "x")

	headers, _ := rec.seen()
	if len(headers) != 6 {
		t.Fatalf("requests = %d, want 6", len(headers))
	}
	for i, h := range headers {
		if h != "Bearer tok-1" {
			t.Errorf("request %d Authorization = %q", i, h)
		}
	}
}

func TestTenantTokenIsReReadAfterRotation(t *testing.T) {
	srv, rec := tenantStub(t, http.StatusOK)
	dir := t.TempDir()
	t.Setenv("SESSION_PLATFORM_ENDPOINT", srv.URL)
	t.Setenv("SESSION_PLATFORM_TOKEN_FILE", writeTokenFile(t, dir, "tok-1"))
	c, err := FromEnv()
	if err != nil {
		t.Fatalf("FromEnv: %v", err)
	}
	_, _ = c.ListSessions(context.Background())
	writeTokenFile(t, dir, "tok-2")
	_, _ = c.ListSessions(context.Background())

	headers, _ := rec.seen()
	if len(headers) != 2 || headers[0] != "Bearer tok-1" || headers[1] != "Bearer tok-2" {
		t.Fatalf("Authorization headers = %q", headers)
	}
}

func TestUnreadableTenantTokenSendsNothing(t *testing.T) {
	srv, rec := tenantStub(t, http.StatusOK)
	dir := t.TempDir()
	path := writeTokenFile(t, dir, "tok-1")
	t.Setenv("SESSION_PLATFORM_ENDPOINT", srv.URL)
	t.Setenv("SESSION_PLATFORM_TOKEN_FILE", path)
	c, err := FromEnv()
	if err != nil {
		t.Fatalf("FromEnv: %v", err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}

	_, err = c.ListSessions(context.Background())
	e, ok := err.(*Error)
	if !ok || !e.Unavailable() {
		t.Fatalf("ListSessions error = %v, want unavailable", err)
	}
	if headers, _ := rec.seen(); len(headers) != 0 {
		t.Fatalf("requests = %d, want 0 — a tokenless request is the operator view", len(headers))
	}
}

func TestFromEnvRefusesAMissingOrEmptyTokenFile(t *testing.T) {
	t.Setenv("SESSION_PLATFORM_ENDPOINT", "http://control-plane.session-platform.svc.cluster.local")
	dir := t.TempDir()
	t.Setenv("SESSION_PLATFORM_TOKEN_FILE", filepath.Join(dir, "absent"))
	if _, err := FromEnv(); err == nil {
		t.Error("FromEnv accepted a missing token file")
	}
	t.Setenv("SESSION_PLATFORM_TOKEN_FILE", writeTokenFile(t, dir, "  "))
	if _, err := FromEnv(); err == nil {
		t.Error("FromEnv accepted an empty token file")
	}
}

func TestNoTokenFileSendsNoAuthorization(t *testing.T) {
	srv, rec := tenantStub(t, http.StatusOK)
	t.Setenv("SESSION_PLATFORM_ENDPOINT", srv.URL)
	t.Setenv("SESSION_PLATFORM_TOKEN_FILE", "")
	c, err := FromEnv()
	if err != nil {
		t.Fatalf("FromEnv: %v", err)
	}
	_, _ = c.ListSessions(context.Background())
	_, present := rec.seen()
	if len(present) != 1 || present[0] {
		t.Fatalf("Authorization present = %v, want one request without it", present)
	}
}

func TestTenantRefusalNamesTheIdentity(t *testing.T) {
	for _, status := range []int{http.StatusUnauthorized, http.StatusForbidden} {
		srv, _ := tenantStub(t, status)
		t.Setenv("SESSION_PLATFORM_ENDPOINT", srv.URL)
		t.Setenv("SESSION_PLATFORM_TOKEN_FILE", writeTokenFile(t, t.TempDir(), "tok-1"))
		c, err := FromEnv()
		if err != nil {
			t.Fatalf("FromEnv: %v", err)
		}
		_, err = c.ListSessions(context.Background())
		if err == nil || !strings.Contains(err.Error(), "tenant identity") ||
			!strings.Contains(err.Error(), "belongs to no tenant") {
			t.Errorf("%d: error = %v", status, err)
		}
	}
}
