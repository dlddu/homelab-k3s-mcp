use std::time::{SystemTime, UNIX_EPOCH};

use axum::body::Body;
use axum::http::{Request, StatusCode};
use http_body_util::BodyExt;
use jsonwebtoken::{encode, EncodingKey, Header};
use serde::Serialize;
use serde_json::json;
use tower::ServiceExt;

const SECRET: &[u8] = b"test-secret-do-not-use-in-prod";
const ISSUER: &str = "https://auth.example.test";
const AUDIENCE: &str = "homelab-k3s-mcp";

fn auth_config() -> homelab_k3s_mcp::AuthConfig {
    std::env::set_var("MCP_OAUTH_ISSUER", ISSUER);
    std::env::set_var("MCP_OAUTH_AUDIENCE", AUDIENCE);
    std::env::set_var(
        "MCP_OAUTH_HS256_SECRET",
        std::str::from_utf8(SECRET).unwrap(),
    );
    std::env::remove_var("MCP_AUTH_DISABLED");
    homelab_k3s_mcp::AuthConfig::from_env()
        .expect("config")
        .expect("auth enabled")
}

#[derive(Serialize)]
struct TestClaims {
    iss: String,
    aud: String,
    exp: usize,
    sub: String,
}

fn token(exp_offset: i64) -> String {
    let now = SystemTime::now()
        .duration_since(UNIX_EPOCH)
        .unwrap()
        .as_secs() as i64;
    let claims = TestClaims {
        iss: ISSUER.to_string(),
        aud: AUDIENCE.to_string(),
        exp: (now + exp_offset) as usize,
        sub: "tester".to_string(),
    };
    encode(
        &Header::default(),
        &claims,
        &EncodingKey::from_secret(SECRET),
    )
    .unwrap()
}

fn mcp_request(token: Option<&str>) -> Request<Body> {
    let mut builder = Request::builder()
        .method("POST")
        .uri("/mcp")
        .header("content-type", "application/json");
    if let Some(t) = token {
        builder = builder.header("authorization", format!("Bearer {t}"));
    }
    builder
        .body(Body::from(
            json!({"jsonrpc": "2.0", "id": 1, "method": "tools/list"}).to_string(),
        ))
        .unwrap()
}

#[tokio::test]
async fn missing_token_returns_401() {
    let app = homelab_k3s_mcp::app(Some(auth_config()));
    let response = app.oneshot(mcp_request(None)).await.unwrap();

    assert_eq!(response.status(), StatusCode::UNAUTHORIZED);
    let www = response
        .headers()
        .get("www-authenticate")
        .expect("www-authenticate header");
    let value = www.to_str().unwrap();
    assert!(value.starts_with("Bearer "));
    assert!(value.contains("resource_metadata="));
}

#[tokio::test]
async fn invalid_token_returns_401() {
    let app = homelab_k3s_mcp::app(Some(auth_config()));
    let response = app
        .oneshot(mcp_request(Some("not-a-real-token")))
        .await
        .unwrap();

    assert_eq!(response.status(), StatusCode::UNAUTHORIZED);
}

#[tokio::test]
async fn expired_token_returns_401() {
    let app = homelab_k3s_mcp::app(Some(auth_config()));
    let response = app.oneshot(mcp_request(Some(&token(-3600)))).await.unwrap();

    assert_eq!(response.status(), StatusCode::UNAUTHORIZED);
}

#[tokio::test]
async fn valid_token_passes_through() {
    let app = homelab_k3s_mcp::app(Some(auth_config()));
    let response = app.oneshot(mcp_request(Some(&token(3600)))).await.unwrap();

    assert_eq!(response.status(), StatusCode::OK);
    let bytes = response.into_body().collect().await.unwrap().to_bytes();
    let body: serde_json::Value = serde_json::from_slice(&bytes).unwrap();
    assert!(body["result"]["tools"].is_array());
}

#[tokio::test]
async fn protected_resource_metadata_published() {
    let app = homelab_k3s_mcp::app(Some(auth_config()));
    let response = app
        .oneshot(
            Request::builder()
                .uri("/.well-known/oauth-protected-resource")
                .body(Body::empty())
                .unwrap(),
        )
        .await
        .unwrap();

    assert_eq!(response.status(), StatusCode::OK);
    let bytes = response.into_body().collect().await.unwrap().to_bytes();
    let body: serde_json::Value = serde_json::from_slice(&bytes).unwrap();
    assert_eq!(body["authorization_servers"][0], ISSUER);
    assert_eq!(body["resource"], AUDIENCE);
}

#[tokio::test]
async fn health_endpoints_remain_public() {
    let app = homelab_k3s_mcp::app(Some(auth_config()));
    let response = app
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
