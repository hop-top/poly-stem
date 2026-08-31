"""Tests for parse_envelope / serialize_envelope round-trip + validate_bytes."""
from __future__ import annotations

import json
from pathlib import Path

import pytest

from stem import (
    Envelope,
    Role,
    Source,
    TextPart,
    Turn,
    parse_envelope,
    serialize_envelope,
    validate_bytes,
)

TESTDATA = Path(__file__).parent / "testdata" / "crtx_v0.1"
EXAMPLES = [
    "minimal.json",
    "tool-call.json",
    "fork.json",
    "dispatch.json",
    "dispatch-nested.json",
    "injection.json",
]


@pytest.mark.parametrize("name", EXAMPLES)
def test_crtx_example_round_trip(name: str) -> None:
    """parse -> re-serialize -> reparse -> deep equality (dict form)."""
    raw_text = (TESTDATA / name).read_text()
    raw_dict = json.loads(raw_text)

    env = parse_envelope(raw_text)
    serialized = serialize_envelope(env)
    env2 = parse_envelope(serialized)

    # Deep equality via canonical dict form (json key ordering may vary).
    assert env2.to_dict() == env.to_dict()
    # And matches the source JSON dict.
    assert env.to_dict() == raw_dict


@pytest.mark.parametrize("name", EXAMPLES)
def test_crtx_example_validates_clean(name: str) -> None:
    errs = validate_bytes((TESTDATA / name).read_bytes())
    assert errs == [], f"{name}: {[str(e) for e in errs]}"


def test_serialize_empty_turns_emits_array_not_null() -> None:
    env = Envelope(
        crtx_version="0.1",
        id="s1",
        created_at="2026-05-28T14:00:00Z",
        updated_at="2026-05-28T14:00:00Z",
        source=Source(kind="stem", version="0.1.0"),
    )
    out = serialize_envelope(env)
    assert '"turns":[]' in out
    assert '"turns":null' not in out


def test_parse_envelope_accepts_bytes() -> None:
    raw = (TESTDATA / "minimal.json").read_bytes()
    env = parse_envelope(raw)
    assert env.crtx_version == "0.1"


def test_parse_envelope_rejects_top_level_array() -> None:
    with pytest.raises(ValueError, match="object"):
        parse_envelope("[]")


def test_parse_envelope_rejects_unknown_top_level_field() -> None:
    raw = """{
        "crtx_version": "0.1",
        "id": "s1",
        "created_at": "2026-05-28T14:00:00Z",
        "updated_at": "2026-05-28T14:00:00Z",
        "source": {"kind": "stem", "version": "0.1.0"},
        "turns": [],
        "drift": true
    }"""
    with pytest.raises(ValueError, match="unknown"):
        parse_envelope(raw)


def test_validate_bytes_round_trips_extension_part_verbatim() -> None:
    raw = json.dumps(
        {
            "crtx_version": "0.1",
            "id": "s-ext",
            "created_at": "2026-05-28T14:00:00Z",
            "updated_at": "2026-05-28T14:00:00Z",
            "source": {"kind": "stem", "version": "0.1.0"},
            "turns": [
                {
                    "id": "t1",
                    "role": "user",
                    "created_at": "2026-05-28T14:00:00Z",
                    "content": [
                        {"type": "x-io.jadb.custom", "note": "keep me", "weight": 7}
                    ],
                }
            ],
        }
    )
    env = parse_envelope(raw)
    serialized = serialize_envelope(env)
    env2 = parse_envelope(serialized)
    assert env2.to_dict() == env.to_dict()
    # The extension body must round-trip verbatim.
    assert env2.turns[0].content[0].to_dict() == {
        "type": "x-io.jadb.custom",
        "note": "keep me",
        "weight": 7,
    }
    assert validate_bytes(raw) == []


def test_round_trip_handcrafted_envelope() -> None:
    env = Envelope(
        crtx_version="0.1",
        id="s-handmade",
        created_at="2026-05-28T14:00:00Z",
        updated_at="2026-05-28T14:00:01Z",
        source=Source(kind="stem", version="0.1.0"),
        turns=[
            Turn(
                id="u-1",
                role=Role.USER,
                created_at="2026-05-28T14:00:00Z",
                content=[TextPart(text="hi")],
            ),
            Turn(
                id="a-1",
                role=Role.ASSISTANT,
                created_at="2026-05-28T14:00:01Z",
                content=[TextPart(text="hello back")],
            ),
        ],
    )
    out = serialize_envelope(env)
    env2 = parse_envelope(out)
    assert env2.to_dict() == env.to_dict()
    assert validate_bytes(out) == []
