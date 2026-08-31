"""parse_envelope / serialize_envelope / validate_bytes — the public I/O API.

These functions wrap the dataclass ``from_dict`` / ``to_dict`` methods and
add the strict-decode + JSON layer. Field naming is snake_case to match
the crtx v0.1 spec exactly.
"""
from __future__ import annotations

import json

from .types import Envelope
from .validate import EnvelopeError, validate


def parse_envelope(data: str | bytes | bytearray) -> Envelope:
    """Strict-decode a JSON-encoded crtx envelope into an ``Envelope``.

    Rejects unknown top-level fields and unknown content-part fields.
    Does NOT run structural validation; use ``validate`` afterward.
    """
    if isinstance(data, (bytes, bytearray)):
        text = data.decode("utf-8")
    else:
        text = data
    raw = json.loads(text)
    if not isinstance(raw, dict):
        raise ValueError("stem: envelope: expected JSON object at top level")
    return Envelope.from_dict(raw)


def serialize_envelope(env: Envelope) -> str:
    """Encode an ``Envelope`` to its canonical JSON string form.

    Empty ``turns`` serializes to ``[]`` (never ``null``). Image parts
    that violate the data/url XOR contract raise ``ValueError`` rather
    than emit invalid JSON.
    """
    return json.dumps(env.to_dict(), ensure_ascii=False, separators=(",", ":"))


def validate_bytes(data: str | bytes | bytearray) -> list[EnvelopeError]:
    """Strict-decode + structural validate in one call.

    Returns a list of errors. Strict decode failures (unknown fields,
    bad JSON) raise ``ValueError`` immediately; only structural
    violations come back through the returned list.
    """
    env = parse_envelope(data)
    return validate(env)
