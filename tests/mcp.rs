use std::sync::{Arc, Mutex};

use async_trait::async_trait;
use axum::body::Body;
use axum::http::{Request, StatusCode};
use homelab_k3s_mcp::aws::{AwsConfigFile, AwsConfigService, AwsError};
use homelab_k3s_mcp::github::{GitHubAppService, GitHubError, InstallationToken};
use homelab_k3s_mcp::k8s::{
    ContainerInfo, ExecOutcome, K8sError, K8sService, LogOptions, LogResult, PodConditionInfo,
    PodDescription, PodEventInfo, PodTarget, WorkloadKind,
};
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

#[derive(Clone, Debug)]
struct LogCall {
    pub kind: WorkloadKind,
    pub namespace: String,
    pub name: String,
    pub options: LogOptions,
}

#[derive(Clone, Debug)]
struct DescribeCall {
    pub namespace: String,
    pub target: PodTarget,
}

#[derive(Default)]
struct FakeK8s {
    pub items: Mutex<Vec<Value>>,
    pub namespaces: Mutex<Vec<Value>>,
    pub namespace_calls: Mutex<u32>,
    pub last_list: Mutex<Option<(WorkloadKind, Option<String>)>>,
    pub restarts: Mutex<Vec<(WorkloadKind, String, String)>>,
    pub scales: Mutex<Vec<(WorkloadKind, String, String, i32)>>,
    pub scale_response: Mutex<Option<Result<i32, K8sError>>>,
    pub exec_calls: Mutex<Vec<ExecCall>>,
    pub exec_response: Mutex<Option<Result<ExecOutcome, K8sError>>>,
    pub log_calls: Mutex<Vec<LogCall>>,
    pub log_response: Mutex<Option<Result<LogResult, K8sError>>>,
    pub describe_calls: Mutex<Vec<DescribeCall>>,
    pub describe_response: Mutex<Option<Result<PodDescription, K8sError>>>,
}

#[async_trait]
impl K8sService for FakeK8s {
    async fn list_namespaces(&self) -> Result<Vec<Value>, K8sError> {
        *self.namespace_calls.lock().unwrap() += 1;
        Ok(self.namespaces.lock().unwrap().clone())
    }

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

    async fn workload_logs(
        &self,
        kind: WorkloadKind,
        namespace: &str,
        name: &str,
        options: &LogOptions,
    ) -> Result<LogResult, K8sError> {
        self.log_calls.lock().unwrap().push(LogCall {
            kind,
            namespace: namespace.into(),
            name: name.into(),
            options: options.clone(),
        });
        match self.log_response.lock().unwrap().take() {
            Some(Ok(result)) => Ok(result),
            Some(Err(err)) => Err(err),
            None => Ok(LogResult {
                pod: format!("{name}-pod-0"),
                container: options.container.clone(),
                logs: String::new(),
            }),
        }
    }

