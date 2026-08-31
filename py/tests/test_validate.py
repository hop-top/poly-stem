"""Structural validation negative-case tests (mirrors validate_test.go)."""
from __future__ import annotations

import json

import pytest

from stem import (
    DispatchedFrom,
    Envelope,
    ImagePart,
    InjectedTurnRef,
    Role,
    Source,
    TextPart,
    ThinkingPart,
    ToolCallPart,
    ToolResultPart,
    Turn,
    parse_envelope,
    serialize_envelope,
    validate,
    validate_bytes,
)


def _base_env(**overrides) -> Envelope:
    env = Envelope(
        crtx_version="0.1",
        id="s-1",
        created_at="2026-05-28T14:00:00Z",
        updated_at="2026-05-28T14:00:00Z",
        source=Source(kind="stem", version="0.1.0"),
    )
    for k, v in overrides.items():
        setattr(env, k, v)
    return env


def _has(errors, path: str) -> bool:
    """Exact path match. Replaces the previous substring helper which
    masked real regressions (e.g. ``"id" in "parent_id: foo"`` is True).
    Use ``_has_msg`` to assert on message contents.
    """
    return any(e.path == path for e in errors)


def _has_msg(errors, fragment: str) -> bool:
    """Substring match on the error *message* (not path). Use when you
    are asserting "some error mentions X" without pinning to a specific
    code site.
    """
    return any(fragment in e.message for e in errors)


def test_validate_nil_envelope() -> None:
    errs = validate(None)  # type: ignore[arg-type]
    assert _has_msg(errs, "nil envelope")


def test_validate_requires_crtx_version() -> None:
    errs = validate(_base_env(crtx_version=""))
    assert _has(errs, "crtx_version")


def test_validate_rejects_bad_crtx_version() -> None:
    errs = validate(_base_env(crtx_version="1.0"))
    assert _has(errs, "crtx_version")


def test_validate_rejects_malformed_crtx_version() -> None:
    errs = validate(_base_env(crtx_version="1"))
    assert _has(errs, "crtx_version")


def test_validate_requires_id() -> None:
    errs = validate(_base_env(id=""))
    assert _has(errs, "id")


def test_validate_requires_source_kind_and_version() -> None:
    errs = validate(_base_env(source=Source(kind="", version="")))
    assert _has(errs, "source")


def test_validate_rejects_source_kind_only() -> None:
    errs = validate(_base_env(source=Source(kind="stem", version="")))
    assert _has(errs, "source")


def test_validate_rejects_source_version_only() -> None:
    errs = validate(_base_env(source=Source(kind="", version="0.1.0")))
    assert _has(errs, "source")


def test_validate_fork_point_requires_parent_id() -> None:
    env = _base_env()
    env.fork_point = 0
    errs = validate(env)
    assert _has(errs, "parent_id")


def test_validate_parent_id_requires_fork_point() -> None:
    env = _base_env()
    env.parent_id = "p"
    errs = validate(env)
    assert _has(errs, "parent_id")


def test_validate_fork_point_negative_rejected() -> None:
    env = _base_env()
    env.parent_id = "p"
    env.fork_point = -1
    errs = validate(env)
    assert _has(errs, "fork_point")


def test_validate_rejects_turn_with_empty_id() -> None:
    env = _base_env()
    env.turns = [
        Turn(
            id="",
            role=Role.USER,
            created_at="2026-05-28T14:00:00Z",
            content=[TextPart(text="hi")],
        )
    ]
    errs = validate(env)
    assert _has(errs, "turns[0].id")


def test_validate_rejects_empty_content() -> None:
    env = _base_env()
    env.turns = [Turn(id="t", role=Role.USER, created_at="2026-05-28T14:00:00Z", content=[])]
    errs = validate(env)
    assert _has(errs, "turns[0].content")


def test_validate_all_roles_accepted() -> None:
    now = "2026-05-28T14:00:00Z"
    for r in (Role.USER, Role.ASSISTANT, Role.SYSTEM, Role.DEVELOPER):
        env = _base_env(id=f"s-{r.value}")
        env.turns = [Turn(id="t-1", role=r, created_at=now, content=[TextPart(text="ok")])]
        assert validate(env) == [], f"role {r.value} must validate"

    # tool role needs preceding tool_call.
    env = _base_env(id="s-tool")
    env.turns = [
        Turn(
            id="t-call",
            role=Role.ASSISTANT,
            created_at=now,
            content=[ToolCallPart(call_id="c1", name="x", input={})],
        ),
        Turn(
            id="t-res",
            role=Role.TOOL,
            created_at=now,
            content=[ToolResultPart(call_id="c1", output={"ok": True})],
        ),
    ]
    assert validate(env) == []


