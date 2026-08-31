# current-time

Smallest viable stem (TS) agent loop. Manual envelope wiring with one
Anthropic Messages tool-use round-trip.

## What it demonstrates

- Construct an `Envelope` by hand (no runtime layer).
- Append `user` / `assistant` / `tool` turns with literal `ContentPart`
  values typed against the SDK.
- Project the Anthropic Messages response (with a `tool_use` block)
  into a stem `ContentPart` of type `tool_call`.
- Execute the tool locally and feed the result back to the model.
- Serialize the envelope to `./session.jsonl` (one envelope per
  line — matches the crtx JSONL convention).
- Reload the JSONL and run `validate()` on the round-trip.

The Anthropic HTTP transport is wrapped with [xrr] via the SDK's
`fetch` option, so the same code records cassettes once and replays
them deterministically forever after.

## Install

```bash
pnpm install
```

(`@hop-top/stem` is consumed via a local `link:` to the sibling
package so changes flow through without a publish step.)

## Run it (replay — default)

No API key required. Cassettes live at
`./cassettes/`:

```bash
pnpm start
```

You should see:

```
wrote .../session.jsonl (4 turns)
envelope round-trip + validate: ok
```

`session.jsonl` is gitignored — runtime output, not source.

## Re-record cassettes

Recording requires a real Anthropic API key:

```bash
XRR_MODE=record ANTHROPIC_API_KEY=sk-ant-... pnpm start
```

Cassettes will be (re-)written under
`./cassettes/`. xrr only persists the
request method, URL, body, and the response status / headers / body —
no `x-api-key` header or other secret material is captured.

## Verify

- Process exits 0.
- `./session.jsonl` exists and is valid JSON (`jq . session.jsonl`).
- The printed line ends in `validate: ok`.

## Common issues

| Symptom | Cause | Fix |
|---------|-------|-----|
| `xrr: cassette miss` | Anthropic request body changed since recording. | Rerun with `XRR_MODE=record` + real `ANTHROPIC_API_KEY`. |
| TS-side cassettes not yet recorded | TS recording is post-v0.1. | Use Go's cassettes via the Go example, or wait for the TS recording pass. |

## Runtime layer note

This example deliberately avoids any runtime layer (provider
abstractions, session stores, tool registries, supervisors) so
polyglot SDKs in TS / Py / Rs / PHP can mirror this exact wire-level
flow. Tier B in a future stem version will automate the same loop in
production code.

[xrr]: https://github.com/hop-top/poly-xrr
