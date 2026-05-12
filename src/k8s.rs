use std::collections::BTreeMap;
use std::fmt;

use async_trait::async_trait;
use k8s_openapi::api::apps::v1::{DaemonSet, Deployment, StatefulSet};
use k8s_openapi::api::core::v1::{Event, Namespace, Pod};
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

#[derive(Debug, Clone, Default, Serialize)]
pub struct ContainerInfo {
    pub name: String,
    pub image: String,
    pub ready: bool,
    pub started: Option<bool>,
    pub restart_count: i32,
    pub state: Option<String>,
    pub reason: Option<String>,
    pub message: Option<String>,
    pub started_at: Option<String>,
    pub finished_at: Option<String>,
    pub exit_code: Option<i32>,
    pub last_state: Option<String>,
    pub last_reason: Option<String>,
    pub last_exit_code: Option<i32>,
}

#[derive(Debug, Clone, Default, Serialize)]
pub struct PodConditionInfo {
    #[serde(rename = "type")]
    pub kind: String,
    pub status: String,
    pub reason: Option<String>,
    pub message: Option<String>,
    pub last_transition_time: Option<String>,
}

#[derive(Debug, Clone, Default, Serialize)]
pub struct PodEventInfo {
    #[serde(rename = "type")]
    pub kind: String,
    pub reason: String,
    pub message: String,
    pub count: i32,
    pub first_timestamp: Option<String>,
    pub last_timestamp: Option<String>,
    pub source: Option<String>,
}

#[derive(Debug, Clone, Serialize)]
pub struct PodDescription {
    pub name: String,
    pub namespace: String,
    pub node: Option<String>,
    pub phase: Option<String>,
    pub pod_ip: Option<String>,
    pub host_ip: Option<String>,
    pub service_account: Option<String>,
    pub priority: Option<i32>,
    pub priority_class_name: Option<String>,
    pub qos_class: Option<String>,
    pub start_time: Option<String>,
    pub creation_timestamp: Option<String>,
    pub labels: BTreeMap<String, String>,
    pub annotations: BTreeMap<String, String>,
    pub node_selector: BTreeMap<String, String>,
    pub owner_references: Vec<Value>,
    pub conditions: Vec<PodConditionInfo>,
    pub init_containers: Vec<ContainerInfo>,
    pub containers: Vec<ContainerInfo>,
    pub events: Vec<PodEventInfo>,
}

/// How `describe_pod` finds the pod to describe.
#[derive(Debug, Clone, PartialEq, Eq)]
pub enum PodTarget {
    /// Exact pod name within the namespace.
    Name(String),
    /// Label selector; resolves to the first Running pod (or any matching
    /// pod when none is Running).
    Selector(String),
    /// Workload kind + name; resolves the workload's pod selector and
    /// picks the first Running pod (or any matching pod when none is Running).
    Workload { kind: WorkloadKind, name: String },
}

#[async_trait]
pub trait K8sService: Send + Sync {
    async fn list_namespaces(&self) -> Result<Vec<Value>, K8sError>;

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

    /// Produce a `kubectl describe pod`-style snapshot for a single pod:
    /// metadata, container statuses, conditions, and recent events. The
    /// pod is resolved from `target` (exact name, label selector, or a
    /// backing workload).
    async fn describe_pod(
        &self,
        namespace: &str,
        target: &PodTarget,
    ) -> Result<PodDescription, K8sError>;
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
    async fn list_namespaces(&self) -> Result<Vec<Value>, K8sError> {
        Err(K8sError::Unavailable(self.reason.clone()))
    }

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

