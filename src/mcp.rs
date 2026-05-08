use std::sync::Arc;

use axum::{extract::State, response::Json, routing::post, Router};
use serde::{Deserialize, Serialize};
use serde_json::{json, Value};

use crate::k8s::{ExecOutcome, K8sError, K8sService, LogOptions, LogResult, WorkloadKind};

const LOGS_DEFAULT_TAIL_LINES: i64 = 200;
const LOGS_MAX_TAIL_LINES: i64 = 5000;

const DEAR_BABY_DEFAULT_SELECTOR: &str = "app=dear-baby";
const DEAR_BABY_DEFAULT_CONTAINER: &str = "backend";
const DEAR_BABY_RESET_BIN: &str = "/reset-onboarding";

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
                "annotations": {
                    "title": "Ping",
                    "readOnlyHint": true,
                    "idempotentHint": true,
                    "openWorldHint": false,
                },
            },
            {
                "name": "workload_list",
                "description": "List Kubernetes workloads (Deployment, StatefulSet, DaemonSet). \
                                Namespace is optional; omit it to list across all namespaces.",
                "inputSchema": {
                    "type": "object",
                    "properties": {
                        "kind": {
                            "type": "string",
                            "enum": ["Deployment", "StatefulSet", "DaemonSet"],
                            "description": "Workload kind."
                        },
                        "namespace": {
                            "type": "string",
                            "description": "Namespace. Optional; omitted = all namespaces."
                        }
                    },
                    "required": ["kind"],
                    "additionalProperties": false,
                },
                "annotations": {
                    "title": "List Workloads",
                    "readOnlyHint": true,
                    "idempotentHint": true,
                    "openWorldHint": false,
                },
            },
            {
                "name": "workload_restart",
                "description": "Trigger a rolling restart of a Kubernetes workload \
                                (Deployment, StatefulSet, DaemonSet).",
                "inputSchema": {
                    "type": "object",
                    "properties": {
                        "kind": {
                            "type": "string",
                            "enum": ["Deployment", "StatefulSet", "DaemonSet"],
                            "description": "Workload kind."
                        },
                        "namespace": {
                            "type": "string",
                            "description": "Namespace of the workload."
                        },
                        "name": {
                            "type": "string",
                            "description": "Workload name."
                        }
                    },
                    "required": ["kind", "namespace", "name"],
                    "additionalProperties": false,
                },
                "annotations": {
                    "title": "Restart Workload",
                    "readOnlyHint": false,
                    "destructiveHint": true,
                    "idempotentHint": false,
                    "openWorldHint": false,
                },
            },
            {
                "name": "workload_scale",
                "description": "Scale a Kubernetes workload by setting spec.replicas. \
                                Supports Deployment and StatefulSet. DaemonSets do not have \
                                replicas and are rejected.",
                "inputSchema": {
                    "type": "object",
                    "properties": {
                        "kind": {
                            "type": "string",
                            "enum": ["Deployment", "StatefulSet"],
                            "description": "Workload kind."
                        },
                        "namespace": {
                            "type": "string",
                            "description": "Namespace of the workload."
                        },
                        "name": {
                            "type": "string",
                            "description": "Workload name."
                        },
                        "replicas": {
                            "type": "integer",
                            "minimum": 0,
                            "description": "Desired replica count (>= 0)."
                        }
                    },
                    "required": ["kind", "namespace", "name", "replicas"],
                    "additionalProperties": false,
                },
                "annotations": {
                    "title": "Scale Workload",
                    "readOnlyHint": false,
                    "destructiveHint": true,
                    "idempotentHint": true,
                    "openWorldHint": false,
                },
            },
            {
                "name": "workload_logs",
                "description": "Fetch container logs from a Kubernetes workload \
                                (Deployment, StatefulSet, DaemonSet). Resolves the \
                                workload's pod selector and returns logs from the \
                                first Running pod (or any matching pod when none is \
                                Running, so previous=true works after a crash loop).",
                "inputSchema": {
                    "type": "object",
                    "properties": {
                        "kind": {
                            "type": "string",
                            "enum": ["Deployment", "StatefulSet", "DaemonSet"],
                            "description": "Workload kind."
                        },
                        "namespace": {
                            "type": "string",
                            "description": "Namespace of the workload."
                        },
                        "name": {
                            "type": "string",
                            "description": "Workload name."
                        },
                        "container": {
                            "type": "string",
                            "description": "Container name. Required when the pod has \
                                            more than one container."
                        },
                        "tail_lines": {
                            "type": "integer",
                            "minimum": 1,
                            "maximum": LOGS_MAX_TAIL_LINES,
                            "description": "Number of trailing log lines to return. \
                                            Defaults to 200; capped at 5000."
                        },
                        "previous": {
                            "type": "boolean",
                            "description": "Return logs from a previously terminated \
                                            container instance. Defaults to false."
                        },
                        "timestamps": {
                            "type": "boolean",
                            "description": "Prefix each log line with an RFC3339 \
                                            timestamp. Defaults to false."
                        },
                        "since_seconds": {
                            "type": "integer",
                            "minimum": 1,
                            "description": "Only return logs newer than this many \
                                            seconds. Optional."
                        }
                    },
                    "required": ["kind", "namespace", "name"],
                    "additionalProperties": false,
                },
                "annotations": {
                    "title": "View Workload Logs",
                    "readOnlyHint": true,
                    "idempotentHint": true,
                    "openWorldHint": false,
                },
            },
            {
                "name": "dear_baby_reset_onboarding",
                "description": "Reset dear-baby onboarding for the user with the given email by \
                                exec'ing the bundled /reset-onboarding CLI inside a running \
                                dear-baby backend pod. Clears onboarded_at, due_date, voice \
                                coachmark dismissal, first_record_at, and ai_preview. Records \
                                themselves are preserved.",
                "inputSchema": {
                    "type": "object",
                    "properties": {
                        "namespace": {
                            "type": "string",
                            "description": "Namespace where the dear-baby backend is deployed."
                        },
                        "email": {
                            "type": "string",
                            "description": "Email of the user whose onboarding should be reset."
                        },
                        "selector": {
                            "type": "string",
                            "description": "Label selector for the backend pod. Defaults to \
                                            'app=dear-baby'."
                        },
                        "container": {
                            "type": "string",
                            "description": "Container name inside the pod. Defaults to 'backend'."
                        }
                    },
                    "required": ["namespace", "email"],
                    "additionalProperties": false,
                },
                "annotations": {
                    "title": "Reset dear-baby Onboarding",
                    "readOnlyHint": false,
                    "destructiveHint": true,
                    "idempotentHint": true,
                    "openWorldHint": false,
                },
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
        "workload_list" => workload_list_tool(k8s, &args).await,
        "workload_restart" => workload_restart_tool(k8s, &args).await,
        "workload_scale" => workload_scale_tool(k8s, &args).await,
        "workload_logs" => workload_logs_tool(k8s, &args).await,
        "dear_baby_reset_onboarding" => dear_baby_reset_onboarding_tool(k8s, &args).await,
        other => Err((-32602, format!("unknown tool: {other}"))),
    }
}

