use std::fmt;

use async_trait::async_trait;
use k8s_openapi::api::apps::v1::{DaemonSet, Deployment, StatefulSet};
use k8s_openapi::api::core::v1::Pod;
use k8s_openapi::apimachinery::pkg::apis::meta::v1::LabelSelector;
use k8s_openapi::NamespaceResourceScope;
use kube::api::{Api, AttachParams, ListParams, LogParams, Patch, PatchParams};
use kube::{Client, Resource};
use serde::Serialize;
use serde_json::{json, Value};
use tokio::io::AsyncReadExt;

const RESTART_ANNOTATION: &str = "kubectl.kubernetes.io/restartedAt";
const FIELD_MANAGER: &str = "homelab-k3s-mcp";

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum WorkloadKind {
    Deployment,
    StatefulSet,
    DaemonSet,
}

impl WorkloadKind {
    pub fn as_str(self) -> &'static str {
        match self {
            WorkloadKind::Deployment => "Deployment",
            WorkloadKind::StatefulSet => "StatefulSet",
            WorkloadKind::DaemonSet => "DaemonSet",
        }
    }

    pub fn parse(s: &str) -> Option<Self> {
        match s {
            "Deployment" | "deployment" | "deploy" => Some(WorkloadKind::Deployment),
            "StatefulSet" | "statefulset" | "sts" => Some(WorkloadKind::StatefulSet),
            "DaemonSet" | "daemonset" | "ds" => Some(WorkloadKind::DaemonSet),
            _ => None,
        }
    }
}

#[derive(Debug)]
pub enum K8sError {
    Unavailable(String),
    Api(String),
}

impl fmt::Display for K8sError {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        match self {
            K8sError::Unavailable(msg) => write!(f, "kubernetes client unavailable: {msg}"),
            K8sError::Api(msg) => write!(f, "kubernetes api error: {msg}"),
        }
    }
}

impl std::error::Error for K8sError {}

impl From<kube::Error> for K8sError {
    fn from(err: kube::Error) -> Self {
        K8sError::Api(err.to_string())
    }
}

#[derive(Debug, Clone, Serialize)]
pub struct ExecOutcome {
    pub pod: String,
    pub stdout: String,
    pub stderr: String,
    pub exit_code: Option<i32>,
    pub success: bool,
}

#[derive(Debug, Clone, Default)]
pub struct LogOptions {
    pub container: Option<String>,
    pub tail_lines: Option<i64>,
    pub previous: bool,
    pub timestamps: bool,
    pub since_seconds: Option<i64>,
}

#[derive(Debug, Clone, Serialize)]
pub struct LogResult {
    pub pod: String,
    pub container: Option<String>,
    pub logs: String,
}

#[async_trait]
pub trait K8sService: Send + Sync {
    async fn list_workloads(
        &self,
        kind: WorkloadKind,
        namespace: Option<&str>,
    ) -> Result<Vec<Value>, K8sError>;

    async fn rollout_restart(
        &self,
        kind: WorkloadKind,
        namespace: &str,
        name: &str,
    ) -> Result<String, K8sError>;

    /// Run `command` inside the first Running pod matching `label_selector`
    /// in `namespace`. `container` is required when the pod has more than
    /// one container; pass `None` to default to the pod's only container.
    async fn exec_in_pod(
        &self,
        namespace: &str,
        label_selector: &str,
        container: Option<&str>,
        command: &[String],
    ) -> Result<ExecOutcome, K8sError>;

    async fn scale_workload(
        &self,
        kind: WorkloadKind,
        namespace: &str,
        name: &str,
        replicas: i32,
    ) -> Result<i32, K8sError>;

    /// Fetch container logs from a pod backing the given workload. Resolves
    /// the workload's pod selector and pulls logs from the first Running pod
    /// matching that selector (falling back to any matching pod if none is
    /// Running, so `previous=true` still works after a crash loop).
    async fn workload_logs(
        &self,
        kind: WorkloadKind,
        namespace: &str,
        name: &str,
        options: &LogOptions,
    ) -> Result<LogResult, K8sError>;
}

pub struct UnavailableK8s {
    reason: String,
}

impl UnavailableK8s {
    pub fn new(reason: impl Into<String>) -> Self {
        Self {
            reason: reason.into(),
        }
    }
}

impl Default for UnavailableK8s {
    fn default() -> Self {
        Self::new("kubernetes client is not configured")
    }
}

#[async_trait]
impl K8sService for UnavailableK8s {
    async fn list_workloads(
        &self,
        _kind: WorkloadKind,
        _namespace: Option<&str>,
    ) -> Result<Vec<Value>, K8sError> {
        Err(K8sError::Unavailable(self.reason.clone()))
    }

