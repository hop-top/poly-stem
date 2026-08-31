//! Multi-agent provenance validation cases — crtx v0.1 envelope.md
//! §3.1, §3.2, §6.2, §6.3, §7.2, §7.3. Mirrors the Go SDK's
//! "Multi-agent provenance" block at the bottom of go/validate_test.go.

use hop_top_stem::{
    parse_envelope, serialize_envelope, validate, ContentPart, DispatchedFrom, Envelope,
    EnvelopeError, InjectedTurnRef, Role, Turn,
};

fn now() -> String {
    "2026-05-30T09:00:00Z".to_string()
}

fn fresh_env(id: &str) -> Envelope {
    Envelope::new(id, now())
}

fn turn(id: &str, role: Role, parts: Vec<ContentPart>) -> Turn {
    Turn {
        id: id.to_string(),
        role,
        created_at: now(),
        content: parts,
        agent_id: None,
        in_reply_to_call_id: None,
        metadata: None,
    }
}

// --- dispatched_from --------------------------------------------------

#[test]
fn dispatched_from_mutually_exclusive_with_fork_fields() {
    let mut env = fresh_env("s-1");
    env.parent_id = Some("parent".to_string());
    env.fork_point = Some(0);
    env.dispatched_from = Some(DispatchedFrom {
        envelope_id: "p".to_string(),
        call_id: "c".to_string(),
    });
    let findings = validate(&env);
    assert!(
        findings
            .iter()
            .any(|e| matches!(e, EnvelopeError::DispatchedFromExclusiveWithFork)),
        "expected DispatchedFromExclusiveWithFork, got: {findings:?}"
    );
}

#[test]
fn dispatched_from_requires_both_members() {
    let mut env = fresh_env("s-1");
    env.dispatched_from = Some(DispatchedFrom {
        envelope_id: "p".to_string(),
        call_id: "".to_string(),
    });
    let findings = validate(&env);
    assert!(
        findings
            .iter()
            .any(|e| matches!(e, EnvelopeError::DispatchedFromIncomplete)),
        "expected DispatchedFromIncomplete, got: {findings:?}"
    );
}

#[test]
fn dispatched_from_alone_accepted() {
    let mut env = fresh_env("s-1");
    env.dispatched_from = Some(DispatchedFrom {
        envelope_id: "parent-env".to_string(),
        call_id: "call-1".to_string(),
    });
    let findings = validate(&env);
    assert!(findings.is_empty(), "findings: {findings:?}");
}

// --- tool_call.parent_call_id ----------------------------------------

#[test]
fn parent_call_id_requires_open_outer_call() {
    let inner = ContentPart::tool_call_nested("inner", "echo", serde_json::json!({}), "outer");
    let mut env = fresh_env("s-1");
    env.turns = vec![turn("a", Role::Assistant, vec![inner])];
    let findings = validate(&env);
    assert!(
        findings
            .iter()
            .any(|e| matches!(e, EnvelopeError::ContentPart { message, .. } if message.contains("parent_call_id"))),
        "expected parent_call_id error, got: {findings:?}"
    );
}

#[test]
fn parent_call_id_accepted_when_outer_open() {
    let outer = ContentPart::tool_call("outer", "dispatch", serde_json::json!({}));
    let inner = ContentPart::tool_call_nested("inner", "sub", serde_json::json!({}), "outer");
    let mut env = fresh_env("s-1");
    env.turns = vec![
        turn("a", Role::Assistant, vec![outer]),
        turn("b", Role::Assistant, vec![inner]),
    ];
    let findings = validate(&env);
    assert!(findings.is_empty(), "findings: {findings:?}");
}

#[test]
fn parent_call_id_rejected_after_outer_resolved() {
    let outer = ContentPart::tool_call("outer", "dispatch", serde_json::json!({}));
    let outer_result = ContentPart::tool_result("outer", serde_json::json!("done"), false);
    let inner = ContentPart::tool_call_nested("inner", "sub", serde_json::json!({}), "outer");
    let mut env = fresh_env("s-1");
    env.turns = vec![
        turn("a", Role::Assistant, vec![outer]),
        turn("t", Role::Tool, vec![outer_result]),
        turn("b", Role::Assistant, vec![inner]),
    ];
    let findings = validate(&env);
    assert!(
        findings
            .iter()
            .any(|e| matches!(e, EnvelopeError::ContentPart { message, .. } if message.contains("parent_call_id"))),
        "expected parent_call_id error after outer resolved, got: {findings:?}"
    );
}

// --- Turn.in_reply_to_call_id ----------------------------------------

#[test]
fn in_reply_to_call_id_requires_open_call() {
    let mut env = fresh_env("s-1");
    let mut t = turn("u", Role::User, vec![ContentPart::text("interjection")]);
    t.in_reply_to_call_id = Some("missing".to_string());
    env.turns = vec![t];
    let findings = validate(&env);
    assert!(
        findings
            .iter()
            .any(|e| matches!(e, EnvelopeError::Turn { message, .. } if message.contains("in_reply_to_call_id"))),
        "expected in_reply_to_call_id error, got: {findings:?}"
    );
}