fn parse_kind(obj: &serde_json::Map<String, Value>) -> Result<WorkloadKind, (i32, String)> {
    let kind_str = obj
        .get("kind")
        .and_then(Value::as_str)
        .ok_or((-32602, "kind is required".to_string()))?;
    WorkloadKind::parse(kind_str).ok_or((
        -32602,
        format!("unknown kind: {kind_str} (expected Deployment, StatefulSet, or DaemonSet)"),
    ))
}

fn optional_string(obj: &serde_json::Map<String, Value>, key: &str) -> Option<String> {
    obj.get(key)
        .and_then(Value::as_str)
        .filter(|s| !s.is_empty())
        .map(str::to_owned)
}

async fn workload_list_tool(k8s: &SharedK8s, args: &Value) -> Result<Value, (i32, String)> {
    let obj = args
        .as_object()
        .ok_or((-32602, "arguments must be an object".to_string()))?;

    let kind = parse_kind(obj)?;
    let namespace = optional_string(obj, "namespace");

    match k8s.list_workloads(kind, namespace.as_deref()).await {
        Ok(items) => Ok(success_json(json!({
            "kind": kind.as_str(),
            "namespace": namespace,
            "items": items,
        }))),
        Err(err) => Ok(tool_error(err)),
    }
}

async fn workload_restart_tool(k8s: &SharedK8s, args: &Value) -> Result<Value, (i32, String)> {
    let obj = args
        .as_object()
        .ok_or((-32602, "arguments must be an object".to_string()))?;

    let kind = parse_kind(obj)?;
    let namespace =
        optional_string(obj, "namespace").ok_or((-32602, "namespace is required".to_string()))?;
    let name = optional_string(obj, "name").ok_or((-32602, "name is required".to_string()))?;

    match k8s.rollout_restart(kind, &namespace, &name).await {
        Ok(restarted_at) => Ok(success_json(json!({
            "kind": kind.as_str(),
            "namespace": namespace,
            "name": name,
            "restartedAt": restarted_at,
        }))),
        Err(err) => Ok(tool_error(err)),
    }
}

