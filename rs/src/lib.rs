//! `hop-top-stem` — Rust SDK for stem, the polyglot AI agent runtime.
//!
//! This crate ships the Tier A surface: the
//! [crtx v0.1](https://github.com/hop-top/spec-crtx) envelope types,
//! strict parse/serialize helpers, and structural validation. It does
//! NOT ship a provider trait, supervisor, or storage backends —
//! consumers wire their own runtime around the envelope shape.
//!
//! ## Quickstart
//!
//! ```
//! use hop_top_stem::{parse_envelope, validate, Envelope};
//!
//! let json = r#"{
//!   "crtx_version": "0.1",
//!   "id": "demo-1",
//!   "created_at": "2026-01-01T00:00:00Z",
//!   "updated_at": "2026-01-01T00:00:00Z",
//!   "source": {"kind": "stem", "version": "0.1.0"},
//!   "turns": []
//! }"#;
//!
//! let env: Envelope = parse_envelope(json).unwrap();
//! assert!(validate(&env).is_empty());
//! ```

pub mod content;
pub mod envelope;
pub mod error;
pub mod validate;

pub use content::{ContentPart, Role};
pub use envelope::{
    parse_envelope, serialize_envelope, DispatchedFrom, Envelope, InjectedTurnRef, Source, Turn,
    CRTX_VERSION, SOURCE_KIND, VERSION,
};
pub use error::EnvelopeError;
pub use validate::{validate, validate_bytes};
