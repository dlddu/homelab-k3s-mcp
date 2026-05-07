use std::sync::Arc;

use axum::{extract::State, response::Json, routing::post, Router};
use serde::{Deserialize, Serialize};
use serde_json::{json, Value};

use crate::k8s::{K8sError, K8sService};

pub const PROTOCOL_VERSION: &str = "2025-06-18";
pub const SERVER_NAME: &str = env!("CARGO_PKG_NAME");
pub const SERVER_VERSION: &str = env!("CARGO_PKG_VERSION");

pub type SharedK8s = Arc<dyn K8sService>;

pub fn router<S: Clone + Send + Sync + 'static>(k8s: SharedK8s) -> Router<S> {
    Router::new().route("/mcp", post(handle)).with_state(k8s)
}

#[derive(Debug, Deserialize)]
pub struct JsonRpcRequest {
    #[serde(default = "default_jsonrpc")]
    pub jsonrpc: String,
    pub id: Option<Value>,
    pub method: String,
    #[serde(default)]
    pub params: Value,
}

fn default_jsonrpc() -> String {
    "2.0".to_string()
}

#[derive(Debug, Serialize)]
pub struct JsonRpcResponse {
    pub jsonrpc: &'static str,
    pub id: Value,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub result: Option<Value>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub error: Option<JsonRpcError>,
}

#[derive(Debug, Serialize)]
pub struct JsonRpcError {
    pub code: i32,
    pub message: String,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub data: Option<Value>,
}

impl JsonRpcResponse {
    fn ok(id: Value, result: Value) -> Self {
        Self {
            jsonrpc: "2.0",
            id,
            result: Some(result),
            error: None,
        }
    }

    fn err(id: Value, code: i32, message: impl Into<String>) -> Self {
        Self {
            jsonrpc: "2.0",
            id,
            result: None,
            error: Some(JsonRpcError {
                code,
                message: message.into(),
                data: None,
            }),
        }
    }
}

pub async fn handle(
    State(k8s): State<SharedK8s>,
    Json(req): Json<JsonRpcRequest>,
) -> Json<JsonRpcResponse> {
    let id = req.id.clone().unwrap_or(Value::Null);

    if req.jsonrpc != "2.0" {
        return Json(JsonRpcResponse::err(id, -32600, "invalid jsonrpc version"));
    }

    let response = match req.method.as_str() {
        "initialize" => initialize(),
        "tools/list" => tools_list(),
        "tools/call" => tools_call(&k8s, &req.params).await,
        "ping" => Ok(json!({})),
        other => Err((-32601, format!("method not found: {other}"))),
    };

    match response {
        Ok(result) => Json(JsonRpcResponse::ok(id, result)),
        Err((code, message)) => Json(JsonRpcResponse::err(id, code, message)),
    }
}

fn initialize() -> Result<Value, (i32, String)> {
    Ok(json!({
        "protocolVersion": PROTOCOL_VERSION,
        "capabilities": { "tools": { "listChanged": false } },
        "serverInfo": { "name": SERVER_NAME, "version": SERVER_VERSION },
    }))
}

fn tools_list() -> Result<Value, (i32, String)> {
    let optional_namespace = json!({
        "type": "object",
        "properties": {
            "namespace": {
                "type": "string",
                "description": "Optional namespace. If omitted, lists across all namespaces."
            }
        },
        "additionalProperties": false,
    });
    let restart_args = json!({
        "type": "object",
        "properties": {
            "namespace": { "type": "string", "description": "Namespace of the workload." },
            "name": { "type": "string", "description": "Name of the workload." }
        },
        "required": ["namespace", "name"],
        "additionalProperties": false,
    });

    Ok(json!({
        "tools": [
            {
                "name": "ping",
                "description": "Health-check tool that always returns 'pong'.",
                "inputSchema": {
                    "type": "object",
                    "properties": {},
                    "additionalProperties": false,
                },
            },
            {
                "name": "list_deployments",
                "description": "List Deployments (apps/v1) in the cluster, optionally filtered by namespace.",
                "inputSchema": optional_namespace,
            },
            {
                "name": "list_statefulsets",
                "description": "List StatefulSets (apps/v1) in the cluster, optionally filtered by namespace.",
                "inputSchema": optional_namespace,
            },
            {
                "name": "list_daemonsets",
                "description": "List DaemonSets (apps/v1) in the cluster, optionally filtered by namespace.",
                "inputSchema": optional_namespace,
            },
            {
                "name": "rollout_restart_deployment",
                "description": "Trigger a rolling restart of a Deployment by patching spec.template.metadata.annotations.",
                "inputSchema": restart_args,
            },
            {
                "name": "rollout_restart_statefulset",
                "description": "Trigger a rolling restart of a StatefulSet by patching spec.template.metadata.annotations.",
                "inputSchema": restart_args,
            },
            {
                "name": "rollout_restart_daemonset",
                "description": "Trigger a rolling restart of a DaemonSet by patching spec.template.metadata.annotations.",
                "inputSchema": restart_args,
            }
        ]
    }))
}