def test_validate_tool_result_requires_prior_tool_call() -> None:
    env = _base_env()
    env.turns = [
        Turn(
            id="t1",
            role=Role.TOOL,
            created_at="2026-05-28T14:00:00Z",
            content=[ToolResultPart(call_id="orphan", output={})],
        )
    ]
    errs = validate(env)
    assert _has_msg(errs, "no preceding open tool_call")


def test_source_from_dict_rejects_empty_kind() -> None:
    """Source.from_dict rejects empty kind at decode for symmetry with
    the content-part discriminator strictness — validate would catch it
    too, but the boundary should refuse to construct missing primaries.
    """
    with pytest.raises(ValueError, match="kind required"):
        Source.from_dict({"kind": "", "version": "0.1.0"})


def test_source_from_dict_rejects_empty_version() -> None:
    with pytest.raises(ValueError, match="version required"):
        Source.from_dict({"kind": "stem", "version": ""})


def test_validate_accepts_empty_thinking_text() -> None:
    """thinking.text="" is permitted by schema; only the key is required."""
    env = _base_env()
    env.turns = [
        Turn(
            id="t",
            role=Role.ASSISTANT,
            created_at="2026-05-28T14:00:00Z",
            content=[ThinkingPart(text="")],
        )
    ]
    assert validate(env) == []


def test_validate_rejects_bad_image_mime() -> None:
    """image.mime must match crtx v0.1 regex ^[a-z]+/[a-zA-Z0-9.+-]+$."""
    env = _base_env()
    env.turns = [
        Turn(
            id="t",
            role=Role.USER,
            created_at="2026-05-28T14:00:00Z",
            content=[ImagePart(mime="NOT_A_MIME", data="aGk=")],
        )
    ]
    errs = validate(env)
    assert _has_msg(errs, "mime")


def test_validate_accepts_empty_tool_call_input() -> None:
    """tool_call.input: {} is valid — tools may genuinely take no args.

    Schema has no minProperties on input; per crtx v0.1 the SDK accepts.
    """
    env = _base_env()
    env.turns = [
        Turn(
            id="a",
            role=Role.ASSISTANT,
            created_at="2026-05-28T14:00:00Z",
            content=[ToolCallPart(call_id="c1", name="current_time", input={})],
        ),
        Turn(
            id="t",
            role=Role.TOOL,
            created_at="2026-05-28T14:00:01Z",
            content=[ToolResultPart(call_id="c1", output={"now": "2026-05-28T14:00:01Z"})],
        ),
    ]
    assert validate(env) == []


def test_validate_image_variant_data_ok_url_added_rejected() -> None:
    env = _base_env()
    env.turns = [
        Turn(
            id="t",
            role=Role.USER,
            created_at="2026-05-28T14:00:00Z",
            content=[ImagePart(mime="image/png", data="aGVsbG8=")],
        )
    ]
    assert validate(env) == []

    # ImagePart construction itself enforces XOR, so simulate a malformed
    # round-trip by mutating after construction.
    env.turns[0].content[0].url = "https://example.com/x.png"
    errs = validate(env)
    assert _has_msg(errs, "exactly one")


def test_validate_rejects_extension_bare_prefix() -> None:
    raw = json.dumps(
        {
            "crtx_version": "0.1",
            "id": "s1",
            "created_at": "2026-05-28T14:00:00Z",
            "updated_at": "2026-05-28T14:00:00Z",
            "source": {"kind": "stem", "version": "0.1.0"},
            "turns": [
                {
                    "id": "t",
                    "role": "user",
                    "created_at": "2026-05-28T14:00:00Z",
                    "content": [{"type": "x-"}],
                }
            ],
        }
    )
    env = parse_envelope(raw)
    errs = validate(env)
    assert _has_msg(errs, "extension type")


def test_validate_rejects_extension_bad_char() -> None:
    raw = json.dumps(
        {
            "crtx_version": "0.1",
            "id": "s1",
            "created_at": "2026-05-28T14:00:00Z",
            "updated_at": "2026-05-28T14:00:00Z",
            "source": {"kind": "stem", "version": "0.1.0"},
            "turns": [
                {
                    "id": "t",
                    "role": "user",
                    "created_at": "2026-05-28T14:00:00Z",
                    "content": [{"type": "x-bad space"}],
                }
            ],
        }
    )
    env = parse_envelope(raw)
    errs = validate(env)
    assert _has_msg(errs, "extension type")


