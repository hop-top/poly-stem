# stem (rs)

Rust SDK for [`stem`](https://github.com/hop-top/poly-stem), the
polyglot AI agent runtime. Ships the
[crtx v0.1](https://github.com/hop-top/spec-crtx) envelope types plus
strict parse / serialize / validate helpers.

> This crate ships the **Tier A** SDK surface: envelope I/O only. No
> provider trait, supervisor, or storage backends — consumers wire
> their own runtime around the envelope shape. The Go SDK in
> `../go/` is the reference for the higher-level runtime layer.

## Use this when

- You are recording an AI session in Rust and want it readable by
  other crtx-aware tools.
- You need typed access to crtx envelopes with `serde`-driven strict
  decode.
- You are building a Rust tool (CLI, indexer, validator) that
  consumes envelopes produced by stem-go.

If you need an agent loop, tool dispatch, or session persistence, use
the Go SDK — those are Go-only at v0.1.

## Before you begin

- Rust 2021 edition (rustc 1.75+ recommended).
- `cargo` for install.
- Runtime dependency: `serde` + `serde_json` (pulled transitively).

## Outcome

After installing, you can parse a crtx v0.1 envelope from JSON, build
one from scratch with strongly-typed structs and enums, and validate
either against the same structural rules the Go reference enforces.

## Install

```text
cargo add hop-top-stem
```

## Quickstart

```rust
use hop_top_stem::{parse_envelope, serialize_envelope, validate, Envelope};

let raw = r#"{
  "crtx_version": "0.1",
  "id": "demo-1",
  "created_at": "2026-01-01T00:00:00Z",
  "updated_at": "2026-01-01T00:00:00Z",
  "source": {"kind": "stem", "version": "0.1.0"},
  "turns": []
}"#;

// Strict decode (rejects unknown fields).
let env: Envelope = parse_envelope(raw).unwrap();

// Structural validation returns every finding in one pass.
let findings = validate(&env);
assert!(findings.is_empty());

// Round-trip back to JSON.
let out = serialize_envelope(&env).unwrap();
```

## API surface

- **Types** — [`Envelope`], [`Turn`], [`Source`], [`Role`],
  [`ContentPart`] (discriminated union: `Text`, `ToolCall`,
  `ToolResult`, `Image`, `Thinking`, `Extension`).
- **Constants** — [`CRTX_VERSION`] (`"0.1"`), [`VERSION`]
  (`"0.1.0"`), [`SOURCE_KIND`] (`"stem"`).
- **Functions** — [`parse_envelope`], [`serialize_envelope`],
  [`validate`], [`validate_bytes`].
- **Errors** — [`EnvelopeError`] (decode / encode / per-rule
  structural variants).

## Extension parts

ContentParts with `type` matching the `^x-[a-zA-Z0-9._-]+$` regex
round-trip verbatim through [`ContentPart::Extension`]; the original
JSON object is preserved in `raw` so unknown extra fields survive
re-serialization byte-equivalent.

## Examples

Sample agent loops live under [`examples/`](./examples/). Both use
[`hop-top-xrr`](https://crates.io/crates/hop-top-xrr) to replay
recorded HTTP cassettes from each sample's own `cassettes/` dir, so the
examples run end-to-end with no API key by default.

```text
cargo run -p stem-example-current-time
cargo run -p stem-example-web-search
```

## Verify

```sh
cd rs && cargo test --workspace
```

Expected: all tests pass. To prove this SDK still matches the wire
spec, run the cross-SDK harness from the repo root:

```sh
make test-parity-rs
```

Expected: `rs: 3/3 ok` against the `minimal`, `tool-call`, and `fork`
fixtures.

## Common issues

| Symptom | Cause | Fix |
|---------|-------|-----|
| `unknown field "<name>"` from `parse_envelope` | Producer wrote a field crtx v0.1 doesn't allow. | Move the field under `metadata`, or remove it. |
| `EnvelopeError` returned by `validate(&env)` | Structural rule failed (role enum, image data/url XOR, orphan tool_result, ...). | Match on the variant; each carries a path / message. |
| `ContentPart::Extension` re-serializes with a different shape | Forgot to use the `x-` prefix on `type`. | Rename to `x-<reverse-dns>` so the extension regex matches and the `raw` body round-trips verbatim. |
| Looking for `Provider` / `Runtime` / `Store` traits | Tier B (runtime) is Go-only at v0.1. | Run `stem-go` as a sidecar and consume the envelopes from Rust. |
| `make test-parity` shows `diff` for `rs` | Canonical JSON diverged from the reference SDK. | Don't mutate canonicalization — fix the serializer. |

For cross-SDK issues (parity diffs, envelope parse errors, xrr
cassette pitfalls), see [`../docs/troubleshooting.md`](../docs/troubleshooting.md).

## Next steps

- Side-by-side polyglot examples: [`../docs/quickstart-polyglot.md`](../docs/quickstart-polyglot.md).
- Record / replay LLM round-trips: [`../docs/record-replay.md`](../docs/record-replay.md).
- crtx v0.1 wire spec: [envelope.md](https://github.com/hop-top/spec-crtx/blob/main/specs/v0.1/envelope.md).

## License

MIT. See the
[`hop-top/poly-stem` LICENSE](https://github.com/hop-top/poly-stem/blob/main/LICENSE).
