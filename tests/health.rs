use std::sync::Arc;

use axum::body::Body;
use axum::http::{Request, StatusCode};
use http_body_util::BodyExt;
use tower::ServiceExt;

fn k8s() -> Arc<dyn homelab_k3s_mcp::K8sService> {
    Arc::new(homelab_k3s_mcp::UnavailableK8s::default())
}

fn github() -> Arc<dyn homelab_k3s_mcp::GitHubAppService> {
    Arc::new(homelab_k3s_mcp::UnavailableGitHubApp::default())
}

fn aws() -> Arc<dyn homelab_k3s_mcp::AwsConfigService> {
    Arc::new(homelab_k3s_mcp::UnavailableAwsConfig::default())
}

#[tokio::test]
async fn root_returns_service_name() {
    let response = homelab_k3s_mcp::app(None, k8s(), github(), aws())
        .oneshot(Request::builder().uri("/").body(Body::empty()).unwrap())
        .await
        .unwrap();

    assert_eq!(response.status(), StatusCode::OK);
    let body = response.into_body().collect().await.unwrap().to_bytes();
    assert_eq!(&body[..], b"homelab-k3s-mcp");
}

#[tokio::test]
async fn healthz_returns_ok_json() {
    let response = homelab_k3s_mcp::app(None, k8s(), github(), aws())
        .oneshot(
            Request::builder()
                .uri("/healthz")
                .body(Body::empty())
                .unwrap(),
        )
        .await
        .unwrap();

    assert_eq!(response.status(), StatusCode::OK);
    let body = response.into_body().collect().await.unwrap().to_bytes();
    let json: serde_json::Value = serde_json::from_slice(&body).unwrap();
    assert_eq!(json["status"], "ok");
    assert_eq!(json["version"], env!("CARGO_PKG_VERSION"));
}

#[tokio::test]
async fn readyz_returns_ready_json() {
    let response = homelab_k3s_mcp::app(None, k8s(), github(), aws())
        .oneshot(
            Request::builder()
                .uri("/readyz")
                .body(Body::empty())
                .unwrap(),
        )
        .await
        .unwrap();

    assert_eq!(response.status(), StatusCode::OK);
    let body = response.into_body().collect().await.unwrap().to_bytes();
    let json: serde_json::Value = serde_json::from_slice(&body).unwrap();
    assert_eq!(json["status"], "ready");
}

#[tokio::test]
async fn unknown_route_returns_404() {
    let response = homelab_k3s_mcp::app(None, k8s(), github(), aws())
        .oneshot(
            Request::builder()
                .uri("/does-not-exist")
                .body(Body::empty())
                .unwrap(),
        )
        .await
        .unwrap();

    assert_eq!(response.status(), StatusCode::NOT_FOUND);
}