async fn workload_scale_tool(k8s: &SharedK8s, args: &Value) -> Result<Value, (i32, String)> {
    let obj = args
        .as_object()
        .ok_or((-32602, "arguments must be an object".to_string()))?;

    let kind = parse_kind(obj)?;
    let namespace =
        optional_string(obj, "namespace").ok_or((-32602, "namespace is required".to_string()))?;
    let name = optional_string(obj, "name").ok_or((-32602, "name is required".to_string()))?;
    let replicas_value = obj
        .get("replicas")
        .ok_or((-32602, "replicas is required".to_string()))?;
    let replicas = replicas_value
        .as_i64()
        .ok_or((-32602, "replicas must be an integer".to_string()))?;
    if replicas < 0 {
        return Err((-32602, "replicas must be >= 0".to_string()));
    }
    let replicas: i32 = replicas
        .try_into()
        .map_err(|_| (-32602, "replicas is too large".to_string()))?;

    match k8s.scale_workload(kind, &namespace, &name, replicas).await {
        Ok(applied) => Ok(success_json(json!({
            "kind": kind.as_str(),
            "namespace": namespace,
            "name": name,
            "replicas": applied,
        }))),
        Err(err) => Ok(tool_error(err)),
    }
}

async fn workload_logs_tool(k8s: &SharedK8s, args: &Value) -> Result<Value, (i32, String)> {
    let obj = args
        .as_object()
        .ok_or((-32602, "arguments must be an object".to_string()))?;

    let kind = parse_kind(obj)?;
    let namespace =
        optional_string(obj, "namespace").ok_or((-32602, "namespace is required".to_string()))?;
    let name = optional_string(obj, "name").ok_or((-32602, "name is required".to_string()))?;

    let container = optional_string(obj, "container");
    let previous = obj
        .get("previous")
        .map(|v| {
            v.as_bool()
                .ok_or((-32602, "previous must be a boolean".to_string()))
        })
        .transpose()?
        .unwrap_or(false);
    let timestamps = obj
        .get("timestamps")
        .map(|v| {
            v.as_bool()
                .ok_or((-32602, "timestamps must be a boolean".to_string()))
        })
        .transpose()?
        .unwrap_or(false);
    let tail_lines = match obj.get("tail_lines") {
        Some(v) => {
            let n = v
                .as_i64()
                .ok_or((-32602, "tail_lines must be an integer".to_string()))?;
            if n < 1 {
                return Err((-32602, "tail_lines must be >= 1".to_string()));
            }
            if n > LOGS_MAX_TAIL_LINES {
                return Err((
                    -32602,
                    format!("tail_lines must be <= {LOGS_MAX_TAIL_LINES}"),
                ));
            }
            Some(n)
        }
        None => Some(LOGS_DEFAULT_TAIL_LINES),
    };
    let since_seconds = match obj.get("since_seconds") {
        Some(v) => {
            let n = v
                .as_i64()
                .ok_or((-32602, "since_seconds must be an integer".to_string()))?;
            if n < 1 {
                return Err((-32602, "since_seconds must be >= 1".to_string()));
            }
            Some(n)
        }
        None => None,
    };

    let options = LogOptions {
        container: container.clone(),
        tail_lines,
        previous,
        timestamps,
        since_seconds,
    };

    match k8s.workload_logs(kind, &namespace, &name, &options).await {
        Ok(result) => Ok(logs_outcome_json(kind, namespace, name, options, result)),
        Err(err) => Ok(tool_error(err)),
    }
}

