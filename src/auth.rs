use std::sync::Arc;

use axum::{
    extract::{Request, State},
    http::{header, HeaderValue, StatusCode},
    middleware::Next,
    response::{IntoResponse, Json, Response},
};
use jsonwebtoken::{decode, Algorithm, DecodingKey, Validation};
use serde::{Deserialize, Serialize};

#[derive(Clone)]
pub struct AuthConfig {
    pub issuer: String,
    pub audience: String,
    pub resource: String,
    decoding_key: Arc<DecodingKey>,
}

impl std::fmt::Debug for AuthConfig {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        f.debug_struct("AuthConfig")
            .field("issuer", &self.issuer)
            .field("audience", &self.audience)
            .field("resource", &self.resource)
            .field("decoding_key", &"<redacted>")
            .finish()
    }
}

impl AuthConfig {
    pub fn from_env() -> Result<Option<Self>, String> {
        if matches!(
            std::env::var("MCP_AUTH_DISABLED").as_deref(),
            Ok("1" | "true")
        ) {
            return Ok(None);
        }

        let issuer = std::env::var("MCP_OAUTH_ISSUER")
            .map_err(|_| "MCP_OAUTH_ISSUER is required when auth is enabled".to_string())?;
        let audience = std::env::var("MCP_OAUTH_AUDIENCE")
            .map_err(|_| "MCP_OAUTH_AUDIENCE is required when auth is enabled".to_string())?;
        let secret = std::env::var("MCP_OAUTH_HS256_SECRET")
            .map_err(|_| "MCP_OAUTH_HS256_SECRET is required when auth is enabled".to_string())?;
        let resource = std::env::var("MCP_OAUTH_RESOURCE").unwrap_or_else(|_| audience.clone());

        Ok(Some(Self {
            issuer,
            audience,
            resource,
            decoding_key: Arc::new(DecodingKey::from_secret(secret.as_bytes())),
        }))
    }

    fn validation(&self) -> Validation {
        let mut v = Validation::new(Algorithm::HS256);
        v.set_audience(&[&self.audience]);
        v.set_issuer(&[&self.issuer]);
        v.validate_exp = true;
        v
    }

    fn verify(&self, token: &str) -> Result<Claims, jsonwebtoken::errors::Error> {
        decode::<Claims>(token, &self.decoding_key, &self.validation()).map(|data| data.claims)
    }
}

#[derive(Debug, Clone, Deserialize)]
pub struct Claims {
    pub sub: Option<String>,
    pub exp: usize,
    #[serde(default)]
    pub scope: Option<String>,
}

#[derive(Debug, Serialize)]
pub struct ProtectedResourceMetadata {
    pub resource: String,
    pub authorization_servers: Vec<String>,
    pub bearer_methods_supported: Vec<&'static str>,
}

pub async fn protected_resource_metadata(
    State(config): State<Arc<AuthConfig>>,
) -> Json<ProtectedResourceMetadata> {
    Json(ProtectedResourceMetadata {
        resource: config.resource.clone(),
        authorization_servers: vec![config.issuer.clone()],
        bearer_methods_supported: vec!["header"],
    })
}

pub async fn require_bearer(
    State(config): State<Arc<AuthConfig>>,
    mut req: Request,
    next: Next,
) -> Response {
    let token = match extract_bearer(&req) {
        Ok(t) => t.to_string(),
        Err(err) => return unauthorized(&config, err).into_response(),
    };

    match config.verify(&token) {
        Ok(claims) => {
            req.extensions_mut().insert(claims);
            next.run(req).await
        }
        Err(err) => {
            tracing::debug!(error = %err, "rejecting token");
            unauthorized(&config, "invalid_token").into_response()
        }
    }
}

fn extract_bearer(req: &Request) -> Result<&str, &'static str> {
    let header = req
        .headers()
        .get(header::AUTHORIZATION)
        .ok_or("missing_token")?;
    let value = header.to_str().map_err(|_| "invalid_request")?;
    let token = value
        .strip_prefix("Bearer ")
        .or_else(|| value.strip_prefix("bearer "))
        .ok_or("invalid_request")?;
    if token.is_empty() {
        return Err("invalid_request");
    }
    Ok(token)
}

fn unauthorized(config: &AuthConfig, error: &'static str) -> Response {
    let challenge = format!(
        "Bearer realm=\"{}\", error=\"{}\", resource_metadata=\"{}/.well-known/oauth-protected-resource\"",
        config.resource, error, config.resource
    );
    let mut resp = (StatusCode::UNAUTHORIZED, error).into_response();
    if let Ok(value) = HeaderValue::from_str(&challenge) {
        resp.headers_mut().insert(header::WWW_AUTHENTICATE, value);
    }
    resp
}
