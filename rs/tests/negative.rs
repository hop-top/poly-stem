//! Negative cases: malformed envelopes the SDK must reject.

use hop_top_stem::{
    parse_envelope, serialize_envelope, validate, validate_bytes, EnvelopeError, Turn,
};

#[test]
fn bad_crtx_version_rejected_by_validate() {
    let raw = r#"{
  "crtx_version": "9.0",
  "id": "s1",
  "created_at": "2026-01-01T00:00:00Z",
  "updated_at": "2026-01-01T00:00:00Z",
  "source": {"kind": "stem", "version": "0.1.0"},
  "turns": []
}"#;
    let env = parse_envelope(raw).unwrap();
    let findings = validate(&env);
    assert!(findings
        .iter()
        .any(|e| matches!(e, EnvelopeError::UnsupportedCrtxVersion { .. })));
}

#[test]
fn missing_required_top_level_field_rejected_by_decode() {
    // Drop "id" — required by Envelope struct.
    let raw = r#"{
  "crtx_version": "0.1",
  "created_at": "2026-01-01T00:00:00Z",
  "updated_at": "2026-01-01T00:00:00Z",
  "source": {"kind": "stem", "version": "0.1.0"},
  "turns": []
}"#;
    assert!(parse_envelope(raw).is_err(), "missing id must fail decode");
}

#[test]
fn invalid_role_rejected_by_validate() {
    // Per crtx spec §4 implementations MUST reject unknown roles. The
    // `Role::Unknown` catch-all variant was removed, so the failure
    // now surfaces at decode time rather than validate time.
    let raw = r#"{
  "crtx_version": "0.1",
  "id": "s1",
  "created_at": "2026-01-01T00:00:00Z",
  "updated_at": "2026-01-01T00:00:00Z",
  "source": {"kind": "stem", "version": "0.1.0"},
  "turns": [{
    "id": "t1",
    "role": "wizard",
    "created_at": "2026-01-01T00:00:00Z",
    "content": [{"type": "text", "text": "hi"}]
  }]
}"#;
    assert!(
        parse_envelope(raw).is_err(),
        "unknown role must fail decode"
    );
}

#[test]
fn unknown_role_string_fails_decode() {
    // Tighter scope: just the Turn payload — confirms the rejection
    // lives at decode time on the Role enum, not on the surrounding
    // structure.
    let raw = r#"{"id":"t","role":"overlord","created_at":"2026-01-01T00:00:00Z","content":[]}"#;
    let res: Result<Turn, _> = serde_json::from_str(raw);
    assert!(res.is_err(), "decode should reject unknown role variant");
}

#[test]
fn image_with_both_data_and_url_rejected_by_validate() {
    let raw = r#"{
  "crtx_version": "0.1",
  "id": "s1",
  "created_at": "2026-01-01T00:00:00Z",
  "updated_at": "2026-01-01T00:00:00Z",
  "source": {"kind": "stem", "version": "0.1.0"},
  "turns": [{
    "id": "t1",
    "role": "user",
    "created_at": "2026-01-01T00:00:00Z",
    "content": [{"type": "image", "mime": "image/png", "data": "aGk=", "url": "https://e.x/p.png"}]
  }]
}"#;
    let env = parse_envelope(raw).unwrap();
    let findings = validate(&env);
    assert!(findings
        .iter()
        .any(|e| matches!(e, EnvelopeError::ContentPart { message, .. } if message.contains("exactly one"))));
}

#[test]
fn image_with_neither_data_nor_url_rejected_by_validate_and_serialize() {
    let raw = r#"{
  "crtx_version": "0.1",
  "id": "s1",
  "created_at": "2026-01-01T00:00:00Z",
  "updated_at": "2026-01-01T00:00:00Z",
  "source": {"kind": "stem", "version": "0.1.0"},
  "turns": [{
    "id": "t1",
    "role": "user",
    "created_at": "2026-01-01T00:00:00Z",
    "content": [{"type": "image", "mime": "image/png"}]
  }]
}"#;
    let env = parse_envelope(raw).unwrap();
    let findings = validate(&env);
    assert!(findings
        .iter()
        .any(|e| matches!(e, EnvelopeError::ContentPart { message, .. } if message.contains("exactly one"))));

    // Serialization must also reject the producer-time foot-gun.
    assert!(
        serialize_envelope(&env).is_err(),
        "serializing invalid image part must fail"
    );
}

