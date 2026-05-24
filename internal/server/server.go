// Package server wires the HTTP routes for the MCP service.
package server

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"time"

	"github.com/dlddu/homelab-k3s-mcp/internal/auth"
	"github.com/dlddu/homelab-k3s-mcp/internal/github"
	"github.com/dlddu/homelab-k3s-mcp/internal/k8s"
	"github.com/dlddu/homelab-k3s-mcp/internal/mcp"
	"github.com/dlddu/homelab-k3s-mcp/internal/version"
)

type health struct {
	Status  string `json:"status"`
	Version string `json:"version"`
}

// App builds the HTTP handler for the service. When authCfg is nil the /mcp
// endpoint is served without authentication.
func App(authCfg *auth.Config, k8sSvc k8s.Service, ghSvc github.Service) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", root)
	mux.HandleFunc("GET /healthz", healthz)
	mux.HandleFunc("GET /readyz", readyz)

	mcpHandler := mcp.NewHandler(k8sSvc, ghSvc)
	if authCfg != nil {
		mux.Handle("GET /.well-known/oauth-protected-resource", auth.MetadataHandler(authCfg))
		mux.Handle("POST /mcp", authCfg.RequireBearer(mcpHandler))
	} else {
		mux.Handle("POST /mcp", mcpHandler)
	}

	return logging(mux)
}

func root(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = w.Write([]byte(version.Name))
}

func healthz(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, health{Status: "ok", Version: version.Version})
}

func readyz(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, health{Status: "ready", Version: version.Version})
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

// statusRecorder captures the response status code for logging.
type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(code int) {
	r.status = code
	r.ResponseWriter.WriteHeader(code)
}

// silentPaths are health/readiness probe endpoints excluded from access logs
// to avoid flooding them with kubelet probe traffic.
var silentPaths = map[string]bool{
	"/healthz": true,
	"/readyz":  true,
}

func logging(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if silentPaths[r.URL.Path] {
			next.ServeHTTP(w, r)
			return
		}
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rec, r)
		slog.Info("http request",
			"method", r.Method,
			"path", r.URL.Path,
			"status", rec.status,
			"duration_ms", time.Since(start).Milliseconds(),
		)
	})
}