def test_validate_accepts_valid_extension_type() -> None:
    raw = json.dumps(
        {
            "crtx_version": "0.1",
            "id": "s1",
            "created_at": "2026-05-28T14:00:00Z",
            "updated_at": "2026-05-28T14:00:00Z",
            "source": {"kind": "stem", "version": "0.1.0"},
            "turns": [
                {
                    "id": "t",
                    "role": "user",
                    "created_at": "2026-05-28T14:00:00Z",
                    "content": [{"type": "x-io.jadb.foo"}],
                }
            ],
        }
    )
    env = parse_envelope(raw)
    assert validate(env) == []


def test_validate_bytes_rejects_unknown_top_level_fields() -> None:
    # Structurally valid envelope, but with an unknown top-level field.
    raw = json.dumps(
        {
            "crtx_version": "0.1",
            "id": "s-vb-1",
            "created_at": "2026-05-28T14:00:00Z",
            "updated_at": "2026-05-28T14:00:00Z",
            "source": {"kind": "stem", "version": "0.1.0"},
            "turns": [],
            "bogus_unknown_top_level": "drift",
        }
    )
    # Parsing strict-decodes -> raises before validate runs.
    with pytest.raises(ValueError, match="unknown"):
        validate_bytes(raw)


def test_validate_bytes_accepts_clean() -> None:
    raw = json.dumps(
        {
            "crtx_version": "0.1",
            "id": "s-vb-2",
            "created_at": "2026-05-28T14:00:00Z",
            "updated_at": "2026-05-28T14:00:00Z",
            "source": {"kind": "stem", "version": "0.1.0"},
            "turns": [],
        }
    )
    assert validate_bytes(raw) == []


def test_parse_envelope_rejects_unknown_part_field() -> None:
    raw = json.dumps(
        {
            "crtx_version": "0.1",
            "id": "s1",
            "created_at": "2026-05-28T14:00:00Z",
            "updated_at": "2026-05-28T14:00:00Z",
            "source": {"kind": "stem", "version": "0.1.0"},
            "turns": [
                {
                    "id": "t",
                    "role": "user",
                    "created_at": "2026-05-28T14:00:00Z",
                    "content": [{"type": "text", "text": "hi", "extra": 1}],
                }
            ],
        }
    )
    with pytest.raises(ValueError, match="unknown"):
        parse_envelope(raw)


# --- Multi-agent provenance (crtx envelope.md §3.1, §3.2, §6.2, §6.3, §7.2, §7.3) ---


def test_validate_dispatched_from_mutually_exclusive_with_fork_fields() -> None:
    env = _base_env()
    env.parent_id = "parent"
    env.fork_point = 0
    env.dispatched_from = DispatchedFrom(envelope_id="p", call_id="c")
    errs = validate(env)
    assert _has_msg(errs, "mutually exclusive")


def test_validate_dispatched_from_requires_both_members() -> None:
    env = _base_env()
    env.dispatched_from = DispatchedFrom(envelope_id="p", call_id="")
    errs = validate(env)
    assert _has_msg(errs, "envelope_id and call_id")


def test_validate_dispatched_from_alone_ok() -> None:
    env = _base_env()
    env.dispatched_from = DispatchedFrom(envelope_id="parent-env", call_id="call-1")
    assert validate(env) == []


def test_validate_parent_call_id_requires_open_outer_call() -> None:
    env = _base_env()
    inner = ToolCallPart(call_id="inner", name="echo", input={})
    inner.parent_call_id = "outer"  # outer not seen
    env.turns = [
        Turn(
            id="a",
            role=Role.ASSISTANT,
            created_at="2026-05-28T14:00:00Z",
            content=[inner],
        )
    ]
    errs = validate(env)
    assert _has_msg(errs, "parent_call_id")


def test_validate_parent_call_id_accepted_when_outer_open() -> None:
    env = _base_env()
    outer = ToolCallPart(call_id="outer", name="dispatch", input={})
    inner = ToolCallPart(call_id="inner", name="sub", input={}, parent_call_id="outer")
    now = "2026-05-28T14:00:00Z"
    env.turns = [
        Turn(id="a", role=Role.ASSISTANT, created_at=now, content=[outer]),
        Turn(id="b", role=Role.ASSISTANT, created_at=now, content=[inner]),
    ]
    assert validate(env) == []


def test_validate_parent_call_id_rejected_after_outer_resolved() -> None:
    env = _base_env()
    outer = ToolCallPart(call_id="outer", name="dispatch", input={})
    outer_result = ToolResultPart(call_id="outer", output="done")
    inner = ToolCallPart(call_id="inner", name="sub", input={}, parent_call_id="outer")
    now = "2026-05-28T14:00:00Z"
    env.turns = [
        Turn(id="a", role=Role.ASSISTANT, created_at=now, content=[outer]),
        Turn(id="t", role=Role.TOOL, created_at=now, content=[outer_result]),
        Turn(id="b", role=Role.ASSISTANT, created_at=now, content=[inner]),
    ]
    errs = validate(env)
    assert _has_msg(errs, "parent_call_id")


