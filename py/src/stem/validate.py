"""Envelope validation.

Mirrors the Go SDK's structural checks: required fields, role enum,
ContentPart discriminator, parent_id/fork_point dependency, and
tool_result -> tool_call linkage. JSON-Schema-level conformance is left
to an external validator; this module is dependency-free and
hand-rolled.
"""
from __future__ import annotations

from dataclasses import dataclass

from .types import (
    PART_TYPE_IMAGE,
    PART_TYPE_TEXT,
    PART_TYPE_THINKING,
    PART_TYPE_TOOL_CALL,
    PART_TYPE_TOOL_RESULT,
    ContentPart,
    Envelope,
    ExtensionPart,
    ImagePart,
    Role,
    TextPart,
    ThinkingPart,
    ToolCallPart,
    ToolResultPart,
    Turn,
    is_extension_type,
    is_valid_extension_type,
    is_valid_mime_type,
)
from .version import CRTX_VERSION


@dataclass
class EnvelopeError:
    """Structured validation failure for one location in an Envelope."""

    path: str
    message: str

    def __str__(self) -> str:
        return f"{self.path}: {self.message}"


def _crtx_version_compatible(v: str) -> bool:
    """Mirror Go semantics: in 0.x we only accept exact match."""
    if v == CRTX_VERSION:
        return True
    if "." not in v or "." not in CRTX_VERSION:
        return False
    want_major = CRTX_VERSION.split(".", 1)[0]
    got_major = v.split(".", 1)[0]
    # In 0.x every minor is breaking; only exact match passes.
    return False if want_major == "0" or got_major == "0" else want_major == got_major


def validate(env: Envelope) -> list[EnvelopeError]:
    """Return a list of structural errors. Empty list == valid Envelope."""
    errors: list[EnvelopeError] = []

    if env is None:
        return [EnvelopeError("", "nil envelope")]

    if env.crtx_version == "":
        errors.append(EnvelopeError("crtx_version", "missing"))
    elif not _crtx_version_compatible(env.crtx_version):
        errors.append(
            EnvelopeError(
                "crtx_version",
                f"{env.crtx_version!r} unsupported (this stem speaks {CRTX_VERSION!r})",
            )
        )

    if env.id == "":
        errors.append(EnvelopeError("id", "missing"))
    if env.created_at == "":
        errors.append(EnvelopeError("created_at", "missing"))
    if env.updated_at == "":
        errors.append(EnvelopeError("updated_at", "missing"))

    if env.source is None or env.source.kind == "" or env.source.version == "":
        errors.append(EnvelopeError("source", "source.kind and source.version required"))

    has_parent = env.parent_id is not None and env.parent_id != ""
    has_fork = env.fork_point is not None
    if has_parent != has_fork:
        errors.append(
            EnvelopeError(
                "parent_id",
                "parent_id and fork_point must both be set or both omitted",
            )
        )
    if env.fork_point is not None and env.fork_point < 0:
        errors.append(EnvelopeError("fork_point", "must be >= 0"))

    # dispatched_from is mutually exclusive with parent_id / fork_point
    # (envelope.md §7.2). Schema enforces this with an if/then/not block;
    # mirror it here so hand-built envelopes are rejected too.
    if env.dispatched_from is not None:
        if has_parent or has_fork:
            errors.append(
                EnvelopeError(
                    "dispatched_from",
                    "mutually exclusive with parent_id/fork_point",
                )
            )
        if env.dispatched_from.envelope_id == "" or env.dispatched_from.call_id == "":
            errors.append(
                EnvelopeError(
                    "dispatched_from",
                    "requires both envelope_id and call_id",
                )
            )

    # injected_turns entries: required tuple plus an
    # injected_after_turn_id that, when set, MUST already exist in this
    # envelope's turns (envelope.md §7.3 — forward references forbidden).
    envelope_turn_ids = {t.id for t in env.turns if t.id}
    for i, ref in enumerate(env.injected_turns):
        base = f"injected_turns[{i}]"
        if ref.envelope_id == "" or ref.start_turn_id == "" or ref.end_turn_id == "":
            errors.append(
                EnvelopeError(
                    base,
                    "envelope_id / start_turn_id / end_turn_id required",
                )
            )
        if (
            ref.injected_after_turn_id
            and ref.injected_after_turn_id not in envelope_turn_ids
        ):
            errors.append(
                EnvelopeError(
                    base,
                    f"injected_after_turn_id {ref.injected_after_turn_id!r} not present in this envelope",
                )
            )

    # Track open tool_calls. A call goes open on tool_call and closes on
    # its matching tool_result. Used by:
    #   - tool_result.call_id linkage
    #   - tool_call.parent_call_id (MUST point at an open outer call)
    #   - Turn.in_reply_to_call_id (MUST point at an open call)
    open_calls: set[str] = set()
    for i, turn in enumerate(env.turns):
        _validate_turn(turn, i, open_calls, errors)

    return errors


