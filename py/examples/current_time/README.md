# current_time (py)

Smallest viable stem agent loop, Python edition. Manual envelope
wiring with one tool-use round-trip against the Anthropic Messages
API. Mirrors `../../../go/examples/current-time/` wire-for-wire so
polyglot SDKs stay in lock-step.

## What it demonstrates

- Construct a `stem.Envelope` directly (no runtime layer).
- Append user / assistant / tool turns with the typed `TextPart` /
  `ToolCallPart` / `ToolResultPart` constructors.
- Project an Anthropic message's `tool_use` block into a stem
  `ToolCallPart`.
- Execute the tool locally and feed the result back to the model.
- Serialize the envelope to `./session.jsonl` (one envelope per line —
  matches the crtx JSONL convention).
- Reload the JSONL and `stem.validate` the round-trip.

The Anthropic HTTP transport is wrapped with [xrr] via a small `httpx`
transport (`xrr_httpx.py`) so the same code records cassettes once and
replays them deterministically forever after.

## Run it

Default mode is replay. Cassettes are vendored at
`./cassettes/`; no API key required:

```
uv sync
uv run python main.py
```

Expected output:

```
wrote .../session.jsonl (4 turns)
envelope round-trip + validate: ok
```

`session.jsonl` is gitignored — runtime output, not source.

## Re-record cassettes

Recording requires a real Anthropic API key:

```
XRR_MODE=record ANTHROPIC_API_KEY=sk-ant-... uv run python main.py
```

Cassettes will be (re-)written under
`./cassettes/`. xrr persists only the
request method, URL, body, and the response status / headers / body —
no `x-api-key` header or other secret material is captured.

## Verify

- Process exits 0.
- `./session.jsonl` exists and is valid JSON (`jq . session.jsonl`).
- The printed line ends in `validate: ok`.

## Common issues

| Symptom | Cause | Fix |
|---------|-------|-----|
| `xrr: cassette miss` | Anthropic request body changed since recording. | Rerun with `XRR_MODE=record` + `ANTHROPIC_API_KEY`. |
| Py-side cassettes not yet recorded | Py recording is post-v0.1. | Replay the Go cassettes via the Go example, or wait for the Py recording pass. |

## Runtime layer note

This example deliberately avoids any runtime sugar so polyglot SDKs in
TS / Rs / PHP can mirror this exact wire-level flow without relying on
language-specific helpers.

[xrr]: https://github.com/hop-top/poly-xrr
