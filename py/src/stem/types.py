"""Core crtx v0.1 types: Role, Source, ContentPart variants, Turn, Envelope.

ContentPart is a discriminated union on the ``type`` field. Built-in
variants (text, tool_call, tool_result, image, thinking) are modeled as
dataclasses sharing the ``ContentPart`` protocol; extension parts
(``x-`` prefix) round-trip verbatim via ``ExtensionPart``.

JSON field names are snake_case per crtx spec. Conversion is handled by
``to_dict`` / ``from_dict`` methods on each type; ``parse_envelope`` and
``serialize_envelope`` wrap those for the public API.
"""
from __future__ import annotations

import re
from dataclasses import dataclass, field
from enum import StrEnum
from typing import Any, ClassVar


class Role(StrEnum):
    """Producer of a Turn. crtx v0.1 enum."""

    USER = "user"
    ASSISTANT = "assistant"
    TOOL = "tool"
    SYSTEM = "system"
    DEVELOPER = "developer"


# Built-in ContentPart type discriminator values.
PART_TYPE_TEXT = "text"
PART_TYPE_TOOL_CALL = "tool_call"
PART_TYPE_TOOL_RESULT = "tool_result"
PART_TYPE_IMAGE = "image"
PART_TYPE_THINKING = "thinking"

_BUILTIN_PART_TYPES = frozenset(
    {
        PART_TYPE_TEXT,
        PART_TYPE_TOOL_CALL,
        PART_TYPE_TOOL_RESULT,
        PART_TYPE_IMAGE,
        PART_TYPE_THINKING,
    }
)

# crtx v0.1 extension type regex: ``^x-[a-zA-Z0-9._-]+$``.
_EXTENSION_TYPE_RE = re.compile(r"^x-[a-zA-Z0-9._\-]+$")

# crtx v0.1 image.mime regex: ``^[a-z]+/[a-zA-Z0-9.+-]+$``.
_MIME_TYPE_RE = re.compile(r"^[a-z]+/[a-zA-Z0-9.+\-]+$")


def is_extension_type(t: str) -> bool:
    """Return True iff ``t`` starts with the ``x-`` prefix."""
    return t.startswith("x-")


def is_valid_extension_type(t: str) -> bool:
    """Return True iff ``t`` matches the crtx v0.1 extension type regex."""
    return bool(_EXTENSION_TYPE_RE.match(t))


def is_valid_mime_type(t: str) -> bool:
    """Return True iff ``t`` matches the crtx v0.1 image.mime regex."""
    return bool(_MIME_TYPE_RE.match(t))


@dataclass
class Source:
    """Identifies the runtime that produced an Envelope.

    ``kind`` + ``version`` are required by crtx; ``instance`` is optional.
    """

    kind: str
    version: str
    instance: str | None = None

    def to_dict(self) -> dict[str, Any]:
        d: dict[str, Any] = {"kind": self.kind, "version": self.version}
        if self.instance is not None and self.instance != "":
            d["instance"] = self.instance
        return d

    @classmethod
    def from_dict(cls, data: dict[str, Any]) -> Source:
        unknown = set(data.keys()) - {"kind", "version", "instance"}
        if unknown:
            raise ValueError(f"stem: source: unknown fields {sorted(unknown)!r}")
        kind = data.get("kind", "")
        version = data.get("version", "")
        # Reject empty kind/version at decode for consistency with the
        # content-part discriminator strictness in this file. validate()
        # would catch it later, but the same boundary should reject
        # missing primaries before the value type ever exists.
        if not isinstance(kind, str) or kind == "":
            raise ValueError("stem: source: kind required (non-empty string)")
        if not isinstance(version, str) or version == "":
            raise ValueError("stem: source: version required (non-empty string)")
        return cls(
            kind=kind,
            version=version,
            instance=data.get("instance"),
        )


# ---------------------------------------------------------------------------
# ContentPart variants
# ---------------------------------------------------------------------------


