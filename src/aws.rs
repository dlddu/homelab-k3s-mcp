use std::collections::BTreeMap;
use std::fmt;
use std::fmt::Write as _;
use std::time::Duration;

use async_trait::async_trait;
use chrono::{DateTime, Utc};
use sha2::{Digest, Sha256};

const DEFAULT_REGION: &str = "us-east-1";
const STS_API_VERSION: &str = "2011-06-15";
const DEFAULT_ROLE_SESSION_NAME: &str = "homelab-k3s-mcp";
const SIGV4_ALGORITHM: &str = "AWS4-HMAC-SHA256";
// SHA-256 of an empty byte string; the payload hash for our bodyless GET requests.
const EMPTY_PAYLOAD_SHA256: &str =
    "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855";

#[derive(Debug)]
pub enum AwsError {
    Unavailable(String),
    Api(String),
}

impl fmt::Display for AwsError {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        match self {
            AwsError::Unavailable(msg) => write!(f, "aws config unavailable: {msg}"),
            AwsError::Api(msg) => write!(f, "aws api error: {msg}"),
        }
    }
}

impl std::error::Error for AwsError {}

#[derive(Debug, Clone)]
pub struct AwsConfigFile {
    pub bucket: String,
    pub key: String,
    pub content_type: Option<String>,
    pub body: String,
}

#[async_trait]
pub trait AwsConfigService: Send + Sync {
    /// Fetch the preconfigured AWS config object from S3 using credentials
    /// obtained by assuming the configured IAM role.
    async fn get_config_file(&self) -> Result<AwsConfigFile, AwsError>;
}

pub struct UnavailableAwsConfig {
    reason: String,
}

impl UnavailableAwsConfig {
    pub fn new(reason: impl Into<String>) -> Self {
        Self {
            reason: reason.into(),
        }
    }
}

impl Default for UnavailableAwsConfig {
    fn default() -> Self {
        Self::new("aws config credentials are not configured")
    }
}

#[async_trait]
impl AwsConfigService for UnavailableAwsConfig {
    async fn get_config_file(&self) -> Result<AwsConfigFile, AwsError> {
        Err(AwsError::Unavailable(self.reason.clone()))
    }
}

struct BaseCredentials {
    access_key_id: String,
    secret_access_key: String,
    session_token: Option<String>,
}

struct TempCredentials {
    access_key_id: String,
    secret_access_key: String,
    session_token: String,
}

pub struct AwsConfigClient {
    bucket: String,
    key: String,
    role_arn: String,
    role_session_name: String,
    region: String,
    base: BaseCredentials,
    http: reqwest::Client,
}

impl AwsConfigClient {
    pub fn from_env() -> Result<Option<Self>, String> {
        let bucket = match std::env::var("AWS_CONFIG_S3_BUCKET") {
            Ok(v) if !v.is_empty() => v,
            _ => return Ok(None),
        };

        let key = require_env("AWS_CONFIG_S3_KEY")?;
        let role_arn = require_env("AWS_CONFIG_ROLE_ARN")?;
        let access_key_id = require_env("AWS_ACCESS_KEY_ID")?;
        let secret_access_key = require_env("AWS_SECRET_ACCESS_KEY")?;
        let session_token = std::env::var("AWS_SESSION_TOKEN")
            .ok()
            .filter(|s| !s.is_empty());

        let region = std::env::var("AWS_REGION")
            .ok()
            .filter(|s| !s.is_empty())
            .or_else(|| {
                std::env::var("AWS_DEFAULT_REGION")
                    .ok()
                    .filter(|s| !s.is_empty())
            })
            .unwrap_or_else(|| DEFAULT_REGION.to_string());

        let role_session_name = std::env::var("AWS_CONFIG_ROLE_SESSION_NAME")
            .ok()
            .filter(|s| !s.is_empty())
            .unwrap_or_else(|| DEFAULT_ROLE_SESSION_NAME.to_string());

        let http = reqwest::Client::builder()
            .timeout(Duration::from_secs(10))
            .build()
            .map_err(|e| format!("build http client: {e}"))?;

        Ok(Some(Self {
            bucket,
            key,
            role_arn,
            role_session_name,
            region,
            base: BaseCredentials {
                access_key_id,
                secret_access_key,
                session_token,
            },
            http,
        }))
    }

