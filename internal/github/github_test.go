package github

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestCreateInstallationTokenNeverExposesSigningKey covers
// github_app_installation_token AC4 (베이스 키 비노출). The RSA private key — and
// the app JWT minted from it that authenticates the mint request — are
// long-lived secrets; only the short-lived installation token that GitHub
// issues may leave the process. This previously had no automated coverage.
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
		gotAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
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
