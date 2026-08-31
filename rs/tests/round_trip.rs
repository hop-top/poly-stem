//! Round-trip the vendored crtx v0.1 examples: parse → serialize →
//! reparse → assert canonical equality.
//!
//! Whitespace-insensitive equality is enforced by comparing
//! `serde_json::Value` trees of the original and re-serialized JSON.

use hop_top_stem::{parse_envelope, serialize_envelope, Envelope};

fn assert_round_trip(label: &str, raw: &str) {
    let env: Envelope = parse_envelope(raw).expect(label);
    let out = serialize_envelope(&env).expect(label);
    let reparsed: Envelope = parse_envelope(&out).expect(label);
    assert_eq!(env, reparsed, "Envelope inequality on round-trip ({label})");

    // Canonical JSON-tree equality between the on-disk fixture and the
    // re-serialized envelope.
    let original_tree: serde_json::Value = serde_json::from_str(raw).unwrap();
    let serialized_tree: serde_json::Value = serde_json::from_str(&out).unwrap();
    assert_eq!(
        original_tree, serialized_tree,
        "canonical JSON drift on round-trip ({label})"
    );
}

#[test]
fn round_trip_minimal() {
    let raw = include_str!("testdata/crtx_v0.1/minimal.json");
    assert_round_trip("minimal", raw);
}

#[test]
fn round_trip_fork() {
    let raw = include_str!("testdata/crtx_v0.1/fork.json");
    assert_round_trip("fork", raw);
}

#[test]
fn round_trip_tool_call() {
    let raw = include_str!("testdata/crtx_v0.1/tool-call.json");
    assert_round_trip("tool-call", raw);
}

#[test]
fn round_trip_dispatch() {
    let raw = include_str!("testdata/crtx_v0.1/dispatch.json");
    assert_round_trip("dispatch", raw);
}

#[test]
fn round_trip_dispatch_nested() {
    let raw = include_str!("testdata/crtx_v0.1/dispatch-nested.json");
    assert_round_trip("dispatch-nested", raw);
}

#[test]
fn round_trip_injection() {
    let raw = include_str!("testdata/crtx_v0.1/injection.json");
    assert_round_trip("injection", raw);
}

#[test]
fn empty_turns_serialize_as_array_not_null() {
    let env = Envelope::new("demo", "2026-01-01T00:00:00Z");
    let out = serialize_envelope(&env).unwrap();
    // turns: [] must appear; turns: null must not.
    assert!(
        out.contains("\"turns\":[]"),
        "expected empty turns array, got: {out}"
    );
    assert!(!out.contains("\"turns\":null"));
}

#[test]
fn extension_part_round_trips_verbatim() {
    let raw = r#"{
  "crtx_version": "0.1",
  "id": "ext-1",
  "created_at": "2026-01-01T00:00:00Z",
  "updated_at": "2026-01-01T00:00:00Z",
  "source": {"kind": "stem", "version": "0.1.0"},
  "turns": [{
    "id": "t1",
    "role": "user",
    "created_at": "2026-01-01T00:00:00Z",
    "content": [{"type": "x-io.jadb.custom", "weird": [1, 2, 3], "more": {"nested": true}}]
  }]
}"#;
    let env: Envelope = parse_envelope(raw).unwrap();
    let out = serialize_envelope(&env).unwrap();
    let reparsed: Envelope = parse_envelope(&out).unwrap();
    assert_eq!(env, reparsed);

    let original_tree: serde_json::Value = serde_json::from_str(raw).unwrap();
    let serialized_tree: serde_json::Value = serde_json::from_str(&out).unwrap();
    assert_eq!(original_tree, serialized_tree);
}
