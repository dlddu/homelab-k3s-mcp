use std::fmt;
use std::time::Duration;

use async_trait::async_trait;
use chrono::SecondsFormat;
use serde::{Deserialize, Serialize};
use serde_json::json;

const DEFAULT_API_BASE: &str = "https://www.grafana.com/api";
const TOKEN_TTL_HOURS: i64 = 1;

#[derive(Debug)]
pub enum GrafanaError {
    Unavailable(String),
    Api(String),
}

impl fmt::Display for GrafanaError {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        match self {
            GrafanaError::Unavailable(msg) => write!(f, "grafana cloud unavailable: {msg}"),
            GrafanaError::Api(msg) => write!(f, "grafana cloud api error: {msg}"),
        }
    }
}

impl std::error::Error for GrafanaError {}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct GrafanaCloudToken {
    pub token: String,
    #[serde(rename = "expiresAt")]
    pub expires_at: String,
    #[serde(default, rename = "accessPolicyId")]
    pub access_policy_id: String,
}

#[async_trait]
pub trait GrafanaCloudService: Send + Sync {
    async fn create_short_lived_token(&self) -> Result<GrafanaCloudToken, GrafanaError>;
}

pub struct UnavailableGrafanaCloud {
    reason: String,
}

impl UnavailableGrafanaCloud {
    pub fn new(reason: impl Into<String>) -> Self {
        Self {
            reason: reason.into(),
        }
    }
}

impl Default for UnavailableGrafanaCloud {
    fn default() -> Self {
        Self::new("grafana cloud credentials are not configured")
    }
}

#[async_trait]
impl GrafanaCloudService for UnavailableGrafanaCloud {
    async fn create_short_lived_token(&self) -> Result<GrafanaCloudToken, GrafanaError> {
        Err(GrafanaError::Unavailable(self.reason.clone()))
    }
}

pub struct GrafanaCloudClient {
    management_token: String,
    access_policy_id: String,
    region: String,
    api_base: String,
    user_agent: String,
    http: reqwest::Client,
}

impl GrafanaCloudClient {
    pub fn from_env() -> Result<Option<Self>, String> {
        let management_token = match std::env::var("GRAFANA_CLOUD_ACCESS_POLICY_TOKEN") {
            Ok(v) if !v.is_empty() => v,
            _ => return Ok(None),
        };

        let access_policy_id = std::env::var("GRAFANA_CLOUD_ACCESS_POLICY_ID")
            .ok()
            .filter(|s| !s.is_empty())
            .ok_or_else(|| {
                "GRAFANA_CLOUD_ACCESS_POLICY_ID is required when \
                 GRAFANA_CLOUD_ACCESS_POLICY_TOKEN is set"
                    .to_string()
            })?;

        let region = std::env::var("GRAFANA_CLOUD_REGION")
            .ok()
            .filter(|s| !s.is_empty())
            .ok_or_else(|| {
                "GRAFANA_CLOUD_REGION is required when GRAFANA_CLOUD_ACCESS_POLICY_TOKEN is set"
                    .to_string()
            })?;

        let api_base = std::env::var("GRAFANA_API_BASE_URL")
            .ok()
            .filter(|s| !s.is_empty())
            .unwrap_or_else(|| DEFAULT_API_BASE.to_string());

        let user_agent = format!("{}/{}", env!("CARGO_PKG_NAME"), env!("CARGO_PKG_VERSION"));

        let http = reqwest::Client::builder()
            .timeout(Duration::from_secs(10))
            .build()
            .map_err(|e| format!("build http client: {e}"))?;

        Ok(Some(Self {
            management_token,
            access_policy_id,
            region,
            api_base: api_base.trim_end_matches('/').to_string(),
            user_agent,
            http,
        }))
    }
}

#[async_trait]
impl GrafanaCloudService for GrafanaCloudClient {
    async fn create_short_lived_token(&self) -> Result<GrafanaCloudToken, GrafanaError> {
        let now = chrono::Utc::now();
        let expires_at = (now + chrono::Duration::hours(TOKEN_TTL_HOURS))
            .to_rfc3339_opts(SecondsFormat::Secs, true);
        // Grafana requires token names to be unique within (org, region); the
        // millisecond timestamp keeps repeated mints from colliding.
        let name = format!("{}-{}", env!("CARGO_PKG_NAME"), now.timestamp_millis());

        let url = format!("{}/v1/tokens?region={}", self.api_base, self.region);
        let body = json!({
            "accessPolicyId": self.access_policy_id,
            "name": name,
            "displayName": name,
            "expiresAt": expires_at,
        });

        let response = self
            .http
            .post(&url)
            .bearer_auth(&self.management_token)
            .header(reqwest::header::ACCEPT, "application/json")
            .header(reqwest::header::USER_AGENT, &self.user_agent)
            .json(&body)
            .send()
            .await
            .map_err(|e| GrafanaError::Api(format!("post {url}: {e}")))?;

        let status = response.status();
        if !status.is_success() {
            let text = response.text().await.unwrap_or_default();
            return Err(GrafanaError::Api(format!(
                "{url} returned {status}: {text}"
            )));
        }

        let mut token = response
            .json::<GrafanaCloudToken>()
            .await
            .map_err(|e| GrafanaError::Api(format!("parse grafana cloud token: {e}")))?;
        token.access_policy_id = self.access_policy_id.clone();
        Ok(token)
    }
}
