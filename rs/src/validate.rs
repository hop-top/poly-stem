//! Structural validation for parsed envelopes.
//!
//! `validate` operates on an already-decoded [`Envelope`] and returns
//! every finding it can. `validate_bytes` does a strict decode first
//! (rejecting unknown top-level fields) and then applies the same
//! rules — use it on trust boundaries where producers may drift.

use std::collections::HashSet;

use crate::content::{is_valid_extension_type, is_valid_mime_type, ContentPart, Role};
use crate::envelope::{Envelope, CRTX_VERSION};
use crate::error::EnvelopeError;

/// Validate `env` against crtx v0.1 structural rules. Returns every
/// finding so a producer can fix them in one pass; empty `Vec` means
/// the envelope is structurally clean.
pub fn validate(env: &Envelope) -> Vec<EnvelopeError> {
    let mut out = Vec::new();

    if env.crtx_version.is_empty() {
        out.push(EnvelopeError::MissingCrtxVersion);
    } else if !crtx_version_compatible(&env.crtx_version) {
        out.push(EnvelopeError::UnsupportedCrtxVersion {
            got: env.crtx_version.clone(),
            want: CRTX_VERSION,
        });
    }
    if env.id.is_empty() {
        out.push(EnvelopeError::MissingId);
    }
    if env.created_at.is_empty() {
        out.push(EnvelopeError::MissingCreatedAt);
    }
    if env.updated_at.is_empty() {
        out.push(EnvelopeError::MissingUpdatedAt);
    }
    if env.source.kind.is_empty() || env.source.version.is_empty() {
        out.push(EnvelopeError::SourceIncomplete);
    }

    if env.parent_id.is_some() != env.fork_point.is_some() {
        out.push(EnvelopeError::ForkPointParentMismatch);
    }
    if let Some(fp) = env.fork_point {
        if fp < 0 {
            out.push(EnvelopeError::ForkPointNegative);
        }
    }

    // dispatched_from is mutually exclusive with parent_id / fork_point
    // (envelope.md §7.2). Both members of DispatchedFrom must be set.
    if let Some(df) = env.dispatched_from.as_ref() {
        if env.parent_id.is_some() || env.fork_point.is_some() {
            out.push(EnvelopeError::DispatchedFromExclusiveWithFork);
        }
        if df.envelope_id.is_empty() || df.call_id.is_empty() {
            out.push(EnvelopeError::DispatchedFromIncomplete);
        }
    }

    // injected_turns entries: required tuple plus an
    // injected_after_turn_id that, when set, MUST already exist in this
    // Envelope's turns at the position the entry is read (envelope.md
    // §7.3 — forward references forbidden). Cross-source resolution is
    // out of scope here.
    if let Some(refs) = env.injected_turns.as_ref() {
        // Collect all turn ids up-front; entries reference turns by id
        // and the spec only forbids forward references — past turn ids
        // are fair game.
        let envelope_turn_ids: HashSet<&str> =
            env.turns.iter().map(|t| t.id.as_str()).collect();
        for (i, r) in refs.iter().enumerate() {
            if r.envelope_id.is_empty()
                || r.start_turn_id.is_empty()
                || r.end_turn_id.is_empty()
            {
                out.push(EnvelopeError::injected_turn(
                    i,
                    "envelope_id / start_turn_id / end_turn_id required",
                ));
            }
            if let Some(after) = r.injected_after_turn_id.as_deref() {
                if !envelope_turn_ids.contains(after) {
                    out.push(EnvelopeError::injected_turn(
                        i,
                        format!(
                            "injected_after_turn_id {after:?} not present in this envelope"
                        ),
                    ));
                }
            }
        }
    }

    // Track open tool_calls. A call goes open on tool_call and closes
    // on its matching tool_result. Consumed by:
    //   - tool_result.call_id linkage
    //   - tool_call.parent_call_id (MUST point at an open outer call)
    //   - Turn.in_reply_to_call_id (MUST point at an open call)
    let mut open_calls: HashSet<String> = HashSet::new();

    for (i, turn) in env.turns.iter().enumerate() {
        validate_turn(i, turn, &mut open_calls, &mut out);
    }

    out
}

/// Strict-decode `input` (rejecting unknown top-level fields) then
/// validate. Returns the union of decode + structural findings; on a
/// decode failure the returned Vec contains a single `Decode` entry.
pub fn validate_bytes(input: &[u8]) -> Vec<EnvelopeError> {
    let env: Envelope = match serde_json::from_slice(input) {
        Ok(e) => e,
        Err(err) => return vec![EnvelopeError::Decode(err)],
    };
    validate(&env)
}