#[test]
fn in_reply_to_call_id_forbidden_on_tool_role() {
    let tc = ContentPart::tool_call("c-1", "x", serde_json::json!({}));
    let tr = ContentPart::tool_result("c-1", serde_json::json!("out"), false);
    let mut env = fresh_env("s-1");
    let mut tool_turn = turn("t", Role::Tool, vec![tr]);
    tool_turn.in_reply_to_call_id = Some("c-1".to_string());
    env.turns = vec![turn("a", Role::Assistant, vec![tc]), tool_turn];
    let findings = validate(&env);
    assert!(
        findings.iter().any(|e| matches!(
            e,
            EnvelopeError::Turn { message, .. } if message.contains("forbidden on tool-role")
        )),
        "expected tool-role prohibition, got: {findings:?}"
    );
}

#[test]
fn in_reply_to_call_id_accepted_while_call_open() {
    let tc = ContentPart::tool_call("c-1", "dispatch", serde_json::json!({}));
    let tr = ContentPart::tool_result("c-1", serde_json::json!("out"), false);
    let mut interject = turn("u", Role::User, vec![ContentPart::text("update")]);
    interject.in_reply_to_call_id = Some("c-1".to_string());
    let mut env = fresh_env("s-1");
    env.turns = vec![
        turn("a", Role::Assistant, vec![tc]),
        interject,
        turn("t", Role::Tool, vec![tr]),
    ];
    let findings = validate(&env);
    assert!(findings.is_empty(), "findings: {findings:?}");
}

// --- agent_id + child_envelope_id round-trip --------------------------

#[test]
fn agent_id_accepted_on_assistant() {
    let mut env = fresh_env("s-1");
    let mut t = turn("a", Role::Assistant, vec![ContentPart::text("hi")]);
    t.agent_id = Some("C".to_string());
    env.turns = vec![t];
    let findings = validate(&env);
    assert!(findings.is_empty(), "findings: {findings:?}");
}

#[test]
fn child_envelope_id_round_trip_on_tool_result() {
    let tc = ContentPart::tool_call("c-1", "dispatch", serde_json::json!({}));
    let tr = ContentPart::tool_result_with_child(
        "c-1",
        serde_json::json!("done"),
        false,
        "child-env",
    );
    let mut env = fresh_env("s-1");
    env.turns = vec![
        turn("a", Role::Assistant, vec![tc]),
        turn("t", Role::Tool, vec![tr]),
    ];
    let findings = validate(&env);
    assert!(findings.is_empty(), "findings: {findings:?}");

    // Round-trip preserves child_envelope_id.
    let serialized = serialize_envelope(&env).unwrap();
    assert!(
        serialized.contains("\"child_envelope_id\":\"child-env\""),
        "expected child_envelope_id on wire, got: {serialized}"
    );
    let reparsed: Envelope = parse_envelope(&serialized).unwrap();
    assert_eq!(env, reparsed);
}

// --- injected_turns --------------------------------------------------

#[test]
fn injected_turns_require_all_three_ids() {
    let mut env = fresh_env("s-1");
    env.injected_turns = Some(vec![InjectedTurnRef {
        envelope_id: "src".to_string(),
        start_turn_id: "".to_string(),
        end_turn_id: "".to_string(),
        injected_after_turn_id: None,
    }]);
    let findings = validate(&env);
    assert!(
        findings.iter().any(|e| matches!(
            e,
            EnvelopeError::InjectedTurn { message, .. } if message.contains("start_turn_id")
        )),
        "expected InjectedTurn finding, got: {findings:?}"
    );
}

#[test]
fn injected_after_turn_id_must_exist_in_envelope() {
    let mut env = fresh_env("s-1");
    env.turns = vec![turn("t-1", Role::User, vec![ContentPart::text("hi")])];
    env.injected_turns = Some(vec![InjectedTurnRef {
        envelope_id: "src".to_string(),
        start_turn_id: "s".to_string(),
        end_turn_id: "s".to_string(),
        injected_after_turn_id: Some("does-not-exist".to_string()),
    }]);
    let findings = validate(&env);
    assert!(
        findings.iter().any(|e| matches!(
            e,
            EnvelopeError::InjectedTurn { message, .. } if message.contains("injected_after_turn_id")
        )),
        "expected InjectedTurn finding, got: {findings:?}"
    );
}

#[test]
fn injected_turns_creation_time_accepted() {
    let mut env = fresh_env("s-1");
    env.injected_turns = Some(vec![InjectedTurnRef {
        envelope_id: "src".to_string(),
        start_turn_id: "a".to_string(),
        end_turn_id: "b".to_string(),
        injected_after_turn_id: None,
    }]);
    let findings = validate(&env);
    assert!(findings.is_empty(), "findings: {findings:?}");
}
