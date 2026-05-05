use axum::{routing::get, Json, Router};
use serde::Serialize;
use tower_http::trace::TraceLayer;

#[derive(Debug, Serialize)]
pub struct Health {
    pub status: &'static str,
    pub version: &'static str,
}

pub fn app() -> Router {
    Router::new()
        .route("/", get(root))
        .route("/healthz", get(healthz))
        .route("/readyz", get(readyz))
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
