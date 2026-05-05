use std::io::IsTerminal;
use std::net::SocketAddr;

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

    let listener = TcpListener::bind(addr).await.expect("bind listener");
    let local = listener.local_addr().expect("local addr");
    tracing::info!(%local, "homelab-k3s-mcp listening");

    axum::serve(listener, homelab_k3s_mcp::app(auth))
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
