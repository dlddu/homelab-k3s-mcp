use std::sync::{Arc, Mutex};

use async_trait::async_trait;
use axum::body::Body;
use axum::http::{Request, StatusCode};
use homelab_k3s_mcp::k8s::{ExecOutcome, K8sError, K8sService, WorkloadKind};
use http_body_util::BodyExt;
use serde_json::{json, Value};
use tower::ServiceExt;

#[derive(Clone, Debug)]
struct ExecCall {
    pub namespace: String,
    pub selector: String,
    pub container: Option<String>,
    pub command: Vec<String>,
}

#[derive(Default)]
struct FakeK8s {
    pub items: Mutex<Vec<Value>>,
    pub last_list: Mutex<Option<(WorkloadKind, Option<String>)>>,
    pub restarts: Mutex<Vec<(WorkloadKind, String, String)>>,
    pub scales: Mutex<Vec<(WorkloadKind, String, String, i32)>>,
    pub scale_response: Mutex<Option<Result<i32, K8sError>>>,
    pub exec_calls: Mutex<Vec<ExecCall>>,
    pub exec_response: Mutex<Option<Result<ExecOutcome, K8sError>>>,
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

    async fn scale_workload(
        &self,
        kind: WorkloadKind,
        namespace: &str,
        name: &str,
        replicas: i32,
    ) -> Result<i32, K8sError> {
        self.scales
            .lock()
            .unwrap()
            .push((kind, namespace.into(), name.into(), replicas));
        match self.scale_response.lock().unwrap().take() {
            Some(Ok(applied)) => Ok(applied),
            Some(Err(err)) => Err(err),
            None => Ok(replicas),
        }
    }