fn crtx_version_compatible(v: &str) -> bool {
    if v == CRTX_VERSION {
        return true;
    }
    // 0.x: every minor is a breaking change; accept exact only.
    let want_major = CRTX_VERSION.split_once('.').map(|(m, _)| m);
    let got_major = v.split_once('.').map(|(m, _)| m);
    matches!((want_major, got_major), (Some("0"), Some("0"))) && v == CRTX_VERSION
}

fn validate_turn(
    index: usize,
    turn: &crate::envelope::Turn,
    open_calls: &mut HashSet<String>,
    out: &mut Vec<EnvelopeError>,
) {
    if turn.id.is_empty() {
        out.push(EnvelopeError::turn(index, "missing id"));
    }
    // Role: serde decode rejects unknown variants at parse time per
    // crtx spec §4; if a Turn exists here, its role is one of the
    // known variants. No runtime check needed.
    if turn.created_at.is_empty() {
        out.push(EnvelopeError::turn(index, "missing created_at"));
    }
    if turn.content.is_empty() {
        out.push(EnvelopeError::turn(index, "content[] must have >=1 part"));
    }

    // in_reply_to_call_id (envelope.md §3.2) MUST reference an open
    // tool_call at the point this Turn is appended. Tool-role Turns
    // SHOULD NOT carry it — tool_result IS the resolution of the call,
    // not an interjection.
    if let Some(call) = turn.in_reply_to_call_id.as_deref() {
        if turn.role == Role::Tool {
            out.push(EnvelopeError::turn(
                index,
                "in_reply_to_call_id: forbidden on tool-role turns",
            ));
        } else if !open_calls.contains(call) {
            out.push(EnvelopeError::turn(
                index,
                format!("in_reply_to_call_id {call:?} has no open tool_call"),
            ));
        }
    }

    for (j, part) in turn.content.iter().enumerate() {
        validate_content(index, j, part, open_calls, out);
    }
}

fn validate_content(
    turn_index: usize,
    part_index: usize,
    part: &ContentPart,
    open_calls: &mut HashSet<String>,
    out: &mut Vec<EnvelopeError>,
) {
    if part.is_extension() {
        if !is_valid_extension_type(part.type_name()) {
            out.push(EnvelopeError::content(
                turn_index,
                part_index,
                format!(
                    "extension type {:?} does not match ^x-[a-zA-Z0-9._-]+$",
                    part.type_name()
                ),
            ));
        }
        return;
    }

    match part {
        ContentPart::Text { .. } => {
            // Empty string permitted by schema (only "type" required).
        }
        ContentPart::ToolCall {
            call_id,
            name,
            parent_call_id,
            ..
        } => {
            if call_id.is_empty() {
                out.push(EnvelopeError::content(
                    turn_index,
                    part_index,
                    "tool_call: missing call_id",
                ));
            }
            if name.is_empty() {
                out.push(EnvelopeError::content(
                    turn_index,
                    part_index,
                    "tool_call: missing name",
                ));
            }
            // parent_call_id (envelope.md §6.2) MUST reference an outer
            // tool_call still open at this position.
            if let Some(p) = parent_call_id.as_deref() {
                if !open_calls.contains(p) {
                    out.push(EnvelopeError::content(
                        turn_index,
                        part_index,
                        format!(
                            "tool_call: parent_call_id {p:?} has no open outer tool_call"
                        ),
                    ));
                }
            }
            open_calls.insert(call_id.clone());
        }
        ContentPart::ToolResult { call_id, .. } => {
            if call_id.is_empty() {
                out.push(EnvelopeError::content(
                    turn_index,
                    part_index,
                    "tool_result: missing call_id",
                ));
            } else if !open_calls.contains(call_id) {
                out.push(EnvelopeError::content(
                    turn_index,
                    part_index,
                    format!("tool_result: call_id {call_id:?} has no preceding open tool_call"),
                ));
            } else {
                // Close the matched call so later turns can't interject
                // into it via in_reply_to_call_id, and nested calls
                // can't claim it as a parent.
                open_calls.remove(call_id);
            }
        }
        ContentPart::Image {
            mime, data, url, ..
        } => {
            if mime.is_empty() {
                out.push(EnvelopeError::content(
                    turn_index,
                    part_index,
                    "image: missing mime",
                ));
            } else if !is_valid_mime_type(mime) {
                out.push(EnvelopeError::content(
                    turn_index,
                    part_index,
                    format!("image: mime {mime:?} does not match ^[a-z]+/[a-zA-Z0-9.+-]+$"),
                ));
            }
            let has_data = data.is_some();
            let has_url = url.is_some();
            if has_data == has_url {
                out.push(EnvelopeError::content(
                    turn_index,
                    part_index,
                    "image: exactly one of data/url required",
                ));
            }
        }
        ContentPart::Thinking { .. } => {
            // Empty string permitted by schema; only the "text" key is
            // required (value may be ""). Mirrors text part rules.
        }
        ContentPart::Extension { .. } => unreachable!("handled above"),
    }
}
