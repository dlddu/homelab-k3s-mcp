use std::fmt;

use async_trait::async_trait;
use k8s_openapi::api::apps::v1::{DaemonSet, Deployment, StatefulSet};
use k8s_openapi::NamespaceResourceScope;
use kube::api::{Api, ListParams, Patch, PatchParams};
use kube::{Client, Resource};
use serde::Serialize;
use serde_json::json;

const RESTART_ANNOTATION: &str = "kubectl.kubernetes.io/restartedAt";
const FIELD_MANAGER: &str = "homelab-k3s-mcp";

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

#[derive(Debug, Serialize)]
pub struct DeploymentSummary {
    pub name: String,
    pub namespace: String,
    pub replicas: i32,
    pub ready_replicas: i32,
    pub updated_replicas: i32,
    pub available_replicas: i32,
    pub creation_timestamp: Option<String>,
}

#[derive(Debug, Serialize)]
pub struct StatefulSetSummary {
    pub name: String,
    pub namespace: String,
    pub replicas: i32,
    pub ready_replicas: i32,
    pub updated_replicas: i32,
    pub current_replicas: i32,
    pub creation_timestamp: Option<String>,
}

#[derive(Debug, Serialize)]
pub struct DaemonSetSummary {
    pub name: String,
    pub namespace: String,
    pub desired_number_scheduled: i32,
    pub current_number_scheduled: i32,
    pub number_ready: i32,
    pub number_available: i32,
    pub updated_number_scheduled: i32,
    pub creation_timestamp: Option<String>,
}

#[async_trait]
pub trait K8sService: Send + Sync {
    async fn list_deployments(
        &self,
        namespace: Option<&str>,
    ) -> Result<Vec<DeploymentSummary>, K8sError>;

    async fn list_statefulsets(
        &self,
        namespace: Option<&str>,
    ) -> Result<Vec<StatefulSetSummary>, K8sError>;

    async fn list_daemonsets(
        &self,
        namespace: Option<&str>,
    ) -> Result<Vec<DaemonSetSummary>, K8sError>;

    async fn rollout_restart_deployment(
        &self,
        namespace: &str,
        name: &str,
    ) -> Result<String, K8sError>;

    async fn rollout_restart_statefulset(
        &self,
        namespace: &str,
        name: &str,
    ) -> Result<String, K8sError>;

    async fn rollout_restart_daemonset(
        &self,
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
    async fn list_deployments(
        &self,
        _namespace: Option<&str>,
    ) -> Result<Vec<DeploymentSummary>, K8sError> {
        Err(K8sError::Unavailable(self.reason.clone()))
    }

    async fn list_statefulsets(
        &self,
        _namespace: Option<&str>,
    ) -> Result<Vec<StatefulSetSummary>, K8sError> {
        Err(K8sError::Unavailable(self.reason.clone()))
    }

    async fn list_daemonsets(
        &self,
        _namespace: Option<&str>,
    ) -> Result<Vec<DaemonSetSummary>, K8sError> {
        Err(K8sError::Unavailable(self.reason.clone()))
    }

    async fn rollout_restart_deployment(
        &self,
        _namespace: &str,
        _name: &str,
    ) -> Result<String, K8sError> {
        Err(K8sError::Unavailable(self.reason.clone()))
    }

    async fn rollout_restart_statefulset(
        &self,
        _namespace: &str,
        _name: &str,
    ) -> Result<String, K8sError> {
        Err(K8sError::Unavailable(self.reason.clone()))
    }

    async fn rollout_restart_daemonset(
        &self,
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

    async fn rollout_restart<K>(&self, namespace: &str, name: &str) -> Result<String, K8sError>
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
        api.patch(
            name,
            &PatchParams::apply(FIELD_MANAGER).force(),
            &Patch::Apply(patch),
        )
        .await?;
        Ok(now)
    }
}

fn creation_timestamp<K>(obj: &K) -> Option<String>
where
    K: Resource,
{
    obj.meta()
        .creation_timestamp
        .as_ref()
        .map(|t| t.0.to_string())
}

#[async_trait]
impl K8sService for KubeService {
    async fn list_deployments(
        &self,
        namespace: Option<&str>,
    ) -> Result<Vec<DeploymentSummary>, K8sError> {
        let api: Api<Deployment> = self.api(namespace);
        let list = api.list(&ListParams::default()).await?;
        Ok(list
            .items
            .iter()
            .map(|d| {
                let status = d.status.as_ref();
                DeploymentSummary {
                    name: d.meta().name.clone().unwrap_or_default(),
                    namespace: d.meta().namespace.clone().unwrap_or_default(),
                    replicas: d.spec.as_ref().and_then(|s| s.replicas).unwrap_or(0),
                    ready_replicas: status.and_then(|s| s.ready_replicas).unwrap_or(0),
                    updated_replicas: status.and_then(|s| s.updated_replicas).unwrap_or(0),
                    available_replicas: status.and_then(|s| s.available_replicas).unwrap_or(0),
                    creation_timestamp: creation_timestamp(d),
                }
            })
            .collect())
    }

    async fn list_statefulsets(
        &self,
        namespace: Option<&str>,
    ) -> Result<Vec<StatefulSetSummary>, K8sError> {
        let api: Api<StatefulSet> = self.api(namespace);
        let list = api.list(&ListParams::default()).await?;
        Ok(list
            .items
            .iter()
            .map(|s| {
                let status = s.status.as_ref();
                StatefulSetSummary {
                    name: s.meta().name.clone().unwrap_or_default(),
                    namespace: s.meta().namespace.clone().unwrap_or_default(),
                    replicas: s.spec.as_ref().and_then(|sp| sp.replicas).unwrap_or(0),
                    ready_replicas: status.and_then(|st| st.ready_replicas).unwrap_or(0),
                    updated_replicas: status.and_then(|st| st.updated_replicas).unwrap_or(0),
                    current_replicas: status.and_then(|st| st.current_replicas).unwrap_or(0),
                    creation_timestamp: creation_timestamp(s),
                }
            })
            .collect())
    }

    async fn list_daemonsets(
        &self,
        namespace: Option<&str>,
    ) -> Result<Vec<DaemonSetSummary>, K8sError> {
        let api: Api<DaemonSet> = self.api(namespace);
        let list = api.list(&ListParams::default()).await?;
        Ok(list
            .items
            .iter()
            .map(|d| {
                let status = d.status.as_ref();
                DaemonSetSummary {
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
                }
            })
            .collect())
    }

    async fn rollout_restart_deployment(
        &self,
        namespace: &str,
        name: &str,
    ) -> Result<String, K8sError> {
        self.rollout_restart::<Deployment>(namespace, name).await
    }

    async fn rollout_restart_statefulset(
        &self,
        namespace: &str,
        name: &str,
    ) -> Result<String, K8sError> {
        self.rollout_restart::<StatefulSet>(namespace, name).await
    }

    async fn rollout_restart_daemonset(
        &self,
        namespace: &str,
        name: &str,
    ) -> Result<String, K8sError> {
        self.rollout_restart::<DaemonSet>(namespace, name).await
    }
}
