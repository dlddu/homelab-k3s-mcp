package grafana

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestCreateTokenPostsExpectedRequest(t *testing.T) {
	var gotAuth, gotPath, gotContentType, gotRegion string
	var gotBody map[string]any

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotContentType = r.Header.Get("Content-Type")
		gotPath = r.URL.Path
		gotRegion = r.URL.Query().Get("region")
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &gotBody)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"token":"glc_secret","name":"` + gotBody["name"].(string) + `","expiresAt":"` + gotBody["expiresAt"].(string) + `"}`))
	}))
	defer srv.Close()

	c := &Client{
		issuerToken:  "glsa_issuer",
		readPolicyID: "policy-123",
		region:       "prod-us-east-0",
		apiBase:      srv.URL + "/api",
		metricsURL:   "https://prometheus-prod-99.grafana.net/api/prom",
		metricsUser:  "123456",
		logsURL:      "https://logs-prod-99.grafana.net",
		logsUser:     "654321",
		userAgent:    "homelab-k3s-mcp/test",
		http:         srv.Client(),
	}

	before := time.Now()
	creds, err := c.CreateToken(context.Background())
	after := time.Now()
	if err != nil {
		t.Fatalf("CreateToken: %v", err)
	}

	if creds.Token != "glc_secret" {
		t.Fatalf("token = %q, want glc_secret", creds.Token)
	}
	if creds.MetricsURL != "https://prometheus-prod-99.grafana.net/api/prom" || creds.MetricsUser != "123456" ||
		creds.LogsURL != "https://logs-prod-99.grafana.net" || creds.LogsUser != "654321" {
		t.Fatalf("credentials = %+v", creds)
	}
	if gotPath != "/api/v1/tokens" {
		t.Fatalf("path = %q, want /api/v1/tokens", gotPath)
	}
	if gotAuth != "Bearer glsa_issuer" {
		t.Fatalf("authorization = %q", gotAuth)
	}
	if gotContentType != "application/json" {
		t.Fatalf("content-type = %q", gotContentType)
	}
	if gotBody["accessPolicyId"] != "policy-123" {
		t.Fatalf("accessPolicyId = %v", gotBody["accessPolicyId"])
	}
	if gotRegion != "prod-us-east-0" {
		t.Fatalf("region query = %q, want prod-us-east-0", gotRegion)
	}
	name, _ := gotBody["name"].(string)
	if !strings.HasPrefix(name, "homelab-k3s-mcp-") {
		t.Fatalf("name = %q, want homelab-k3s-mcp- prefix", name)
	}
	if gotBody["displayName"] != name {
		t.Fatalf("displayName = %v, want %q", gotBody["displayName"], name)
	}

	expiresRaw, _ := gotBody["expiresAt"].(string)
	expiresAt, perr := time.Parse(time.RFC3339, expiresRaw)
	if perr != nil {
		t.Fatalf("expiresAt %q not RFC3339: %v", expiresRaw, perr)
	}
	if expiresAt.Before(before.Add(tokenTTL-time.Minute)) || expiresAt.After(after.Add(tokenTTL+time.Minute)) {
		t.Fatalf("expiresAt = %s, want ~1h from now", expiresAt)
	}
}

func TestCreateTokenWrapsNon2xx(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"message":"access denied"}`))
	}))
	defer srv.Close()

	c := &Client{readPolicyID: "p", apiBase: srv.URL, http: srv.Client()}
	_, err := c.CreateToken(context.Background())
	if err == nil {
		t.Fatal("expected error")
	}
	var gErr *Error
	if !errors.As(err, &gErr) || gErr.kind != kindAPI {
		t.Fatalf("error = %v (%T)", err, err)
	}
	if !strings.Contains(err.Error(), "access denied") {
		t.Fatalf("error = %q", err.Error())
	}
}

func TestCreateTokenRejectsResponseWithoutToken(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"name":"no-token"}`))
	}))
	defer srv.Close()

	c := &Client{readPolicyID: "p", apiBase: srv.URL, http: srv.Client()}
	_, err := c.CreateToken(context.Background())
	if err == nil || !strings.Contains(err.Error(), "did not include a token") {
		t.Fatalf("error = %v", err)
	}
}

func TestUnavailableCreateToken(t *testing.T) {
	_, err := NewUnavailable("boom").CreateToken(context.Background())
	if err == nil || !strings.Contains(err.Error(), "grafana cloud unavailable: boom") {
		t.Fatalf("error = %v", err)
	}
}

func TestFromEnvUnsetIssuerReturnsNil(t *testing.T) {
	t.Setenv("GRAFANA_ISSUER_TOKEN", "")
	client, err := FromEnv()
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if client != nil {
		t.Fatalf("client = %v, want nil", client)
	}
}

func TestFromEnvRequiresReadPolicyID(t *testing.T) {
	t.Setenv("GRAFANA_ISSUER_TOKEN", "glsa_issuer")
	t.Setenv("GRAFANA_READ_POLICY_ID", "")
	if _, err := FromEnv(); err == nil || !strings.Contains(err.Error(), "GRAFANA_READ_POLICY_ID") {
		t.Fatalf("missing policy id err = %v", err)
	}
}

// setQueryEnv sets the four metrics/logs query variables that FromEnv requires
// once the integration is enabled.
func setQueryEnv(t *testing.T) {
	t.Helper()
	t.Setenv("GRAFANA_METRICS_URL", "https://prometheus-prod-99.grafana.net/api/prom")
	t.Setenv("GRAFANA_METRICS_USER", "123456")
	t.Setenv("GRAFANA_LOGS_URL", "https://logs-prod-99.grafana.net")
	t.Setenv("GRAFANA_LOGS_USER", "654321")
}

func TestFromEnvRequiresQueryConfig(t *testing.T) {
	t.Setenv("GRAFANA_ISSUER_TOKEN", "glsa_issuer")
	t.Setenv("GRAFANA_READ_POLICY_ID", "policy-123")
	setQueryEnv(t)
	t.Setenv("GRAFANA_LOGS_URL", "")
	if _, err := FromEnv(); err == nil || !strings.Contains(err.Error(), "GRAFANA_LOGS_URL") {
		t.Fatalf("missing logs url err = %v", err)
	}
}

func TestFromEnvDefaultsAPIBase(t *testing.T) {
	t.Setenv("GRAFANA_ISSUER_TOKEN", "glsa_issuer")
	t.Setenv("GRAFANA_READ_POLICY_ID", "policy-123")
	setQueryEnv(t)
	t.Setenv("GRAFANA_API_URL", "")
	client, err := FromEnv()
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if client == nil || client.apiBase != defaultAPIBase {
		t.Fatalf("apiBase = %v, want %s", client, defaultAPIBase)
	}
}

func TestFromEnvTrimsAPIBaseTrailingSlash(t *testing.T) {
	t.Setenv("GRAFANA_ISSUER_TOKEN", "glsa_issuer")
	t.Setenv("GRAFANA_READ_POLICY_ID", "policy-123")
	setQueryEnv(t)
	t.Setenv("GRAFANA_API_URL", "https://grafana.example.com/")
	client, err := FromEnv()
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if client.apiBase != "https://grafana.example.com" {
		t.Fatalf("apiBase = %q", client.apiBase)
	}
}
