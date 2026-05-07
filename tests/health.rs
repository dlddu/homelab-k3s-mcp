use std::sync::Arc;

use axum::body::Body;
use axum::http::{Request, StatusCode};
use http_body_util::BodyExt;
use tower::ServiceExt;

fn k8s() -> Arc<dyn homelab_k3s_mcp::K8sService> {
    Arc::new(homelab_k3s_mcp::UnavailableK8s::default())
}

#[tokio::test]
async fn root_returns_service_name() {
    let response = homelab_k3s_mcp::app(None, k8s())
        .oneshot(Request::builder().uri("/").body(Body::empty()).unwrap())
        .await
        .unwrap();

    assert_eq!(response.status(), StatusCode::OK);
    let body = response.into_body().collect().await.unwrap().to_bytes();
    assert_eq!(&body[..], b"homelab-k3s-mcp");
}

#[tokio::test]
async fn healthz_returns_ok_json() {
    let response = homelab_k3s_mcp::app(None, k8s())
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
    let response = homelab_k3s_mcp::app(None, k8s())
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
    let response = homelab_k3s_mcp::app(None, k8s())
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

#[tokio::test]
async fn readyz_reports_503_when_kube_init_failed() {
    let unhealthy: Arc<dyn homelab_k3s_mcp::K8sService> =
        Arc::new(homelab_k3s_mcp::UnavailableK8s::init_failed(
            "init kube client: in-cluster: ...; kubeconfig: ...",
        ));
    let response = homelab_k3s_mcp::app(None, unhealthy)
        .oneshot(
            Request::builder()
                .uri("/readyz")
                .body(Body::empty())
                .unwrap(),
        )
        .await
        .unwrap();

    assert_eq!(response.status(), StatusCode::SERVICE_UNAVAILABLE);
    let body = response.into_body().collect().await.unwrap().to_bytes();
    let json: serde_json::Value = serde_json::from_slice(&body).unwrap();
    assert_eq!(json["status"], "kubernetes client unavailable");
}

#[tokio::test]
async fn healthz_stays_ok_when_kube_init_failed() {
    // /healthz is a liveness signal: the process itself is fine, even if
    // the kube client failed to initialize. Only /readyz should fail.
    let unhealthy: Arc<dyn homelab_k3s_mcp::K8sService> =
        Arc::new(homelab_k3s_mcp::UnavailableK8s::init_failed("boom"));
    let response = homelab_k3s_mcp::app(None, unhealthy)
        .oneshot(
            Request::builder()
                .uri("/healthz")
                .body(Body::empty())
                .unwrap(),
        )
        .await
        .unwrap();

    assert_eq!(response.status(), StatusCode::OK);
}
