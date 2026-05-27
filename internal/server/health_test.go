package server_test

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/dlddu/homelab-k3s-mcp/internal/server"
)

func jsonRequest(uri string, body any) *http.Request {
	var buf bytes.Buffer
	_ = json.NewEncoder(&buf).Encode(body)
	req := httptest.NewRequest(http.MethodPost, uri, &buf)
	req.Header.Set("Content-Type", "application/json")
	return req
}

func serve(handler http.Handler, req *http.Request) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec
}

func bodyJSON(t *testing.T, rec *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &m); err != nil {
		t.Fatalf("decode body: %v (body=%s)", err, rec.Body.String())
	}
	return m
}

// at walks a decoded JSON value by string keys and int indices.
func at(t *testing.T, v any, path ...any) any {
	t.Helper()
	for _, p := range path {
		switch key := p.(type) {
		case string:
			m, ok := v.(map[string]any)
			if !ok {
				t.Fatalf("expected object at %v, got %T", p, v)
			}
			v = m[key]
		case int:
			a, ok := v.([]any)
			if !ok {
				t.Fatalf("expected array at %v, got %T", p, v)
			}
			v = a[key]
		}
	}
	return v
}

func TestRootReturnsServiceName(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	server.App(nil, unavailableK8s(), unavailableGitHub(), unavailableAWS(), unavailableGrafana()).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	body, _ := io.ReadAll(rec.Body)
	if string(body) != "homelab-k3s-mcp" {
		t.Fatalf("body = %q, want homelab-k3s-mcp", string(body))
	}
}

func TestHealthzReturnsOK(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	server.App(nil, unavailableK8s(), unavailableGitHub(), unavailableAWS(), unavailableGrafana()).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	body := bodyJSON(t, rec)
	if body["status"] != "ok" {
		t.Fatalf("status = %v, want ok", body["status"])
	}
	if body["version"] != "0.1.0" {
		t.Fatalf("version = %v, want 0.1.0", body["version"])
	}
}

func TestReadyzReturnsReady(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/readyz", nil)
	server.App(nil, unavailableK8s(), unavailableGitHub(), unavailableAWS(), unavailableGrafana()).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	body := bodyJSON(t, rec)
	if body["status"] != "ready" {
		t.Fatalf("status = %v, want ready", body["status"])
	}
}

func TestUnknownRouteReturns404(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/does-not-exist", nil)
	server.App(nil, unavailableK8s(), unavailableGitHub(), unavailableAWS(), unavailableGrafana()).ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}