@dataclass
class TextPart:
    """``text`` ContentPart variant."""

    text: str
    metadata: dict[str, Any] | None = None
    type: str = field(default=PART_TYPE_TEXT, init=False)

    _ALLOWED_FIELDS: ClassVar[frozenset[str]] = frozenset({"type", "text", "metadata"})

    def to_dict(self) -> dict[str, Any]:
        d: dict[str, Any] = {"type": self.type, "text": self.text}
        if self.metadata:
            d["metadata"] = self.metadata
        return d

    @classmethod
    def from_dict(cls, data: dict[str, Any]) -> TextPart:
        unknown = set(data.keys()) - cls._ALLOWED_FIELDS
        if unknown:
            raise ValueError(f"stem: text part: unknown fields {sorted(unknown)!r}")
        return cls(text=data.get("text", ""), metadata=data.get("metadata"))


@dataclass
class ToolCallPart:
    """``tool_call`` ContentPart variant.

    ``parent_call_id`` is the crtx v0.1 multi-agent provenance field
    that marks this call as nested inside an outer call still open at
    this Turn. See envelope.md §6.2.
    """

    call_id: str
    name: str
    input: Any
    parent_call_id: str | None = None
    metadata: dict[str, Any] | None = None
    type: str = field(default=PART_TYPE_TOOL_CALL, init=False)

    _ALLOWED_FIELDS: ClassVar[frozenset[str]] = frozenset(
        {"type", "call_id", "name", "input", "parent_call_id", "metadata"}
    )

    def to_dict(self) -> dict[str, Any]:
        d: dict[str, Any] = {
            "type": self.type,
            "call_id": self.call_id,
            "name": self.name,
            "input": self.input if self.input is not None else {},
        }
        if self.parent_call_id:
            d["parent_call_id"] = self.parent_call_id
        if self.metadata:
            d["metadata"] = self.metadata
        return d

    @classmethod
    def from_dict(cls, data: dict[str, Any]) -> ToolCallPart:
        unknown = set(data.keys()) - cls._ALLOWED_FIELDS
        if unknown:
            raise ValueError(f"stem: tool_call part: unknown fields {sorted(unknown)!r}")
        return cls(
            call_id=data.get("call_id", ""),
            name=data.get("name", ""),
            input=data.get("input"),
            parent_call_id=data.get("parent_call_id"),
            metadata=data.get("metadata"),
        )


@dataclass
class ToolResultPart:
    """``tool_result`` ContentPart variant.

    ``child_envelope_id`` is the crtx v0.1 multi-agent provenance field
    that points at the dispatch-nest child Envelope when the call was
    executed out-of-band. See envelope.md §7.2.
    """

    call_id: str
    output: Any
    is_error: bool = False
    child_envelope_id: str | None = None
    metadata: dict[str, Any] | None = None
    type: str = field(default=PART_TYPE_TOOL_RESULT, init=False)

    _ALLOWED_FIELDS: ClassVar[frozenset[str]] = frozenset(
        {"type", "call_id", "output", "is_error", "child_envelope_id", "metadata"}
    )

    def to_dict(self) -> dict[str, Any]:
        d: dict[str, Any] = {
            "type": self.type,
            "call_id": self.call_id,
            "output": self.output,
        }
        if self.is_error:
            d["is_error"] = True
        if self.child_envelope_id:
            d["child_envelope_id"] = self.child_envelope_id
        if self.metadata:
            d["metadata"] = self.metadata
        return d

    @classmethod
    def from_dict(cls, data: dict[str, Any]) -> ToolResultPart:
        unknown = set(data.keys()) - cls._ALLOWED_FIELDS
        if unknown:
            raise ValueError(
                f"stem: tool_result part: unknown fields {sorted(unknown)!r}"
            )
        return cls(
            call_id=data.get("call_id", ""),
            output=data.get("output"),
            is_error=bool(data.get("is_error", False)),
            child_envelope_id=data.get("child_envelope_id"),
            metadata=data.get("metadata"),
        )


