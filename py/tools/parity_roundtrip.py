"""parity_roundtrip — read a crtx v0.1 envelope JSON file at argv[1],
parse via ``stem.parse_envelope``, re-serialize via
``stem.serialize_envelope``, write the result to stdout.

Used by ``tools/parity/runner.sh``. Run from the repo root as::

    cd py && PYTHONPATH=src python3 tools/parity_roundtrip.py <path>

Or via uv::

    cd py && uv run python tools/parity_roundtrip.py <path>
"""
from __future__ import annotations

import sys
from pathlib import Path

from stem import parse_envelope, serialize_envelope


def main(argv: list[str]) -> int:
    if len(argv) != 2:
        print("usage: parity_roundtrip.py <envelope.json>", file=sys.stderr)
        return 2

    fixture = Path(argv[1])
    try:
        text = fixture.read_text(encoding="utf-8")
    except OSError as err:
        print(f"parity-roundtrip: read {fixture}: {err}", file=sys.stderr)
        return 1

    try:
        env = parse_envelope(text)
    except Exception as err:  # noqa: BLE001 — surface every parse failure
        print(f"parity-roundtrip: parse: {err}", file=sys.stderr)
        return 1

    try:
        out = serialize_envelope(env)
    except Exception as err:  # noqa: BLE001
        print(f"parity-roundtrip: serialize: {err}", file=sys.stderr)
        return 1

    sys.stdout.write(out)
    return 0


if __name__ == "__main__":
    raise SystemExit(main(sys.argv))
