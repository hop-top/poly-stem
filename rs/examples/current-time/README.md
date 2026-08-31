# current-time (Rust)

Smallest viable stem agent loop in Rust. Manual envelope wiring with
one tool-use round-trip against the Anthropic Messages API.

Polyglot sibling: [`go/examples/current-time/`](../../../go/examples/current-time/).

## What it demonstrates

- Construct an `Envelope` directly via the SDK (no runtime layer).
- Append user / assistant / tool turns using `ContentPart::text`,
  `::tool_call`, `::tool_result`.
- Project an Anthropic Messages response (with a `tool_use` block)
  into a stem `ContentPart::ToolCall`.
- Execute the tool locally and feed the result back to the model.
- Serialize the envelope to `./session.jsonl` and reload+`validate`.

The Anthropic HTTP traffic is replayed from cassettes vendored at
`./cassettes/` (recorded once by the Go
example, replayed by every polyglot port).

## Run it

Default mode is replay. No API key required:

```
cargo run -p stem-example-current-time
```

Expected output:

```
wrote ./session.jsonl (4 turns)
envelope round-trip + validate: ok
```

`session.jsonl` is gitignored — runtime output, not source.

## Why not the upstream `hop-top-xrr` HttpAdapter?

`hop-top-xrr` v0.1.0-alpha.5 ships an `HttpAdapter` whose request
body type is `Vec<u8>`, which serializes as a YAML sequence of bytes
instead of the `body: <string>` shape the Go-recorded cassettes use.
Until that mismatch is resolved upstream (TODO post-cassette pass),
this example loads cassettes directly via `serde_yaml` and computes
the same SHA-256-based fingerprint Go's adapter uses, so fingerprints
match byte-for-byte across languages.

## Re-record

Re-recording requires the Go example + a live Anthropic API key:

```
cd ../../../go/examples/current-time
XRR_MODE=record ANTHROPIC_API_KEY=sk-ant-... go run .
```

Cassettes land under `./cassettes/` and every
polyglot port (Go / TS / Py / Rs / PHP) replays the same files.

## Verify

- Process exits 0.
- `./session.jsonl` exists and is valid JSON (`jq . session.jsonl`).
- The printed line ends in `validate: ok`.

## Common issues

| Symptom | Cause | Fix |
|---------|-------|-----|
| `xrr: cassette miss` | Anthropic request body changed since recording. | Re-record via the Go example with `XRR_MODE=record` + `ANTHROPIC_API_KEY`. |
| YAML parse error from cassette | Cassette was written by `hop-top-xrr` v0.1.0-alpha.5+ with `Vec<u8>` body shape. | Use the Go-recorded cassettes (already vendored), or wait for upstream alignment. |
