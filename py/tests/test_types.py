"""Unit tests for the public stem SDK types."""
from __future__ import annotations

import pytest

from stem import (
    CRTX_VERSION,
    SOURCE_KIND,
    VERSION,
    Envelope,
    ExtensionPart,
    ImagePart,
    Role,
    Source,
    TextPart,
    ThinkingPart,
    ToolCallPart,
    ToolResultPart,
    Turn,
    content_part_from_dict,
    is_extension_type,
    is_valid_extension_type,
)


def test_version_constants() -> None:
    assert CRTX_VERSION == "0.1"
    assert VERSION == "0.1.0"
    assert SOURCE_KIND == "stem"


def test_role_enum_values() -> None:
    assert Role.USER.value == "user"
    assert Role.ASSISTANT.value == "assistant"
    assert Role.TOOL.value == "tool"
    assert Role.SYSTEM.value == "system"
    assert Role.DEVELOPER.value == "developer"


def test_source_round_trip_minimal() -> None:
    s = Source(kind="stem", version="0.1.0")
    d = s.to_dict()
    assert d == {"kind": "stem", "version": "0.1.0"}
    assert Source.from_dict(d) == s


def test_source_round_trip_with_instance() -> None:
    s = Source(kind="stem", version="0.1.0", instance="host/pid")
    d = s.to_dict()
    assert d == {"kind": "stem", "version": "0.1.0", "instance": "host/pid"}
    assert Source.from_dict(d) == s


def test_source_rejects_unknown_field() -> None:
    with pytest.raises(ValueError, match="unknown"):
        Source.from_dict({"kind": "stem", "version": "0.1.0", "bogus": 1})


def test_text_part_round_trip() -> None:
    p = TextPart(text="hi")
    assert p.type == "text"
    assert p.to_dict() == {"type": "text", "text": "hi"}
    assert content_part_from_dict({"type": "text", "text": "hi"}) == p


def test_text_part_with_metadata() -> None:
    p = TextPart(text="hi", metadata={"io.jadb.tag": "x"})
    assert p.to_dict() == {
        "type": "text",
        "text": "hi",
        "metadata": {"io.jadb.tag": "x"},
    }


def test_text_part_rejects_unknown_field() -> None:
    with pytest.raises(ValueError, match="unknown"):
        content_part_from_dict({"type": "text", "text": "hi", "extra": 1})


def test_tool_call_part_round_trip() -> None:
    p = ToolCallPart(call_id="c1", name="echo", input={"a": 1})
    d = p.to_dict()
    assert d == {"type": "tool_call", "call_id": "c1", "name": "echo", "input": {"a": 1}}
    assert content_part_from_dict(d) == p


def test_tool_call_part_default_input() -> None:
    p = ToolCallPart(call_id="c1", name="echo", input=None)
    # to_dict normalizes None input to {} so emitted JSON is schema-valid.
    assert p.to_dict()["input"] == {}


def test_tool_result_part_round_trip() -> None:
    p = ToolResultPart(call_id="c1", output={"ok": True})
    d = p.to_dict()
    assert d == {"type": "tool_result", "call_id": "c1", "output": {"ok": True}}
    assert content_part_from_dict(d) == p


def test_tool_result_part_is_error_omitted_when_false() -> None:
    p = ToolResultPart(call_id="c1", output="boom", is_error=False)
    assert "is_error" not in p.to_dict()


def test_tool_result_part_is_error_emitted_when_true() -> None:
    p = ToolResultPart(call_id="c1", output="boom", is_error=True)
    assert p.to_dict()["is_error"] is True


def test_image_part_with_data_only() -> None:
    p = ImagePart(mime="image/png", data="aGVsbG8=")
    d = p.to_dict()
    assert d == {"type": "image", "mime": "image/png", "data": "aGVsbG8="}


def test_image_part_with_url_only() -> None:
    p = ImagePart(mime="image/png", url="https://example.com/x.png", alt="x")
    d = p.to_dict()
    assert d == {
        "type": "image",
        "mime": "image/png",
        "url": "https://example.com/x.png",
        "alt": "x",
    }


def test_image_part_rejects_both_data_and_url() -> None:
    with pytest.raises(ValueError, match="exactly one"):
        ImagePart(mime="image/png", data="x", url="https://y")


def test_image_part_rejects_neither_data_nor_url() -> None:
    with pytest.raises(ValueError, match="exactly one"):
        ImagePart(mime="image/png")


def test_thinking_part_round_trip() -> None:
    p = ThinkingPart(text="reason", signature="sig1")
    d = p.to_dict()
    assert d == {"type": "thinking", "text": "reason", "signature": "sig1"}
    assert content_part_from_dict(d) == p


def test_extension_part_round_trip_verbatim() -> None:
    raw = {"type": "x-io.jadb.custom", "note": "keep me", "weight": 7}
    p = content_part_from_dict(raw)
    assert isinstance(p, ExtensionPart)
    assert p.type == "x-io.jadb.custom"
    out = p.to_dict()
    assert out == raw


def test_extension_type_helpers() -> None:
    assert is_extension_type("x-foo")
    assert not is_extension_type("text")
    assert is_valid_extension_type("x-io.jadb.foo")
    assert not is_valid_extension_type("x-")
    assert not is_valid_extension_type("x-bad space")


def test_content_part_missing_type_rejected() -> None:
    with pytest.raises(ValueError, match="missing type"):
        content_part_from_dict({"text": "hi"})


def test_content_part_unknown_type_rejected() -> None:
    with pytest.raises(ValueError, match="unknown type"):
        content_part_from_dict({"type": "video", "url": "..."})


def test_turn_round_trip() -> None:
    t = Turn(
        id="t1",
        role=Role.USER,
        created_at="2026-05-28T14:00:00Z",
        content=[TextPart(text="hi")],
    )
    d = t.to_dict()
    assert d["role"] == "user"
    assert d["content"] == [{"type": "text", "text": "hi"}]
    assert Turn.from_dict(d) == t


def test_turn_rejects_unknown_field() -> None:
    with pytest.raises(ValueError, match="unknown"):
        Turn.from_dict(
            {
                "id": "t1",
                "role": "user",
                "created_at": "x",
                "content": [],
                "wat": 1,
            }
        )


def test_turn_rejects_unknown_role() -> None:
    with pytest.raises(ValueError, match="unknown role"):
        Turn.from_dict(
            {"id": "t1", "role": "wizard", "created_at": "x", "content": []}
        )


def test_envelope_round_trip_empty_turns() -> None:
    env = Envelope(
        crtx_version="0.1",
        id="s1",
        created_at="2026-05-28T14:00:00Z",
        updated_at="2026-05-28T14:00:00Z",
        source=Source(kind="stem", version="0.1.0"),
    )
    d = env.to_dict()
    # Empty turns -> []
    assert d["turns"] == []
    back = Envelope.from_dict(d)
    assert back == env


def test_envelope_rejects_unknown_top_level_field() -> None:
    with pytest.raises(ValueError, match="unknown"):
        Envelope.from_dict(
            {
                "crtx_version": "0.1",
                "id": "s1",
                "created_at": "x",
                "updated_at": "x",
                "source": {"kind": "stem", "version": "0.1.0"},
                "turns": [],
                "bogus_field": "drift",
            }
        )
