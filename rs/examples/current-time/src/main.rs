//! current-time — smallest viable stem agent loop in Rust.
//!
//! Mirrors `go/examples/current-time/main.go` step-for-step:
//!
//!   1. Open the polyglot xrr cassette directory at
//!      `./cassettes/` in replay mode.
//!   2. Construct a fresh `stem::Envelope` and append a user turn
//!      asking the time.
//!   3. POST to Anthropic Messages API with a `current_time` tool
//!      definition (request body is read from the cassette).
//!   4. Project the assistant `tool_use` block into a stem `ToolCall`
//!      ContentPart and append an assistant turn.
//!   5. Execute the tool locally (returns a fixed timestamp).
//!   6. Append a tool-role turn carrying a `ToolResult` ContentPart.
//!   7. Second Anthropic call with the tool result echoed back.
//!   8. Append the final assistant text turn.
//!   9. Serialize the envelope to `./session.jsonl` and reload+validate.
//!
//! All HTTP traffic is intercepted via the cassette directory — no
//! API key required in default replay mode. The Anthropic request
//! bodies are reproduced verbatim from the Go-recorded cassettes so
//! the xrr fingerprint matches byte-for-byte.

mod cassette;

use std::path::PathBuf;

use anyhow::{bail, Context, Result};
use hop_top_stem::{
    parse_envelope, serialize_envelope, validate, ContentPart, Envelope, Role, Turn,
};

use crate::cassette::{replay, CassetteDir};

const CASSETTE_DIR: &str = "./cassettes";
const ANTHROPIC_URL: &str = "https://api.anthropic.com/v1/messages";
const JSONL_PATH: &str = "./session.jsonl";

// Bodies copied verbatim from the recorded cassette so fingerprints
// match byte-for-byte under xrr replay. Re-recording from Go is the
// source of truth for any reshape.
const FIRST_BODY: &str = r#"{"max_tokens":1024,"messages":[{"content":[{"text":"What time is it?","type":"text"}],"role":"user"}],"model":"claude-sonnet-4-6","tools":[{"input_schema":{"properties":{},"required":[],"type":"object"},"name":"current_time","description":"Returns the current UTC time as an ISO 8601 string."}]}"#;
const SECOND_BODY: &str = r#"{"max_tokens":1024,"messages":[{"content":[{"text":"What time is it?","type":"text"}],"role":"user"},{"content":[{"id":"toolu_synth_time_1","input":{},"name":"current_time","type":"tool_use"}],"role":"assistant"},{"content":[{"tool_use_id":"toolu_synth_time_1","is_error":false,"content":[{"text":"{\"time\":\"2026-01-01T00:00:00Z\",\"zone\":\"UTC\"}","type":"text"}],"type":"tool_result"}],"role":"user"}],"model":"claude-sonnet-4-6","tools":[{"input_schema":{"properties":{},"required":[],"type":"object"},"name":"current_time","description":"Returns the current UTC time as an ISO 8601 string."}]}"#;

