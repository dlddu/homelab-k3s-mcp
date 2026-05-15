use std::io::IsTerminal;
use std::net::SocketAddr;
use std::sync::Arc;

use homelab_k3s_mcp::{
    GitHubAppClient, GitHubAppService, K8sService, KubeService, UnavailableGitHubApp,
    UnavailableK8s,
};
use tokio::net::TcpListener;
use tokio::signal;
use tracing_subscriber::EnvFilter;

#[tokio::main]
async fn main() {
    tracing_subscriber::fmt()
        .with_env_filter(
            EnvFilter::try_from_default_env().unwrap_or_else(|_| EnvFilter::new("info")),
        )
        .with_ansi(std::io::stdout().is_terminal())
        .init();

    let addr: SocketAddr = std::env::var("LISTEN_ADDR")
        .unwrap_or_else(|_| "0.0.0.0:3000".to_string())
        .parse()
        .expect("invalid LISTEN_ADDR");

    let auth = homelab_k3s_mcp::AuthConfig::from_env()
        .await
        .expect("invalid auth config");
    if auth.is_none() {
        tracing::warn!("MCP_AUTH_DISABLED is set: serving /mcp without authentication");
    }

    let k8s: Arc<dyn K8sService> = if matches!(
        std::env::var("MCP_K8S_DISABLED").as_deref(),
        Ok("1" | "true")
    ) {
        tracing::warn!("MCP_K8S_DISABLED is set: kubernetes tools will return errors");
        Arc::new(UnavailableK8s::new("kubernetes integration is disabled"))
    } else {
        match KubeService::try_new().await {
            Ok(svc) => Arc::new(svc),
            Err(err) => {
                tracing::error!(%err, "failed to initialize kubernetes client; tools will return errors");
                Arc::new(UnavailableK8s::new(err.to_string()))
            }
        }
    };

    let github: Arc<dyn GitHubAppService> = match GitHubAppClient::from_env() {
        Ok(Some(client)) => {
            tracing::info!("github app credentials loaded");
            Arc::new(client)
        }
        Ok(None) => {
            tracing::warn!(
                "GITHUB_APP_CLIENT_ID not set: github_app_installation_token tool will return errors"
            );
            Arc::new(UnavailableGitHubApp::default())
        }
        Err(err) => {
            tracing::error!(%err, "failed to initialize github app client; tool will return errors");
            Arc::new(UnavailableGitHubApp::new(err))
        }
    };

    let listener = TcpListener::bind(addr).await.expect("bind listener");
    let local = listener.local_addr().expect("local addr");
    tracing::info!(%local, "homelab-k3s-mcp listening");

    axum::serve(listener, homelab_k3s_mcp::app(auth, k8s, github))
        .with_graceful_shutdown(shutdown_signal())
        .await
        .expect("server error");
}

async fn shutdown_signal() {
    let ctrl_c = async {
        signal::ctrl_c().await.expect("install Ctrl+C handler");
    };

    #[cfg(unix)]
    let terminate = async {
        signal::unix::signal(signal::unix::SignalKind::terminate())
            .expect("install SIGTERM handler")
            .recv()
            .await;
    };

    #[cfg(not(unix))]
    let terminate = std::future::pending::<()>();

    tokio::select! {
        _ = ctrl_c => {},
        _ = terminate => {},
    }

    tracing::info!("shutdown signal received");
}
