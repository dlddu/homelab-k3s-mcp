// Command homelab-k3s-mcp serves the homelab k3s MCP endpoint over HTTP.
package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/dlddu/homelab-k3s-mcp/internal/auth"
	"github.com/dlddu/homelab-k3s-mcp/internal/awsconfig"
	"github.com/dlddu/homelab-k3s-mcp/internal/gatekeeper"
	"github.com/dlddu/homelab-k3s-mcp/internal/github"
	"github.com/dlddu/homelab-k3s-mcp/internal/grafana"
	"github.com/dlddu/homelab-k3s-mcp/internal/k8s"
	"github.com/dlddu/homelab-k3s-mcp/internal/mcp"
	"github.com/dlddu/homelab-k3s-mcp/internal/opensearch"
	"github.com/dlddu/homelab-k3s-mcp/internal/server"
	"github.com/dlddu/homelab-k3s-mcp/internal/sessionplatform"
	"github.com/dlddu/homelab-k3s-mcp/internal/version"
)

func main() {
	initLogger()

	addr := os.Getenv("LISTEN_ADDR")
	if addr == "" {
		addr = "0.0.0.0:3000"
	}

	ctx := context.Background()

	authCfg, err := auth.FromEnv(ctx)
	if err != nil {
		slog.Error("invalid auth config", "error", err)
		os.Exit(1)
	}
	if authCfg == nil {
		slog.Warn("MCP_AUTH_DISABLED is set: serving /mcp without authentication")
	} else {
		slog.Info("mcp authentication enabled",
			"api_keys", authCfg.APIKeyCount(),
			"oauth", authCfg.OAuthConfigured(),
		)
		if !authCfg.OAuthConfigured() {
			slog.Info("MCP_OAUTH_* not set: OAuth discovery disabled, authenticating with API keys only")
		}
	}

	// The tool registry is checked before anything is served. A tool that is
	// advertised without a declaration of the RBAC pairs it exercises would let
	// the approval gate judge on a table that does not describe reality, and
	// that mismatch passes silently at runtime — so it stops the process here
	// instead (prd-approval-gate AC1).
	if err := mcp.Validate(); err != nil {
		slog.Error("refusing to start", "error", err)
		os.Exit(1)
	}
	for tool, reason := range mcp.Exemptions() {
		slog.Warn("tool exercises a gated verb but runs outside the approval gate", "tool", tool, "reason", reason)
	}

	k8sSvc := buildK8sService()
	ghSvc := buildGitHubService()
	awsSvc := buildAWSService(ctx)
	grafanaSvc := buildGrafanaService()
	osSvc := buildOpenSearchService(ctx)
	sessionSvc := buildSessionPlatformService()
	gate, gatedKinds := buildGate()

	srv := &http.Server{
		Addr:    addr,
		Handler: server.App(authCfg, k8sSvc, ghSvc, awsSvc, grafanaSvc, osSvc, sessionSvc, mcp.WithGate(gate, gatedKinds)),
	}

	shutdownCtx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	go func() {
		slog.Info("homelab-k3s-mcp listening", "addr", addr)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			slog.Error("server error", "error", err)
			os.Exit(1)
		}
	}()

	<-shutdownCtx.Done()
	slog.Info("shutdown signal received")

	timeoutCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(timeoutCtx); err != nil {
		slog.Error("graceful shutdown failed", "error", err)
	}
}

func initLogger() {
	level := slog.LevelInfo
	if lv := os.Getenv("LOG_LEVEL"); lv != "" {
		_ = level.UnmarshalText([]byte(strings.ToLower(lv)))
	}
	handler := slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: level})
	slog.SetDefault(slog.New(handler))
}

