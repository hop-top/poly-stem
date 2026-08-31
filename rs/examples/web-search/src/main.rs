//! web-search — second stem agent example in Rust.
//!
//! Same envelope-wiring shape as `../current-time/`, but the tool
//! `web_search` makes a real-shaped POST to the Tavily search API. A
//! single envelope captures the full transcript; both upstream
//! services (Anthropic + Tavily) replay from the same cassette
//! directory.
//!
//! Steps mirror `go/examples/web-search/main.go` step-for-step.

mod cassette;

use std::path::PathBuf;

use anyhow::{bail, Context, Result};
use hop_top_stem::{
    parse_envelope, serialize_envelope, validate, ContentPart, Envelope, Role, Turn,
};

use crate::cassette::{replay, CassetteDir};

const CASSETTE_DIR: &str = "./cassettes";
const ANTHROPIC_URL: &str = "https://api.anthropic.com/v1/messages";
const TAVILY_URL: &str = "https://api.tavily.com/search";
const JSONL_PATH: &str = "./session.jsonl";

// Bodies copied verbatim from the recorded cassettes so the xrr
// fingerprints match byte-for-byte under replay. See ../current-time/
// for the rationale.
const ANTHROPIC_FIRST_BODY: &str = r#"{"max_tokens":1024,"messages":[{"content":[{"text":"What's the latest stable Go release?","type":"text"}],"role":"user"}],"model":"claude-sonnet-4-6","tools":[{"input_schema":{"properties":{"query":{"description":"Search query string.","type":"string"}},"required":["query"],"type":"object"},"name":"web_search","description":"Run a web search and return the top results."}]}"#;
const TAVILY_BODY: &str = r#"{"api_key":"replay-mode-no-key-required","max_results":3,"query":"latest stable Go release version"}"#;
const ANTHROPIC_SECOND_BODY: &str = r#"{"max_tokens":1024,"messages":[{"content":[{"text":"What's the latest stable Go release?","type":"text"}],"role":"user"},{"content":[{"text":"I'll search the web for the latest stable Go release.","type":"text"},{"id":"toolu_synth_ws_1","input":{"query":"latest stable Go release version"},"name":"web_search","type":"tool_use"}],"role":"assistant"},{"content":[{"tool_use_id":"toolu_synth_ws_1","is_error":false,"content":[{"text":"{\"query\":\"latest stable Go release version\",\"results\":[{\"title\":\"Go 1.24 Release Notes - The Go Programming Language\",\"url\":\"https://go.dev/doc/go1.24\",\"snippet\":\"Go 1.24 is the latest stable release of the Go programming language, released in February 2025. It introduces generic type aliases, weak pointers, and a new tools directive for go.mod.\"},{\"title\":\"Releases - Go\",\"url\":\"https://go.dev/dl/\",\"snippet\":\"The Go team announces stable releases at https://go.dev/dl/. The current stable release is Go 1.24.x; previous stable line is Go 1.23.x.\"},{\"title\":\"Go Release History\",\"url\":\"https://go.dev/doc/devel/release\",\"snippet\":\"Go follows a six-month release cadence. Recent stable releases: 1.24 (Feb 2025), 1.23 (Aug 2024), 1.22 (Feb 2024).\"}]}","type":"text"}],"type":"tool_result"}],"role":"user"}],"model":"claude-sonnet-4-6","tools":[{"input_schema":{"properties":{"query":{"description":"Search query string.","type":"string"}},"required":["query"],"type":"object"},"name":"web_search","description":"Run a web search and return the top results."}]}"#;

fn main() -> Result<()> {
    let cassette_dir = resolve_cassette_dir()?;
    let cassettes = CassetteDir::new(cassette_dir);

    // --- 2. fresh envelope + user turn.
    let mut env = Envelope::new("web-search-demo", "1970-01-01T00:00:00Z");
    env.turns.push(Turn {
        id: "t-user-0".to_string(),
        role: Role::User,
        created_at: "1970-01-01T00:00:00Z".to_string(),
        content: vec![ContentPart::text("What's the latest stable Go release?")],
        agent_id: None,
        in_reply_to_call_id: None,
        metadata: None,
    });

    // --- 3. first Anthropic call.
    let (status, first_body) = replay(&cassettes, "POST", ANTHROPIC_URL, ANTHROPIC_FIRST_BODY)
        .context("anthropic first call (replay)")?;
    if status != 200 {
        bail!("anthropic first call: status {}", status);
    }
    let first: serde_json::Value =
        serde_json::from_str(&first_body).context("decode first anthropic body")?;

    // --- 4. project assistant tool_use.
    let (assistant_turn, tool_use_id, _tool_input) =
        assistant_turn_from_message(&first, "t-assistant-1")?;
    env.turns.push(assistant_turn);
    let tool_use_id = tool_use_id.context("model returned no tool_use; cassette out of sync")?;

    // --- 5. tool exec via Tavily replay.
    let (tavily_status, tavily_body) =
        replay(&cassettes, "POST", TAVILY_URL, TAVILY_BODY).context("tavily (replay)")?;
    if tavily_status != 200 {
        bail!("tavily: status {}", tavily_status);
    }
    let tavily: serde_json::Value =
        serde_json::from_str(&tavily_body).context("decode tavily body")?;
    let normalized = normalize_tavily(&tavily);

    // --- 6. tool-role turn.
    env.turns.push(Turn {
        id: "t-tool-2".to_string(),
        role: Role::Tool,
        created_at: "1970-01-01T00:00:00Z".to_string(),
        content: vec![ContentPart::tool_result(&tool_use_id, normalized, false)],
        agent_id: None,
        in_reply_to_call_id: None,
        metadata: None,
    });

    // --- 7. second Anthropic call.
    let (status, second_body) = replay(&cassettes, "POST", ANTHROPIC_URL, ANTHROPIC_SECOND_BODY)
        .context("anthropic second call (replay)")?;
    if status != 200 {
        bail!("anthropic second call: status {}", status);
    }
    let second: serde_json::Value =
        serde_json::from_str(&second_body).context("decode second anthropic body")?;

    // --- 8. final assistant turn.
    let (final_turn, _, _) = assistant_turn_from_message(&second, "t-assistant-3")?;
    env.turns.push(final_turn);

    // --- 9. write + reload + validate.
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
            _ => {}
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

/// Normalize Tavily's response shape into the 3-tuple shape the Go
/// example feeds back to the model. `content` → `snippet` rename.
fn normalize_tavily(raw: &serde_json::Value) -> serde_json::Value {
    let query = raw
        .get("query")
        .and_then(|v| v.as_str())
        .unwrap_or_default();
    let mut results: Vec<serde_json::Value> = Vec::new();
    if let Some(arr) = raw.get("results").and_then(|v| v.as_array()) {
        for r in arr {
            let title = r.get("title").and_then(|v| v.as_str()).unwrap_or_default();
            let url = r.get("url").and_then(|v| v.as_str()).unwrap_or_default();
            let snippet = r
                .get("content")
                .and_then(|v| v.as_str())
                .unwrap_or_default();
            results.push(serde_json::json!({
                "title": title,
                "url": url,
                "snippet": snippet,
            }));
        }
    }
    serde_json::json!({
        "query": query,
        "results": results,
    })
}