def _validate_turn(
    turn: Turn,
    index: int,
    open_calls: set[str],
    errors: list[EnvelopeError],
) -> None:
    base = f"turns[{index}]"
    if turn.id == "":
        errors.append(EnvelopeError(f"{base}.id", "missing"))
    if not isinstance(turn.role, Role):
        errors.append(EnvelopeError(f"{base}.role", f"unknown role {turn.role!r}"))
    if turn.created_at == "":
        errors.append(EnvelopeError(f"{base}.created_at", "missing"))
    if not turn.content:
        errors.append(EnvelopeError(f"{base}.content", "content[] must have >=1 part"))

    # in_reply_to_call_id (envelope.md §3.2) MUST reference an open
    # tool_call at the point this Turn is appended. Tool-role Turns
    # SHOULD NOT carry it — tool_result IS the resolution of the call,
    # not an interjection.
    if turn.in_reply_to_call_id:
        if turn.role == Role.TOOL:
            errors.append(
                EnvelopeError(
                    f"{base}.in_reply_to_call_id",
                    "forbidden on tool-role turns",
                )
            )
        elif turn.in_reply_to_call_id not in open_calls:
            errors.append(
                EnvelopeError(
                    f"{base}.in_reply_to_call_id",
                    f"{turn.in_reply_to_call_id!r} has no open tool_call",
                )
            )

    for j, part in enumerate(turn.content):
        _validate_content_part(part, f"{base}.content[{j}]", open_calls, errors)


def _validate_content_part(
    part: ContentPart,
    path: str,
    open_calls: set[str],
    errors: list[EnvelopeError],
) -> None:
    t = getattr(part, "type", "")
    if t == "":
        errors.append(EnvelopeError(path, "missing type"))
        return

    if isinstance(part, ExtensionPart) or is_extension_type(t):
        if not is_valid_extension_type(t):
            errors.append(
                EnvelopeError(
                    path,
                    f"extension type {t!r} does not match ^x-[a-zA-Z0-9._-]+$",
                )
            )
        return

    if isinstance(part, TextPart):
        # Empty text permitted by schema.
        return

    if isinstance(part, ToolCallPart):
        if part.call_id == "":
            errors.append(EnvelopeError(path, "tool_call: missing call_id"))
        if part.name == "":
            errors.append(EnvelopeError(path, "tool_call: missing name"))
        if part.input is None:
            errors.append(EnvelopeError(path, "tool_call: missing input"))
        # parent_call_id (envelope.md §6.2) MUST reference an outer
        # tool_call still open at this position.
        if part.parent_call_id and part.parent_call_id not in open_calls:
            errors.append(
                EnvelopeError(
                    path,
                    f"tool_call: parent_call_id {part.parent_call_id!r} has no open outer tool_call",
                )
            )
        if part.call_id:
            open_calls.add(part.call_id)
        return

    if isinstance(part, ToolResultPart):
        if part.call_id == "":
            errors.append(EnvelopeError(path, "tool_result: missing call_id"))
        elif part.call_id not in open_calls:
            errors.append(
                EnvelopeError(
                    path,
                    f"tool_result: call_id {part.call_id!r} has no preceding open tool_call",
                )
            )
        else:
            # Close the matched call so later turns can't interject into
            # it via in_reply_to_call_id, and nested calls can't claim
            # it as a parent.
            open_calls.discard(part.call_id)
        if part.output is None:
            errors.append(EnvelopeError(path, "tool_result: missing output"))
        return

    if isinstance(part, ImagePart):
        if part.mime == "":
            errors.append(EnvelopeError(path, "image: missing mime"))
        elif not is_valid_mime_type(part.mime):
            errors.append(
                EnvelopeError(
                    path,
                    f"image: mime {part.mime!r} does not match ^[a-z]+/[a-zA-Z0-9.+-]+$",
                )
            )
        has_data = bool(part.data)
        has_url = bool(part.url)
        if has_data == has_url:
            errors.append(EnvelopeError(path, "image: exactly one of data/url required"))
        return

    if isinstance(part, ThinkingPart):
        # Empty thinking.text permitted by schema; only the key itself
        # is required (value may be ""). Mirrors text part rules.
        return

    # Type discriminator survived round-trip but doesn't match any known
    # variant class — defensive fallback.
    if t not in {
        PART_TYPE_TEXT,
        PART_TYPE_TOOL_CALL,
        PART_TYPE_TOOL_RESULT,
        PART_TYPE_IMAGE,
        PART_TYPE_THINKING,
    }:
        errors.append(EnvelopeError(path, f"unknown ContentPart type {t!r}"))
