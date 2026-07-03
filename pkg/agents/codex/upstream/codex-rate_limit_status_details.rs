// MIRROR of openai/codex — codex-rs/codex-backend-openapi-models/src/models/rate_limit_status_details.rs
// Acquired 2026-06-23. Blob SHA: ca9fdfe2406d5d03a557cd3b8018c88abe80476d
// primary_window = 5h, secondary_window = weekly (the two windows /status shows).
/*
 * codex-backend
 * The version of the OpenAPI document: 0.0.1
 */
use crate::models;
use serde::Deserialize;
use serde::Serialize;

#[derive(Clone, Default, Debug, PartialEq, Serialize, Deserialize)]
pub struct RateLimitStatusDetails {
    #[serde(rename = "allowed")]
    pub allowed: bool,
    #[serde(rename = "limit_reached")]
    pub limit_reached: bool,
    #[serde(rename = "primary_window")]
    pub primary_window: Option<Option<Box<models::RateLimitWindowSnapshot>>>,
    #[serde(rename = "secondary_window")]
    pub secondary_window: Option<Option<Box<models::RateLimitWindowSnapshot>>>,
}
