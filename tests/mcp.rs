use axum::body::Body;
use axum::http::{Request, StatusCode};
use http_body_util::BodyExt;
use serde_json::{json, Value};
use tower::ServiceExt;

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
    let response = homelab_k3s_mcp::app(None)
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
async fn tools_list_includes_ping() {
    let response = homelab_k3s_mcp::app(None)
        .oneshot(json_request(
            "/mcp",
            json!({"jsonrpc": "2.0", "id": 2, "method": "tools/list"}),
        ))
        .await
        .unwrap();

    let body = body_json(response).await;
    let tools = body["result"]["tools"].as_array().expect("tools array");
    assert_eq!(tools.len(), 1);
    assert_eq!(tools[0]["name"], "ping");
}

#[tokio::test]
async fn ping_tool_returns_pong() {
    let response = homelab_k3s_mcp::app(None)
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
    let response = homelab_k3s_mcp::app(None)
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
    let response = homelab_k3s_mcp::app(None)
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