#[test]
fn malformed_extension_type_rejected_by_validate() {
    let raw = r#"{
  "crtx_version": "0.1",
  "id": "s1",
  "created_at": "2026-01-01T00:00:00Z",
  "updated_at": "2026-01-01T00:00:00Z",
  "source": {"kind": "stem", "version": "0.1.0"},
  "turns": [{
    "id": "t1",
    "role": "user",
    "created_at": "2026-01-01T00:00:00Z",
    "content": [{"type": "x-bad space"}]
  }]
}"#;
    let env = parse_envelope(raw).unwrap();
    let findings = validate(&env);
    assert!(findings
        .iter()
        .any(|e| matches!(e, EnvelopeError::ContentPart { message, .. } if message.contains("extension type"))));
}

#[test]
fn orphan_tool_result_rejected_by_validate() {
    let raw = r#"{
  "crtx_version": "0.1",
  "id": "s1",
  "created_at": "2026-01-01T00:00:00Z",
  "updated_at": "2026-01-01T00:00:00Z",
  "source": {"kind": "stem", "version": "0.1.0"},
  "turns": [{
    "id": "t1",
    "role": "tool",
    "created_at": "2026-01-01T00:00:00Z",
    "content": [{"type": "tool_result", "call_id": "orphan", "output": null}]
  }]
}"#;
    let env = parse_envelope(raw).unwrap();
    let findings = validate(&env);
    assert!(findings.iter().any(|e| matches!(
        e,
        EnvelopeError::ContentPart { message, .. } if message.contains("no preceding open tool_call")
    )));
}

#[test]
fn validate_bytes_rejects_unknown_top_level_fields() {
    let raw = br#"{
  "crtx_version": "0.1",
  "id": "s1",
  "created_at": "2026-01-01T00:00:00Z",
  "updated_at": "2026-01-01T00:00:00Z",
  "source": {"kind": "stem", "version": "0.1.0"},
  "turns": [],
  "bogus_unknown_top_level": "drift"
}"#;
    let findings = validate_bytes(raw);
    assert!(findings
        .iter()
        .any(|e| matches!(e, EnvelopeError::Decode(_))));
}

#[test]
fn validate_bytes_accepts_clean() {
    let raw = br#"{
  "crtx_version": "0.1",
  "id": "s1",
  "created_at": "2026-01-01T00:00:00Z",
  "updated_at": "2026-01-01T00:00:00Z",
  "source": {"kind": "stem", "version": "0.1.0"},
  "turns": []
}"#;
    assert!(validate_bytes(raw).is_empty());
}

#[test]
fn parent_id_without_fork_point_rejected_by_validate() {
    // Envelope-shape only: build via JSON so the optional fields are
    // explicit in the input.
    let raw = r#"{
  "crtx_version": "0.1",
  "id": "child",
  "parent_id": "parent-x",
  "created_at": "2026-01-01T00:00:00Z",
  "updated_at": "2026-01-01T00:00:00Z",
  "source": {"kind": "stem", "version": "0.1.0"},
  "turns": []
}"#;
    let env = parse_envelope(raw).unwrap();
    let findings = validate(&env);
    assert!(findings
        .iter()
        .any(|e| matches!(e, EnvelopeError::ForkPointParentMismatch)));
}

#[test]
fn empty_thinking_text_accepted_by_validate() {
    // thinking.text "" is valid per schema; only the key is required.
    let raw = r#"{
  "crtx_version": "0.1",
  "id": "s1",
  "created_at": "2026-01-01T00:00:00Z",
  "updated_at": "2026-01-01T00:00:00Z",
  "source": {"kind": "stem", "version": "0.1.0"},
  "turns": [{
    "id": "t1",
    "role": "assistant",
    "created_at": "2026-01-01T00:00:00Z",
    "content": [{"type": "thinking", "text": ""}]
  }]
}"#;
    let env = parse_envelope(raw).unwrap();
    let findings = validate(&env);
    assert!(findings.is_empty(), "findings: {findings:?}");
}