fn main() -> Result<()> {
    let cassette_dir = resolve_cassette_dir()?;
    let cassettes = CassetteDir::new(cassette_dir);

    // --- 2. fresh stem Envelope + initial user turn.
    let mut env = Envelope::new("current-time-demo", "1970-01-01T00:00:00Z");
    env.turns.push(Turn {
        id: "t-user-0".to_string(),
        role: Role::User,
        created_at: "1970-01-01T00:00:00Z".to_string(),
        content: vec![ContentPart::text("What time is it?")],
        agent_id: None,
        in_reply_to_call_id: None,
        metadata: None,
    });

    // --- 3. first Anthropic call (replayed from cassette).
    let (status, first_body) = replay(&cassettes, "POST", ANTHROPIC_URL, FIRST_BODY)
        .context("first anthropic call (replay)")?;
    if status != 200 {
        bail!("anthropic first call: status {}", status);
    }
    let first: serde_json::Value =
        serde_json::from_str(&first_body).context("decode first anthropic body")?;

    // --- 4. project assistant tool_use into a stem ToolCall.
    let (assistant_turn, tool_use_id, tool_input) =
        assistant_turn_from_message(&first, "t-assistant-1")?;
    env.turns.push(assistant_turn);
    let tool_use_id = tool_use_id.context("model returned no tool_use; cassette out of sync")?;

    // --- 5. local tool exec.
    let tool_output = execute_current_time(&tool_input);

    // --- 6. tool-role turn.
    let result_part = ContentPart::tool_result(&tool_use_id, tool_output.clone(), false);
    env.turns.push(Turn {
        id: "t-tool-2".to_string(),
        role: Role::Tool,
        created_at: "1970-01-01T00:00:00Z".to_string(),
        content: vec![result_part],
        agent_id: None,
        in_reply_to_call_id: None,
        metadata: None,
    });

    // --- 7. second Anthropic call.
    let (status, second_body) = replay(&cassettes, "POST", ANTHROPIC_URL, SECOND_BODY)
        .context("second anthropic call (replay)")?;
    if status != 200 {
        bail!("anthropic second call: status {}", status);
    }
    let second: serde_json::Value =
        serde_json::from_str(&second_body).context("decode second anthropic body")?;

    // --- 8. final assistant text turn.
    let (final_turn, _, _) = assistant_turn_from_message(&second, "t-assistant-3")?;
    env.turns.push(final_turn);

    // --- 9. write session.jsonl, reload, validate.
    let json = serialize_envelope(&env).context("serialize envelope")?;
    std::fs::write(JSONL_PATH, format!("{json}\n"))
        .with_context(|| format!("write {JSONL_PATH}"))?;
    println!("wrote {JSONL_PATH} ({} turns)", env.turns.len());

    let reloaded_raw = std::fs::read_to_string(JSONL_PATH).context("reload jsonl")?;
    let reloaded: Envelope = parse_envelope(reloaded_raw.trim()).context("parse reloaded")?;
    let findings = validate(&reloaded);
    if !findings.is_empty() {
        bail!("validate findings: {findings:?}");
    }
    println!("envelope round-trip + validate: ok");
    Ok(())
}

fn resolve_cassette_dir() -> Result<PathBuf> {
    let manifest_dir = std::env::var("CARGO_MANIFEST_DIR")
        .context("CARGO_MANIFEST_DIR not set (run via `cargo run`)")?;
    let mut p = PathBuf::from(manifest_dir);
    for seg in CASSETTE_DIR.split('/') {
        p.push(seg);
    }
    Ok(p)
}

/// Project an Anthropic Messages response into a stem assistant Turn
/// and surface the first tool_use id+input found, if any.
fn assistant_turn_from_message(
    msg: &serde_json::Value,
    turn_id: &str,
) -> Result<(Turn, Option<String>, serde_json::Value)> {
    let content = msg
        .get("content")
        .and_then(|v| v.as_array())
        .context("anthropic message: missing content[]")?;

    let mut parts: Vec<ContentPart> = Vec::new();
    let mut tool_id: Option<String> = None;
    let mut tool_input: serde_json::Value = serde_json::Value::Null;

    for block in content {
        let block_type = block
            .get("type")
            .and_then(|v| v.as_str())
            .unwrap_or_default();
        match block_type {
            "text" => {
                let text = block.get("text").and_then(|v| v.as_str()).unwrap_or("");
                parts.push(ContentPart::text(text));
            }
            "tool_use" => {
                let id = block.get("id").and_then(|v| v.as_str()).unwrap_or("");
                let name = block.get("name").and_then(|v| v.as_str()).unwrap_or("");
                let input = block
                    .get("input")
                    .cloned()
                    .unwrap_or(serde_json::Value::Object(serde_json::Map::new()));
                parts.push(ContentPart::tool_call(id, name, input.clone()));
                if tool_id.is_none() {
                    tool_id = Some(id.to_string());
                    tool_input = input;
                }
            }
            other => {
                eprintln!("ignoring unknown anthropic block type {other:?}");
            }
        }
    }

    if parts.is_empty() {
        bail!("anthropic message had no usable content blocks");
    }

    Ok((
        Turn {
            id: turn_id.to_string(),
            role: Role::Assistant,
            created_at: "1970-01-01T00:00:00Z".to_string(),
            content: parts,
            agent_id: None,
            in_reply_to_call_id: None,
            metadata: None,
        },
        tool_id,
        tool_input,
    ))
}

/// Local handler for the `current_time` tool. Returns a fixed
/// timestamp so the recorded cassette stays stable.
fn execute_current_time(_input: &serde_json::Value) -> serde_json::Value {
    serde_json::json!({
        "time": "2026-01-01T00:00:00Z",
        "zone": "UTC",
    })
}
