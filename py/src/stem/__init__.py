"""Python SDK for stem — crtx v0.1 envelope I/O.

Tier A scope: envelope parsing, serialization, and structural validation.
No runtime, no Provider, no ToolRegistry, no Supervisor, no storage.

See the package README for the spec contract and end-to-end examples.
"""
from .envelope import parse_envelope, serialize_envelope, validate_bytes
from .types import (
    PART_TYPE_IMAGE,
    PART_TYPE_TEXT,
    PART_TYPE_THINKING,
    PART_TYPE_TOOL_CALL,
    PART_TYPE_TOOL_RESULT,
    ContentPart,
    DispatchedFrom,
    Envelope,
    ExtensionPart,
    ImagePart,
    InjectedTurnRef,
    Role,
    Source,
    TextPart,
    ThinkingPart,
    ToolCallPart,
    ToolResultPart,
    Turn,
    content_part_from_dict,
    content_part_to_dict,
    is_extension_type,
    is_valid_extension_type,
)
from .validate import EnvelopeError, validate
from .version import CRTX_VERSION, SOURCE_KIND, VERSION

__all__ = [
    "CRTX_VERSION",
    "ContentPart",
    "DispatchedFrom",
    "Envelope",
    "EnvelopeError",
    "ExtensionPart",
    "ImagePart",
    "InjectedTurnRef",
    "PART_TYPE_IMAGE",
    "PART_TYPE_TEXT",
    "PART_TYPE_THINKING",
    "PART_TYPE_TOOL_CALL",
    "PART_TYPE_TOOL_RESULT",
    "Role",
    "SOURCE_KIND",
    "Source",
    "TextPart",
    "ThinkingPart",
    "ToolCallPart",
    "ToolResultPart",
    "Turn",
    "VERSION",
    "content_part_from_dict",
    "content_part_to_dict",
    "is_extension_type",
    "is_valid_extension_type",
    "parse_envelope",
    "serialize_envelope",
    "validate",
    "validate_bytes",
]