    async fn assume_role(&self) -> Result<TempCredentials, AwsError> {
        let host = format!("sts.{}.amazonaws.com", self.region);

        let mut params = [
            ("Action", "AssumeRole"),
            ("RoleArn", self.role_arn.as_str()),
            ("RoleSessionName", self.role_session_name.as_str()),
            ("Version", STS_API_VERSION),
        ];
        params.sort_by(|a, b| a.0.cmp(b.0));
        let canonical_query = params
            .iter()
            .map(|(k, v)| format!("{}={}", uri_encode(k, true), uri_encode(v, true)))
            .collect::<Vec<_>>()
            .join("&");

        let headers = build_signed_headers(
            "GET",
            &host,
            "/",
            &canonical_query,
            EMPTY_PAYLOAD_SHA256,
            &self.region,
            "sts",
            &self.base.access_key_id,
            &self.base.secret_access_key,
            self.base.session_token.as_deref(),
            &[],
            Utc::now(),
        );

        let url = format!("https://{host}/?{canonical_query}");
        let mut request = self.http.get(&url);
        for (name, value) in &headers {
            request = request.header(name, value);
        }

        let response = request
            .send()
            .await
            .map_err(|e| AwsError::Api(format!("sts assume-role request: {e}")))?;
        let status = response.status();
        let text = response
            .text()
            .await
            .map_err(|e| AwsError::Api(format!("read sts response: {e}")))?;
        if !status.is_success() {
            return Err(AwsError::Api(format!(
                "sts assume-role returned {status}: {}",
                text.trim()
            )));
        }

        let access_key_id = extract_xml_tag(&text, "AccessKeyId")
            .ok_or_else(|| AwsError::Api("sts response missing AccessKeyId".to_string()))?;
        let secret_access_key = extract_xml_tag(&text, "SecretAccessKey")
            .ok_or_else(|| AwsError::Api("sts response missing SecretAccessKey".to_string()))?;
        let session_token = extract_xml_tag(&text, "SessionToken")
            .ok_or_else(|| AwsError::Api("sts response missing SessionToken".to_string()))?;

        Ok(TempCredentials {
            access_key_id,
            secret_access_key,
            session_token,
        })
    }

    async fn get_object(&self, creds: &TempCredentials) -> Result<AwsConfigFile, AwsError> {
        let host = format!("{}.s3.{}.amazonaws.com", self.bucket, self.region);
        let canonical_uri = canonical_s3_key_path(&self.key);
        let extra = [(
            "x-amz-content-sha256".to_string(),
            EMPTY_PAYLOAD_SHA256.to_string(),
        )];

        let headers = build_signed_headers(
            "GET",
            &host,
            &canonical_uri,
            "",
            EMPTY_PAYLOAD_SHA256,
            &self.region,
            "s3",
            &creds.access_key_id,
            &creds.secret_access_key,
            Some(&creds.session_token),
            &extra,
            Utc::now(),
        );

        let url = format!("https://{host}{canonical_uri}");
        let mut request = self.http.get(&url);
        for (name, value) in &headers {
            request = request.header(name, value);
        }

        let response = request
            .send()
            .await
            .map_err(|e| AwsError::Api(format!("s3 get-object request: {e}")))?;
        let status = response.status();
        let content_type = response
            .headers()
            .get(reqwest::header::CONTENT_TYPE)
            .and_then(|v| v.to_str().ok())
            .map(str::to_string);
        let body = response
            .text()
            .await
            .map_err(|e| AwsError::Api(format!("read s3 object body: {e}")))?;
        if !status.is_success() {
            return Err(AwsError::Api(format!(
                "s3 get-object returned {status}: {}",
                body.trim()
            )));
        }

        Ok(AwsConfigFile {
            bucket: self.bucket.clone(),
            key: self.key.clone(),
            content_type,
            body,
        })
    }
}

#[async_trait]
impl AwsConfigService for AwsConfigClient {
    async fn get_config_file(&self) -> Result<AwsConfigFile, AwsError> {
        let creds = self.assume_role().await?;
        self.get_object(&creds).await
    }
}

fn require_env(name: &str) -> Result<String, String> {
    std::env::var(name)
        .ok()
        .filter(|s| !s.is_empty())
        .ok_or_else(|| format!("{name} is required when AWS_CONFIG_S3_BUCKET is set"))
}

/// Build the AWS canonical URI for an S3 object key: a leading slash followed
/// by the URI-encoded key, preserving `/` separators (per the S3 signing rules).
fn canonical_s3_key_path(key: &str) -> String {
    let trimmed = key.strip_prefix('/').unwrap_or(key);
    format!("/{}", uri_encode(trimmed, false))
}

