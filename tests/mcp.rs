use std::sync::{Arc, Mutex};

use async_trait::async_trait;
use axum::body::Body;
use axum::http::{Request, StatusCode};
use homelab_k3s_mcp::k8s::{K8sError, K8sService, WorkloadKind};
use http_body_util::BodyExt;
use serde_json::{json, Value};
use tower::ServiceExt;

#[derive(Default)]
struct FakeK8s {
    pub items: Mutex<Vec<Value>>,
    pub last_list: Mutex<Option<(WorkloadKind, Option<String>)>>,
    pub restarts: Mutex<Vec<(WorkloadKind, String, String)>>,
}

#[async_trait]
impl K8sService for FakeK8s {
    async fn list_workloads(
        &self,
        kind: WorkloadKind,
        namespace: Option<&str>,
    ) -> Result<Vec<Value>, K8sError> {
        *self.last_list.lock().unwrap() = Some((kind, namespace.map(str::to_owned)));
        Ok(self.items.lock().unwrap().clone())
    }

    async fn rollout_restart(
        &self,
        kind: WorkloadKind,
        namespace: &str,
        name: &str,
    ) -> Result<String, K8sError> {
        self.restarts
            .lock()
            .unwrap()
            .push((kind, namespace.into(), name.into()));
        Ok("2026-05-07T00:00:00Z".into())
    }
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

    assert_eq!(tools.len(), 3);
    assert!(names.contains(&"ping"));
    assert!(names.contains(&"workload_list"));
    assert!(names.contains(&"workload_restart"));
}

fn find_tool<'a>(tools: &'a [Value], name: &str) -> &'a Value {
    tools
        .iter()
        .find(|t| t["name"].as_str() == Some(name))
        .unwrap_or_else(|| panic!("tool {name} not found"))
}

#[tokio::test]
async fn tools_list_advertises_annotations() {
    let response = homelab_k3s_mcp::app(None, unavailable_k8s())
        .oneshot(json_request(
            "/mcp",
            json!({"jsonrpc": "2.0", "id": 6, "method": "tools/list"}),
        ))
        .await
        .unwrap();

    let body = body_json(response).await;
    let tools = body["result"]["tools"].as_array().expect("tools array");

    let ping = find_tool(tools, "ping");
    assert_eq!(ping["annotations"]["title"], "Ping");
    assert_eq!(ping["annotations"]["readOnlyHint"], true);
    assert_eq!(ping["annotations"]["idempotentHint"], true);
    assert_eq!(ping["annotations"]["openWorldHint"], false);

    let list = find_tool(tools, "workload_list");
    assert_eq!(list["annotations"]["title"], "List Workloads");
    assert_eq!(list["annotations"]["readOnlyHint"], true);
    assert_eq!(list["annotations"]["idempotentHint"], true);
    assert_eq!(list["annotations"]["openWorldHint"], false);

    let restart = find_tool(tools, "workload_restart");
    assert_eq!(restart["annotations"]["title"], "Restart Workload");
    assert_eq!(restart["annotations"]["readOnlyHint"], false);
    assert_eq!(restart["annotations"]["destructiveHint"], true);
    assert_eq!(restart["annotations"]["idempotentHint"], false);
    assert_eq!(restart["annotations"]["openWorldHint"], false);
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
async fn workload_list_dispatches_to_service() {
    let fake = Arc::new(FakeK8s::default());
    *fake.items.lock().unwrap() = vec![json!({
        "name": "api",
        "namespace": "default",
        "replicas": 3,
    })];
    let app = homelab_k3s_mcp::app(None, fake.clone());

    let response = app
        .oneshot(json_request(
            "/mcp",
            json!({
                "jsonrpc": "2.0",
                "id": 10,
                "method": "tools/call",
                "params": {
                    "name": "workload_list",
                    "arguments": {
                        "kind": "Deployment",
                        "namespace": "default"
                    }
                }
            }),
        ))
        .await
        .unwrap();

    let body = body_json(response).await;
    assert_eq!(body["result"]["isError"], false);
    let payload = &body["result"]["structuredContent"];
    assert_eq!(payload["kind"], "Deployment");
    assert_eq!(payload["namespace"], "default");
    assert_eq!(payload["items"][0]["name"], "api");

    let last = fake.last_list.lock().unwrap();
    let (kind, ns) = last.as_ref().unwrap();
    assert_eq!(*kind, WorkloadKind::Deployment);
    assert_eq!(ns.as_deref(), Some("default"));
}

#[tokio::test]
async fn workload_list_without_namespace_lists_all() {
    let fake = Arc::new(FakeK8s::default());
    let app = homelab_k3s_mcp::app(None, fake.clone());

    let response = app
        .oneshot(json_request(
            "/mcp",
            json!({
                "jsonrpc": "2.0",
                "id": 11,
                "method": "tools/call",
                "params": {
                    "name": "workload_list",
                    "arguments": { "kind": "StatefulSet" }
                }
            }),
        ))
        .await
        .unwrap();

    let body = body_json(response).await;
    assert_eq!(body["result"]["isError"], false);
    let last = fake.last_list.lock().unwrap();
    let (kind, ns) = last.as_ref().unwrap();
    assert_eq!(*kind, WorkloadKind::StatefulSet);
    assert!(ns.is_none());
}

#[tokio::test]
async fn workload_rollout_restart_dispatches_to_service() {
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
                    "name": "workload_restart",
                    "arguments": {
                        "kind": "DaemonSet",
                        "namespace": "kube-system",
                        "name": "kindnet"
                    }
                }
            }),
        ))
        .await
        .unwrap();

    let body = body_json(response).await;
    assert_eq!(body["result"]["isError"], false);
    let payload = &body["result"]["structuredContent"];
    assert_eq!(payload["kind"], "DaemonSet");
    assert_eq!(payload["namespace"], "kube-system");
    assert_eq!(payload["name"], "kindnet");
    assert!(payload["restartedAt"].is_string());

    let restarts = fake.restarts.lock().unwrap();
    assert_eq!(restarts.len(), 1);
    assert_eq!(restarts[0].0, WorkloadKind::DaemonSet);
    assert_eq!(restarts[0].1, "kube-system");
    assert_eq!(restarts[0].2, "kindnet");
}

#[tokio::test]
async fn workload_restart_requires_namespace_and_name() {
    let response = homelab_k3s_mcp::app(None, unavailable_k8s())
        .oneshot(json_request(
            "/mcp",
            json!({
                "jsonrpc": "2.0",
                "id": 30,
                "method": "tools/call",
                "params": {
                    "name": "workload_restart",
                    "arguments": {
                        "kind": "Deployment",
                        "namespace": "default"
                    }
                }
            }),
        ))
        .await
        .unwrap();

    let body = body_json(response).await;
    assert_eq!(body["error"]["code"], -32602);
}

#[tokio::test]
async fn workload_rejects_unknown_kind() {
    let response = homelab_k3s_mcp::app(None, unavailable_k8s())
        .oneshot(json_request(
            "/mcp",
            json!({
                "jsonrpc": "2.0",
                "id": 31,
                "method": "tools/call",
                "params": {
                    "name": "workload_list",
                    "arguments": { "kind": "Pod" }
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
                "id": 40,
                "method": "tools/call",
                "params": {
                    "name": "workload_list",
                    "arguments": { "kind": "Deployment" }
                }
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
