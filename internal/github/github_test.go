package github

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"io"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
)

var statusPathRe = regexp.MustCompile(`^/repos/[^/]+/[^/]+/statuses/[0-9a-fA-F]{40}$`)

type recordedRequest struct {
	Method string
	Path   string
	Body   map[string]any
}

type fakeGitHub struct {
	// nil makes GET /app/installations/{id} answer 500.
	installation map[string]any
	// nil echoes back whatever the mint request asked for, as GitHub does.
	mintedPermissions map[string]any
	// empty omits `account` from the installation.
	account string
	// 0 is 201. 422 answers with GitHub's wording for an unknown commit.
	statusStatus int

	requests []recordedRequest
}

func (f *fakeGitHub) handler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		var body map[string]any
		if len(raw) > 0 {
			_ = json.Unmarshal(raw, &body)
		}
		f.requests = append(f.requests, recordedRequest{Method: r.Method, Path: r.URL.Path, Body: body})

		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/app/installations/42":
			if f.installation == nil {
				w.WriteHeader(http.StatusInternalServerError)
				_, _ = w.Write([]byte(`{"message":"Server Error"}`))
				return
			}
			w.WriteHeader(http.StatusOK)
			body := map[string]any{"id": 42, "permissions": f.installation}
			if f.account != "" {
				body["account"] = map[string]any{"login": f.account}
			}
			_ = json.NewEncoder(w).Encode(body)
		case r.Method == http.MethodPost && r.URL.Path == "/app/installations/42/access_tokens":
			permissions := f.mintedPermissions
			if permissions == nil {
				permissions, _ = body["permissions"].(map[string]any)
			}
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"token":                "ghs_mock_42",
				"expires_at":           "2099-01-01T00:00:00Z",
				"permissions":          permissions,
				"repository_selection": "all",
			})
		case r.Method == http.MethodPost && statusPathRe.MatchString(r.URL.Path):
			if f.statusStatus == http.StatusUnprocessableEntity {
				w.WriteHeader(http.StatusUnprocessableEntity)
				_, _ = w.Write([]byte(`{"message":"No commit found for SHA"}`))
				return
			}
			w.WriteHeader(http.StatusCreated)
			state, _ := body["state"].(string)
			statusContext, _ := body["context"].(string)
			description, _ := body["description"].(string)
			targetURL, _ := body["target_url"].(string)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id":      9001,
				"state":   state,
				"context": statusContext,
				// No "sha", as on real GitHub. A stub that echoes it fills in
				// the field for the tool and hides a missing fill (seen live).
				"description": description,
				"target_url":  targetURL,
				"created_at":  "2026-09-20T00:00:00Z",
			})
		case r.Method == http.MethodDelete && r.URL.Path == "/installation/token":
			w.WriteHeader(http.StatusNoContent)
		default:
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"message":"Not Found"}`))
		}
	}
}

func (f *fakeGitHub) bodyOf(method, path string) map[string]any {
	for _, req := range f.requests {
		if req.Method == method && req.Path == path {
			return req.Body
		}
	}
	return nil
}

func (f *fakeGitHub) countOf(method, path string) int {
	n := 0
	for _, req := range f.requests {
		if req.Method == method && req.Path == path {
			n++
		}
	}
	return n
}

func (f *fakeGitHub) mintCount() int {
	return f.countOf(http.MethodPost, "/app/installations/42/access_tokens")
}

func newTestClient(t *testing.T, fake *fakeGitHub) *Client {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	srv := httptest.NewServer(fake.handler())
	t.Cleanup(srv.Close)
	return &Client{
		clientID:       "Iv1.test",
		installationID: 42,
		privateKey:     key,
		apiBase:        srv.URL,
		userAgent:      "homelab-k3s-mcp/test",
		http:           srv.Client(),
	}
}

// TestGitHubTokenRejectsStatusesWrite covers AC5 clause 1 (explicit write).
func TestGitHubTokenRejectsStatusesWrite(t *testing.T) {
	cases := []struct {
		name        string
		permissions map[string]any
	}{
		{
			name:        "alone",
			permissions: map[string]any{"statuses": "write"},
		},
		{
			name:        "alongside another permission",
			permissions: map[string]any{"contents": "read", "statuses": "write"},
		},
		{
			name:        "mixed case",
			permissions: map[string]any{"statuses": "WRITE"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fake := &fakeGitHub{installation: map[string]any{"contents": "read"}}
			c := newTestClient(t, fake)

			tok, err := c.CreateInstallationToken(context.Background(), nil, tc.permissions)
			if err == nil {
				t.Fatalf("CreateInstallationToken = %+v, want an error", tok)
			}
			if !strings.Contains(err.Error(), "github_commit_status_create") {
				t.Errorf("error = %q, want it to point at github_commit_status_create", err)
			}
			if got := len(fake.requests); got != 0 {
				t.Errorf("upstream saw %d requests (%+v), want 0", got, fake.requests)
			}
		})
	}
}

// TestGitHubTokenAllowsStatusesRead covers AC5 clause 1 (explicit read).
func TestGitHubTokenAllowsStatusesRead(t *testing.T) {
	fake := &fakeGitHub{}
	c := newTestClient(t, fake)

	tok, err := c.CreateInstallationToken(context.Background(), nil, map[string]any{"statuses": "read"})
	if err != nil {
		t.Fatalf("CreateInstallationToken: %v", err)
	}
	if got := permissionLevel(tok.Permissions, "statuses"); got != "read" {
		t.Errorf("minted statuses = %q, want read", got)
	}
	if got := fake.mintCount(); got != 1 {
		t.Errorf("mint requests = %d, want 1", got)
	}
	if got := fake.countOf(http.MethodGet, "/app/installations/42"); got != 0 {
		t.Errorf("installation lookups = %d, want 0", got)
	}
}

// TestGitHubTokenDefaultDowngradesStatusesToRead covers AC5 clause 2.
func TestGitHubTokenDefaultDowngradesStatusesToRead(t *testing.T) {
	fake := &fakeGitHub{installation: map[string]any{
		"contents": "write",
		"metadata": "read",
		"statuses": "write",
	}}
	c := newTestClient(t, fake)

	tok, err := c.CreateInstallationToken(context.Background(), nil, nil)
	if err != nil {
		t.Fatalf("CreateInstallationToken: %v", err)
	}

	if got := fake.mintCount(); got != 1 {
		t.Fatalf("mint requests = %d, want 1", got)
	}
	var mint recordedRequest
	for _, req := range fake.requests {
		if req.Method == http.MethodPost {
			mint = req
		}
	}
	sent, ok := mint.Body["permissions"].(map[string]any)
	if !ok {
		t.Fatalf("mint body = %+v, want an explicit permissions map", mint.Body)
	}
	if got := permissionLevel(sent, "statuses"); got != "read" {
		t.Errorf("requested statuses = %q, want read", got)
	}
	// The downgrade must not double as a scope change.
	if got := permissionLevel(sent, "contents"); got != "write" {
		t.Errorf("requested contents = %q, want write", got)
	}
	if got := permissionLevel(tok.Permissions, "statuses"); got != "read" {
		t.Errorf("minted statuses = %q, want read", got)
	}
}

// TestGitHubTokenRefusesWhenInstallationUnreadable covers AC5 clause 2's
// fail-closed half.
func TestGitHubTokenRefusesWhenInstallationUnreadable(t *testing.T) {
	fake := &fakeGitHub{installation: nil}
	c := newTestClient(t, fake)

	tok, err := c.CreateInstallationToken(context.Background(), nil, nil)
	if err == nil {
		t.Fatalf("CreateInstallationToken = %+v, want an error", tok)
	}
	if got := fake.mintCount(); got != 0 {
		t.Errorf("mint requests = %d, want 0 — refusal must not fall back to an omitted request", got)
	}
}

// TestGitHubTokenRevokesTokenCarryingStatusesWrite covers AC5 clause 3.
func TestGitHubTokenRevokesTokenCarryingStatusesWrite(t *testing.T) {
	fake := &fakeGitHub{
		installation:      map[string]any{"contents": "read"},
		mintedPermissions: map[string]any{"contents": "read", "statuses": "write"},
	}
	c := newTestClient(t, fake)

	tok, err := c.CreateInstallationToken(context.Background(), nil, map[string]any{"contents": "read"})
	if err == nil {
		t.Fatalf("CreateInstallationToken = %+v, want an error", tok)
	}
	if got := fake.countOf(http.MethodDelete, "/installation/token"); got != 1 {
		t.Errorf("revocations = %d, want 1", got)
	}
	if strings.Contains(err.Error(), "ghs_mock_42") {
		t.Errorf("error leaks the revoked token: %q", err)
	}
	if tok != nil {
		t.Errorf("token = %+v, want nil", tok)
	}
}

// TestCreateInstallationTokenNeverExposesSigningKey covers
// github_app_installation_token AC4 (베이스 키 비노출). The RSA private key — and
// the app JWT minted from it that authenticates the mint request — are
// long-lived secrets; only the short-lived installation token that GitHub
// issues may leave the process.
func TestCreateInstallationTokenNeverExposesSigningKey(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	keyPEM := string(pem.EncodeToMemory(&pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: x509.MarshalPKCS1PrivateKey(key),
	}))

	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		// gotAuth must stay the mint call's header, not the AC5 lookup's.
		if r.Method == http.MethodGet {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"id":42,"permissions":{"contents":"read","metadata":"read"}}`))
			return
		}
		gotAuth = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"token":"ghs_freshly_minted","expires_at":"2026-01-01T00:00:00Z","repository_selection":"all"}`))
	}))
	defer srv.Close()

	c := &Client{
		clientID:       "Iv1.test",
		installationID: 42,
		privateKey:     key,
		apiBase:        srv.URL,
		userAgent:      "homelab-k3s-mcp/test",
		http:           srv.Client(),
	}

	tok, err := c.CreateInstallationToken(context.Background(), nil, nil)
	if err != nil {
		t.Fatalf("CreateInstallationToken: %v", err)
	}

	// The request must be authenticated with an app JWT signed by the key, so
	// echoing that JWT would itself be a leak of derived signing material.
	appJWT := strings.TrimPrefix(gotAuth, "Bearer ")
	if appJWT == gotAuth || strings.Count(appJWT, ".") != 2 {
		t.Fatalf("upstream Authorization = %q, want a signed app JWT", gotAuth)
	}
	if tok.Token != "ghs_freshly_minted" {
		t.Fatalf("installation token = %q, want ghs_freshly_minted", tok.Token)
	}

	// No returned field may carry the app JWT or the private key material.
	fields := map[string]string{
		"Token":               tok.Token,
		"ExpiresAt":           tok.ExpiresAt,
		"RepositorySelection": tok.RepositorySelection,
	}
	for name, val := range fields {
		if strings.Contains(val, appJWT) {
			t.Errorf("InstallationToken.%s leaks the app JWT", name)
		}
		if strings.Contains(val, keyPEM) || strings.Contains(val, "PRIVATE KEY") {
			t.Errorf("InstallationToken.%s leaks private key material: %q", name, val)
		}
	}
}
