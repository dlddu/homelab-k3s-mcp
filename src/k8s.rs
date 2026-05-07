use std::fmt;

use async_trait::async_trait;
use k8s_openapi::api::apps::v1::{DaemonSet, Deployment, StatefulSet};
use k8s_openapi::NamespaceResourceScope;
use kube::api::{Api, ListParams, Patch, PatchParams};
use kube::config::KubeConfigOptions;
use kube::{Client, Config, Resource};
use serde::Serialize;
use serde_json::{json, Value};

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
}

pub struct KubeService {
    client: Client,
}

impl KubeService {
    pub async fn try_new() -> Result<Self, K8sError> {
        let config = Self::load_config().await?;
        let client = Client::try_from(config)
            .map_err(|e| K8sError::Unavailable(format!("build kube client: {e}")))?;
        Ok(Self { client })
    }

    async fn load_config() -> Result<Config, K8sError> {
        match Config::incluster() {
            Ok(config) => {
                tracing::info!(
                    cluster_url = %config.cluster_url,
                    namespace = %config.default_namespace,
                    "loaded in-cluster kube config",
                );
                Ok(config)
            }
            Err(in_cluster_err) => {
                tracing::debug!(
                    error = %in_cluster_err,
                    "in-cluster kube config not available; falling back to kubeconfig",
                );
                match Config::from_kubeconfig(&KubeConfigOptions::default()).await {
                    Ok(config) => {
                        tracing::info!(
                            cluster_url = %config.cluster_url,
                            namespace = %config.default_namespace,
                            "loaded kube config from kubeconfig",
                        );
                        Ok(config)
                    }
                    Err(kubeconfig_err) => Err(K8sError::Unavailable(format!(
                        "init kube client: in-cluster: ({in_cluster_err}); kubeconfig: ({kubeconfig_err})"
                    ))),
                }
            }
        }
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
            field_manager: Some(FIELD_MANAGER.to_string()),
            ..PatchParams::default()
        };
        api.patch(name, &params, &Patch::Strategic(&patch)).await?;
        Ok(now)
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
}
