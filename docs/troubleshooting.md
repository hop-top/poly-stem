# Troubleshoot stem

Cross-cutting failure modes for stem, across all five SDKs. Per-SDK
troubleshooting tables live in each SDK's `README.md`; this page
covers the issues that span languages.

## Use this when

- You hit an error that's not in your SDK's README.
- A failure reproduces in more than one SDK.
- `make test-parity` shows a `diff` cell.
- Sample apps replay in one language but not another.

## Before you debug

Confirm you can isolate the failure:

- Which SDK reproduces it? Try the Go reference first — if Go works
  and the polyglot SDK doesn't, suspect the polyglot SDK's
  serialization.
- Which operation? `parse` / `serialize` / `validate` / `run sample` /
  `make test-parity` each have different failure surfaces.
- Which fixture or input? The three vendored fixtures (`minimal`,
  `tool-call`, `fork`) under each SDK's `testdata/crtx_v0.1/` are
  the canonical reproductions.

## Install + module resolution

| Symptom | Cause | Fix |
|---------|-------|-----|
| `go: hop.top/stem@... unknown module` | GOPROXY can't reach `hop.top`. | Set `GOPROXY=https://proxy.golang.org,direct`, or pull via mirror `github.com/hop-top/poly-stem/tree/main/go`. |
| `npm ERR! 404` for `@hop-top/stem` | Scope not yet published, or registry mirror. | Confirm `pnpm config get registry` points at `https://registry.npmjs.org/`. The package is public; no auth needed. |
| `pip: No matching distribution for hop-top-stem` | Python version too old. | Requires Python 3.12+. Check `python --version`. |
| `cargo: failed to select a version for hop-top-stem` | Crate yanked, or version range too narrow. | Drop the constraint or pin to `0.1.0-alpha.0` explicitly. |
| `composer: could not find a matching version of package hop-top/stem` | Composer cache stale. | `composer clear-cache && composer require hop-top/stem`. |

## Envelope parse / validate

These fire across every SDK; the canonical fixes are the same.

| Symptom | Cause | Fix |
|---------|-------|-----|
| `unknown envelope field <name>` | Producer wrote a field crtx v0.1 doesn't allow at the top level. | Move it under `metadata`, or remove. |
| `unknown ContentPart type "<name>"` | Non-standard content part missing the `x-` prefix. | Rename `type` to `x-<reverse-dns>` so it round-trips as `ExtensionPart`. |
| `tool_result.call_id "X" has no preceding tool_call` | The tool turn's `call_id` doesn't match an earlier `tool_call` in the same envelope. | Reuse the assistant's `tool_call.call_id` verbatim. |
| `image part: exactly one of data/url required` | `ImagePart` has both or neither. | Pick one — base64 in `data` or remote URL in `url`. |
| `crtx_version "0.0" unsupported` | Wrong spec version on the wire. | This SDK speaks `0.1`. Bump the producer or pin an older SDK release. |
| `parent_id` set but `fork_point` missing (or vice versa) | The pair must appear together. | Set both, or omit both. See [envelope.md §7.2](https://github.com/hop-top/spec-crtx/blob/main/specs/v0.1/envelope.md#72-dispatch-nests). |

## Record / replay (xrr)

See [`record-replay.md`](record-replay.md) for the full workflow.

| Symptom | Cause | Fix |
|---------|-------|-----|
| `xrr: cassette miss for http-XXXXXXXX` | Request body changed since the cassette was recorded. | Re-record with `XRR_MODE=record` + the relevant API key. Body fingerprint hashes the full bytes; even reordered JSON keys change it. |
| `xrr: cassette dir does not exist` | First-time use, or wrong path. | `mkdir -p ./cassettes` before instantiating the cassette, or commit the directory from elsewhere. |
| Replay works in Go but not in TS / Py / Rs / PHP | Cassettes are per-language. Different LLM SDKs emit different JSON for the same logical request. | Record per language; don't share cassette directories across SDKs. |
| `xrr: unknown XRR_MODE "..."` | Typo. | Use `record`, `replay`, `passthrough`, or unset (defaults to replay). |
| Recorded cassette contains an API key | A request header leaked into the request capture. | xrr captures response headers only, but if your client logs request headers itself, scrub before committing. Audit cassettes before pushing. |

## Cross-SDK parity (`make test-parity`)

| Symptom | Cause | Fix |
|---------|-------|-----|
| One cell shows `diff` | That SDK's canonical JSON diverges from the reference. | Inspect the unified diff the runner prints. Fix the non-conformant SDK — don't change canonicalization. |
| `FAIL` cell | Helper exited nonzero. | Stderr is printed below the matrix. Run the helper directly: `python3 tools/parity/runner.py <sdk>`. |
| `BADJSON` cell | Helper output isn't valid JSON. | The helper is supposed to write canonical JSON to stdout. Check it isn't printing logs to stdout instead of stderr. |
| All TS / Py / PHP cells fail before TS is built | TS helper requires `ts/dist/`. | Run `make test-parity` (it runs `pnpm build` first) rather than `runner.py` directly. |

## Runtime (Go-only at v0.1)

| Symptom | Cause | Fix |
|---------|-------|-----|
| Looking for `Provider` / `Runtime` / `Store` in TS / Py / Rs / PHP | These ship Go-only at v0.1. | Run `stem-go` as a sidecar / subprocess; consume the envelopes it emits from the polyglot SDK. |
| `stem: no provider configured` | Forgot `RuntimeWithProvider` on `NewRuntime`. | Pass `stem.RuntimeWithProvider(provider)`. |
| `stem: max tool depth exceeded` | Provider keeps emitting `tool_call` parts. | Raise the cap with `RuntimeWithMaxToolRounds(n)`, or fix the Provider. |
| `Dispatch` panics with `ErrCallNotOpen` | Tried to dispatch a child from a `call_id` that doesn't have an open `tool_call` in the parent. | Confirm the parent has appended the `tool_call` and not yet appended its `tool_result`. |

## When to escalate

- The parity matrix is green but the fixtures don't match what your
  producer emits → file an issue against the producer or open one in
  [`hop-top/poly-stem`](https://github.com/hop-top/poly-stem/issues)
  with a reduced fixture.
- crtx spec ambiguity (two SDKs disagree on a corner case the spec
  doesn't pin down) → land the fix in
  [`hop-top/spec-crtx`](https://github.com/hop-top/spec-crtx) first.
  Do NOT paper it over by changing canonicalization in one SDK.

## Next steps

- Per-SDK Common Issues tables: [`../go/README.md`](../go/README.md),
  [`../ts/README.md`](../ts/README.md), [`../py/README.md`](../py/README.md),
  [`../rs/README.md`](../rs/README.md), [`../php/README.md`](../php/README.md).
- Wire spec: [crtx v0.1 envelope](https://github.com/hop-top/spec-crtx/blob/main/specs/v0.1/envelope.md).
- Record / replay workflow: [record-replay.md](record-replay.md).
- Report a security issue: [`../SECURITY.md`](../SECURITY.md).