    async fn describe_pod(
        &self,
        namespace: &str,
        target: &PodTarget,
    ) -> Result<PodDescription, K8sError> {
        let inferred_name = match target {
            PodTarget::Name(n) => n.clone(),
            PodTarget::Selector(s) => format!("pod-for-{s}"),
            PodTarget::Workload { kind, name } => format!("{}-{name}-0", kind.as_str()),
        };
        self.describe_calls.lock().unwrap().push(DescribeCall {
            namespace: namespace.into(),
            target: target.clone(),
        });
        match self.describe_response.lock().unwrap().take() {
            Some(Ok(d)) => Ok(d),
            Some(Err(err)) => Err(err),
            None => Ok(PodDescription {
                name: inferred_name,
                namespace: namespace.into(),
                node: None,
                phase: None,
                pod_ip: None,
                host_ip: None,
                service_account: None,
                priority: None,
                priority_class_name: None,
                qos_class: None,
                start_time: None,
                creation_timestamp: None,
                labels: Default::default(),
                annotations: Default::default(),
                node_selector: Default::default(),
                owner_references: Vec::new(),
                conditions: Vec::new(),
                init_containers: Vec::new(),
                containers: Vec::new(),
                events: Vec::new(),
            }),
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

fn unavailable_github() -> Arc<dyn GitHubAppService> {
    Arc::new(homelab_k3s_mcp::UnavailableGitHubApp::default())
}

fn unavailable_aws() -> Arc<dyn AwsConfigService> {
    Arc::new(homelab_k3s_mcp::UnavailableAwsConfig::default())
}

#[derive(Default)]
struct FakeAws {
    pub calls: Mutex<u32>,
    pub response: Mutex<Option<Result<AwsConfigFile, AwsError>>>,
}

#[async_trait]
impl AwsConfigService for FakeAws {
    async fn get_config_file(&self) -> Result<AwsConfigFile, AwsError> {
        *self.calls.lock().unwrap() += 1;
        match self.response.lock().unwrap().take() {
            Some(Ok(file)) => Ok(file),
            Some(Err(err)) => Err(err),
            None => Ok(AwsConfigFile {
                bucket: "homelab-config".into(),
                key: "aws/config".into(),
                content_type: Some("text/plain".into()),
                body: "[default]\nregion = ap-northeast-2\n".into(),
            }),
        }
    }
}

#[derive(Clone, Debug)]
struct InstallationTokenCall {
    pub repositories: Option<Vec<String>>,
    pub permissions: Option<Value>,
}

#[derive(Default)]
struct FakeGitHub {
    pub calls: Mutex<Vec<InstallationTokenCall>>,
    pub response: Mutex<Option<Result<InstallationToken, GitHubError>>>,
}

#[async_trait]
impl GitHubAppService for FakeGitHub {
    async fn create_installation_token(
        &self,
        repositories: Option<Vec<String>>,
        permissions: Option<Value>,
    ) -> Result<InstallationToken, GitHubError> {
        self.calls.lock().unwrap().push(InstallationTokenCall {
            repositories: repositories.clone(),
            permissions: permissions.clone(),
        });
        match self.response.lock().unwrap().take() {
            Some(Ok(token)) => Ok(token),
            Some(Err(err)) => Err(err),
            None => Ok(InstallationToken {
                token: "ghs_fake".into(),
                expires_at: "2026-05-07T01:00:00Z".into(),
                permissions: Some(json!({ "contents": "read" })),
                repository_selection: Some("all".into()),
            }),
        }
    }
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
    let response = homelab_k3s_mcp::app(
        None,
        unavailable_k8s(),
        unavailable_github(),
        unavailable_aws(),
    )
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
    let response = homelab_k3s_mcp::app(
        None,
        unavailable_k8s(),
        unavailable_github(),
        unavailable_aws(),
    )
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

    assert_eq!(tools.len(), 10);
    assert!(names.contains(&"ping"));
    assert!(names.contains(&"namespace_list"));
    assert!(names.contains(&"workload_list"));
    assert!(names.contains(&"workload_restart"));
    assert!(names.contains(&"workload_scale"));
    assert!(names.contains(&"workload_logs"));
    assert!(names.contains(&"pod_describe"));
    assert!(names.contains(&"dear_baby_reset_onboarding"));
    assert!(names.contains(&"github_app_installation_token"));
    assert!(names.contains(&"aws_config_get"));
}

fn find_tool<'a>(tools: &'a [Value], name: &str) -> &'a Value {
    tools
        .iter()
        .find(|t| t["name"].as_str() == Some(name))
        .unwrap_or_else(|| panic!("tool {name} not found"))
}

#[tokio::test]
async fn tools_list_advertises_annotations() {
    let response = homelab_k3s_mcp::app(
        None,
        unavailable_k8s(),
        unavailable_github(),
        unavailable_aws(),
    )
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
    let response = homelab_k3s_mcp::app(
        None,
        unavailable_k8s(),
        unavailable_github(),
        unavailable_aws(),
    )
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
    let response = homelab_k3s_mcp::app(
        None,
        unavailable_k8s(),
        unavailable_github(),
        unavailable_aws(),
    )
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
    let response = homelab_k3s_mcp::app(
        None,
        unavailable_k8s(),
        unavailable_github(),
        unavailable_aws(),
    )
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
    let app = homelab_k3s_mcp::app(None, fake.clone(), unavailable_github(), unavailable_aws());

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
    let app = homelab_k3s_mcp::app(None, fake.clone(), unavailable_github(), unavailable_aws());

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
async fn tools_list_advertises_namespace_list() {
    let response = homelab_k3s_mcp::app(
        None,
        unavailable_k8s(),
        unavailable_github(),
        unavailable_aws(),
    )
    .oneshot(json_request(
        "/mcp",
        json!({"jsonrpc": "2.0", "id": 12, "method": "tools/list"}),
    ))
    .await
    .unwrap();

    let body = body_json(response).await;
    let tools = body["result"]["tools"].as_array().expect("tools array");
    let namespace = find_tool(tools, "namespace_list");

    assert_eq!(namespace["annotations"]["title"], "List Namespaces");
    assert_eq!(namespace["annotations"]["readOnlyHint"], true);
    assert_eq!(namespace["annotations"]["idempotentHint"], true);
    assert_eq!(namespace["annotations"]["openWorldHint"], false);

    let required = namespace["inputSchema"]["required"].as_array();
    assert!(required.is_none() || required.unwrap().is_empty());
    let props = namespace["inputSchema"]["properties"]
        .as_object()
        .expect("properties object");
    assert!(props.is_empty());
}

#[tokio::test]
async fn namespace_list_dispatches_to_service() {
    let fake = Arc::new(FakeK8s::default());
    *fake.namespaces.lock().unwrap() = vec![
        json!({
            "name": "default",
            "phase": "Active",
            "creation_timestamp": "2026-05-01T00:00:00Z",
        }),
        json!({
            "name": "kube-system",
            "phase": "Active",
            "creation_timestamp": "2026-05-01T00:00:00Z",
        }),
    ];
    let app = homelab_k3s_mcp::app(None, fake.clone(), unavailable_github(), unavailable_aws());

    let response = app
        .oneshot(json_request(
            "/mcp",
            json!({
                "jsonrpc": "2.0",
                "id": 13,
                "method": "tools/call",
                "params": { "name": "namespace_list", "arguments": {} }
            }),
        ))
        .await
        .unwrap();

    let body = body_json(response).await;
    assert_eq!(body["result"]["isError"], false);
    let payload = &body["result"]["structuredContent"];
    let items = payload["items"].as_array().expect("items array");
    assert_eq!(items.len(), 2);
    assert_eq!(items[0]["name"], "default");
    assert_eq!(items[1]["name"], "kube-system");
    assert_eq!(*fake.namespace_calls.lock().unwrap(), 1);
}

#[tokio::test]
async fn namespace_list_surfaces_unavailable_as_tool_error() {
    let response = homelab_k3s_mcp::app(
        None,
        unavailable_k8s(),
        unavailable_github(),
        unavailable_aws(),
    )
    .oneshot(json_request(
        "/mcp",
        json!({
            "jsonrpc": "2.0",
            "id": 14,
            "method": "tools/call",
            "params": { "name": "namespace_list", "arguments": {} }
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
async fn workload_rollout_restart_dispatches_to_service() {
    let fake = Arc::new(FakeK8s::default());
    let app = homelab_k3s_mcp::app(None, fake.clone(), unavailable_github(), unavailable_aws());

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
    let response = homelab_k3s_mcp::app(
        None,
        unavailable_k8s(),
        unavailable_github(),
        unavailable_aws(),
    )
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
    let app = homelab_k3s_mcp::app(None, fake.clone(), unavailable_github(), unavailable_aws());

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
    let app = homelab_k3s_mcp::app(None, fake.clone(), unavailable_github(), unavailable_aws());

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
    let response = homelab_k3s_mcp::app(
        None,
        unavailable_k8s(),
        unavailable_github(),
        unavailable_aws(),
    )
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
    let response = homelab_k3s_mcp::app(
        None,
        unavailable_k8s(),
        unavailable_github(),
        unavailable_aws(),
    )
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
    let response = homelab_k3s_mcp::app(
        None,
        unavailable_k8s(),
        unavailable_github(),
        unavailable_aws(),
    )
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
    let response = homelab_k3s_mcp::app(
        None,
        unavailable_k8s(),
        unavailable_github(),
        unavailable_aws(),
    )
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
    let response = homelab_k3s_mcp::app(
        None,
        unavailable_k8s(),
        unavailable_github(),
        unavailable_aws(),
    )
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
    let response = homelab_k3s_mcp::app(
        None,
        unavailable_k8s(),
        unavailable_github(),
        unavailable_aws(),
    )
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
    let app = homelab_k3s_mcp::app(None, fake.clone(), unavailable_github(), unavailable_aws());

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
    let app = homelab_k3s_mcp::app(None, fake.clone(), unavailable_github(), unavailable_aws());

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
    let app = homelab_k3s_mcp::app(None, fake.clone(), unavailable_github(), unavailable_aws());

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
    let response = homelab_k3s_mcp::app(
        None,
        unavailable_k8s(),
        unavailable_github(),
        unavailable_aws(),
    )
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

#[tokio::test]
async fn tools_list_advertises_workload_logs() {
    let response = homelab_k3s_mcp::app(
        None,
        unavailable_k8s(),
        unavailable_github(),
        unavailable_aws(),
    )
    .oneshot(json_request(
        "/mcp",
        json!({"jsonrpc": "2.0", "id": 80, "method": "tools/list"}),
    ))
    .await
    .unwrap();

    let body = body_json(response).await;
    let tools = body["result"]["tools"].as_array().expect("tools array");
    let logs = find_tool(tools, "workload_logs");

    assert_eq!(logs["annotations"]["title"], "View Workload Logs");
    assert_eq!(logs["annotations"]["readOnlyHint"], true);
    assert_eq!(logs["annotations"]["idempotentHint"], true);

    let required = logs["inputSchema"]["required"]
        .as_array()
        .expect("required array");
    let names: Vec<&str> = required.iter().filter_map(|v| v.as_str()).collect();
    assert_eq!(names, vec!["kind", "namespace", "name"]);

    let kinds = logs["inputSchema"]["properties"]["kind"]["enum"]
        .as_array()
        .expect("kind enum");
    let kind_names: Vec<&str> = kinds.iter().filter_map(|v| v.as_str()).collect();
    assert_eq!(kind_names, vec!["Deployment", "StatefulSet", "DaemonSet"]);

    assert_eq!(
        logs["inputSchema"]["properties"]["tail_lines"]["maximum"],
        5000
    );
}

#[tokio::test]
async fn workload_logs_dispatches_with_defaults() {
    let fake = Arc::new(FakeK8s::default());
    *fake.log_response.lock().unwrap() = Some(Ok(LogResult {
        pod: "api-7d9c9f6b8b-xyz".into(),
        container: None,
        logs: "line one\nline two\n".into(),
    }));
    let app = homelab_k3s_mcp::app(None, fake.clone(), unavailable_github(), unavailable_aws());

    let response = app
        .oneshot(json_request(
            "/mcp",
            json!({
                "jsonrpc": "2.0",
                "id": 81,
                "method": "tools/call",
                "params": {
                    "name": "workload_logs",
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
    assert_eq!(body["result"]["isError"], false);
    let payload = &body["result"]["structuredContent"];
    assert_eq!(payload["kind"], "Deployment");
    assert_eq!(payload["namespace"], "default");
    assert_eq!(payload["name"], "api");
    assert_eq!(payload["pod"], "api-7d9c9f6b8b-xyz");
    assert_eq!(payload["tailLines"], 200);
    assert_eq!(payload["previous"], false);
    assert_eq!(payload["timestamps"], false);
    assert!(payload["sinceSeconds"].is_null());
    assert_eq!(payload["logs"], "line one\nline two\n");
    assert_eq!(body["result"]["content"][0]["text"], "line one\nline two\n");

    let calls = fake.log_calls.lock().unwrap();
    assert_eq!(calls.len(), 1);
    let call = &calls[0];
    assert_eq!(call.kind, WorkloadKind::Deployment);
    assert_eq!(call.namespace, "default");
    assert_eq!(call.name, "api");
    assert_eq!(call.options.tail_lines, Some(200));
    assert!(call.options.container.is_none());
    assert!(!call.options.previous);
    assert!(!call.options.timestamps);
    assert!(call.options.since_seconds.is_none());
}

#[tokio::test]
async fn workload_logs_honours_overrides() {
    let fake = Arc::new(FakeK8s::default());
    let app = homelab_k3s_mcp::app(None, fake.clone(), unavailable_github(), unavailable_aws());

    let response = app
        .oneshot(json_request(
            "/mcp",
            json!({
                "jsonrpc": "2.0",
                "id": 82,
                "method": "tools/call",
                "params": {
                    "name": "workload_logs",
                    "arguments": {
                        "kind": "StatefulSet",
                        "namespace": "data",
                        "name": "redis",
                        "container": "redis",
                        "tail_lines": 500,
                        "previous": true,
                        "timestamps": true,
                        "since_seconds": 3600
                    }
                }
            }),
        ))
        .await
        .unwrap();

    let body = body_json(response).await;
    assert_eq!(body["result"]["isError"], false);

    let calls = fake.log_calls.lock().unwrap();
    assert_eq!(calls.len(), 1);
    let call = &calls[0];
    assert_eq!(call.kind, WorkloadKind::StatefulSet);
    assert_eq!(call.namespace, "data");
    assert_eq!(call.name, "redis");
    assert_eq!(call.options.container.as_deref(), Some("redis"));
    assert_eq!(call.options.tail_lines, Some(500));
    assert!(call.options.previous);
    assert!(call.options.timestamps);
    assert_eq!(call.options.since_seconds, Some(3600));
}

#[tokio::test]
async fn workload_logs_rejects_tail_lines_over_max() {
    let response = homelab_k3s_mcp::app(
        None,
        unavailable_k8s(),
        unavailable_github(),
        unavailable_aws(),
    )
    .oneshot(json_request(
        "/mcp",
        json!({
            "jsonrpc": "2.0",
            "id": 83,
            "method": "tools/call",
            "params": {
                "name": "workload_logs",
                "arguments": {
                    "kind": "Deployment",
                    "namespace": "default",
                    "name": "api",
                    "tail_lines": 100000
                }
            }
        }),
    ))
    .await
    .unwrap();

    let body = body_json(response).await;
    assert_eq!(body["error"]["code"], -32602);
    let msg = body["error"]["message"].as_str().unwrap_or("");
    assert!(msg.contains("tail_lines"), "{msg}");
}

#[tokio::test]
async fn workload_logs_requires_namespace_and_name() {
    let response = homelab_k3s_mcp::app(
        None,
        unavailable_k8s(),
        unavailable_github(),
        unavailable_aws(),
    )
    .oneshot(json_request(
        "/mcp",
        json!({
            "jsonrpc": "2.0",
            "id": 84,
            "method": "tools/call",
            "params": {
                "name": "workload_logs",
                "arguments": { "kind": "Deployment" }
            }
        }),
    ))
    .await
    .unwrap();

    let body = body_json(response).await;
    assert_eq!(body["error"]["code"], -32602);
}

#[tokio::test]
async fn workload_logs_renders_placeholder_for_empty_output() {
    let fake = Arc::new(FakeK8s::default());
    *fake.log_response.lock().unwrap() = Some(Ok(LogResult {
        pod: "api-1".into(),
        container: None,
        logs: String::new(),
    }));
    let app = homelab_k3s_mcp::app(None, fake.clone(), unavailable_github(), unavailable_aws());

    let response = app
        .oneshot(json_request(
            "/mcp",
            json!({
                "jsonrpc": "2.0",
                "id": 85,
                "method": "tools/call",
                "params": {
                    "name": "workload_logs",
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
    assert_eq!(body["result"]["isError"], false);
    assert_eq!(body["result"]["content"][0]["text"], "(no log output)");
    assert_eq!(body["result"]["structuredContent"]["logs"], "");
}

#[tokio::test]
async fn tools_list_advertises_pod_describe() {
    let response = homelab_k3s_mcp::app(
        None,
        unavailable_k8s(),
        unavailable_github(),
        unavailable_aws(),
    )
    .oneshot(json_request(
        "/mcp",
        json!({"jsonrpc": "2.0", "id": 90, "method": "tools/list"}),
    ))
    .await
    .unwrap();

    let body = body_json(response).await;
    let tools = body["result"]["tools"].as_array().expect("tools array");
    let describe = find_tool(tools, "pod_describe");

    assert_eq!(describe["annotations"]["title"], "Describe Pod");
    assert_eq!(describe["annotations"]["readOnlyHint"], true);
    assert_eq!(describe["annotations"]["idempotentHint"], true);
    assert_eq!(describe["annotations"]["openWorldHint"], false);

    let required = describe["inputSchema"]["required"]
        .as_array()
        .expect("required array");
    let names: Vec<&str> = required.iter().filter_map(|v| v.as_str()).collect();
    assert_eq!(names, vec!["namespace"]);

    let props = describe["inputSchema"]["properties"]
        .as_object()
        .expect("properties object");
    for key in ["name", "selector", "workload_kind", "workload_name"] {
        assert!(props.contains_key(key), "missing property {key}");
    }
    let workload_kinds = props["workload_kind"]["enum"]
        .as_array()
        .expect("workload_kind enum");
    let workload_kind_names: Vec<&str> = workload_kinds.iter().filter_map(|v| v.as_str()).collect();
    assert_eq!(
        workload_kind_names,
        vec!["Deployment", "StatefulSet", "DaemonSet"]
    );
}

#[tokio::test]
async fn pod_describe_dispatches_and_renders_structured_payload() {
    let fake = Arc::new(FakeK8s::default());
    let mut labels = std::collections::BTreeMap::new();
    labels.insert("app".to_string(), "api".to_string());
    *fake.describe_response.lock().unwrap() = Some(Ok(PodDescription {
        name: "api-7d9c9f6b8b-xyz".into(),
        namespace: "default".into(),
        node: Some("k3s-node-1".into()),
        phase: Some("Running".into()),
        pod_ip: Some("10.0.0.42".into()),
        host_ip: Some("192.168.1.10".into()),
        service_account: Some("default".into()),
        priority: Some(0),
        priority_class_name: None,
        qos_class: Some("BestEffort".into()),
        start_time: Some("2026-05-10T12:00:00Z".into()),
        creation_timestamp: Some("2026-05-10T11:59:50Z".into()),
        labels,
        annotations: Default::default(),
        node_selector: Default::default(),
        owner_references: Vec::new(),
        conditions: vec![PodConditionInfo {
            kind: "Ready".into(),
            status: "True".into(),
            reason: None,
            message: None,
            last_transition_time: None,
        }],
        init_containers: Vec::new(),
        containers: vec![ContainerInfo {
            name: "api".into(),
            image: "ghcr.io/example/api:1.2.3".into(),
            ready: true,
            started: Some(true),
            restart_count: 2,
            state: Some("running".into()),
            started_at: Some("2026-05-10T12:00:01Z".into()),
            last_state: Some("terminated".into()),
            last_reason: Some("Error".into()),
            last_exit_code: Some(137),
            ..ContainerInfo::default()
        }],
        events: vec![PodEventInfo {
            kind: "Warning".into(),
            reason: "BackOff".into(),
            message: "Back-off restarting failed container".into(),
            count: 5,
            first_timestamp: Some("2026-05-10T11:00:00Z".into()),
            last_timestamp: Some("2026-05-10T11:55:00Z".into()),
            source: Some("kubelet".into()),
        }],
    }));
    let app = homelab_k3s_mcp::app(None, fake.clone(), unavailable_github(), unavailable_aws());

    let response = app
        .oneshot(json_request(
            "/mcp",
            json!({
                "jsonrpc": "2.0",
                "id": 91,
                "method": "tools/call",
                "params": {
                    "name": "pod_describe",
                    "arguments": {
                        "namespace": "default",
                        "name": "api-7d9c9f6b8b-xyz"
                    }
                }
            }),
        ))
        .await
        .unwrap();

    let body = body_json(response).await;
    assert_eq!(body["result"]["isError"], false);
    let payload = &body["result"]["structuredContent"];
    assert_eq!(payload["name"], "api-7d9c9f6b8b-xyz");
    assert_eq!(payload["namespace"], "default");
    assert_eq!(payload["node"], "k3s-node-1");
    assert_eq!(payload["phase"], "Running");
    assert_eq!(payload["pod_ip"], "10.0.0.42");
    assert_eq!(payload["containers"][0]["name"], "api");
    assert_eq!(
        payload["containers"][0]["image"],
        "ghcr.io/example/api:1.2.3"
    );
    assert_eq!(payload["containers"][0]["state"], "running");
    assert_eq!(payload["containers"][0]["restart_count"], 2);
    assert_eq!(payload["containers"][0]["last_state"], "terminated");
    assert_eq!(payload["containers"][0]["last_exit_code"], 137);
    assert_eq!(payload["conditions"][0]["type"], "Ready");
    assert_eq!(payload["conditions"][0]["status"], "True");
    assert_eq!(payload["events"][0]["type"], "Warning");
    assert_eq!(payload["events"][0]["reason"], "BackOff");
    assert_eq!(payload["events"][0]["count"], 5);

    let text = body["result"]["content"][0]["text"].as_str().unwrap_or("");
    assert!(text.contains("Name:         api-7d9c9f6b8b-xyz"), "{text}");
    assert!(text.contains("Namespace:    default"), "{text}");
    assert!(text.contains("Node:         k3s-node-1"), "{text}");
    assert!(text.contains("ghcr.io/example/api:1.2.3"), "{text}");
    assert!(text.contains("BackOff"), "{text}");

    let calls = fake.describe_calls.lock().unwrap();
    assert_eq!(calls.len(), 1);
    assert_eq!(calls[0].namespace, "default");
    assert_eq!(
        calls[0].target,
        PodTarget::Name("api-7d9c9f6b8b-xyz".to_string())
    );
}

#[tokio::test]
async fn pod_describe_accepts_label_selector_target() {
    let fake = Arc::new(FakeK8s::default());
    let app = homelab_k3s_mcp::app(None, fake.clone(), unavailable_github(), unavailable_aws());

    let response = app
        .oneshot(json_request(
            "/mcp",
            json!({
                "jsonrpc": "2.0",
                "id": 95,
                "method": "tools/call",
                "params": {
                    "name": "pod_describe",
                    "arguments": {
                        "namespace": "default",
                        "selector": "app=api"
                    }
                }
            }),
        ))
        .await
        .unwrap();

    let body = body_json(response).await;
    assert_eq!(body["result"]["isError"], false);

    let calls = fake.describe_calls.lock().unwrap();
    assert_eq!(calls.len(), 1);
    assert_eq!(calls[0].namespace, "default");
    assert_eq!(calls[0].target, PodTarget::Selector("app=api".to_string()));
}

#[tokio::test]
async fn pod_describe_accepts_workload_target() {
    let fake = Arc::new(FakeK8s::default());
    let app = homelab_k3s_mcp::app(None, fake.clone(), unavailable_github(), unavailable_aws());

    let response = app
        .oneshot(json_request(
            "/mcp",
            json!({
                "jsonrpc": "2.0",
                "id": 96,
                "method": "tools/call",
                "params": {
                    "name": "pod_describe",
                    "arguments": {
                        "namespace": "default",
                        "workload_kind": "Deployment",
                        "workload_name": "api"
                    }
                }
            }),
        ))
        .await
        .unwrap();

    let body = body_json(response).await;
    assert_eq!(body["result"]["isError"], false);

    let calls = fake.describe_calls.lock().unwrap();
    assert_eq!(calls.len(), 1);
    assert_eq!(calls[0].namespace, "default");
    assert_eq!(
        calls[0].target,
        PodTarget::Workload {
            kind: WorkloadKind::Deployment,
            name: "api".to_string()
        }
    );
}

#[tokio::test]
async fn pod_describe_rejects_mutually_exclusive_targets() {
    let response = homelab_k3s_mcp::app(
        None,
        unavailable_k8s(),
        unavailable_github(),
        unavailable_aws(),
    )
    .oneshot(json_request(
        "/mcp",
        json!({
            "jsonrpc": "2.0",
            "id": 97,
            "method": "tools/call",
            "params": {
                "name": "pod_describe",
                "arguments": {
                    "namespace": "default",
                    "name": "api-0",
                    "selector": "app=api"
                }
            }
        }),
    ))
    .await
    .unwrap();

    let body = body_json(response).await;
    assert_eq!(body["error"]["code"], -32602);
    let msg = body["error"]["message"].as_str().unwrap_or("");
    assert!(msg.contains("mutually exclusive"), "{msg}");
}

#[tokio::test]
async fn pod_describe_rejects_partial_workload_target() {
    let response = homelab_k3s_mcp::app(
        None,
        unavailable_k8s(),
        unavailable_github(),
        unavailable_aws(),
    )
    .oneshot(json_request(
        "/mcp",
        json!({
            "jsonrpc": "2.0",
            "id": 98,
            "method": "tools/call",
            "params": {
                "name": "pod_describe",
                "arguments": {
                    "namespace": "default",
                    "workload_kind": "Deployment"
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
async fn pod_describe_renders_no_events_placeholder() {
    let fake = Arc::new(FakeK8s::default());
    let app = homelab_k3s_mcp::app(None, fake.clone(), unavailable_github(), unavailable_aws());

    let response = app
        .oneshot(json_request(
            "/mcp",
            json!({
                "jsonrpc": "2.0",
                "id": 92,
                "method": "tools/call",
                "params": {
                    "name": "pod_describe",
                    "arguments": {
                        "namespace": "default",
                        "name": "api-0"
                    }
                }
            }),
        ))
        .await
        .unwrap();

    let body = body_json(response).await;
    assert_eq!(body["result"]["isError"], false);
    let text = body["result"]["content"][0]["text"].as_str().unwrap_or("");
    assert!(text.contains("Events:       <none>"), "{text}");
}

#[tokio::test]
async fn pod_describe_requires_a_target() {
    let response = homelab_k3s_mcp::app(
        None,
        unavailable_k8s(),
        unavailable_github(),
        unavailable_aws(),
    )
    .oneshot(json_request(
        "/mcp",
        json!({
            "jsonrpc": "2.0",
            "id": 93,
            "method": "tools/call",
            "params": {
                "name": "pod_describe",
                "arguments": { "namespace": "default" }
            }
        }),
    ))
    .await
    .unwrap();

    let body = body_json(response).await;
    assert_eq!(body["error"]["code"], -32602);
    let msg = body["error"]["message"].as_str().unwrap_or("");
    assert!(msg.contains("name"), "{msg}");
    assert!(msg.contains("selector"), "{msg}");
}

#[tokio::test]
async fn pod_describe_surfaces_k8s_error_as_tool_error() {
    let fake = Arc::new(FakeK8s::default());
    *fake.describe_response.lock().unwrap() =
        Some(Err(K8sError::Api("pods \"missing\" not found".to_string())));
    let app = homelab_k3s_mcp::app(None, fake.clone(), unavailable_github(), unavailable_aws());

    let response = app
        .oneshot(json_request(
            "/mcp",
            json!({
                "jsonrpc": "2.0",
                "id": 94,
                "method": "tools/call",
                "params": {
                    "name": "pod_describe",
                    "arguments": { "namespace": "default", "name": "missing" }
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
        .contains("not found"));
}

#[tokio::test]
async fn tools_list_advertises_github_app_installation_token() {
    let response = homelab_k3s_mcp::app(
        None,
        unavailable_k8s(),
        unavailable_github(),
        unavailable_aws(),
    )
    .oneshot(json_request(
        "/mcp",
        json!({"jsonrpc": "2.0", "id": 70, "method": "tools/list"}),
    ))
    .await
    .unwrap();

    let body = body_json(response).await;
    let tools = body["result"]["tools"].as_array().expect("tools array");

    let token = find_tool(tools, "github_app_installation_token");
    assert!(
        token["inputSchema"]["required"].is_null()
            || token["inputSchema"]["required"]
                .as_array()
                .map(|a| a.is_empty())
                .unwrap_or(false),
        "tool should not require any input fields"
    );
    let props = token["inputSchema"]["properties"]
        .as_object()
        .expect("properties");
    assert!(!props.contains_key("installation_id"));
    assert!(props.contains_key("repositories"));
    assert!(props.contains_key("permissions"));

    assert_eq!(
        token["annotations"]["title"],
        "GitHub App Installation Token"
    );
    assert_eq!(token["annotations"]["readOnlyHint"], false);
    assert_eq!(token["annotations"]["destructiveHint"], false);
    assert_eq!(token["annotations"]["idempotentHint"], false);
    assert_eq!(token["annotations"]["openWorldHint"], true);
}

#[tokio::test]
async fn github_app_installation_token_dispatches_with_defaults() {
    let fake = Arc::new(FakeGitHub::default());
    *fake.response.lock().unwrap() = Some(Ok(InstallationToken {
        token: "ghs_short_lived".into(),
        expires_at: "2026-05-07T01:00:00Z".into(),
        permissions: Some(json!({ "contents": "read", "metadata": "read" })),
        repository_selection: Some("all".into()),
    }));
    let app = homelab_k3s_mcp::app(None, unavailable_k8s(), fake.clone(), unavailable_aws());

    let response = app
        .oneshot(json_request(
            "/mcp",
            json!({
                "jsonrpc": "2.0",
                "id": 71,
                "method": "tools/call",
                "params": {
                    "name": "github_app_installation_token",
                    "arguments": {}
                }
            }),
        ))
        .await
        .unwrap();

    let body = body_json(response).await;
    assert_eq!(body["result"]["isError"], false);
    assert!(body["result"]["structuredContent"].is_null());

    let resource = &body["result"]["content"][0];
    assert_eq!(resource["type"], "resource");
    assert_eq!(resource["resource"]["mimeType"], "text/plain");
    let uri = resource["resource"]["uri"]
        .as_str()
        .expect("resource uri")
        .to_string();
    assert!(
        uri.ends_with(".env"),
        "uri should look like an env file: {uri}"
    );
    let text = resource["resource"]["text"]
        .as_str()
        .expect("resource text");
    assert!(text.contains("GITHUB_TOKEN=ghs_short_lived"));
    assert!(text.contains("# Expires at: 2026-05-07T01:00:00Z"));
    assert!(text.contains("# Repository selection: all"));
    assert!(text.contains("contents=read"));
    assert!(text.contains("metadata=read"));

    let calls = fake.calls.lock().unwrap();
    assert_eq!(calls.len(), 1);
    let call = &calls[0];
    assert!(call.repositories.is_none());
    assert!(call.permissions.is_none());
}

#[tokio::test]
async fn github_app_installation_token_passes_through_scope() {
    let fake = Arc::new(FakeGitHub::default());
    let app = homelab_k3s_mcp::app(None, unavailable_k8s(), fake.clone(), unavailable_aws());

    let response = app
        .oneshot(json_request(
            "/mcp",
            json!({
                "jsonrpc": "2.0",
                "id": 72,
                "method": "tools/call",
                "params": {
                    "name": "github_app_installation_token",
                    "arguments": {
                        "repositories": ["homelab-k3s-mcp", "infra"],
                        "permissions": { "contents": "read", "pull_requests": "write" }
                    }
                }
            }),
        ))
        .await
        .unwrap();

    let body = body_json(response).await;
    assert_eq!(body["result"]["isError"], false);

    let calls = fake.calls.lock().unwrap();
    assert_eq!(calls.len(), 1);
    let call = &calls[0];
    assert_eq!(
        call.repositories.as_deref(),
        Some(&["homelab-k3s-mcp".to_string(), "infra".to_string()][..])
    );
    let perms = call.permissions.as_ref().expect("permissions forwarded");
    assert_eq!(perms["contents"], "read");
    assert_eq!(perms["pull_requests"], "write");
}

#[tokio::test]
async fn github_app_installation_token_dispatches_without_arguments_field() {
    let fake = Arc::new(FakeGitHub::default());
    let app = homelab_k3s_mcp::app(None, unavailable_k8s(), fake.clone(), unavailable_aws());

    let response = app
        .oneshot(json_request(
            "/mcp",
            json!({
                "jsonrpc": "2.0",
                "id": 73,
                "method": "tools/call",
                "params": { "name": "github_app_installation_token" }
            }),
        ))
        .await
        .unwrap();

    let body = body_json(response).await;
    assert_eq!(body["result"]["isError"], false);
    assert_eq!(fake.calls.lock().unwrap().len(), 1);
}

#[tokio::test]
async fn github_app_installation_token_unavailable_returns_tool_error() {
    let response = homelab_k3s_mcp::app(
        None,
        unavailable_k8s(),
        unavailable_github(),
        unavailable_aws(),
    )
    .oneshot(json_request(
        "/mcp",
        json!({
            "jsonrpc": "2.0",
            "id": 74,
            "method": "tools/call",
            "params": {
                "name": "github_app_installation_token",
                "arguments": {}
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
        .contains("github app"));
}

#[tokio::test]
async fn github_app_installation_token_rejects_non_array_repositories() {
    let response = homelab_k3s_mcp::app(
        None,
        unavailable_k8s(),
        unavailable_github(),
        unavailable_aws(),
    )
    .oneshot(json_request(
        "/mcp",
        json!({
            "jsonrpc": "2.0",
            "id": 75,
            "method": "tools/call",
            "params": {
                "name": "github_app_installation_token",
                "arguments": { "repositories": "not-a-list" }
            }
        }),
    ))
    .await
    .unwrap();

    let body = body_json(response).await;
    assert_eq!(body["error"]["code"], -32602);
}

#[tokio::test]
async fn tools_list_advertises_aws_config_get() {
    let response = homelab_k3s_mcp::app(
        None,
        unavailable_k8s(),
        unavailable_github(),
        unavailable_aws(),
    )
    .oneshot(json_request(
        "/mcp",
        json!({"jsonrpc": "2.0", "id": 100, "method": "tools/list"}),
    ))
    .await
    .unwrap();

    let body = body_json(response).await;
    let tools = body["result"]["tools"].as_array().expect("tools array");
    let aws = find_tool(tools, "aws_config_get");

    assert_eq!(aws["annotations"]["title"], "Get AWS Config File");
    assert_eq!(aws["annotations"]["readOnlyHint"], true);
    assert_eq!(aws["annotations"]["idempotentHint"], true);
    assert_eq!(aws["annotations"]["openWorldHint"], true);

    let required = aws["inputSchema"]["required"].as_array();
    assert!(required.is_none() || required.unwrap().is_empty());
    let props = aws["inputSchema"]["properties"]
        .as_object()
        .expect("properties object");
    assert!(props.is_empty());
}

#[tokio::test]
async fn aws_config_get_dispatches_to_service() {
    let fake = Arc::new(FakeAws::default());
    *fake.response.lock().unwrap() = Some(Ok(AwsConfigFile {
        bucket: "homelab-config".into(),
        key: "aws/config".into(),
        content_type: Some("text/plain".into()),
        body: "[default]\nregion = ap-northeast-2\noutput = json\n".into(),
    }));
    let app = homelab_k3s_mcp::app(None, unavailable_k8s(), unavailable_github(), fake.clone());

    let response = app
        .oneshot(json_request(
            "/mcp",
            json!({
                "jsonrpc": "2.0",
                "id": 101,
                "method": "tools/call",
                "params": { "name": "aws_config_get", "arguments": {} }
            }),
        ))
        .await
        .unwrap();

    let body = body_json(response).await;
    assert_eq!(body["result"]["isError"], false);
    assert_eq!(
        body["result"]["content"][0]["text"],
        "[default]\nregion = ap-northeast-2\noutput = json\n"
    );
    let payload = &body["result"]["structuredContent"];
    assert_eq!(payload["bucket"], "homelab-config");
    assert_eq!(payload["key"], "aws/config");
    assert_eq!(payload["contentType"], "text/plain");
    assert_eq!(*fake.calls.lock().unwrap(), 1);
}

#[tokio::test]
async fn aws_config_get_dispatches_without_arguments_field() {
    let fake = Arc::new(FakeAws::default());
    let app = homelab_k3s_mcp::app(None, unavailable_k8s(), unavailable_github(), fake.clone());

    let response = app
        .oneshot(json_request(
            "/mcp",
            json!({
                "jsonrpc": "2.0",
                "id": 102,
                "method": "tools/call",
                "params": { "name": "aws_config_get" }
            }),
        ))
        .await
        .unwrap();

    let body = body_json(response).await;
    assert_eq!(body["result"]["isError"], false);
    assert_eq!(*fake.calls.lock().unwrap(), 1);
}

#[tokio::test]
async fn aws_config_get_surfaces_error_as_tool_error() {
    let fake = Arc::new(FakeAws::default());
    *fake.response.lock().unwrap() = Some(Err(AwsError::Api(
        "s3 get-object returned 403 Forbidden: AccessDenied".to_string(),
    )));
    let app = homelab_k3s_mcp::app(None, unavailable_k8s(), unavailable_github(), fake.clone());

    let response = app
        .oneshot(json_request(
            "/mcp",
            json!({
                "jsonrpc": "2.0",
                "id": 103,
                "method": "tools/call",
                "params": { "name": "aws_config_get", "arguments": {} }
            }),
        ))
        .await
        .unwrap();

    let body = body_json(response).await;
    assert_eq!(body["result"]["isError"], true);
    assert!(body["result"]["content"][0]["text"]
        .as_str()
        .unwrap_or("")
        .contains("aws api error"));
}

#[tokio::test]
async fn aws_config_get_unavailable_returns_tool_error() {
    let response = homelab_k3s_mcp::app(
        None,
        unavailable_k8s(),
        unavailable_github(),
        unavailable_aws(),
    )
    .oneshot(json_request(
        "/mcp",
        json!({
            "jsonrpc": "2.0",
            "id": 104,
            "method": "tools/call",
            "params": { "name": "aws_config_get", "arguments": {} }
        }),
    ))
    .await
    .unwrap();

    let body = body_json(response).await;
    assert_eq!(body["result"]["isError"], true);
    assert!(body["result"]["content"][0]["text"]
        .as_str()
        .unwrap_or("")
        .contains("aws config"));
}
