use std::sync::Arc;

use axum::{middleware, routing::get, Json, Router};
use serde::Serialize;
use tower_http::trace::TraceLayer;

pub mod auth;
pub mod github;
pub mod grafana;
pub mod k8s;
pub mod mcp;

pub use auth::AuthConfig;
pub use github::{GitHubAppClient, GitHubAppService, UnavailableGitHubApp};
pub use grafana::{GrafanaCloudClient, GrafanaCloudService, UnavailableGrafanaCloud};
pub use k8s::{K8sService, KubeService, UnavailableK8s};
pub use mcp::McpState;

#[derive(Debug, Serialize)]
pub struct Health {
    pub status: &'static str,
    pub version: &'static str,
}

pub fn app(
    auth: Option<AuthConfig>,
    k8s: Arc<dyn K8sService>,
    github: Arc<dyn GitHubAppService>,
    grafana: Arc<dyn GrafanaCloudService>,
) -> Router {
    let state = McpState {
        k8s,
        github,
        grafana,
    };

    let public = Router::new()
        .route("/", get(root))
        .route("/healthz", get(healthz))
        .route("/readyz", get(readyz));

    let (well_known, protected) = match auth {
        Some(config) => {
            let auth_state = Arc::new(config);
            let metadata = Router::new()
                .route(
                    "/.well-known/oauth-protected-resource",
                    get(auth::protected_resource_metadata),
                )
                .with_state(auth_state.clone());
            let mcp = mcp::router(state).route_layer(middleware::from_fn_with_state(
                auth_state,
                auth::require_bearer,
            ));
            (metadata, mcp)
        }
        None => (Router::new(), mcp::router(state)),
    };

    public
        .merge(well_known)
        .merge(protected)
        .layer(TraceLayer::new_for_http())
}

async fn root() -> &'static str {
    env!("CARGO_PKG_NAME")
}

async fn healthz() -> Json<Health> {
    Json(Health {
        status: "ok",
        version: env!("CARGO_PKG_VERSION"),
    })
}

async fn readyz() -> Json<Health> {
    Json(Health {
        status: "ready",
        version: env!("CARGO_PKG_VERSION"),
    })
}
