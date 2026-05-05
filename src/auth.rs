use std::collections::HashMap;
use std::sync::Arc;
use std::time::Duration;

use axum::{
    extract::{Request, State},
    http::{header, HeaderValue, StatusCode},
    middleware::Next,
    response::{IntoResponse, Json, Response},
};
use jsonwebtoken::{decode, decode_header, Algorithm, DecodingKey, Validation};
use serde::{Deserialize, Serialize};
use tokio::sync::RwLock;

#[derive(Clone)]
pub struct AuthConfig {
    pub issuer: String,
    pub audience: String,
    pub resource: String,
    jwks_uri: String,
    keys: Arc<RwLock<HashMap<String, DecodingKey>>>,
    http: reqwest::Client,
}

impl std::fmt::Debug for AuthConfig {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        f.debug_struct("AuthConfig")
            .field("issuer", &self.issuer)
            .field("audience", &self.audience)
            .field("resource", &self.resource)
            .field("jwks_uri", &self.jwks_uri)
            .finish()
    }
}

#[derive(Debug, Deserialize)]
struct ProviderMetadata {
    jwks_uri: String,
}

#[derive(Debug, Deserialize)]
struct Jwks {
    keys: Vec<Jwk>,
}

#[derive(Debug, Deserialize)]
struct Jwk {
    kid: String,
    kty: String,
    n: Option<String>,
    e: Option<String>,
}

impl AuthConfig {
    pub async fn from_env() -> Result<Option<Self>, String> {
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
        let resource = std::env::var("MCP_OAUTH_RESOURCE").unwrap_or_else(|_| audience.clone());

        let http = reqwest::Client::builder()
            .timeout(Duration::from_secs(10))
            .build()
            .map_err(|e| format!("build http client: {e}"))?;

        let metadata_url = format!(
            "{}/.well-known/openid-configuration",
            issuer.trim_end_matches('/')
        );
        let metadata: ProviderMetadata = http
            .get(&metadata_url)
            .send()
            .await
            .map_err(|e| format!("fetch {metadata_url}: {e}"))?
            .error_for_status()
            .map_err(|e| format!("openid-configuration: {e}"))?
            .json()
            .await
            .map_err(|e| format!("parse openid-configuration: {e}"))?;

        let config = Self {
            issuer,
            audience,
            resource,
            jwks_uri: metadata.jwks_uri,
            keys: Arc::new(RwLock::new(HashMap::new())),
            http,
        };
        config.refresh_keys().await?;

        Ok(Some(config))
    }

    async fn refresh_keys(&self) -> Result<(), String> {
        let jwks: Jwks = self
            .http
            .get(&self.jwks_uri)
            .send()
            .await
            .map_err(|e| format!("fetch {}: {e}", self.jwks_uri))?
            .error_for_status()
            .map_err(|e| format!("jwks: {e}"))?
            .json()
            .await
            .map_err(|e| format!("parse jwks: {e}"))?;

        let mut new_keys = HashMap::new();
        for jwk in jwks.keys {
            if jwk.kty != "RSA" {
                continue;
            }
            let (Some(n), Some(e)) = (jwk.n.as_deref(), jwk.e.as_deref()) else {
                continue;
            };
            match DecodingKey::from_rsa_components(n, e) {
                Ok(key) => {
                    new_keys.insert(jwk.kid, key);
                }
                Err(err) => tracing::warn!(error = %err, "invalid jwk; skipping"),
            }
        }

        if new_keys.is_empty() {
            return Err("jwks contains no usable RSA keys".to_string());
        }

        *self.keys.write().await = new_keys;
        Ok(())
    }

    async fn key_for_kid(&self, kid: &str) -> Option<DecodingKey> {
        if let Some(k) = self.keys.read().await.get(kid).cloned() {
            return Some(k);
        }
        if let Err(err) = self.refresh_keys().await {
            tracing::warn!(error = %err, "jwks refresh failed");
            return None;
        }
        self.keys.read().await.get(kid).cloned()
    }

    fn validation(&self) -> Validation {
        let mut v = Validation::new(Algorithm::RS256);
        v.set_audience(&[&self.audience]);
        v.set_issuer(&[&self.issuer]);
        v.validate_exp = true;
        v
    }

    async fn verify(&self, token: &str) -> Result<Claims, &'static str> {
        let header = decode_header(token).map_err(|_| "invalid_token")?;
        let kid = header.kid.ok_or("invalid_token")?;
        let key = self.key_for_kid(&kid).await.ok_or("invalid_token")?;
        decode::<Claims>(token, &key, &self.validation())
            .map(|d| d.claims)
            .map_err(|err| {
                tracing::debug!(error = %err, "token decode failed");
                "invalid_token"
            })
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

    match config.verify(&token).await {
        Ok(claims) => {
            req.extensions_mut().insert(claims);
            next.run(req).await
        }
        Err(err) => unauthorized(&config, err).into_response(),
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