async fn tools_call(k8s: &SharedK8s, params: &Value) -> Result<Value, (i32, String)> {
    let name = params
        .get("name")
        .and_then(Value::as_str)
        .ok_or((-32602, "missing tool name".to_string()))?;
    let args = params.get("arguments").cloned().unwrap_or(Value::Null);

    match name {
        "ping" => Ok(json!({
            "content": [{ "type": "text", "text": "pong" }],
            "isError": false,
        })),
        "list_deployments" => {
            let namespace = optional_namespace_arg(&args)?;
            match k8s.list_deployments(namespace.as_deref()).await {
                Ok(items) => Ok(success_json(json!({ "items": items }))),
                Err(err) => Ok(tool_error(err)),
            }
        }
        "list_statefulsets" => {
            let namespace = optional_namespace_arg(&args)?;
            match k8s.list_statefulsets(namespace.as_deref()).await {
                Ok(items) => Ok(success_json(json!({ "items": items }))),
                Err(err) => Ok(tool_error(err)),
            }
        }
        "list_daemonsets" => {
            let namespace = optional_namespace_arg(&args)?;
            match k8s.list_daemonsets(namespace.as_deref()).await {
                Ok(items) => Ok(success_json(json!({ "items": items }))),
                Err(err) => Ok(tool_error(err)),
            }
        }
        "rollout_restart_deployment" => {
            let (namespace, target) = restart_args(&args)?;
            match k8s.rollout_restart_deployment(&namespace, &target).await {
                Ok(restarted_at) => Ok(restart_success(
                    "Deployment",
                    &namespace,
                    &target,
                    &restarted_at,
                )),
                Err(err) => Ok(tool_error(err)),
            }
        }
        "rollout_restart_statefulset" => {
            let (namespace, target) = restart_args(&args)?;
            match k8s.rollout_restart_statefulset(&namespace, &target).await {
                Ok(restarted_at) => Ok(restart_success(
                    "StatefulSet",
                    &namespace,
                    &target,
                    &restarted_at,
                )),
                Err(err) => Ok(tool_error(err)),
            }
        }
        "rollout_restart_daemonset" => {
            let (namespace, target) = restart_args(&args)?;
            match k8s.rollout_restart_daemonset(&namespace, &target).await {
                Ok(restarted_at) => Ok(restart_success(
                    "DaemonSet",
                    &namespace,
                    &target,
                    &restarted_at,
                )),
                Err(err) => Ok(tool_error(err)),
            }
        }
        other => Err((-32602, format!("unknown tool: {other}"))),
    }
}

fn optional_namespace_arg(args: &Value) -> Result<Option<String>, (i32, String)> {
    if args.is_null() {
        return Ok(None);
    }
    let obj = args
        .as_object()
        .ok_or((-32602, "arguments must be an object".to_string()))?;
    match obj.get("namespace") {
        None | Some(Value::Null) => Ok(None),
        Some(Value::String(s)) if s.is_empty() => Ok(None),
        Some(Value::String(s)) => Ok(Some(s.clone())),
        Some(_) => Err((-32602, "namespace must be a string".to_string())),
    }
}

fn restart_args(args: &Value) -> Result<(String, String), (i32, String)> {
    let obj = args
        .as_object()
        .ok_or((-32602, "arguments must be an object".to_string()))?;
    let namespace = obj
        .get("namespace")
        .and_then(Value::as_str)
        .filter(|s| !s.is_empty())
        .ok_or((-32602, "namespace is required".to_string()))?
        .to_string();
    let name = obj
        .get("name")
        .and_then(Value::as_str)
        .filter(|s| !s.is_empty())
        .ok_or((-32602, "name is required".to_string()))?
        .to_string();
    Ok((namespace, name))
}

fn success_json(payload: Value) -> Value {
    let text = serde_json::to_string_pretty(&payload).unwrap_or_else(|_| payload.to_string());
    json!({
        "content": [{ "type": "text", "text": text }],
        "structuredContent": payload,
        "isError": false,
    })
}

fn restart_success(kind: &str, namespace: &str, name: &str, restarted_at: &str) -> Value {
    let payload = json!({
        "kind": kind,
        "namespace": namespace,
        "name": name,
        "restartedAt": restarted_at,
    });
    let text = format!("rollout restart triggered for {kind} {namespace}/{name} at {restarted_at}");
    json!({
        "content": [{ "type": "text", "text": text }],
        "structuredContent": payload,
        "isError": false,
    })
}

fn tool_error(err: K8sError) -> Value {
    json!({
        "content": [{ "type": "text", "text": err.to_string() }],
        "isError": true,
    })
}