    async fn describe_pod(
        &self,
        _namespace: &str,
        _target: &PodTarget,
    ) -> Result<PodDescription, K8sError> {
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

    async fn first_pod_matching(&self, namespace: &str, selector: &str) -> Result<Pod, K8sError> {
        let pods: Api<Pod> = Api::namespaced(self.client.clone(), namespace);
        let lp = ListParams::default().labels(selector);
        let list = pods.list(&lp).await?;
        list.items
            .iter()
            .find(|p| p.status.as_ref().and_then(|s| s.phase.as_deref()) == Some("Running"))
            .or_else(|| list.items.first())
            .cloned()
            .ok_or_else(|| {
                K8sError::Api(format!(
                    "no pod matched selector {selector:?} in namespace {namespace:?}"
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
struct NamespaceSummary {
    name: String,
    phase: Option<String>,
    creation_timestamp: Option<String>,
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
    async fn list_namespaces(&self) -> Result<Vec<Value>, K8sError> {
        let api: Api<Namespace> = Api::all(self.client.clone());
        let list = api.list(&ListParams::default()).await?;
        Ok(list
            .items
            .iter()
            .map(|n| {
                to_value(NamespaceSummary {
                    name: n.meta().name.clone().unwrap_or_default(),
                    phase: n.status.as_ref().and_then(|s| s.phase.clone()),
                    creation_timestamp: creation_timestamp(n),
                })
            })
            .collect())
    }

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

    async fn describe_pod(
        &self,
        namespace: &str,
        target: &PodTarget,
    ) -> Result<PodDescription, K8sError> {
        let pod = match target {
            PodTarget::Name(name) => {
                let pods: Api<Pod> = Api::namespaced(self.client.clone(), namespace);
                pods.get(name).await?
            }
            PodTarget::Selector(selector) => self.first_pod_matching(namespace, selector).await?,
            PodTarget::Workload { kind, name } => {
                let selector = self.workload_pod_selector(*kind, namespace, name).await?;
                self.first_pod_matching(namespace, &selector).await?
            }
        };

        let pod_name = pod.metadata.name.clone().unwrap_or_default();
        let events_api: Api<Event> = Api::namespaced(self.client.clone(), namespace);
        let field_selector =
            format!("involvedObject.name={pod_name},involvedObject.namespace={namespace}");
        let lp = ListParams::default().fields(&field_selector);
        let events = match events_api.list(&lp).await {
            Ok(list) => list.items,
            // Events are best-effort. If listing fails (e.g. RBAC), we still
            // want the rest of the description to come through.
            Err(_) => Vec::new(),
        };

        Ok(build_pod_description(&pod, &events))
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

fn build_pod_description(pod: &Pod, events: &[Event]) -> PodDescription {
    let meta = &pod.metadata;
    let spec = pod.spec.as_ref();
    let status = pod.status.as_ref();

    let containers_spec: BTreeMap<String, String> = spec
        .map(|s| {
            s.containers
                .iter()
                .map(|c| (c.name.clone(), c.image.clone().unwrap_or_default()))
                .collect()
        })
        .unwrap_or_default();
    let init_containers_spec: BTreeMap<String, String> = spec
        .and_then(|s| s.init_containers.as_ref())
        .map(|cs| {
            cs.iter()
                .map(|c| (c.name.clone(), c.image.clone().unwrap_or_default()))
                .collect()
        })
        .unwrap_or_default();

    let container_statuses = status
        .and_then(|s| s.container_statuses.as_ref())
        .map(|v| v.as_slice())
        .unwrap_or(&[]);
    let init_container_statuses = status
        .and_then(|s| s.init_container_statuses.as_ref())
        .map(|v| v.as_slice())
        .unwrap_or(&[]);

    let containers = build_container_infos(container_statuses, &containers_spec);
    let init_containers = build_container_infos(init_container_statuses, &init_containers_spec);

    let conditions = status
        .and_then(|s| s.conditions.as_ref())
        .map(|cs| {
            cs.iter()
                .map(|c| PodConditionInfo {
                    kind: c.type_.clone(),
                    status: c.status.clone(),
                    reason: c.reason.clone(),
                    message: c.message.clone(),
                    last_transition_time: c.last_transition_time.as_ref().map(|t| t.0.to_string()),
                })
                .collect()
        })
        .unwrap_or_default();

    let mut event_infos: Vec<PodEventInfo> = events
        .iter()
        .map(|e| PodEventInfo {
            kind: e.type_.clone().unwrap_or_default(),
            reason: e.reason.clone().unwrap_or_default(),
            message: e.message.clone().unwrap_or_default(),
            count: e.count.unwrap_or(1),
            first_timestamp: e.first_timestamp.as_ref().map(|t| t.0.to_string()),
            last_timestamp: e.last_timestamp.as_ref().map(|t| t.0.to_string()),
            source: e.source.as_ref().and_then(|s| s.component.clone()),
        })
        .collect();
    event_infos.sort_by(|a, b| a.last_timestamp.cmp(&b.last_timestamp));

    let owner_references = meta
        .owner_references
        .as_ref()
        .map(|refs| {
            refs.iter()
                .map(|r| {
                    json!({
                        "apiVersion": r.api_version,
                        "kind": r.kind,
                        "name": r.name,
                        "uid": r.uid,
                        "controller": r.controller,
                    })
                })
                .collect()
        })
        .unwrap_or_default();

    PodDescription {
        name: meta.name.clone().unwrap_or_default(),
        namespace: meta.namespace.clone().unwrap_or_default(),
        node: spec.and_then(|s| s.node_name.clone()),
        phase: status.and_then(|s| s.phase.clone()),
        pod_ip: status.and_then(|s| s.pod_ip.clone()),
        host_ip: status.and_then(|s| s.host_ip.clone()),
        service_account: spec.and_then(|s| s.service_account_name.clone()),
        priority: spec.and_then(|s| s.priority),
        priority_class_name: spec.and_then(|s| s.priority_class_name.clone()),
        qos_class: status.and_then(|s| s.qos_class.clone()),
        start_time: status
            .and_then(|s| s.start_time.as_ref())
            .map(|t| t.0.to_string()),
        creation_timestamp: meta.creation_timestamp.as_ref().map(|t| t.0.to_string()),
        labels: meta.labels.clone().unwrap_or_default(),
        annotations: meta.annotations.clone().unwrap_or_default(),
        node_selector: spec
            .and_then(|s| s.node_selector.clone())
            .unwrap_or_default(),
        owner_references,
        conditions,
        init_containers,
        containers,
        events: event_infos,
    }
}

fn build_container_infos(
    statuses: &[k8s_openapi::api::core::v1::ContainerStatus],
    spec_images: &BTreeMap<String, String>,
) -> Vec<ContainerInfo> {
    statuses
        .iter()
        .map(|cs| {
            let mut info = ContainerInfo {
                name: cs.name.clone(),
                image: if cs.image.is_empty() {
                    spec_images.get(&cs.name).cloned().unwrap_or_default()
                } else {
                    cs.image.clone()
                },
                ready: cs.ready,
                started: cs.started,
                restart_count: cs.restart_count,
                ..ContainerInfo::default()
            };
            if let Some(state) = cs.state.as_ref() {
                if let Some(running) = state.running.as_ref() {
                    info.state = Some("running".into());
                    info.started_at = running.started_at.as_ref().map(|t| t.0.to_string());
                } else if let Some(waiting) = state.waiting.as_ref() {
                    info.state = Some("waiting".into());
                    info.reason = waiting.reason.clone();
                    info.message = waiting.message.clone();
                } else if let Some(terminated) = state.terminated.as_ref() {
                    info.state = Some("terminated".into());
                    info.reason = terminated.reason.clone();
                    info.message = terminated.message.clone();
                    info.exit_code = Some(terminated.exit_code);
                    info.started_at = terminated.started_at.as_ref().map(|t| t.0.to_string());
                    info.finished_at = terminated.finished_at.as_ref().map(|t| t.0.to_string());
                }
            }
            if let Some(last) = cs.last_state.as_ref() {
                if last.running.is_some() {
                    info.last_state = Some("running".into());
                } else if let Some(waiting) = last.waiting.as_ref() {
                    info.last_state = Some("waiting".into());
                    info.last_reason = waiting.reason.clone();
                } else if let Some(terminated) = last.terminated.as_ref() {
                    info.last_state = Some("terminated".into());
                    info.last_reason = terminated.reason.clone();
                    info.last_exit_code = Some(terminated.exit_code);
                }
            }
            info
        })
        .collect()
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