@dataclass
class ImagePart:
    """``image`` ContentPart variant.

    Exactly one of ``data`` / ``url`` MUST be set per crtx v0.1.
    Construction validates this contract; ``to_dict`` re-validates so
    producers cannot silently emit invalid envelopes.
    """

    mime: str
    data: str | None = None
    url: str | None = None
    alt: str | None = None
    metadata: dict[str, Any] | None = None
    type: str = field(default=PART_TYPE_IMAGE, init=False)

    _ALLOWED_FIELDS: ClassVar[frozenset[str]] = frozenset(
        {"type", "mime", "data", "url", "alt", "metadata"}
    )

    def __post_init__(self) -> None:
        has_data = bool(self.data)
        has_url = bool(self.url)
        if has_data == has_url:
            raise ValueError(
                "stem: image part: exactly one of data/url required"
            )

    def to_dict(self) -> dict[str, Any]:
        has_data = bool(self.data)
        has_url = bool(self.url)
        if has_data == has_url:
            raise ValueError(
                "stem: image part: exactly one of data/url required"
            )
        d: dict[str, Any] = {"type": self.type, "mime": self.mime}
        if self.data:
            d["data"] = self.data
        if self.url:
            d["url"] = self.url
        if self.alt:
            d["alt"] = self.alt
        if self.metadata:
            d["metadata"] = self.metadata
        return d

    @classmethod
    def from_dict(cls, data: dict[str, Any]) -> ImagePart:
        unknown = set(data.keys()) - cls._ALLOWED_FIELDS
        if unknown:
            raise ValueError(f"stem: image part: unknown fields {sorted(unknown)!r}")
        return cls(
            mime=data.get("mime", ""),
            data=data.get("data"),
            url=data.get("url"),
            alt=data.get("alt"),
            metadata=data.get("metadata"),
        )


@dataclass
class ThinkingPart:
    """``thinking`` ContentPart variant."""

    text: str
    signature: str | None = None
    metadata: dict[str, Any] | None = None
    type: str = field(default=PART_TYPE_THINKING, init=False)

    _ALLOWED_FIELDS: ClassVar[frozenset[str]] = frozenset(
        {"type", "text", "signature", "metadata"}
    )

    def to_dict(self) -> dict[str, Any]:
        d: dict[str, Any] = {"type": self.type, "text": self.text}
        if self.signature:
            d["signature"] = self.signature
        if self.metadata:
            d["metadata"] = self.metadata
        return d

    @classmethod
    def from_dict(cls, data: dict[str, Any]) -> ThinkingPart:
        unknown = set(data.keys()) - cls._ALLOWED_FIELDS
        if unknown:
            raise ValueError(f"stem: thinking part: unknown fields {sorted(unknown)!r}")
        return cls(
            text=data.get("text", ""),
            signature=data.get("signature"),
            metadata=data.get("metadata"),
        )


@dataclass
class ExtensionPart:
    """Holds the raw JSON form of an ``x-`` prefixed extension part.

    The body round-trips verbatim. Validation of the extension ``type``
    regex is enforced separately in ``validate``.
    """

    type: str
    raw: dict[str, Any]

    def to_dict(self) -> dict[str, Any]:
        # raw IS the canonical form. Ensure "type" matches the stored type;
        # if absent, inject it so producers cannot ship a typeless body.
        d = dict(self.raw)
        d["type"] = self.type
        return d

    @classmethod
    def from_dict(cls, data: dict[str, Any]) -> ExtensionPart:
        t = data.get("type", "")
        return cls(type=t, raw=dict(data))


# Discriminated-union alias for the public API.
ContentPart = TextPart | ToolCallPart | ToolResultPart | ImagePart | ThinkingPart | ExtensionPart


def content_part_from_dict(data: dict[str, Any]) -> ContentPart:
    """Decode a single ContentPart from its JSON dict form."""
    if not isinstance(data, dict):
        raise ValueError(f"stem: ContentPart: expected object, got {type(data).__name__}")
    t = data.get("type")
    if not isinstance(t, str) or t == "":
        raise ValueError("stem: ContentPart: missing type")
    if is_extension_type(t):
        return ExtensionPart.from_dict(data)
    if t == PART_TYPE_TEXT:
        return TextPart.from_dict(data)
    if t == PART_TYPE_TOOL_CALL:
        return ToolCallPart.from_dict(data)
    if t == PART_TYPE_TOOL_RESULT:
        return ToolResultPart.from_dict(data)
    if t == PART_TYPE_IMAGE:
        return ImagePart.from_dict(data)
    if t == PART_TYPE_THINKING:
        return ThinkingPart.from_dict(data)
    raise ValueError(
        f"stem: ContentPart: unknown type {t!r} (extension parts MUST use x- prefix)"
    )


def content_part_to_dict(part: ContentPart) -> dict[str, Any]:
    """Encode a single ContentPart to its JSON dict form."""
    return part.to_dict()


