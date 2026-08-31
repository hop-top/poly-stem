//! Thin Go-compatible cassette reader for the stem examples.
//!
//! `hop-top-xrr` v0.1.0-alpha.5 ships an [`HttpAdapter`] whose request
//! body type is `Vec<u8>`, which is not wire-compatible with the
//! Go-recorded cassettes in `./cassettes/` (those use
//! `body: <string>`). Rather than re-record cassettes per-language —
//! defeating the polyglot replay-once promise — this module reads the
//! Go-shaped YAML directly and exposes a `replay(method, url, body)`
//! shortcut.
//!
//! The fingerprint algorithm mirrors Go's `xrr/adapters/http`
//! exactly: `sha256(canonical_json{method, path, body_hash})[:4]`,
//! where `body_hash = sha256(body)[:4]`. Identical bytes in, identical
//! 8-hex-char fingerprint out.

use std::path::PathBuf;

use anyhow::{anyhow, bail, Context, Result};
use serde::Deserialize;
use sha2::{Digest, Sha256};

/// Recorded HTTP response payload as it lands in `*.resp.yaml`.
#[derive(Debug, Deserialize)]
struct RespPayload {
    status: u16,
    #[serde(default)]
    #[allow(dead_code)] // surfaced for parity, not currently consumed
    headers: std::collections::HashMap<String, String>,
    #[serde(default)]
    body: String,
}

#[derive(Debug, Deserialize)]
struct Envelope<T> {
    #[allow(dead_code)]
    xrr: String,
    #[allow(dead_code)]
    adapter: String,
    #[allow(dead_code)]
    fingerprint: String,
    #[allow(dead_code)]
    recorded_at: String,
    payload: T,
}

/// Reader over a directory of Go-shaped `http-<fp>.{req,resp}.yaml`
/// pairs.
pub struct CassetteDir {
    dir: PathBuf,
}

impl CassetteDir {
    pub fn new(dir: impl Into<PathBuf>) -> Self {
        Self { dir: dir.into() }
    }

    /// Compute the Go-compatible fingerprint for a request.
    pub fn fingerprint(method: &str, url: &str, body: &str) -> Result<String> {
        let path_query = extract_path_query(url);
        let body_hash_bytes = Sha256::digest(body.as_bytes());
        let body_hash = hex::encode(&body_hash_bytes[..4]);
        // serde_json key order is alphabetic by default for BTreeMap; Go's
        // map[string]any order is non-deterministic but `json.Marshal`
        // sorts keys. Mirror by handing the fields to serde_json via a
        // sorted struct of object literals.
        let canonical = serde_json::to_string(&serde_json::json!({
            "body_hash": body_hash,
            "method": method,
            "path": path_query,
        }))
        .context("fingerprint marshal")?;
        let sum = Sha256::digest(canonical.as_bytes());
        Ok(hex::encode(&sum[..4]))
    }

    /// Replay a recorded HTTP response. Returns `(status, body)`.
    pub fn replay(&self, method: &str, url: &str, body: &str) -> Result<(u16, String)> {
        let fp = Self::fingerprint(method, url, body)?;
        let resp_path = self.dir.join(format!("http-{fp}.resp.yaml"));
        let data = std::fs::read_to_string(&resp_path).with_context(|| {
            format!(
                "cassette miss: {} ({} {} body={}B)",
                resp_path.display(),
                method,
                url,
                body.len()
            )
        })?;
        let env: Envelope<RespPayload> =
            serde_yaml::from_str(&data).context("decode resp cassette")?;
        if env.payload.status >= 300 {
            bail!(
                "cassette {} encodes non-2xx status: {}",
                resp_path.display(),
                env.payload.status
            );
        }
        Ok((env.payload.status, env.payload.body))
    }
}

fn extract_path_query(url: &str) -> String {
    let rest = url
        .strip_prefix("https://")
        .or_else(|| url.strip_prefix("http://"))
        .unwrap_or(url);
    match rest.find('/') {
        Some(idx) => rest[idx..].to_string(),
        None => "/".to_string(),
    }
}

/// Convenience wrapper around `CassetteDir::replay` that also lets a
/// future record-mode codepath slot in. For now record mode is
/// disabled — the polyglot examples replay only.
pub fn replay(dir: &CassetteDir, method: &str, url: &str, body: &str) -> Result<(u16, String)> {
    if std::env::var("XRR_MODE").as_deref() == Ok("record") {
        return Err(anyhow!(
            "XRR_MODE=record not supported by the Rust example yet; \
             re-record from go/examples/<sample>/ and rerun in replay mode"
        ));
    }
    dir.replay(method, url, body)
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn fingerprint_matches_go_algorithm() {
        // Known cassette pair fp=d5e33672 (anthropic first call) — taken
        // from the committed ./cassettes/.
        let body = r#"{"max_tokens":1024,"messages":[{"content":[{"text":"What time is it?","type":"text"}],"role":"user"}],"model":"claude-sonnet-4-6","tools":[{"input_schema":{"properties":{},"required":[],"type":"object"},"name":"current_time","description":"Returns the current UTC time as an ISO 8601 string."}]}"#;
        let fp = CassetteDir::fingerprint("POST", "https://api.anthropic.com/v1/messages", body)
            .unwrap();
        assert_eq!(fp, "d5e33672");
    }

    #[test]
    fn fingerprint_strips_host() {
        let a = CassetteDir::fingerprint("GET", "https://host-a.com/path", "").unwrap();
        let b = CassetteDir::fingerprint("GET", "https://host-b.com/path", "").unwrap();
        assert_eq!(a, b);
    }
}
