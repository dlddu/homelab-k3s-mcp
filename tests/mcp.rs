use std::sync::{Arc, Mutex};

use async_trait::async_trait;
use axum::body::Body;
use axum::http::{Request, StatusCode};
use homelab_k3s_mcp::k8s::{
    DaemonSetSummary, DeploymentSummary, K8sError, K8sService, StatefulSetSummary,
};
use http_body_util::BodyExt;
use serde_json::{json, Value};
use tower::ServiceExt;

#[derive(Default)]
struct FakeK8s {
    pub deployments: Vec<DeploymentSummary>,
    pub statefulsets: Vec<StatefulSetSummary>,
    pub daemonsets: Vec<DaemonSetSummary>,
    pub last_list_namespace: Mutex<Option<String>>,
    pub restarts: Mutex<Vec<(String, String, String)>>,
}

#[async_trait]
impl K8sService for FakeK8s {
    async fn list_deployments(
        &self,
        namespace: Option<&str>,
    ) -> Result<Vec<DeploymentSummary>, K8sError> {
        *self.last_list_namespace.lock().unwrap() = namespace.map(str::to_owned);
        Ok(clone_deployments(&self.deployments))
    }

    async fn list_statefulsets(
        &self,
        namespace: Option<&str>,
    ) -> Result<Vec<StatefulSetSummary>, K8sError> {
        *self.last_list_namespace.lock().unwrap() = namespace.map(str::to_owned);
        Ok(clone_statefulsets(&self.statefulsets))
    }

    async fn list_daemonsets(
        &self,
        namespace: Option<&str>,
    ) -> Result<Vec<DaemonSetSummary>, K8sError> {
        *self.last_list_namespace.lock().unwrap() = namespace.map(str::to_owned);
        Ok(clone_daemonsets(&self.daemonsets))
    }

    async fn rollout_restart_deployment(
        &self,
        namespace: &str,
        name: &str,
    ) -> Result<String, K8sError> {
        self.restarts
            .lock()
            .unwrap()
            .push(("Deployment".into(), namespace.into(), name.into()));
        Ok("2026-05-07T00:00:00Z".into())
    }

    async fn rollout_restart_statefulset(
        &self,
        namespace: &str,
        name: &str,
    ) -> Result<String, K8sError> {
        self.restarts
            .lock()
            .unwrap()
            .push(("StatefulSet".into(), namespace.into(), name.into()));
        Ok("2026-05-07T00:00:00Z".into())
    }

    async fn rollout_restart_daemonset(
        &self,
        namespace: &str,
        name: &str,
    ) -> Result<String, K8sError> {
        self.restarts
            .lock()
            .unwrap()
            .push(("DaemonSet".into(), namespace.into(), name.into()));
        Ok("2026-05-07T00:00:00Z".into())
    }
}

fn clone_deployments(items: &[DeploymentSummary]) -> Vec<DeploymentSummary> {
    items
        .iter()
        .map(|d| DeploymentSummary {
            name: d.name.clone(),
            namespace: d.namespace.clone(),
            replicas: d.replicas,
            ready_replicas: d.ready_replicas,
            updated_replicas: d.updated_replicas,
            available_replicas: d.available_replicas,
            creation_timestamp: d.creation_timestamp.clone(),
        })
        .collect()
}

fn clone_statefulsets(items: &[StatefulSetSummary]) -> Vec<StatefulSetSummary> {
    items
        .iter()
        .map(|s| StatefulSetSummary {
            name: s.name.clone(),
            namespace: s.namespace.clone(),
            replicas: s.replicas,
            ready_replicas: s.ready_replicas,
            updated_replicas: s.updated_replicas,
            current_replicas: s.current_replicas,
            creation_timestamp: s.creation_timestamp.clone(),
        })
        .collect()
}

fn clone_daemonsets(items: &[DaemonSetSummary]) -> Vec<DaemonSetSummary> {
    items
        .iter()
        .map(|d| DaemonSetSummary {
            name: d.name.clone(),
            namespace: d.namespace.clone(),
            desired_number_scheduled: d.desired_number_scheduled,
            current_number_scheduled: d.current_number_scheduled,
            number_ready: d.number_ready,
            number_available: d.number_available,
            updated_number_scheduled: d.updated_number_scheduled,
            creation_timestamp: d.creation_timestamp.clone(),
        })
        .collect()
}

fn unavailable_k8s() -> Arc<dyn K8sService> {
    Arc::new(homelab_k3s_mcp::UnavailableK8s::default())
}

