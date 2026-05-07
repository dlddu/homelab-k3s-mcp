use std::net::{Ipv4Addr, SocketAddr};
use std::sync::Arc;

use tokio::net::TcpListener;

async fn spawn_server() -> SocketAddr {
    let listener = TcpListener::bind(SocketAddr::from((Ipv4Addr::LOCALHOST, 0)))
        .await
        .expect("bind ephemeral port");
    let addr = listener.local_addr().expect("local addr");

    let k8s: Arc<dyn homelab_k3s_mcp::K8sService> =
        Arc::new(homelab_k3s_mcp::UnavailableK8s::default());

    tokio::spawn(async move {
        axum::serve(listener, homelab_k3s_mcp::app(None, k8s))
            .await
            .expect("server error");
    });

    addr
}

#[tokio::test]
async fn server_serves_healthz_over_tcp() {
    let addr = spawn_server().await;

    let body: serde_json::Value = reqwest::Client::new()
        .get(format!("http://{addr}/healthz"))
        .send()
        .await
        .expect("send request")
        .error_for_status()
        .expect("non-2xx status")
        .json()
        .await
        .expect("decode json");

    assert_eq!(body["status"], "ok");
    assert_eq!(body["version"], env!("CARGO_PKG_VERSION"));
}

#[tokio::test]
async fn server_serves_root_over_tcp() {
    let addr = spawn_server().await;

    let text = reqwest::get(format!("http://{addr}/"))
        .await
        .expect("send request")
        .text()
        .await
        .expect("read body");

    assert_eq!(text, "homelab-k3s-mcp");
}