# ---------------------------------------------------------------------------
# Turn + Envelope
# ---------------------------------------------------------------------------


@dataclass
class DispatchedFrom:
    """Marks an Envelope as a dispatch nest spawned by an open tool_call
    in another Envelope. Both members are required by the schema;
    mutually exclusive with Envelope.parent_id / Envelope.fork_point.
    See envelope.md §7.2.
    """

    envelope_id: str
    call_id: str

    _ALLOWED_FIELDS: ClassVar[frozenset[str]] = frozenset({"envelope_id", "call_id"})

    def to_dict(self) -> dict[str, Any]:
        return {"envelope_id": self.envelope_id, "call_id": self.call_id}

    @classmethod
    def from_dict(cls, data: dict[str, Any]) -> DispatchedFrom:
        unknown = set(data.keys()) - cls._ALLOWED_FIELDS
        if unknown:
            raise ValueError(
                f"stem: dispatched_from: unknown fields {sorted(unknown)!r}"
            )
        return cls(
            envelope_id=data.get("envelope_id", ""),
            call_id=data.get("call_id", ""),
        )


@dataclass
class InjectedTurnRef:
    """References a slice of Turns in another Envelope, included as part
    of this Envelope's context. Append-only. See envelope.md §7.3.
    """

    envelope_id: str
    start_turn_id: str
    end_turn_id: str
    injected_after_turn_id: str | None = None

    _ALLOWED_FIELDS: ClassVar[frozenset[str]] = frozenset(
        {"envelope_id", "start_turn_id", "end_turn_id", "injected_after_turn_id"}
    )

    def to_dict(self) -> dict[str, Any]:
        d: dict[str, Any] = {
            "envelope_id": self.envelope_id,
            "start_turn_id": self.start_turn_id,
            "end_turn_id": self.end_turn_id,
        }
        if self.injected_after_turn_id:
            d["injected_after_turn_id"] = self.injected_after_turn_id
        return d

    @classmethod
    def from_dict(cls, data: dict[str, Any]) -> InjectedTurnRef:
        unknown = set(data.keys()) - cls._ALLOWED_FIELDS
        if unknown:
            raise ValueError(
                f"stem: injected_turn_ref: unknown fields {sorted(unknown)!r}"
            )
        return cls(
            envelope_id=data.get("envelope_id", ""),
            start_turn_id=data.get("start_turn_id", ""),
            end_turn_id=data.get("end_turn_id", ""),
            injected_after_turn_id=data.get("injected_after_turn_id"),
        )


_TURN_ALLOWED_FIELDS = frozenset(
    {"id", "role", "created_at", "content", "agent_id", "in_reply_to_call_id", "metadata"}
)


@dataclass
class Turn:
    """One contribution within an Envelope.

    ``created_at`` is stored as an ISO-8601 / RFC-3339 string for
    round-trip fidelity with the wire format (consumers may parse it
    into a datetime as needed).

    ``agent_id`` and ``in_reply_to_call_id`` are the crtx v0.1
    multi-agent provenance fields. See envelope.md §3.1 and §3.2.
    """

    id: str
    role: Role
    created_at: str
    content: list[ContentPart] = field(default_factory=list)
    agent_id: str | None = None
    in_reply_to_call_id: str | None = None
    metadata: dict[str, Any] | None = None

    def to_dict(self) -> dict[str, Any]:
        role_val = self.role.value if isinstance(self.role, Role) else str(self.role)
        d: dict[str, Any] = {
            "id": self.id,
            "role": role_val,
            "created_at": self.created_at,
            "content": [p.to_dict() for p in self.content],
        }
        if self.agent_id:
            d["agent_id"] = self.agent_id
        if self.in_reply_to_call_id:
            d["in_reply_to_call_id"] = self.in_reply_to_call_id
        if self.metadata:
            d["metadata"] = self.metadata
        return d

    @classmethod
    def from_dict(cls, data: dict[str, Any]) -> Turn:
        unknown = set(data.keys()) - _TURN_ALLOWED_FIELDS
        if unknown:
            raise ValueError(f"stem: turn: unknown fields {sorted(unknown)!r}")

        raw_role = data.get("role", "")
        try:
            role = Role(raw_role) if isinstance(raw_role, str) else Role(str(raw_role))
        except ValueError as e:
            # Defer enum-rejection to validate() so callers can collect
            # all errors at once; store the raw value verbatim.
            raise ValueError(f"stem: turn: unknown role {raw_role!r}") from e

        content_raw = data.get("content", [])
        if not isinstance(content_raw, list):
            raise ValueError("stem: turn: content must be a list")
        content = [content_part_from_dict(p) for p in content_raw]

        return cls(
            id=data.get("id", ""),
            role=role,
            created_at=data.get("created_at", ""),
            content=content,
            agent_id=data.get("agent_id"),
            in_reply_to_call_id=data.get("in_reply_to_call_id"),
            metadata=data.get("metadata"),
        )


