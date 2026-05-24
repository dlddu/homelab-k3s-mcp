use std::fmt;

use async_trait::async_trait;
use aws_sdk_s3::error::DisplayErrorContext;
use aws_sdk_s3::primitives::DateTimeFormat;
use serde::Serialize;

const DEFAULT_SESSION_NAME: &str = env!("CARGO_PKG_NAME");

#[derive(Debug)]
pub enum AwsError {
    Unavailable(String),
    Api(String),
}

impl fmt::Display for AwsError {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        match self {
            AwsError::Unavailable(msg) => write!(f, "aws config unavailable: {msg}"),
            AwsError::Api(msg) => write!(f, "aws s3 error: {msg}"),
        }
    }
}

impl std::error::Error for AwsError {}

#[derive(Debug, Clone, Serialize)]
pub struct ConfigObject {
    pub bucket: String,
    pub key: String,
    pub content: String,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub content_type: Option<String>,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub content_length: Option<i64>,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub etag: Option<String>,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub last_modified: Option<String>,
}

#[async_trait]
pub trait AwsConfigService: Send + Sync {
    /// Fetch the configured S3 object (fixed bucket/key) and return its body
    /// as text along with object metadata.
    async fn fetch_config(&self) -> Result<ConfigObject, AwsError>;
}

pub struct UnavailableAws {
    reason: String,
}

impl UnavailableAws {
    pub fn new(reason: impl Into<String>) -> Self {
        Self {
            reason: reason.into(),
        }
    }
}

impl Default for UnavailableAws {
    fn default() -> Self {
        Self::new("aws config integration is not configured")
    }
}

#[async_trait]
impl AwsConfigService for UnavailableAws {
    async fn fetch_config(&self) -> Result<ConfigObject, AwsError> {
        Err(AwsError::Unavailable(self.reason.clone()))
    }
}

pub struct S3ConfigClient {
    client: aws_sdk_s3::Client,
    bucket: String,
    key: String,
}

impl S3ConfigClient {
    pub async fn from_env() -> Result<Option<Self>, String> {
        let bucket = match std::env::var("AWS_CONFIG_S3_BUCKET") {
            Ok(v) if !v.is_empty() => v,
            _ => return Ok(None),
        };

        let key = std::env::var("AWS_CONFIG_S3_KEY")
            .ok()
            .filter(|s| !s.is_empty())
            .ok_or_else(|| {
                "AWS_CONFIG_S3_KEY is required when AWS_CONFIG_S3_BUCKET is set".to_string()
            })?;

        let role_arn = std::env::var("AWS_CONFIG_ASSUME_ROLE_ARN")
            .ok()
            .filter(|s| !s.is_empty())
            .ok_or_else(|| {
                "AWS_CONFIG_ASSUME_ROLE_ARN is required when AWS_CONFIG_S3_BUCKET is set"
                    .to_string()
            })?;

        let session_name = std::env::var("AWS_CONFIG_ASSUME_ROLE_SESSION_NAME")
            .ok()
            .filter(|s| !s.is_empty())
            .unwrap_or_else(|| DEFAULT_SESSION_NAME.to_string());

        let external_id = std::env::var("AWS_CONFIG_ASSUME_ROLE_EXTERNAL_ID")
            .ok()
            .filter(|s| !s.is_empty());

        let region = std::env::var("AWS_REGION")
            .ok()
            .filter(|s| !s.is_empty())
            .or_else(|| {
                std::env::var("AWS_DEFAULT_REGION")
                    .ok()
                    .filter(|s| !s.is_empty())
            });

        // Build the HTTPS connector on rustls + ring, matching the rest of the
        // project (kube) and keeping aws-lc-rs (which needs cmake at build
        // time) out of the dependency tree.
        let http_client = aws_smithy_http_client::Builder::new()
            .tls_provider(aws_smithy_http_client::tls::Provider::Rustls(
                aws_smithy_http_client::tls::rustls_provider::CryptoMode::Ring,
            ))
            .build_https();

        let mut loader =
            aws_config::defaults(aws_config::BehaviorVersion::latest()).http_client(http_client);
        if let Some(region) = region {
            loader = loader.region(aws_config::Region::new(region));
        }
        // Base credentials resolve through the default chain, which on the
        // homelab nodes lands on the EC2 instance profile (IMDS).
        let base = loader.load().await;

        let mut role = aws_config::sts::AssumeRoleProvider::builder(role_arn)
            .session_name(session_name)
            .configure(&base);
        if let Some(external_id) = external_id {
            role = role.external_id(external_id);
        }
        let credentials = role.build().await;

        let s3_config = aws_sdk_s3::config::Builder::from(&base)
            .credentials_provider(credentials)
            .build();

        Ok(Some(Self {
            client: aws_sdk_s3::Client::from_conf(s3_config),
            bucket,
            key,
        }))
    }
}

#[async_trait]
impl AwsConfigService for S3ConfigClient {
    async fn fetch_config(&self) -> Result<ConfigObject, AwsError> {
        let resp = self
            .client
            .get_object()
            .bucket(&self.bucket)
            .key(&self.key)
            .send()
            .await
            .map_err(|e| {
                AwsError::Api(format!(
                    "get_object s3://{}/{}: {}",
                    self.bucket,
                    self.key,
                    DisplayErrorContext(&e)
                ))
            })?;

        let content_type = resp.content_type().map(str::to_string);
        let content_length = resp.content_length();
        let etag = resp.e_tag().map(str::to_string);
        let last_modified = resp
            .last_modified()
            .and_then(|t| t.fmt(DateTimeFormat::DateTime).ok());

        let bytes = resp.body.collect().await.map_err(|e| {
            AwsError::Api(format!("read body s3://{}/{}: {e}", self.bucket, self.key))
        })?;
        let content = String::from_utf8_lossy(&bytes.into_bytes()).into_owned();

        Ok(ConfigObject {
            bucket: self.bucket.clone(),
            key: self.key.clone(),
            content,
            content_type,
            content_length,
            etag,
            last_modified,
        })
    }
}
