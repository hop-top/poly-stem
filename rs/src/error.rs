//! Error types returned by parse / serialize / validate.

use thiserror::Error;

/// Errors emitted by the envelope SDK.
///
/// `Decode` wraps strict-mode `serde_json` failures (including
/// `deny_unknown_fields` rejections). `Encode` wraps serialization
/// failures. Every other variant is a structural-validation finding
/// surfaced by [`crate::validate`] / [`crate::validate_bytes`].
#[derive(Debug, Error)]
pub enum EnvelopeError {
    #[error("decode error: {0}")]
    Decode(#[from] serde_json::Error),

    #[error("encode error: {0}")]
    Encode(String),

    #[error("missing crtx_version")]
    MissingCrtxVersion,

    #[error("unsupported crtx_version: {got} (this stem speaks {want})")]
    UnsupportedCrtxVersion { got: String, want: &'static str },

    #[error("missing id")]
    MissingId,

    #[error("missing created_at")]
    MissingCreatedAt,

    #[error("missing updated_at")]
    MissingUpdatedAt,

    #[error("source.kind and source.version are required")]
    SourceIncomplete,

    #[error("parent_id and fork_point must both be set or both omitted")]
    ForkPointParentMismatch,

    #[error("fork_point must be >= 0")]
    ForkPointNegative,

    #[error("dispatched_from is mutually exclusive with parent_id/fork_point")]
    DispatchedFromExclusiveWithFork,

    #[error("dispatched_from requires both envelope_id and call_id")]
    DispatchedFromIncomplete,

    #[error("injected_turns[{index}]: {message}")]
    InjectedTurn { index: usize, message: String },

    #[error("turns[{index}]: {message}")]
    Turn { index: usize, message: String },

    #[error("turns[{turn_index}].content[{part_index}]: {message}")]
    ContentPart {
        turn_index: usize,
        part_index: usize,
        message: String,
    },
}

impl EnvelopeError {
    /// Helper used by validators to attach an index for a Turn-level
    /// violation.
    pub(crate) fn turn(index: usize, message: impl Into<String>) -> Self {
        Self::Turn {
            index,
            message: message.into(),
        }
    }

    /// Helper used by validators to attach an index for an
    /// `injected_turns` entry violation.
    pub(crate) fn injected_turn(index: usize, message: impl Into<String>) -> Self {
        Self::InjectedTurn {
            index,
            message: message.into(),
        }
    }

    /// Helper used by validators to attach indices for a ContentPart
    /// violation inside a Turn.
    pub(crate) fn content(
        turn_index: usize,
        part_index: usize,
        message: impl Into<String>,
    ) -> Self {
        Self::ContentPart {
            turn_index,
            part_index,
            message: message.into(),
        }
    }
}