    async fn exec_in_pod(
        &self,
        namespace: &str,
        label_selector: &str,
        container: Option<&str>,
        command: &[String],
    ) -> Result<ExecOutcome, K8sError> {
        self.exec_calls.lock().unwrap().push(ExecCall {
            namespace: namespace.into(),
            selector: label_selector.into(),
            container: container.map(str::to_owned),
            command: command.to_vec(),
        });
        match self.exec_response.lock().unwrap().take() {
            Some(Ok(outcome)) => Ok(outcome),
            Some(Err(err)) => Err(err),
            None => Ok(ExecOutcome {
                pod: "dear-baby-abcd".into(),
                stdout: String::new(),
                stderr: String::new(),
                exit_code: Some(0),
                success: true,
            }),
        }
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

    assert_eq!(tools.len(), 5);
    assert!(names.contains(&"ping"));
    assert!(names.contains(&"workload_list"));
    assert!(names.contains(&"workload_restart"));
    assert!(names.contains(&"workload_scale"));
    assert!(names.contains(&"dear_baby_reset_onboarding"));
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
async fn workload_scale_dispatches_to_service() {
    let fake = Arc::new(FakeK8s::default());
    let app = homelab_k3s_mcp::app(None, fake.clone());

    let response = app
        .oneshot(json_request(
            "/mcp",
            json!({
                "jsonrpc": "2.0",
                "id": 70,
                "method": "tools/call",
                "params": {
                    "name": "workload_scale",
                    "arguments": {
                        "kind": "Deployment",
                        "namespace": "default",
                        "name": "api",
                        "replicas": 3
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
    assert_eq!(payload["name"], "api");
    assert_eq!(payload["replicas"], 3);

    let scales = fake.scales.lock().unwrap();
    assert_eq!(scales.len(), 1);
    assert_eq!(scales[0].0, WorkloadKind::Deployment);
    assert_eq!(scales[0].1, "default");
    assert_eq!(scales[0].2, "api");
    assert_eq!(scales[0].3, 3);
}

#[tokio::test]
async fn workload_scale_supports_zero_replicas() {
    let fake = Arc::new(FakeK8s::default());
    let app = homelab_k3s_mcp::app(None, fake.clone());

    let response = app
        .oneshot(json_request(
            "/mcp",
            json!({
                "jsonrpc": "2.0",
                "id": 71,
                "method": "tools/call",
                "params": {
                    "name": "workload_scale",
                    "arguments": {
                        "kind": "StatefulSet",
                        "namespace": "data",
                        "name": "redis",
                        "replicas": 0
                    }
                }
            }),
        ))
        .await
        .unwrap();

    let body = body_json(response).await;
    assert_eq!(body["result"]["isError"], false);
    assert_eq!(body["result"]["structuredContent"]["replicas"], 0);

    let scales = fake.scales.lock().unwrap();
    assert_eq!(scales[0].0, WorkloadKind::StatefulSet);
    assert_eq!(scales[0].3, 0);
}

#[tokio::test]
async fn workload_scale_rejects_negative_replicas() {
    let response = homelab_k3s_mcp::app(None, unavailable_k8s())
        .oneshot(json_request(
            "/mcp",
            json!({
                "jsonrpc": "2.0",
                "id": 72,
                "method": "tools/call",
                "params": {
                    "name": "workload_scale",
                    "arguments": {
                        "kind": "Deployment",
                        "namespace": "default",
                        "name": "api",
                        "replicas": -1
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
async fn workload_scale_requires_replicas() {
    let response = homelab_k3s_mcp::app(None, unavailable_k8s())
        .oneshot(json_request(
            "/mcp",
            json!({
                "jsonrpc": "2.0",
                "id": 73,
                "method": "tools/call",
                "params": {
                    "name": "workload_scale",
                    "arguments": {
                        "kind": "Deployment",
                        "namespace": "default",
                        "name": "api"
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
async fn tools_list_advertises_workload_scale_annotations() {
    let response = homelab_k3s_mcp::app(None, unavailable_k8s())
        .oneshot(json_request(
            "/mcp",
            json!({"jsonrpc": "2.0", "id": 74, "method": "tools/list"}),
        ))
        .await
        .unwrap();

    let body = body_json(response).await;
    let tools = body["result"]["tools"].as_array().expect("tools array");
    let scale = find_tool(tools, "workload_scale");
    assert_eq!(scale["annotations"]["title"], "Scale Workload");
    assert_eq!(scale["annotations"]["destructiveHint"], true);
    assert_eq!(scale["annotations"]["idempotentHint"], true);

    let kinds = scale["inputSchema"]["properties"]["kind"]["enum"]
        .as_array()
        .expect("kind enum");
    let kind_names: Vec<&str> = kinds.iter().filter_map(|v| v.as_str()).collect();
    assert_eq!(kind_names, vec!["Deployment", "StatefulSet"]);
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

#[tokio::test]
async fn tools_list_advertises_dear_baby_reset_onboarding() {
    let response = homelab_k3s_mcp::app(None, unavailable_k8s())
        .oneshot(json_request(
            "/mcp",
            json!({"jsonrpc": "2.0", "id": 50, "method": "tools/list"}),
        ))
        .await
        .unwrap();

    let body = body_json(response).await;
    let tools = body["result"]["tools"].as_array().expect("tools array");

    let reset = find_tool(tools, "dear_baby_reset_onboarding");
    let required = reset["inputSchema"]["required"]
        .as_array()
        .expect("required array");
    let names: Vec<&str> = required.iter().filter_map(|v| v.as_str()).collect();
    assert!(names.contains(&"namespace"));
    assert!(names.contains(&"email"));

    assert_eq!(reset["annotations"]["title"], "Reset dear-baby Onboarding");
    assert_eq!(reset["annotations"]["destructiveHint"], true);
    assert_eq!(reset["annotations"]["idempotentHint"], true);
}

#[tokio::test]
async fn dear_baby_reset_onboarding_dispatches_exec_with_defaults() {
    let fake = Arc::new(FakeK8s::default());
    *fake.exec_response.lock().unwrap() = Some(Ok(ExecOutcome {
        pod: "dear-baby-7d9c9f6b8b-xyz".into(),
        stdout: "reset onboarding for user@example.com\n".into(),
        stderr: String::new(),
        exit_code: Some(0),
        success: true,
    }));
    let app = homelab_k3s_mcp::app(None, fake.clone());

    let response = app
        .oneshot(json_request(
            "/mcp",
            json!({
                "jsonrpc": "2.0",
                "id": 60,
                "method": "tools/call",
                "params": {
                    "name": "dear_baby_reset_onboarding",
                    "arguments": {
                        "namespace": "dear-baby",
                        "email": "user@example.com"
                    }
                }
            }),
        ))
        .await
        .unwrap();

    let body = body_json(response).await;
    assert_eq!(body["result"]["isError"], false);
    let payload = &body["result"]["structuredContent"];
    assert_eq!(payload["namespace"], "dear-baby");
    assert_eq!(payload["email"], "user@example.com");
    assert_eq!(payload["selector"], "app=dear-baby");
    assert_eq!(payload["container"], "backend");
    assert_eq!(payload["pod"], "dear-baby-7d9c9f6b8b-xyz");
    assert_eq!(payload["exitCode"], 0);
    assert_eq!(payload["success"], true);
    assert!(payload["stdout"]
        .as_str()
        .unwrap_or("")
        .contains("reset onboarding"));

    let calls = fake.exec_calls.lock().unwrap();
    assert_eq!(calls.len(), 1);
    let call = &calls[0];
    assert_eq!(call.namespace, "dear-baby");
    assert_eq!(call.selector, "app=dear-baby");
    assert_eq!(call.container.as_deref(), Some("backend"));
    assert_eq!(
        call.command,
        vec![
            "/reset-onboarding".to_string(),
            "user@example.com".to_string()
        ]
    );
}

#[tokio::test]
async fn dear_baby_reset_onboarding_honours_overrides() {
    let fake = Arc::new(FakeK8s::default());
    let app = homelab_k3s_mcp::app(None, fake.clone());

    let response = app
        .oneshot(json_request(
            "/mcp",
            json!({
                "jsonrpc": "2.0",
                "id": 61,
                "method": "tools/call",
                "params": {
                    "name": "dear_baby_reset_onboarding",
                    "arguments": {
                        "namespace": "staging",
                        "email": "qa@example.com",
                        "selector": "app=dear-baby,track=canary",
                        "container": "api"
                    }
                }
            }),
        ))
        .await
        .unwrap();

    let body = body_json(response).await;
    assert_eq!(body["result"]["isError"], false);

    let calls = fake.exec_calls.lock().unwrap();
    assert_eq!(calls.len(), 1);
    let call = &calls[0];
    assert_eq!(call.namespace, "staging");
    assert_eq!(call.selector, "app=dear-baby,track=canary");
    assert_eq!(call.container.as_deref(), Some("api"));
}

#[tokio::test]
async fn dear_baby_reset_onboarding_reports_non_zero_exit() {
    let fake = Arc::new(FakeK8s::default());
    *fake.exec_response.lock().unwrap() = Some(Ok(ExecOutcome {
        pod: "dear-baby-1".into(),
        stdout: String::new(),
        stderr: "no user found with email \"missing@example.com\"\n".into(),
        exit_code: Some(1),
        success: false,
    }));
    let app = homelab_k3s_mcp::app(None, fake.clone());

    let response = app
        .oneshot(json_request(
            "/mcp",
            json!({
                "jsonrpc": "2.0",
                "id": 62,
                "method": "tools/call",
                "params": {
                    "name": "dear_baby_reset_onboarding",
                    "arguments": {
                        "namespace": "dear-baby",
                        "email": "missing@example.com"
                    }
                }
            }),
        ))
        .await
        .unwrap();

    let body = body_json(response).await;
    assert_eq!(body["result"]["isError"], true);
    let payload = &body["result"]["structuredContent"];
    assert_eq!(payload["success"], false);
    assert_eq!(payload["exitCode"], 1);
    assert!(payload["stderr"]
        .as_str()
        .unwrap_or("")
        .contains("no user found"));
}

#[tokio::test]
async fn dear_baby_reset_onboarding_requires_namespace_and_email() {
    let response = homelab_k3s_mcp::app(None, unavailable_k8s())
        .oneshot(json_request(
            "/mcp",
            json!({
                "jsonrpc": "2.0",
                "id": 63,
                "method": "tools/call",
                "params": {
                    "name": "dear_baby_reset_onboarding",
                    "arguments": { "email": "user@example.com" }
                }
            }),
        ))
        .await
        .unwrap();

    let body = body_json(response).await;
    assert_eq!(body["error"]["code"], -32602);
}