    async fn rollout_restart(
        &self,
        _kind: WorkloadKind,
        _namespace: &str,
        _name: &str,
    ) -> Result<String, K8sError> {
        Err(K8sError::Unavailable(self.reason.clone()))
    }

    async fn exec_in_pod(
        &self,
        _namespace: &str,
        _label_selector: &str,
        _container: Option<&str>,
        _command: &[String],
    ) -> Result<ExecOutcome, K8sError> {
        Err(K8sError::Unavailable(self.reason.clone()))
    }

    async fn scale_workload(
        &self,
        _kind: WorkloadKind,
        _namespace: &str,
        _name: &str,
        _replicas: i32,
    ) -> Result<i32, K8sError> {
        Err(K8sError::Unavailable(self.reason.clone()))
    }

    async fn workload_logs(
        &self,
        _kind: WorkloadKind,
        _namespace: &str,
        _name: &str,
        _options: &LogOptions,
    ) -> Result<LogResult, K8sError> {
        Err(K8sError::Unavailable(self.reason.clone()))
    }
}

pub struct KubeService {
    client: Client,
}

impl KubeService {
    pub async fn try_new() -> Result<Self, K8sError> {
        let client = Client::try_default()
            .await
            .map_err(|e| K8sError::Unavailable(format!("init kube client: {e}")))?;
        Ok(Self { client })
    }

    fn api<K>(&self, namespace: Option<&str>) -> Api<K>
    where
        K: Resource<Scope = NamespaceResourceScope>,
        <K as Resource>::DynamicType: Default,
    {
        match namespace {
            Some(ns) => Api::namespaced(self.client.clone(), ns),
            None => Api::all(self.client.clone()),
        }
    }

    async fn restart<K>(&self, namespace: &str, name: &str) -> Result<String, K8sError>
    where
        K: Resource<Scope = NamespaceResourceScope>
            + Clone
            + serde::de::DeserializeOwned
            + std::fmt::Debug,
        <K as Resource>::DynamicType: Default,
    {
        let api: Api<K> = Api::namespaced(self.client.clone(), namespace);
        let now = chrono::Utc::now().to_rfc3339_opts(chrono::SecondsFormat::Secs, true);
        let patch = json!({
            "spec": {
                "template": {
                    "metadata": {
                        "annotations": { RESTART_ANNOTATION: now }
                    }
                }
            }
        });
        let params = PatchParams {
            field_manager: Some(FIELD_MANAGER.into()),
            ..Default::default()
        };
        api.patch(name, &params, &Patch::Strategic(patch)).await?;
        Ok(now)
    }

    async fn workload_pod_selector(
        &self,
        kind: WorkloadKind,
        namespace: &str,
        name: &str,
    ) -> Result<String, K8sError> {
        let selector = match kind {
            WorkloadKind::Deployment => {
                let api: Api<Deployment> = Api::namespaced(self.client.clone(), namespace);
                api.get(name).await?.spec.map(|s| s.selector)
            }
            WorkloadKind::StatefulSet => {
                let api: Api<StatefulSet> = Api::namespaced(self.client.clone(), namespace);
                api.get(name).await?.spec.map(|s| s.selector)
            }
            WorkloadKind::DaemonSet => {
                let api: Api<DaemonSet> = Api::namespaced(self.client.clone(), namespace);
                api.get(name).await?.spec.map(|s| s.selector)
            }
        };
        selector
            .as_ref()
            .and_then(label_selector_to_string)
            .ok_or_else(|| {
                K8sError::Api(format!(
                    "{} {namespace}/{name} has no usable spec.selector.matchLabels",
                    kind.as_str()
                ))
            })
    }

    async fn scale<K>(&self, namespace: &str, name: &str, replicas: i32) -> Result<i32, K8sError>
    where
        K: Resource<Scope = NamespaceResourceScope>
            + Clone
            + serde::de::DeserializeOwned
            + std::fmt::Debug,
        <K as Resource>::DynamicType: Default,
    {
        let api: Api<K> = Api::namespaced(self.client.clone(), namespace);
        let patch = json!({ "spec": { "replicas": replicas } });
        let params = PatchParams {
            field_manager: Some(FIELD_MANAGER.into()),
            ..Default::default()
        };
        api.patch(name, &params, &Patch::Strategic(patch)).await?;
        Ok(replicas)
    }
}

#[derive(Debug, Serialize)]
struct DeploymentSummary {
    name: String,
    namespace: String,
    replicas: i32,
    ready_replicas: i32,
    updated_replicas: i32,
    available_replicas: i32,
    creation_timestamp: Option<String>,
}

#[derive(Debug, Serialize)]
struct StatefulSetSummary {
    name: String,
    namespace: String,
    replicas: i32,
    ready_replicas: i32,
    updated_replicas: i32,
    current_replicas: i32,
    creation_timestamp: Option<String>,
}