fn logs_outcome_json(
    kind: WorkloadKind,
    namespace: String,
    name: String,
    options: LogOptions,
    result: LogResult,
) -> Value {
    let payload = json!({
        "kind": kind.as_str(),
        "namespace": namespace,
        "name": name,
        "pod": result.pod,
        "container": result.container,
        "tailLines": options.tail_lines,
        "previous": options.previous,
        "timestamps": options.timestamps,
        "sinceSeconds": options.since_seconds,
        "logs": result.logs,
    });
    let text = if result.logs.is_empty() {
        "(no log output)".to_string()
    } else {
        result.logs.clone()
    };
    json!({
        "content": [{ "type": "text", "text": text }],
        "structuredContent": payload,
        "isError": false,
    })
}

async fn dear_baby_reset_onboarding_tool(
    k8s: &SharedK8s,
    args: &Value,
) -> Result<Value, (i32, String)> {
    let obj = args
        .as_object()
        .ok_or((-32602, "arguments must be an object".to_string()))?;

    let namespace =
        optional_string(obj, "namespace").ok_or((-32602, "namespace is required".to_string()))?;
    let email = optional_string(obj, "email").ok_or((-32602, "email is required".to_string()))?;
    let selector =
        optional_string(obj, "selector").unwrap_or_else(|| DEAR_BABY_DEFAULT_SELECTOR.to_string());
    let container = optional_string(obj, "container")
        .unwrap_or_else(|| DEAR_BABY_DEFAULT_CONTAINER.to_string());

    let command = vec![DEAR_BABY_RESET_BIN.to_string(), email.clone()];

    match k8s
        .exec_in_pod(&namespace, &selector, Some(&container), &command)
        .await
    {
        Ok(outcome) => Ok(reset_outcome_json(
            namespace, email, selector, container, outcome,
        )),
        Err(err) => Ok(tool_error(err)),
    }
}

fn reset_outcome_json(
    namespace: String,
    email: String,
    selector: String,
    container: String,
    outcome: ExecOutcome,
) -> Value {
    let payload = json!({
        "namespace": namespace,
        "email": email,
        "selector": selector,
        "container": container,
        "pod": outcome.pod,
        "exitCode": outcome.exit_code,
        "stdout": outcome.stdout,
        "stderr": outcome.stderr,
        "success": outcome.success,
    });
    let text = serde_json::to_string_pretty(&payload).unwrap_or_else(|_| payload.to_string());
    json!({
        "content": [{ "type": "text", "text": text }],
        "structuredContent": payload,
        "isError": !outcome.success,
    })
}

fn success_json(payload: Value) -> Value {
    let text = serde_json::to_string_pretty(&payload).unwrap_or_else(|_| payload.to_string());
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
