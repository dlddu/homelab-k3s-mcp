use std::sync::Arc;

use axum::extract::State;
use axum::http::StatusCode;
use axum::response::IntoResponse;
use axum::{middleware, routing::get, Json, Router};
use serde::Serialize;
use tower_http::trace::TraceLayer;

pub mod auth;
pub mod k8s;
pub mod mcp;

pub use auth::AuthConfig;
pub use k8s::{K8sService, KubeService, UnavailableK8s};

#[derive(Debug, Serialize)]
pub struct Health {
    pub status: &'static str,
    pub version: &'static str,
}

pub fn app(auth: Option<AuthConfig>, k8s: Arc<dyn K8sService>) -> Router {
    let public = Router::new()
        .route("/", get(root))
        .route("/healthz", get(healthz));
    let ready = Router::new()
        .route("/readyz", get(readyz))
        .with_state(k8s.clone());

    let (well_known, protected) = match auth {
        Some(config) => {
            let state = Arc::new(config);
            let metadata = Router::new()
                .route(
                    "/.well-known/oauth-protected-resource",
                    get(auth::protected_resource_metadata),
                )
                .with_state(state.clone());
            let mcp = mcp::router(k8s)
                .route_layer(middleware::from_fn_with_state(state, auth::require_bearer));
            (metadata, mcp)
        }
        None => (Router::new(), mcp::router(k8s)),
    };

    public
        .merge(ready)
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

async fn readyz(State(k8s): State<Arc<dyn K8sService>>) -> impl IntoResponse {
    if k8s.is_healthy() {
        (
            StatusCode::OK,
            Json(Health {
                status: "ready",
                version: env!("CARGO_PKG_VERSION"),
            }),
        )
    } else {
        (
            StatusCode::SERVICE_UNAVAILABLE,
            Json(Health {
                status: "kubernetes client unavailable",
                version: env!("CARGO_PKG_VERSION"),
            }),
        )
    }
}