fn json_request(uri: &str, body: Value) -> Request<Body> {
    Request::builder()
        .method("POST")
        .uri(uri)
        .header("content-type", "application/json")
        .body(Body::from(body.to_string()))
        .unwrap()
}

async fn body_json(response: axum::response::Response) -> Value {
    let bytes = response.into_body().collect().await.unwrap().to_bytes();
    serde_json::from_slice(&bytes).unwrap()
}

#[tokio::test]
async fn initialize_returns_server_info() {
    let response = homelab_k3s_mcp::app(None, unavailable_k8s())
        .oneshot(json_request(
            "/mcp",
            json!({"jsonrpc": "2.0", "id": 1, "method": "initialize"}),
        ))
        .await
        .unwrap();

    assert_eq!(response.status(), StatusCode::OK);
    let body = body_json(response).await;
    assert_eq!(body["jsonrpc"], "2.0");
    assert_eq!(body["id"], 1);
    assert_eq!(body["result"]["serverInfo"]["name"], "homelab-k3s-mcp");
    assert!(body["result"]["capabilities"]["tools"].is_object());
}

#[tokio::test]
async fn tools_list_includes_workload_tools() {
    let response = homelab_k3s_mcp::app(None, unavailable_k8s())
        .oneshot(json_request(
            "/mcp",
            json!({"jsonrpc": "2.0", "id": 2, "method": "tools/list"}),
        ))
        .await
        .unwrap();

    let body = body_json(response).await;
    let tools = body["result"]["tools"].as_array().expect("tools array");
    let names: Vec<&str> = tools
        .iter()
        .map(|t| t["name"].as_str().unwrap_or_default())
        .collect();

    for expected in [
        "ping",
        "list_deployments",
        "list_statefulsets",
        "list_daemonsets",
        "rollout_restart_deployment",
        "rollout_restart_statefulset",
        "rollout_restart_daemonset",
    ] {
        assert!(names.contains(&expected), "missing tool: {expected}");
    }
}

#[tokio::test]
async fn ping_tool_returns_pong() {
    let response = homelab_k3s_mcp::app(None, unavailable_k8s())
        .oneshot(json_request(
            "/mcp",
            json!({
                "jsonrpc": "2.0",
                "id": 3,
                "method": "tools/call",
                "params": {"name": "ping", "arguments": {}},
            }),
        ))
        .await
        .unwrap();

    let body = body_json(response).await;
    assert_eq!(body["result"]["content"][0]["text"], "pong");
    assert_eq!(body["result"]["isError"], false);
}

#[tokio::test]
async fn unknown_method_returns_jsonrpc_error() {
    let response = homelab_k3s_mcp::app(None, unavailable_k8s())
        .oneshot(json_request(
            "/mcp",
            json!({"jsonrpc": "2.0", "id": 4, "method": "does/not/exist"}),
        ))
        .await
        .unwrap();

    let body = body_json(response).await;
    assert_eq!(body["error"]["code"], -32601);
}

#[tokio::test]
async fn unknown_tool_returns_jsonrpc_error() {
    let response = homelab_k3s_mcp::app(None, unavailable_k8s())
        .oneshot(json_request(
            "/mcp",
            json!({
                "jsonrpc": "2.0",
                "id": 5,
                "method": "tools/call",
                "params": {"name": "nonexistent"},
            }),
        ))
        .await
        .unwrap();

    let body = body_json(response).await;
    assert_eq!(body["error"]["code"], -32602);
}

#[tokio::test]
async fn list_deployments_returns_summaries_from_service() {
    let fake = Arc::new(FakeK8s {
        deployments: vec![DeploymentSummary {
            name: "api".into(),
            namespace: "default".into(),
            replicas: 3,
            ready_replicas: 3,
            updated_replicas: 3,
            available_replicas: 3,
            creation_timestamp: Some("2026-05-01T00:00:00Z".into()),
        }],
        ..Default::default()
    });
    let app = homelab_k3s_mcp::app(None, fake.clone());

    let response = app
        .oneshot(json_request(
            "/mcp",
            json!({
                "jsonrpc": "2.0",
                "id": 10,
                "method": "tools/call",
                "params": {
                    "name": "list_deployments",
                    "arguments": {"namespace": "default"}
                }
            }),
        ))
        .await
        .unwrap();

    let body = body_json(response).await;
    assert_eq!(body["result"]["isError"], false);
    let items = &body["result"]["structuredContent"]["items"];
    assert_eq!(items[0]["name"], "api");
    assert_eq!(items[0]["namespace"], "default");
    assert_eq!(items[0]["replicas"], 3);

    assert_eq!(
        fake.last_list_namespace.lock().unwrap().as_deref(),
        Some("default")
    );
}

