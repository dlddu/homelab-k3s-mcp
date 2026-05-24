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
	"github.com/dlddu/homelab-k3s-mcp/internal/github"
	"github.com/dlddu/homelab-k3s-mcp/internal/k8s"
	"github.com/dlddu/homelab-k3s-mcp/internal/server"
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
	}

	k8sSvc := buildK8sService()
	ghSvc := buildGitHubService()
	awsSvc := buildAWSService(ctx)

	srv := &http.Server{
		Addr:    addr,
		Handler: server.App(authCfg, k8sSvc, ghSvc, awsSvc),
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