#[derive(Debug, Serialize)]
struct DaemonSetSummary {
    name: String,
    namespace: String,
    desired_number_scheduled: i32,
    current_number_scheduled: i32,
    number_ready: i32,
    number_available: i32,
    updated_number_scheduled: i32,
    creation_timestamp: Option<String>,
}

fn label_selector_to_string(selector: &LabelSelector) -> Option<String> {
    let labels = selector.match_labels.as_ref()?;
    if labels.is_empty() {
        return None;
    }
    Some(
        labels
            .iter()
            .map(|(k, v)| format!("{k}={v}"))
            .collect::<Vec<_>>()
            .join(","),
    )
}

fn creation_timestamp<K: Resource>(obj: &K) -> Option<String> {
    obj.meta()
        .creation_timestamp
        .as_ref()
        .map(|t| t.0.to_string())
}

fn to_value<T: Serialize>(value: T) -> Value {
    serde_json::to_value(value).unwrap_or(Value::Null)
}

#[async_trait]
impl K8sService for KubeService {
    async fn list_workloads(
        &self,
        kind: WorkloadKind,
        namespace: Option<&str>,
    ) -> Result<Vec<Value>, K8sError> {
        match kind {
            WorkloadKind::Deployment => {
                let api: Api<Deployment> = self.api(namespace);
                let list = api.list(&ListParams::default()).await?;
                Ok(list
                    .items
                    .iter()
                    .map(|d| {
                        let status = d.status.as_ref();
                        to_value(DeploymentSummary {
                            name: d.meta().name.clone().unwrap_or_default(),
                            namespace: d.meta().namespace.clone().unwrap_or_default(),
                            replicas: d.spec.as_ref().and_then(|s| s.replicas).unwrap_or(0),
                            ready_replicas: status.and_then(|s| s.ready_replicas).unwrap_or(0),
                            updated_replicas: status.and_then(|s| s.updated_replicas).unwrap_or(0),
                            available_replicas: status
                                .and_then(|s| s.available_replicas)
                                .unwrap_or(0),
                            creation_timestamp: creation_timestamp(d),
                        })
                    })
                    .collect())
            }
            WorkloadKind::StatefulSet => {
                let api: Api<StatefulSet> = self.api(namespace);
                let list = api.list(&ListParams::default()).await?;
                Ok(list
                    .items
                    .iter()
                    .map(|s| {
                        let status = s.status.as_ref();
                        to_value(StatefulSetSummary {
                            name: s.meta().name.clone().unwrap_or_default(),
                            namespace: s.meta().namespace.clone().unwrap_or_default(),
                            replicas: s.spec.as_ref().and_then(|sp| sp.replicas).unwrap_or(0),
                            ready_replicas: status.and_then(|st| st.ready_replicas).unwrap_or(0),
                            updated_replicas: status
                                .and_then(|st| st.updated_replicas)
                                .unwrap_or(0),
                            current_replicas: status
                                .and_then(|st| st.current_replicas)
                                .unwrap_or(0),
                            creation_timestamp: creation_timestamp(s),
                        })
                    })
                    .collect())
            }
            WorkloadKind::DaemonSet => {
                let api: Api<DaemonSet> = self.api(namespace);
                let list = api.list(&ListParams::default()).await?;
                Ok(list
                    .items
                    .iter()
                    .map(|d| {
                        let status = d.status.as_ref();
                        to_value(DaemonSetSummary {
                            name: d.meta().name.clone().unwrap_or_default(),
                            namespace: d.meta().namespace.clone().unwrap_or_default(),
                            desired_number_scheduled: status
                                .map(|s| s.desired_number_scheduled)
                                .unwrap_or(0),
                            current_number_scheduled: status
                                .map(|s| s.current_number_scheduled)
                                .unwrap_or(0),
                            number_ready: status.map(|s| s.number_ready).unwrap_or(0),
                            number_available: status.and_then(|s| s.number_available).unwrap_or(0),
                            updated_number_scheduled: status
                                .and_then(|s| s.updated_number_scheduled)
                                .unwrap_or(0),
                            creation_timestamp: creation_timestamp(d),
                        })
                    })
                    .collect())
            }
        }
    }

    async fn rollout_restart(
        &self,
        kind: WorkloadKind,
        namespace: &str,
        name: &str,
    ) -> Result<String, K8sError> {
        match kind {
            WorkloadKind::Deployment => self.restart::<Deployment>(namespace, name).await,
            WorkloadKind::StatefulSet => self.restart::<StatefulSet>(namespace, name).await,
            WorkloadKind::DaemonSet => self.restart::<DaemonSet>(namespace, name).await,
        }
    }