#[tokio::test]
async fn list_deployments_without_namespace_lists_all() {
    let fake = Arc::new(FakeK8s::default());
    let app = homelab_k3s_mcp::app(None, fake.clone());

    let response = app
        .oneshot(json_request(
            "/mcp",
            json!({
                "jsonrpc": "2.0",
                "id": 11,
                "method": "tools/call",
                "params": {"name": "list_deployments", "arguments": {}}
            }),
        ))
        .await
        .unwrap();

    let body = body_json(response).await;
    assert_eq!(body["result"]["isError"], false);
    assert!(fake.last_list_namespace.lock().unwrap().is_none());
}

#[tokio::test]
async fn rollout_restart_deployment_calls_service() {
    let fake = Arc::new(FakeK8s::default());
    let app = homelab_k3s_mcp::app(None, fake.clone());

    let response = app
        .oneshot(json_request(
            "/mcp",
            json!({
                "jsonrpc": "2.0",
                "id": 20,
                "method": "tools/call",
                "params": {
                    "name": "rollout_restart_deployment",
                    "arguments": {"namespace": "kube-system", "name": "coredns"}
                }
            }),
        ))
        .await
        .unwrap();

    let body = body_json(response).await;
    assert_eq!(body["result"]["isError"], false);
    assert_eq!(body["result"]["structuredContent"]["kind"], "Deployment");
    assert_eq!(body["result"]["structuredContent"]["name"], "coredns");

    let restarts = fake.restarts.lock().unwrap();
    assert_eq!(restarts.len(), 1);
    assert_eq!(restarts[0].0, "Deployment");
    assert_eq!(restarts[0].1, "kube-system");
    assert_eq!(restarts[0].2, "coredns");
}

#[tokio::test]
async fn rollout_restart_statefulset_calls_service() {
    let fake = Arc::new(FakeK8s::default());
    let app = homelab_k3s_mcp::app(None, fake.clone());

    let response = app
        .oneshot(json_request(
            "/mcp",
            json!({
                "jsonrpc": "2.0",
                "id": 21,
                "method": "tools/call",
                "params": {
                    "name": "rollout_restart_statefulset",
                    "arguments": {"namespace": "data", "name": "postgres"}
                }
            }),
        ))
        .await
        .unwrap();

    let body = body_json(response).await;
    assert_eq!(body["result"]["isError"], false);
    let restarts = fake.restarts.lock().unwrap();
    assert_eq!(restarts[0].0, "StatefulSet");
}

#[tokio::test]
async fn rollout_restart_daemonset_calls_service() {
    let fake = Arc::new(FakeK8s::default());
    let app = homelab_k3s_mcp::app(None, fake.clone());

    let response = app
        .oneshot(json_request(
            "/mcp",
            json!({
                "jsonrpc": "2.0",
                "id": 22,
                "method": "tools/call",
                "params": {
                    "name": "rollout_restart_daemonset",
                    "arguments": {"namespace": "kube-system", "name": "kindnet"}
                }
            }),
        ))
        .await
        .unwrap();

    let body = body_json(response).await;
    assert_eq!(body["result"]["isError"], false);
    let restarts = fake.restarts.lock().unwrap();
    assert_eq!(restarts[0].0, "DaemonSet");
}

#[tokio::test]
async fn rollout_restart_requires_namespace_and_name() {
    let response = homelab_k3s_mcp::app(None, unavailable_k8s())
        .oneshot(json_request(
            "/mcp",
            json!({
                "jsonrpc": "2.0",
                "id": 30,
                "method": "tools/call",
                "params": {
                    "name": "rollout_restart_deployment",
                    "arguments": {"namespace": "default"}
                }
            }),
        ))
        .await
        .unwrap();

    let body = body_json(response).await;
    assert_eq!(body["error"]["code"], -32602);
}

#[tokio::test]
async fn unavailable_k8s_returns_tool_error() {
    let response = homelab_k3s_mcp::app(None, unavailable_k8s())
        .oneshot(json_request(
            "/mcp",
            json!({
                "jsonrpc": "2.0",
                "id": 31,
                "method": "tools/call",
                "params": {"name": "list_deployments", "arguments": {}}
            }),
        ))
        .await
        .unwrap();

    let body = body_json(response).await;
    assert_eq!(body["result"]["isError"], true);
    assert!(body["result"]["content"][0]["text"]
        .as_str()
        .unwrap_or("")
        .contains("kubernetes"));
}
