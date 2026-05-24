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

func TestCreateShortLivedTokenIssuesFromPolicy(t *testing.T) {
	var (
		gotAuth    string
		gotRegion  string
		gotPath    string
		gotPayload map[string]any
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotRegion = r.URL.Query().Get("region")
		gotPath = r.URL.Path
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &gotPayload)

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{
			"accessPolicyId": "ap-123",
			"name": "issued-name",
			"expiresAt": "2099-01-01T00:00:00Z",
			"token": "glc_issued"
		}`))
	}))
	defer srv.Close()

	c := &Client{
		managementToken: "glc_management",
		accessPolicyID:  "ap-123",
		region:          "prod-us-east-0",
		apiBase:         srv.URL,
		http:            srv.Client(),
	}

	before := time.Now().UTC()
	token, err := c.CreateShortLivedToken(context.Background())
	if err != nil {
		t.Fatalf("CreateShortLivedToken: %v", err)
	}

	if token.Token != "glc_issued" || token.ExpiresAt != "2099-01-01T00:00:00Z" || token.AccessPolicyID != "ap-123" {
		t.Fatalf("token = %+v", token)
	}
	if gotPath != "/v1/tokens" {
		t.Fatalf("path = %q", gotPath)
	}
	if gotRegion != "prod-us-east-0" {
		t.Fatalf("region = %q", gotRegion)
	}
	if gotAuth != "Bearer glc_management" {
		t.Fatalf("authorization = %q", gotAuth)
	}
	if gotPayload["accessPolicyId"] != "ap-123" {
		t.Fatalf("payload.accessPolicyId = %v", gotPayload["accessPolicyId"])
	}
	name, _ := gotPayload["name"].(string)
	if !strings.HasPrefix(name, "homelab-k3s-mcp-") {
		t.Fatalf("payload.name = %q", name)
	}
	if gotPayload["displayName"] != name {
		t.Fatalf("payload.displayName = %v, want %q", gotPayload["displayName"], name)
	}

	expiresAt, err := time.Parse(time.RFC3339, gotPayload["expiresAt"].(string))
	if err != nil {
		t.Fatalf("parse requested expiresAt: %v", err)
	}
	delta := expiresAt.Sub(before)
	if delta < 59*time.Minute || delta > 61*time.Minute {
		t.Fatalf("requested expiry %v from now is not ~1h", delta)
	}
}

func TestCreateShortLivedTokenWrapsAPIError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"message":"access denied"}`))
	}))
	defer srv.Close()

	c := &Client{accessPolicyID: "ap-123", region: "prod", apiBase: srv.URL, http: srv.Client()}
	_, err := c.CreateShortLivedToken(context.Background())
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

func TestCreateShortLivedTokenRejectsEmptyToken(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"expiresAt":"2099-01-01T00:00:00Z"}`))
	}))
	defer srv.Close()

	c := &Client{accessPolicyID: "ap-123", region: "prod", apiBase: srv.URL, http: srv.Client()}
	if _, err := c.CreateShortLivedToken(context.Background()); err == nil || !strings.Contains(err.Error(), "did not include a token") {
		t.Fatalf("error = %v", err)
	}
}

func TestUnavailableCreateShortLivedToken(t *testing.T) {
	_, err := NewUnavailable("boom").CreateShortLivedToken(context.Background())
	if err == nil || !strings.Contains(err.Error(), "grafana cloud unavailable: boom") {
		t.Fatalf("error = %v", err)
	}
}

func TestFromEnvUnsetTokenReturnsNil(t *testing.T) {
	t.Setenv("GRAFANA_CLOUD_ACCESS_POLICY_TOKEN", "")
	client, err := FromEnv()
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if client != nil {
		t.Fatalf("client = %v, want nil", client)
	}
}

func TestFromEnvRequiresPolicyAndRegion(t *testing.T) {
	t.Setenv("GRAFANA_CLOUD_ACCESS_POLICY_TOKEN", "glc_management")

	t.Setenv("GRAFANA_CLOUD_ACCESS_POLICY_ID", "")
	t.Setenv("GRAFANA_CLOUD_REGION", "prod-us-east-0")
	if _, err := FromEnv(); err == nil || !strings.Contains(err.Error(), "GRAFANA_CLOUD_ACCESS_POLICY_ID") {
		t.Fatalf("missing policy err = %v", err)
	}

	t.Setenv("GRAFANA_CLOUD_ACCESS_POLICY_ID", "ap-123")
	t.Setenv("GRAFANA_CLOUD_REGION", "")
	if _, err := FromEnv(); err == nil || !strings.Contains(err.Error(), "GRAFANA_CLOUD_REGION") {
		t.Fatalf("missing region err = %v", err)
	}
}

func TestFromEnvDefaultsAPIBase(t *testing.T) {
	t.Setenv("GRAFANA_CLOUD_ACCESS_POLICY_TOKEN", "glc_management")
	t.Setenv("GRAFANA_CLOUD_ACCESS_POLICY_ID", "ap-123")
	t.Setenv("GRAFANA_CLOUD_REGION", "prod-us-east-0")
	t.Setenv("GRAFANA_API_BASE_URL", "")

	client, err := FromEnv()
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if client == nil || client.apiBase != defaultAPIBase {
		t.Fatalf("apiBase = %v, want %s", client, defaultAPIBase)
	}
}