_ENVELOPE_ALLOWED_FIELDS = frozenset(
    {
        "crtx_version",
        "id",
        "created_at",
        "updated_at",
        "source",
        "turns",
        "parent_id",
        "fork_point",
        "dispatched_from",
        "injected_turns",
        "metadata",
    }
)


@dataclass
class Envelope:
    """A stem-shaped crtx Envelope: one conversation.

    JSON-encoded form matches the crtx v0.1 schema exactly.

    ``dispatched_from`` and ``injected_turns`` are the crtx v0.1
    multi-agent provenance fields. See envelope.md §7.2 and §7.3.
    """

    crtx_version: str
    id: str
    created_at: str
    updated_at: str
    source: Source
    turns: list[Turn] = field(default_factory=list)
    parent_id: str | None = None
    fork_point: int | None = None
    dispatched_from: DispatchedFrom | None = None
    injected_turns: list[InjectedTurnRef] = field(default_factory=list)
    metadata: dict[str, Any] | None = None

    def to_dict(self) -> dict[str, Any]:
        d: dict[str, Any] = {
            "crtx_version": self.crtx_version,
            "id": self.id,
            "created_at": self.created_at,
            "updated_at": self.updated_at,
            "source": self.source.to_dict(),
            # Empty turns serializes to [] not null — crtx requirement.
            "turns": [t.to_dict() for t in self.turns],
        }
        if self.parent_id is not None and self.parent_id != "":
            d["parent_id"] = self.parent_id
        if self.fork_point is not None:
            d["fork_point"] = self.fork_point
        if self.dispatched_from is not None:
            d["dispatched_from"] = self.dispatched_from.to_dict()
        if self.injected_turns:
            d["injected_turns"] = [r.to_dict() for r in self.injected_turns]
        if self.metadata:
            d["metadata"] = self.metadata
        return d

    @classmethod
    def from_dict(cls, data: dict[str, Any]) -> Envelope:
        unknown = set(data.keys()) - _ENVELOPE_ALLOWED_FIELDS
        if unknown:
            raise ValueError(f"stem: envelope: unknown fields {sorted(unknown)!r}")

        raw_source = data.get("source")
        if not isinstance(raw_source, dict):
            raise ValueError("stem: envelope: source must be an object")
        source = Source.from_dict(raw_source)

        raw_turns = data.get("turns", [])
        if not isinstance(raw_turns, list):
            raise ValueError("stem: envelope: turns must be a list")
        turns = [Turn.from_dict(t) for t in raw_turns]

        raw_dispatched_from = data.get("dispatched_from")
        dispatched_from: DispatchedFrom | None = None
        if raw_dispatched_from is not None:
            if not isinstance(raw_dispatched_from, dict):
                raise ValueError("stem: envelope: dispatched_from must be an object")
            dispatched_from = DispatchedFrom.from_dict(raw_dispatched_from)

        raw_injected = data.get("injected_turns", [])
        if not isinstance(raw_injected, list):
            raise ValueError("stem: envelope: injected_turns must be a list")
        injected_turns = [InjectedTurnRef.from_dict(r) for r in raw_injected]

        return cls(
            crtx_version=data.get("crtx_version", ""),
            id=data.get("id", ""),
            created_at=data.get("created_at", ""),
            updated_at=data.get("updated_at", ""),
            source=source,
            turns=turns,
            parent_id=data.get("parent_id"),
            fork_point=data.get("fork_point"),
            dispatched_from=dispatched_from,
            injected_turns=injected_turns,
            metadata=data.get("metadata"),
        )