func buildK8sService() k8s.Service {
	if disabled := os.Getenv("MCP_K8S_DISABLED"); disabled == "1" || disabled == "true" {
		slog.Warn("MCP_K8S_DISABLED is set: kubernetes tools will return errors")
		return k8s.NewUnavailable("kubernetes integration is disabled")
	}
	svc, err := k8s.New()
	if err != nil {
		slog.Error("failed to initialize kubernetes client; tools will return errors", "error", err)
		return k8s.NewUnavailable(err.Error())
	}
	return svc
}

func buildGitHubService() github.Service {
	client, err := github.FromEnv()
	if err != nil {
		slog.Error("failed to initialize github app client; tool will return errors", "error", err)
		return github.NewUnavailable(err.Error())
	}
	if client == nil {
		slog.Warn("GITHUB_APP_CLIENT_ID not set: github_app_installation_token tool will return errors")
		return github.NewUnavailable("")
	}
	slog.Info("github app credentials loaded")
	return client
}

func buildAWSService(ctx context.Context) awsconfig.Service {
	client, err := awsconfig.FromEnv(ctx)
	if err != nil {
		slog.Error("failed to initialize aws config client; tool will return errors", "error", err)
		return awsconfig.NewUnavailable(err.Error())
	}
	if client == nil {
		slog.Warn("AWS_CONFIG_S3_BUCKET not set: aws_config_get tool will return errors")
		return awsconfig.NewUnavailable("")
	}
	slog.Info("aws config integration loaded")
	return client
}

func buildOpenSearchService(ctx context.Context) opensearch.Service {
	client, err := opensearch.FromEnv(ctx)
	if err != nil {
		slog.Error("failed to initialize opensearch client; tools will return errors", "error", err)
		return opensearch.NewUnavailable(err.Error())
	}
	if client == nil {
		slog.Warn("OPENSEARCH_ENDPOINT not set: opensearch tools will return errors")
		return opensearch.NewUnavailable("")
	}
	slog.Info("opensearch integration loaded")
	return client
}

func buildSessionPlatformService() sessionplatform.Service {
	client, err := sessionplatform.FromEnv()
	if err != nil {
		slog.Error("failed to initialize session platform client; tool will return errors", "error", err)
		return sessionplatform.NewUnavailable(err.Error())
	}
	if client == nil {
		slog.Warn("SESSION_PLATFORM_ENDPOINT not set: session_list, session_read and session_write tools will return errors")
		return sessionplatform.NewUnavailable("")
	}
	slog.Info("session platform integration loaded")
	return client
}

// buildGate wires the approval gate. An unconfigured gate is not a startup
// error: the server's degradation rule is that a missing integration disables
// its own tools and nothing else. Here that "its own tools" happens to be every
// gated call, which is the intended shape — if the approval path is down,
// changing the cluster should stop (prd-approval-gate AC5).
func buildGate() (gatekeeper.Gate, []string) {
	cfg, err := gatekeeper.FromEnv()
	if err != nil {
		slog.Error("invalid approval gate config; gated calls will be refused", "error", err)
		return gatekeeper.NewUnavailable(err), nil
	}
	if cfg == nil {
		slog.Warn("GATEKEEPER_BASE_URL not set: gated calls will be refused")
		return gatekeeper.NewUnavailable(nil), nil
	}
	slog.Info("approval gate enabled",
		"timeout", cfg.Timeout,
		"poll_interval", cfg.PollInterval,
		"gated_kinds", cfg.GatedKinds,
		"push_notifications", cfg.UserID != "",
	)
	return gatekeeper.New(*cfg, version.Name), cfg.GatedKinds
}

func buildGrafanaService() grafana.Service {
	client, err := grafana.FromEnv()
	if err != nil {
		slog.Error("failed to initialize grafana cloud client; tool will return errors", "error", err)
		return grafana.NewUnavailable(err.Error())
	}
	if client == nil {
		slog.Warn("GRAFANA_ISSUER_TOKEN not set: grafana_token tool will return errors")
		return grafana.NewUnavailable("")
	}
	slog.Info("grafana cloud integration loaded")
	return client
}
