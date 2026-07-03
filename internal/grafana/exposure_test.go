package grafana

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestCreateTokenNeverEchoesIssuerToken covers grafana_token AC4 (발급자 토큰
// 비노출). The issuer token is a long-lived secret used only to authenticate the
// mint request; only the freshly minted short-lived token may leave the
// process. This previously had no automated coverage.
func TestCreateTokenNeverEchoesIssuerToken(t *testing.T) {
	const issuerToken = "glsa_super_secret_issuer_do_not_leak"

	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"token":"glc_freshly_minted","expiresAt":"2026-01-01T00:00:00Z"}`))
	}))
	defer srv.Close()

	c := &Client{
		issuerToken:  issuerToken,
		readPolicyID: "policy-123",
		apiBase:      srv.URL + "/api",
		metricsURL:   "https://prometheus-prod-99.grafana.net/api/prom",
		metricsUser:  "111111",
		logsURL:      "https://logs-prod-99.grafana.net",
		logsUser:     "222222",
		userAgent:    "homelab-k3s-mcp/test",
		http:         srv.Client(),
	}

	creds, err := c.CreateToken(context.Background())
	if err != nil {
		t.Fatalf("CreateToken: %v", err)
	}

	// The secret must actually have been used as the request credential;
	// otherwise this test would also pass for an implementation that never
	// touches the issuer token, making the non-exposure check meaningless.
	if gotAuth != "Bearer "+issuerToken {
		t.Fatalf("upstream Authorization = %q, want the issuer token as bearer", gotAuth)
	}
	if creds.Token != "glc_freshly_minted" {
		t.Fatalf("minted token = %q, want glc_freshly_minted", creds.Token)
	}

	// No returned field may contain the issuer token, verbatim or embedded.
	fields := map[string]string{
		"Token":       creds.Token,
		"ExpiresAt":   creds.ExpiresAt,
		"MetricsURL":  creds.MetricsURL,
		"MetricsUser": creds.MetricsUser,
		"LogsURL":     creds.LogsURL,
		"LogsUser":    creds.LogsUser,
	}
	for name, val := range fields {
		if strings.Contains(val, issuerToken) {
			t.Errorf("Credentials.%s leaks the issuer token: %q", name, val)
		}
	}
}
