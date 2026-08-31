# current-time

Smallest viable stem agent loop. Manual envelope wiring with one
tool-use round-trip against the Anthropic Messages API.

## What it demonstrates

- Construct a `stem.Session` directly (no runtime layer).
- Append user / assistant / tool turns with the typed
  `stem.TextPart` / `stem.ToolCallPart` / `stem.ToolResultPart`
  constructors.
- Project an Anthropic `Message` response (with a `tool_use` block)
  into a stem `ContentPart` of type `tool_call`.
- Execute the tool locally and feed the result back to the model.
- Serialize the envelope to `./session.jsonl` (one envelope per
  line — matches the crtx JSONL convention).
- Reload the JSONL and `stem.Validate` the round-trip.

The Anthropic HTTP transport is wrapped with [xrr] so the same code
records cassettes once and replays them deterministically forever
after.

## Run it

Default mode is replay. Cassettes are vendored at
`./cassettes/`; no API key required:

```
go run .
```

You should see:

```
wrote ./session.jsonl (4 turns)
envelope round-trip + validate: ok
```

`session.jsonl` is gitignored — it is runtime output, not source.

## Re-record cassettes

Recording requires a real Anthropic API key:

```
XRR_MODE=record ANTHROPIC_API_KEY=sk-ant-... go run .
```

Cassettes will be (re-)written under
`./cassettes/`. xrr only persists the
request method, URL, body, and the response status / headers / body —
no `X-Api-Key` header or other secret material is captured.

## Verify

- Process exits 0.
- `./session.jsonl` exists and is valid JSON (`jq . session.jsonl`).
- The printed line ends in `validate: ok`.

## Common issues

| Symptom | Cause | Fix |
|---------|-------|-----|
| `xrr: cassette miss for http-XXXXXXXX` | Request body changed since cassette was recorded. | Rerun with `XRR_MODE=record` + real `ANTHROPIC_API_KEY`. |
| `unauthorized` during record | Bad / missing key. | Confirm `ANTHROPIC_API_KEY=sk-ant-...` is exported. |

## Runtime layer note

This example deliberately avoids stem's runtime layer (the
`stem.NewSession(..., WithProvider(...), WithStore(...),
WithToolRegistry(...))` shape and `runtime.go`) so polyglot SDKs in
TS / Py / Rs / PHP can mirror this exact wire-level flow without
relying on Go-only sugar. The runtime layer automates the same loop
in production code — see `go/runtime.go`.

[xrr]: https://github.com/hop-top/xrr
