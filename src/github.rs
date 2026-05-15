use std::fmt;
use std::time::Duration;

use async_trait::async_trait;
use jsonwebtoken::{encode, Algorithm, EncodingKey, Header};
use serde::{Deserialize, Serialize};
use serde_json::{json, Value};

const DEFAULT_API_BASE: &str = "https://api.github.com";
const GITHUB_API_VERSION: &str = "2022-11-28";
const JWT_TTL_SECS: i64 = 540;
const JWT_CLOCK_SKEW_SECS: i64 = 60;

#[derive(Debug)]
pub enum GitHubError {
    Unavailable(String),
    Api(String),
}

impl fmt::Display for GitHubError {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        match self {
            GitHubError::Unavailable(msg) => write!(f, "github app unavailable: {msg}"),
            GitHubError::Api(msg) => write!(f, "github api error: {msg}"),
        }
    }
}

impl std::error::Error for GitHubError {}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct InstallationToken {
    pub token: String,
    pub expires_at: String,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub permissions: Option<Value>,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub repository_selection: Option<String>,
}

#[async_trait]
pub trait GitHubAppService: Send + Sync {
    async fn create_installation_token(
        &self,
        repositories: Option<Vec<String>>,
        permissions: Option<Value>,
    ) -> Result<InstallationToken, GitHubError>;
}

pub struct UnavailableGitHubApp {
    reason: String,
}

impl UnavailableGitHubApp {
    pub fn new(reason: impl Into<String>) -> Self {
        Self {
            reason: reason.into(),
        }
    }
}

impl Default for UnavailableGitHubApp {
    fn default() -> Self {
        Self::new("github app credentials are not configured")
    }
}

#[async_trait]
impl GitHubAppService for UnavailableGitHubApp {
    async fn create_installation_token(
        &self,
        _repositories: Option<Vec<String>>,
        _permissions: Option<Value>,
    ) -> Result<InstallationToken, GitHubError> {
        Err(GitHubError::Unavailable(self.reason.clone()))
    }
}

#[derive(Serialize)]
struct AppClaims<'a> {
    iat: i64,
    exp: i64,
    iss: &'a str,
}

pub struct GitHubAppClient {
    app_id: String,
    installation_id: i64,
    encoding_key: EncodingKey,
    api_base: String,
    user_agent: String,
    http: reqwest::Client,
}

impl GitHubAppClient {
    pub fn from_env() -> Result<Option<Self>, String> {
        let app_id = match std::env::var("GITHUB_APP_ID") {
            Ok(v) if !v.is_empty() => v,
            _ => return Ok(None),
        };

        let installation_id = std::env::var("GITHUB_APP_INSTALLATION_ID")
            .ok()
            .filter(|s| !s.is_empty())
            .ok_or_else(|| {
                "GITHUB_APP_INSTALLATION_ID is required when GITHUB_APP_ID is set".to_string()
            })?
            .parse::<i64>()
            .map_err(|e| format!("parse GITHUB_APP_INSTALLATION_ID: {e}"))?;

        let pem = std::env::var("GITHUB_APP_PRIVATE_KEY")
            .ok()
            .filter(|s| !s.is_empty())
            .ok_or_else(|| {
                "GITHUB_APP_PRIVATE_KEY is required when GITHUB_APP_ID is set".to_string()
            })?;

        let encoding_key = EncodingKey::from_rsa_pem(pem.as_bytes())
            .map_err(|e| format!("parse github app private key: {e}"))?;

        let api_base = std::env::var("GITHUB_API_BASE_URL")
            .ok()
            .filter(|s| !s.is_empty())
            .unwrap_or_else(|| DEFAULT_API_BASE.to_string());

        let user_agent = format!("{}/{}", env!("CARGO_PKG_NAME"), env!("CARGO_PKG_VERSION"));

        let http = reqwest::Client::builder()
            .timeout(Duration::from_secs(10))
            .build()
            .map_err(|e| format!("build http client: {e}"))?;

        Ok(Some(Self {
            app_id,
            installation_id,
            encoding_key,
            api_base: api_base.trim_end_matches('/').to_string(),
            user_agent,
            http,
        }))
    }

    fn app_jwt(&self) -> Result<String, GitHubError> {
        let now = chrono::Utc::now().timestamp();
        let claims = AppClaims {
            iat: now - JWT_CLOCK_SKEW_SECS,
            exp: now + JWT_TTL_SECS,
            iss: &self.app_id,
        };
        encode(&Header::new(Algorithm::RS256), &claims, &self.encoding_key)
            .map_err(|e| GitHubError::Api(format!("sign app jwt: {e}")))
    }
}

#[async_trait]
impl GitHubAppService for GitHubAppClient {
    async fn create_installation_token(
        &self,
        repositories: Option<Vec<String>>,
        permissions: Option<Value>,
    ) -> Result<InstallationToken, GitHubError> {
        let jwt = self.app_jwt()?;
        let url = format!(
            "{}/app/installations/{}/access_tokens",
            self.api_base, self.installation_id
        );

        let mut body = serde_json::Map::new();
        if let Some(repos) = repositories {
            body.insert("repositories".to_string(), json!(repos));
        }
        if let Some(perms) = permissions {
            body.insert("permissions".to_string(), perms);
        }

        let response = self
            .http
            .post(&url)
            .bearer_auth(jwt)
            .header(reqwest::header::ACCEPT, "application/vnd.github+json")
            .header("X-GitHub-Api-Version", GITHUB_API_VERSION)
            .header(reqwest::header::USER_AGENT, &self.user_agent)
            .json(&Value::Object(body))
            .send()
            .await
            .map_err(|e| GitHubError::Api(format!("post {url}: {e}")))?;

        let status = response.status();
        if !status.is_success() {
            let text = response.text().await.unwrap_or_default();
            return Err(GitHubError::Api(format!("{url} returned {status}: {text}")));
        }

        response
            .json::<InstallationToken>()
            .await
            .map_err(|e| GitHubError::Api(format!("parse installation token: {e}")))
    }
}
