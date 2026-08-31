# stem (py)

Python SDK for `stem`, the polyglot AI agent runtime. Implements
envelope I/O for the [crtx v0.1](https://github.com/hop-top/spec-crtx) spec:
parse / serialize / structurally validate the conversation envelope
that crosses every wire and hits every disk.

> This repository is a read-only language mirror. Open issues and pull
> requests in [`hop-top/poly-stem`](https://github.com/hop-top/poly-stem).

## Scope (Tier A)

This SDK is **envelope I/O only**. It does not ship a runtime,
provider, tool registry, supervisor, or storage backend. Build those on
top — `stem`'s contract is the envelope.

## Use this when

- You are recording an AI session in Python and want it readable by
  other crtx-aware tools.
- You are converting a CLI's JSONL transcript into crtx for downstream
  indexing.
- You are inspecting or rewriting envelope files outside Go.
- You need strict decode (rejects unknown fields) and structured
  validation errors.

If you need an agent loop, tool dispatch, or session persistence, use
the Go SDK — those are Go-only at v0.1.

## Before you begin

- CPython 3.12 or newer.
- `pip` (or `uv`, `poetry`, `pdm`) for install.
- Zero runtime dependencies.

## Outcome

After installing, you can parse a crtx v0.1 envelope from JSON, build
one from scratch with dataclass-style constructors, and validate
either against the same structural rules the Go reference enforces.

## Install

```bash
pip install hop-top-stem
```

Requires Python 3.12+.

## Quickstart

Construct an envelope, serialize it, and validate the round-trip.

```python
from stem import (
    CRTX_VERSION,
    Envelope,
    Role,
    Source,
    TextPart,
    Turn,
    parse_envelope,
    serialize_envelope,
    validate_bytes,
)

env = Envelope(
    crtx_version=CRTX_VERSION,
    id="01JCRTX0EXAMPLE0001",
    created_at="2026-05-28T14:00:00Z",
    updated_at="2026-05-28T14:00:01Z",
    source=Source(kind="stem", version="0.1.0"),
    turns=[
        Turn(
            id="t-user-0",
            role=Role.USER,
            created_at="2026-05-28T14:00:00Z",
            content=[TextPart(text="Hello.")],
        ),
        Turn(
            id="t-assistant-1",
            role=Role.ASSISTANT,
            created_at="2026-05-28T14:00:01Z",
            content=[TextPart(text="Hi! How can I help?")],
        ),
    ],
)

wire = serialize_envelope(env)         # canonical JSON
back = parse_envelope(wire)            # strict-decode (rejects unknown fields)
assert validate_bytes(wire) == []      # [] == valid
```

## Tool-use round-trip

Manual envelope wiring for one assistant + tool round-trip. The same
pattern the polyglot sample apps follow — see [`examples/`](./examples).

```python
from stem import (
    Envelope, Role, Source, Turn,
    TextPart, ToolCallPart, ToolResultPart,
)

sess = Envelope(
    crtx_version="0.1",
    id="s-tool-demo",
    created_at="2026-05-28T14:05:00Z",
    updated_at="2026-05-28T14:05:03Z",
    source=Source(kind="stem", version="0.1.0"),
    turns=[
        Turn(id="u-1", role=Role.USER,
             created_at="2026-05-28T14:05:00Z",
             content=[TextPart(text="What time is it?")]),
        Turn(id="a-1", role=Role.ASSISTANT,
             created_at="2026-05-28T14:05:01Z",
             content=[ToolCallPart(call_id="c1", name="current_time", input={})]),
        Turn(id="t-1", role=Role.TOOL,
             created_at="2026-05-28T14:05:02Z",
             content=[ToolResultPart(call_id="c1",
                                     output={"time": "2026-01-01T00:00:00Z"})]),
        Turn(id="a-2", role=Role.ASSISTANT,
             created_at="2026-05-28T14:05:03Z",
             content=[TextPart(text="It's 2026-01-01T00:00:00Z UTC.")]),
    ],
)
```

## Extension parts

ContentParts whose `type` starts with `x-` round-trip verbatim. Use
reverse-DNS (`x-io.jadb.custom`) to avoid collisions. The body is
preserved by ``ExtensionPart`` exactly as received; unknown built-in
fields, by contrast, are rejected by strict decode.

```python
from stem import parse_envelope, serialize_envelope

raw = '{"type":"x-io.jadb.custom","note":"keep me"}'
# Use in a Turn.content[] and parse_envelope/serialize_envelope round-trip it verbatim.
```

## Validation

`validate(env)` returns a list of `EnvelopeError` for structural
violations: missing required fields, unknown roles, image data/url
XOR, orphan `tool_result`, bad `crtx_version`. Empty list ==
structurally valid.

`validate_bytes(data)` runs strict decode (rejects unknown top-level
fields) followed by `validate`. Use this on the boundary when accepting
envelopes from external producers; use `validate(env)` on already-parsed
envelopes.

## Public API

`Envelope`, `Turn`, `Source`, `Role`, `EnvelopeError`,
`TextPart`, `ToolCallPart`, `ToolResultPart`, `ImagePart`,
`ThinkingPart`, `ExtensionPart`, `parse_envelope`, `serialize_envelope`,
`validate`, `validate_bytes`, `content_part_from_dict`,
`content_part_to_dict`, `is_extension_type`, `is_valid_extension_type`,
`CRTX_VERSION`, `VERSION`, `SOURCE_KIND`,
`PART_TYPE_TEXT`, `PART_TYPE_TOOL_CALL`, `PART_TYPE_TOOL_RESULT`,
`PART_TYPE_IMAGE`, `PART_TYPE_THINKING`.

## Verify

```sh
cd py && uv sync && uv run pytest
```

Expected: all tests pass. To prove this SDK still matches the wire
spec, run the cross-SDK harness from the repo root:

```sh
make test-parity-py
```

Expected: `py: 3/3 ok` against the `minimal`, `tool-call`, and `fork`
fixtures.

## Common issues

| Symptom | Cause | Fix |
|---------|-------|-----|
| `unknown envelope field <name>` on parse | Producer wrote a field crtx v0.1 doesn't allow. | Move the field under `metadata`, or remove it. |
| `EnvelopeError` returned by `validate(env)` | Structural rule failed (role enum, image data/url XOR, orphan tool_result, ...). | Inspect `EnvelopeError.path` and `EnvelopeError.message`. |
| `unknown ContentPart type "my-thing"` | Custom content part missing the `x-` prefix. | Rename `type` to `x-<reverse-dns>` so it round-trips as an extension. |
| Looking for `Provider`, `Runtime`, `Store` | Tier B (runtime) is Go-only at v0.1. | Run `stem-go` as a sidecar and consume the envelopes from Python. |
| `make test-parity` shows `diff` for `py` | Canonical JSON diverged from the reference SDK. | Don't mutate canonicalization — fix the serializer. |

For cross-SDK issues (parity diffs, envelope parse errors, xrr
cassette pitfalls), see [`../docs/troubleshooting.md`](../docs/troubleshooting.md).

## Spec

[crtx v0.1 envelope spec](https://github.com/hop-top/spec-crtx/tree/main/specs/v0.1).

## Next steps

- Side-by-side polyglot examples: [`../docs/quickstart-polyglot.md`](../docs/quickstart-polyglot.md).
- Record / replay LLM round-trips: [`../docs/record-replay.md`](../docs/record-replay.md).

## License

MIT. See the [`hop-top/poly-stem` LICENSE](https://github.com/hop-top/poly-stem/blob/main/LICENSE).