/// Headers to attach to the outgoing request, including the SigV4
/// `Authorization` header. `Host` is intentionally omitted because the HTTP
/// client sets it from the request URL.
#[allow(clippy::too_many_arguments)]
fn build_signed_headers(
    method: &str,
    host: &str,
    canonical_uri: &str,
    canonical_query: &str,
    payload_hash: &str,
    region: &str,
    service: &str,
    access_key: &str,
    secret_key: &str,
    session_token: Option<&str>,
    extra_headers: &[(String, String)],
    now: DateTime<Utc>,
) -> Vec<(String, String)> {
    let amz_date = now.format("%Y%m%dT%H%M%SZ").to_string();
    let date_stamp = now.format("%Y%m%d").to_string();

    let mut signed: BTreeMap<String, String> = BTreeMap::new();
    signed.insert("host".to_string(), host.to_string());
    signed.insert("x-amz-date".to_string(), amz_date.clone());
    for (name, value) in extra_headers {
        signed.insert(name.to_lowercase(), value.clone());
    }
    if let Some(token) = session_token {
        signed.insert("x-amz-security-token".to_string(), token.to_string());
    }

    let authorization = sigv4_authorization(
        method,
        canonical_uri,
        canonical_query,
        &signed,
        payload_hash,
        access_key,
        secret_key,
        region,
        service,
        &amz_date,
        &date_stamp,
    );

    let mut out = Vec::with_capacity(signed.len() + 1);
    out.push(("x-amz-date".to_string(), amz_date));
    if let Some(token) = session_token {
        out.push(("x-amz-security-token".to_string(), token.to_string()));
    }
    for (name, value) in extra_headers {
        out.push((name.clone(), value.clone()));
    }
    out.push(("authorization".to_string(), authorization));
    out
}

/// Compute the SigV4 `Authorization` header value. `signed_headers` must
/// contain every header to be signed (lowercased name -> value) and is already
/// ordered by the `BTreeMap`.
#[allow(clippy::too_many_arguments)]
fn sigv4_authorization(
    method: &str,
    canonical_uri: &str,
    canonical_query: &str,
    signed_headers: &BTreeMap<String, String>,
    payload_hash: &str,
    access_key: &str,
    secret_key: &str,
    region: &str,
    service: &str,
    amz_date: &str,
    date_stamp: &str,
) -> String {
    let mut canonical_headers = String::new();
    for (name, value) in signed_headers {
        let _ = writeln!(canonical_headers, "{name}:{}", value.trim());
    }
    let signed_header_names = signed_headers.keys().cloned().collect::<Vec<_>>().join(";");

    let canonical_request = format!(
        "{method}\n{canonical_uri}\n{canonical_query}\n{canonical_headers}\n{signed_header_names}\n{payload_hash}"
    );

    let scope = format!("{date_stamp}/{region}/{service}/aws4_request");
    let string_to_sign = format!(
        "{SIGV4_ALGORITHM}\n{amz_date}\n{scope}\n{}",
        hex_sha256(canonical_request.as_bytes())
    );

    let signing_key = derive_signing_key(secret_key, date_stamp, region, service);
    let signature = to_hex(&hmac_sha256(&signing_key, string_to_sign.as_bytes()));

    format!(
        "{SIGV4_ALGORITHM} Credential={access_key}/{scope}, SignedHeaders={signed_header_names}, Signature={signature}"
    )
}

fn derive_signing_key(secret_key: &str, date_stamp: &str, region: &str, service: &str) -> [u8; 32] {
    let k_date = hmac_sha256(
        format!("AWS4{secret_key}").as_bytes(),
        date_stamp.as_bytes(),
    );
    let k_region = hmac_sha256(&k_date, region.as_bytes());
    let k_service = hmac_sha256(&k_region, service.as_bytes());
    hmac_sha256(&k_service, b"aws4_request")
}

/// HMAC-SHA256, implemented directly on top of `sha2` to avoid pulling in an
/// extra dependency.
fn hmac_sha256(key: &[u8], message: &[u8]) -> [u8; 32] {
    const BLOCK_SIZE: usize = 64;

    let mut block = [0u8; BLOCK_SIZE];
    if key.len() > BLOCK_SIZE {
        block[..32].copy_from_slice(&Sha256::digest(key));
    } else {
        block[..key.len()].copy_from_slice(key);
    }

    let mut inner = [0x36u8; BLOCK_SIZE];
    let mut outer = [0x5cu8; BLOCK_SIZE];
    for i in 0..BLOCK_SIZE {
        inner[i] ^= block[i];
        outer[i] ^= block[i];
    }

    let mut hasher = Sha256::new();
    hasher.update(inner);
    hasher.update(message);
    let inner_digest = hasher.finalize();

    let mut hasher = Sha256::new();
    hasher.update(outer);
    hasher.update(inner_digest);

    let mut out = [0u8; 32];
    out.copy_from_slice(&hasher.finalize());
    out
}

fn hex_sha256(data: &[u8]) -> String {
    to_hex(&Sha256::digest(data))
}