def test_validate_in_reply_to_call_id_requires_open_call() -> None:
    env = _base_env()
    env.turns = [
        Turn(
            id="u",
            role=Role.USER,
            created_at="2026-05-28T14:00:00Z",
            content=[TextPart(text="interjection")],
            in_reply_to_call_id="missing",
        )
    ]
    errs = validate(env)
    assert _has_msg(errs, "no open tool_call")


def test_validate_in_reply_to_call_id_forbidden_on_tool_role() -> None:
    env = _base_env()
    tc = ToolCallPart(call_id="c-1", name="x", input={})
    tr = ToolResultPart(call_id="c-1", output="out")
    now = "2026-05-28T14:00:00Z"
    env.turns = [
        Turn(id="a", role=Role.ASSISTANT, created_at=now, content=[tc]),
        Turn(
            id="t",
            role=Role.TOOL,
            created_at=now,
            content=[tr],
            in_reply_to_call_id="c-1",
        ),
    ]
    errs = validate(env)
    assert _has_msg(errs, "forbidden on tool-role")


def test_validate_in_reply_to_call_id_accepted_while_call_open() -> None:
    env = _base_env()
    tc = ToolCallPart(call_id="c-1", name="dispatch", input={})
    tr = ToolResultPart(call_id="c-1", output="out")
    now = "2026-05-28T14:00:00Z"
    env.turns = [
        Turn(id="a", role=Role.ASSISTANT, created_at=now, content=[tc]),
        Turn(
            id="u",
            role=Role.USER,
            created_at=now,
            content=[TextPart(text="update")],
            in_reply_to_call_id="c-1",
        ),
        Turn(id="t", role=Role.TOOL, created_at=now, content=[tr]),
    ]
    assert validate(env) == []


def test_validate_accepts_agent_id_on_applicable_roles() -> None:
    env = _base_env()
    env.turns = [
        Turn(
            id="a",
            role=Role.ASSISTANT,
            created_at="2026-05-28T14:00:00Z",
            content=[TextPart(text="hi")],
            agent_id="C",
        )
    ]
    assert validate(env) == []


def test_validate_accepts_child_envelope_id_on_tool_result() -> None:
    env = _base_env()
    tc = ToolCallPart(call_id="c-1", name="dispatch", input={})
    tr = ToolResultPart(call_id="c-1", output="done", child_envelope_id="child-env")
    now = "2026-05-28T14:00:00Z"
    env.turns = [
        Turn(id="a", role=Role.ASSISTANT, created_at=now, content=[tc]),
        Turn(id="t", role=Role.TOOL, created_at=now, content=[tr]),
    ]
    assert validate(env) == []
    # Round-trip preserves it.
    serialized = serialize_envelope(env)
    assert '"child_envelope_id":"child-env"' in serialized


def test_validate_injected_turns_require_all_three_ids() -> None:
    env = _base_env()
    env.injected_turns = [
        InjectedTurnRef(envelope_id="src", start_turn_id="", end_turn_id="")
    ]
    errs = validate(env)
    assert _has_msg(errs, "envelope_id / start_turn_id / end_turn_id")


def test_validate_injected_after_turn_id_must_exist_in_envelope() -> None:
    env = _base_env()
    env.turns = [
        Turn(
            id="t-1",
            role=Role.USER,
            created_at="2026-05-28T14:00:00Z",
            content=[TextPart(text="hi")],
        )
    ]
    env.injected_turns = [
        InjectedTurnRef(
            envelope_id="src",
            start_turn_id="s",
            end_turn_id="s",
            injected_after_turn_id="does-not-exist",
        )
    ]
    errs = validate(env)
    assert _has_msg(errs, "injected_after_turn_id")


def test_validate_injected_turns_creation_time_accepted() -> None:
    env = _base_env()
    env.injected_turns = [
        InjectedTurnRef(envelope_id="src", start_turn_id="a", end_turn_id="b")
    ]
    assert validate(env) == []


def test_parse_envelope_rejects_orphan_tool_result_via_validate() -> None:
    raw = json.dumps(
        {
            "crtx_version": "0.1",
            "id": "s1",
            "created_at": "2026-05-28T14:00:00Z",
            "updated_at": "2026-05-28T14:00:00Z",
            "source": {"kind": "stem", "version": "0.1.0"},
            "turns": [
                {
                    "id": "t",
                    "role": "tool",
                    "created_at": "2026-05-28T14:00:00Z",
                    "content": [{"type": "tool_result", "call_id": "orphan", "output": {}}],
                }
            ],
        }
    )
    errs = validate_bytes(raw)
    assert _has_msg(errs, "no preceding open tool_call")