#[test]
fn bad_image_mime_rejected_by_validate() {
    // crtx v0.1 image.mime regex ^[a-z]+/[a-zA-Z0-9.+-]+$ — validate
    // must reject a non-conformant literal.
    let raw = r#"{
  "crtx_version": "0.1",
  "id": "s1",
  "created_at": "2026-01-01T00:00:00Z",
  "updated_at": "2026-01-01T00:00:00Z",
  "source": {"kind": "stem", "version": "0.1.0"},
  "turns": [{
    "id": "t1",
    "role": "user",
    "created_at": "2026-01-01T00:00:00Z",
    "content": [{"type": "image", "mime": "NOT_A_MIME", "data": "aGk="}]
  }]
}"#;
    let env = parse_envelope(raw).unwrap();
    let findings = validate(&env);
    assert!(findings.iter().any(
        |e| matches!(e, EnvelopeError::ContentPart { message, .. } if message.contains("mime"))
    ));
}

#[test]
fn empty_tool_call_input_accepted_by_validate() {
    // tool_call.input: {} is valid — tools may genuinely take no args
    // (current_time, etc.). Schema has no minProperties; SDK must accept.
    let raw = r#"{
  "crtx_version": "0.1",
  "id": "s1",
  "created_at": "2026-01-01T00:00:00Z",
  "updated_at": "2026-01-01T00:00:00Z",
  "source": {"kind": "stem", "version": "0.1.0"},
  "turns": [
    {"id": "a", "role": "assistant", "created_at": "2026-01-01T00:00:00Z",
     "content": [{"type": "tool_call", "call_id": "c1", "name": "current_time", "input": {}}]},
    {"id": "t", "role": "tool", "created_at": "2026-01-01T00:00:00Z",
     "content": [{"type": "tool_result", "call_id": "c1", "output": {"now": "2026"}}]}
  ]
}"#;
    let env = parse_envelope(raw).unwrap();
    let findings = validate(&env);
    assert!(findings.is_empty(), "findings: {findings:?}");
}

#[test]
fn fork_point_without_parent_id_rejected_by_validate() {
    // Inverse of parent_id_without_fork_point: a fork_point with no
    // parent_id is a missing pair too. Validate must reject either
    // direction.
    let raw = r#"{
  "crtx_version": "0.1",
  "id": "child",
  "fork_point": 0,
  "created_at": "2026-01-01T00:00:00Z",
  "updated_at": "2026-01-01T00:00:00Z",
  "source": {"kind": "stem", "version": "0.1.0"},
  "turns": []
}"#;
    let env = parse_envelope(raw).unwrap();
    let findings = validate(&env);
    assert!(findings
        .iter()
        .any(|e| matches!(e, EnvelopeError::ForkPointParentMismatch)));
}

#[test]
fn vendored_examples_validate_clean() {
    for (label, raw) in [
        ("minimal", include_str!("testdata/crtx_v0.1/minimal.json")),
        ("fork", include_str!("testdata/crtx_v0.1/fork.json")),
        (
            "tool-call",
            include_str!("testdata/crtx_v0.1/tool-call.json"),
        ),
        ("dispatch", include_str!("testdata/crtx_v0.1/dispatch.json")),
        (
            "dispatch-nested",
            include_str!("testdata/crtx_v0.1/dispatch-nested.json"),
        ),
        (
            "injection",
            include_str!("testdata/crtx_v0.1/injection.json"),
        ),
    ] {
        let env = parse_envelope(raw).unwrap_or_else(|e| panic!("{label}: {e}"));
        let findings = validate(&env);
        assert!(
            findings.is_empty(),
            "{label} produced findings: {findings:?}"
        );
    }
}