    async fn scale_workload(
        &self,
        kind: WorkloadKind,
        namespace: &str,
        name: &str,
        replicas: i32,
    ) -> Result<i32, K8sError> {
        match kind {
            WorkloadKind::Deployment => self.scale::<Deployment>(namespace, name, replicas).await,
            WorkloadKind::StatefulSet => self.scale::<StatefulSet>(namespace, name, replicas).await,
            WorkloadKind::DaemonSet => Err(K8sError::Api(
                "DaemonSet does not have replicas; cannot scale".to_string(),
            )),
        }
    }

    async fn workload_logs(
        &self,
        kind: WorkloadKind,
        namespace: &str,
        name: &str,
        options: &LogOptions,
    ) -> Result<LogResult, K8sError> {
        let selector = self.workload_pod_selector(kind, namespace, name).await?;
        let pods: Api<Pod> = Api::namespaced(self.client.clone(), namespace);
        let lp = ListParams::default().labels(&selector);
        let list = pods.list(&lp).await?;

        // Prefer the first Running pod, but fall back to any matching pod so
        // `previous=true` works against pods stuck in CrashLoopBackOff.
        let pod = list
            .items
            .iter()
            .find(|p| p.status.as_ref().and_then(|s| s.phase.as_deref()) == Some("Running"))
            .or_else(|| list.items.first())
            .cloned()
            .ok_or_else(|| {
                K8sError::Api(format!(
                    "no pod matched selector {selector:?} for {} {namespace}/{name}",
                    kind.as_str()
                ))
            })?;

        let pod_name = pod.metadata.name.clone().unwrap_or_default();

        let log_params = LogParams {
            container: options.container.clone(),
            tail_lines: options.tail_lines,
            previous: options.previous,
            timestamps: options.timestamps,
            since_seconds: options.since_seconds,
            ..LogParams::default()
        };

        let logs = pods.logs(&pod_name, &log_params).await?;

        Ok(LogResult {
            pod: pod_name,
            container: options.container.clone(),
            logs,
        })
    }

    async fn exec_in_pod(
        &self,
        namespace: &str,
        label_selector: &str,
        container: Option<&str>,
        command: &[String],
    ) -> Result<ExecOutcome, K8sError> {
        let pods: Api<Pod> = Api::namespaced(self.client.clone(), namespace);
        let lp = ListParams::default().labels(label_selector);
        let list = pods.list(&lp).await?;

        let pod = list
            .items
            .into_iter()
            .find(|p| p.status.as_ref().and_then(|s| s.phase.as_deref()) == Some("Running"))
            .ok_or_else(|| {
                K8sError::Api(format!(
                    "no Running pod matched selector {label_selector:?} in namespace {namespace:?}"
                ))
            })?;

        let pod_name = pod.metadata.name.clone().unwrap_or_default();

        let mut params = AttachParams::default().stderr(true).stdout(true);
        if let Some(c) = container {
            params = params.container(c);
        }

        let mut attached = pods.exec(&pod_name, command, &params).await?;

        let stdout_handle = attached.stdout();
        let stderr_handle = attached.stderr();
        let status_fut = attached
            .take_status()
            .ok_or_else(|| K8sError::Api("exec produced no status channel".to_string()))?;

        async fn drain<R: tokio::io::AsyncRead + Unpin>(reader: Option<R>) -> Vec<u8> {
            let mut buf = Vec::new();
            if let Some(mut r) = reader {
                let _ = r.read_to_end(&mut buf).await;
            }
            buf
        }

        let (stdout_bytes, stderr_bytes, status) =
            tokio::join!(drain(stdout_handle), drain(stderr_handle), status_fut,);
        let _ = attached.join().await;

        let success = status.as_ref().and_then(|s| s.status.as_deref()) == Some("Success");
        // Status="Success" with no NonZeroExitCode reason ⇒ exit 0.
        let success_default = if success { Some(0) } else { None };
        let exit_code = status
            .as_ref()
            .and_then(exit_code_from_status)
            .or(success_default);

        Ok(ExecOutcome {
            pod: pod_name,
            stdout: String::from_utf8_lossy(&stdout_bytes).into_owned(),
            stderr: String::from_utf8_lossy(&stderr_bytes).into_owned(),
            exit_code,
            success: exit_code == Some(0),
        })
    }
}

// The Kubernetes apiserver reports a non-zero exec exit by setting
// status.status="Failure", reason="NonZeroExitCode", and threading the
// numeric code through details.causes[reason="ExitCode"].message. There
// is no first-class field for it.
fn exit_code_from_status(
    status: &k8s_openapi::apimachinery::pkg::apis::meta::v1::Status,
) -> Option<i32> {
    let details = status.details.as_ref()?;
    let causes = details.causes.as_ref()?;
    let cause = causes
        .iter()
        .find(|c| c.reason.as_deref() == Some("ExitCode"))?;
    cause.message.as_deref()?.parse::<i32>().ok()
}
