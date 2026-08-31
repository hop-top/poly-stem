//! `Envelope`, `Turn`, `Source` — top-level crtx v0.1 types and
//! parse/serialize helpers.
//!
//! Timestamps are exchanged as RFC 3339 strings (mirroring the spec)
//! and exposed as `String` to avoid pulling `chrono` into the SDK
//! dependency surface. Callers that need typed time values can parse
//! the strings themselves.

use serde::{Deserialize, Serialize};

use crate::content::{ContentPart, Role};
use crate::error::EnvelopeError;

/// crtx spec version this SDK speaks. Every emitted envelope carries
/// this value on `crtx_version`.
pub const CRTX_VERSION: &str = "0.1";

/// stem semver. Surfaced as `Source.version` on stem-produced
/// envelopes when using [`Source::default_for_stem`].
pub const VERSION: &str = "0.1.0";

/// Default `Source.kind` for envelopes produced by this SDK.
pub const SOURCE_KIND: &str = "stem";

/// Identifies the runtime that produced an envelope.
#[derive(Debug, Clone, Serialize, Deserialize, PartialEq)]
#[serde(deny_unknown_fields)]
pub struct Source {
    pub kind: String,
    pub version: String,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub instance: Option<String>,
}

impl Source {
    /// Canonical Source for stem-produced envelopes: kind=stem,
    /// version=[`VERSION`].
    pub fn default_for_stem() -> Self {
        Self {
            kind: SOURCE_KIND.to_string(),
            version: VERSION.to_string(),
            instance: None,
        }
    }
}

/// One contribution within an [`Envelope`]. Ordered by position in
/// `Envelope.turns`, not by `created_at`.
///
/// `agent_id` and `in_reply_to_call_id` are crtx v0.1 multi-agent
/// provenance fields. See envelope.md §3.1 and §3.2 in the spec.
#[derive(Debug, Clone, Serialize, Deserialize, PartialEq)]
#[serde(deny_unknown_fields)]
pub struct Turn {
    pub id: String,
    pub role: Role,
    pub created_at: String,
    pub content: Vec<ContentPart>,
    /// Stable identifier of the producing agent when multiple
    /// assistants contribute to one Envelope. See envelope.md §3.1.
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub agent_id: Option<String>,
    /// References a still-open `tool_call.call_id` earlier in the
    /// Envelope. Marks this Turn as a mid-flight message into the
    /// lifetime of that call. See envelope.md §3.2.
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub in_reply_to_call_id: Option<String>,
    /// crtx v0.1 `metadata` is typed `object` — using a `Map` here
    /// (rather than a free `Value`) gives a compile-time guarantee the
    /// SDK cannot emit a non-object metadata payload.
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub metadata: Option<serde_json::Map<String, serde_json::Value>>,
}

/// Marks an Envelope as a dispatch nest spawned by an open `tool_call`
/// in another Envelope. Both members are required by the schema;
/// mutually exclusive with `parent_id` / `fork_point`. See envelope.md
/// §7.2.
#[derive(Debug, Clone, Serialize, Deserialize, PartialEq)]
#[serde(deny_unknown_fields)]
pub struct DispatchedFrom {
    pub envelope_id: String,
    pub call_id: String,
}

/// Reference to a slice of Turns in another Envelope to be included as
/// part of this Envelope's context. Append-only. See envelope.md §7.3.
#[derive(Debug, Clone, Serialize, Deserialize, PartialEq)]
#[serde(deny_unknown_fields)]
pub struct InjectedTurnRef {
    pub envelope_id: String,
    pub start_turn_id: String,
    pub end_turn_id: String,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub injected_after_turn_id: Option<String>,
}

/// A crtx v0.1 envelope: one conversation, ordered turns, optional
/// fork pointer.
#[derive(Debug, Clone, Serialize, Deserialize, PartialEq)]
#[serde(deny_unknown_fields)]
pub struct Envelope {
    pub crtx_version: String,
    pub id: String,
    pub created_at: String,
    pub updated_at: String,
    pub source: Source,
    /// Always serialized — empty conversations emit `"turns": []`,
    /// never `null` or absent.
    #[serde(default)]
    pub turns: Vec<Turn>,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub parent_id: Option<String>,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub fork_point: Option<i64>,
    /// Set when this Envelope was spawned by an open `tool_call` in
    /// another Envelope as a dispatch nest. Mutually exclusive with
    /// `parent_id` / `fork_point`. See envelope.md §7.2.
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub dispatched_from: Option<DispatchedFrom>,
    /// Ordered, append-only list of references to Turn slices in other
    /// Envelopes. See envelope.md §7.3.
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub injected_turns: Option<Vec<InjectedTurnRef>>,
    /// crtx v0.1 `metadata` is typed `object` — see `Turn.metadata`.
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub metadata: Option<serde_json::Map<String, serde_json::Value>>,
}

impl Envelope {
    /// Construct a fresh envelope with `crtx_version` + default
    /// [`Source`] populated. `created_at` and `updated_at` are taken
    /// verbatim from the caller — pick RFC 3339 strings.
    pub fn new(id: impl Into<String>, created_at: impl Into<String>) -> Self {
        let now = created_at.into();
        Self {
            crtx_version: CRTX_VERSION.to_string(),
            id: id.into(),
            created_at: now.clone(),
            updated_at: now,
            source: Source::default_for_stem(),
            turns: Vec::new(),
            parent_id: None,
            fork_point: None,
            dispatched_from: None,
            injected_turns: None,
            metadata: None,
        }
    }
}

/// Strict-decode JSON into an [`Envelope`]. Rejects unknown top-level
/// fields, unknown variant fields, and any structural drift.
pub fn parse_envelope(input: &str) -> Result<Envelope, EnvelopeError> {
    let env: Envelope = serde_json::from_str(input)?;
    Ok(env)
}

/// Serialize an [`Envelope`] to its canonical JSON form.
///
/// Empty turns serialize as `[]`. Image parts return an `Encode` error
/// rather than silently emitting invalid JSON when neither/both of
/// `data` / `url` are set.
pub fn serialize_envelope(env: &Envelope) -> Result<String, EnvelopeError> {
    serde_json::to_string(env).map_err(|e| EnvelopeError::Encode(e.to_string()))
}
