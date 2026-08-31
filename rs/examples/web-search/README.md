# web-search (Rust)

Second stem agent example in Rust. Same envelope-wiring shape as
[`../current-time`](../current-time), but the tool is `web_search`,
backed by the Tavily search API. One stem `Envelope` captures the
full transcript; two upstream services (Anthropic + Tavily) are
recorded into the same xrr cassette directory because each HTTP call
has a distinct method + URL + body fingerprint.

Polyglot sibling: [`go/examples/web-search/`](../../../go/examples/web-search/).

## What it demonstrates

- Two upstream services replayed through one cassette directory.
- The same Rust SDK shape (`parse_envelope`, `serialize_envelope`,
  `validate`) handling a multi-tool transcript.
- A real-world POST body (Tavily JSON) round-tripping through the
  recorded fingerprint without any custom adapter wiring.
- The Tavily request body always carries a stable placeholder for
  `api_key` (`"replay-mode-no-key-required"`) so the shipped
  cassettes replay deterministically regardless of who recorded them.
  Real keys never enter the request body or the cassette.

## Run it

Default mode is replay. Cassettes ship under
`./cassettes/`; no API keys required:

```
cargo run -p stem-example-web-search
```

Expected output:

```
wrote ./session.jsonl (4 turns)
envelope round-trip + validate: ok
```

`session.jsonl` is gitignored — runtime output, not source.

## Re-record cassettes

Re-recording requires the Go example plus live API keys for both
Anthropic and Tavily. See the Go sibling's README for the full
procedure:

```
cd ../../../go/examples/web-search
XRR_MODE=record ANTHROPIC_API_KEY=sk-ant-... TAVILY_API_KEY=tvly-... go run .
```

Cassettes land under `./cassettes/` and every
polyglot port (Go / TS / Py / Rs / PHP) replays the same files.

## Verify

- Process exits 0.
- `./session.jsonl` exists and is valid JSON.
- The printed line ends in `validate: ok`.

## Common issues

| Symptom | Cause | Fix |
|---------|-------|-----|
| `xrr: cassette miss` for Tavily / Anthropic | Request body changed since recording. | Re-record via the Go example with both keys. |
| YAML parse error from cassette | Cassette was written with `Vec<u8>` body shape. | Use the Go-recorded cassettes (already vendored). |

## Why not the upstream `hop-top-xrr` HttpAdapter?

`hop-top-xrr` v0.1.0-alpha.5 ships an `HttpAdapter` whose request
body type is `Vec<u8>`, which serializes as a YAML sequence of bytes
instead of the `body: <string>` shape the Go-recorded cassettes use.
Until that mismatch is resolved upstream (TODO post-cassette pass),
this example loads cassettes directly via `serde_yaml` and computes
the same SHA-256-based fingerprint Go's adapter uses, so fingerprints
match byte-for-byte across languages.