fn to_hex(bytes: &[u8]) -> String {
    let mut out = String::with_capacity(bytes.len() * 2);
    for byte in bytes {
        let _ = write!(out, "{byte:02x}");
    }
    out
}

/// AWS-flavoured percent-encoding: every byte except the unreserved set is
/// encoded. `/` is left untouched when `encode_slash` is false (used for S3
/// object key paths).
fn uri_encode(input: &str, encode_slash: bool) -> String {
    let mut out = String::with_capacity(input.len());
    for &byte in input.as_bytes() {
        match byte {
            b'A'..=b'Z' | b'a'..=b'z' | b'0'..=b'9' | b'-' | b'_' | b'.' | b'~' => {
                out.push(byte as char);
            }
            b'/' if !encode_slash => out.push('/'),
            _ => {
                let _ = write!(out, "%{byte:02X}");
            }
        }
    }
    out
}

fn extract_xml_tag(xml: &str, tag: &str) -> Option<String> {
    let open = format!("<{tag}>");
    let close = format!("</{tag}>");
    let start = xml.find(&open)? + open.len();
    let rest = &xml[start..];
    let end = rest.find(&close)?;
    Some(rest[..end].to_string())
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn uri_encode_preserves_unreserved_and_encodes_the_rest() {
        assert_eq!(uri_encode("abcABC123-_.~", true), "abcABC123-_.~");
        assert_eq!(uri_encode("a b/c", true), "a%20b%2Fc");
        assert_eq!(uri_encode("a b/c", false), "a%20b/c");
        assert_eq!(
            uri_encode("arn:aws:iam::123456789012:role/demo", true),
            "arn%3Aaws%3Aiam%3A%3A123456789012%3Arole%2Fdemo"
        );
    }

    #[test]
    fn canonical_s3_key_path_encodes_and_normalises_leading_slash() {
        assert_eq!(canonical_s3_key_path("config/app.conf"), "/config/app.conf");
        assert_eq!(
            canonical_s3_key_path("/config/app.conf"),
            "/config/app.conf"
        );
        assert_eq!(canonical_s3_key_path("a b.conf"), "/a%20b.conf");
    }

    #[test]
    fn extract_xml_tag_reads_first_match() {
        let xml = "<Credentials><AccessKeyId>AKIA</AccessKeyId>\
                   <SecretAccessKey>secret</SecretAccessKey></Credentials>";
        assert_eq!(extract_xml_tag(xml, "AccessKeyId").as_deref(), Some("AKIA"));
        assert_eq!(
            extract_xml_tag(xml, "SecretAccessKey").as_deref(),
            Some("secret")
        );
        assert_eq!(extract_xml_tag(xml, "SessionToken"), None);
    }

    // Derived from the published AWS SigV4 test suite (`get-vanilla`).
    #[test]
    fn sigv4_authorization_matches_aws_get_vanilla_vector() {
        let mut signed = BTreeMap::new();
        signed.insert("host".to_string(), "example.amazonaws.com".to_string());
        signed.insert("x-amz-date".to_string(), "20150830T123600Z".to_string());

        let authorization = sigv4_authorization(
            "GET",
            "/",
            "",
            &signed,
            EMPTY_PAYLOAD_SHA256,
            "AKIDEXAMPLE",
            "wJalrXUtnFEMI/K7MDENG+bPxRfiCYEXAMPLEKEY",
            "us-east-1",
            "service",
            "20150830T123600Z",
            "20150830",
        );

        assert_eq!(
            authorization,
            "AWS4-HMAC-SHA256 Credential=AKIDEXAMPLE/20150830/us-east-1/service/aws4_request, \
             SignedHeaders=host;x-amz-date, \
             Signature=5fa00fa31553b73ebf1942676e86291e8372ff2a2260956d9b8aae1d763fbf31"
        );
    }

    // Published AWS signing-key derivation example (service `iam`).
    #[test]
    fn derive_signing_key_matches_aws_example() {
        let key = derive_signing_key(
            "wJalrXUtnFEMI/K7MDENG+bPxRfiCYEXAMPLEKEY",
            "20120215",
            "us-east-1",
            "iam",
        );
        assert_eq!(
            to_hex(&key),
            "f4780e2d9f65fa895f9c67b32ce1baf0b0d8a43505a000a1a9e090d414db404d"
        );
    }

    #[test]
    fn hmac_sha256_matches_rfc4231_case() {
        // RFC 4231 test case 1: key = 0x0b * 20, data = "Hi There".
        let key = [0x0bu8; 20];
        let mac = hmac_sha256(&key, b"Hi There");
        assert_eq!(
            to_hex(&mac),
            "b0344c61d8db38535ca8afceaf0bf12b881dc200c9833da726e9376c2e32cff7"
        );
    }
}
