# tools/parity

Cross-SDK envelope parity harness for stem's 5 polyglot SDKs.

## What this proves

Given the same crtx v0.1 envelope, every SDK MUST parse it into its
native type, re-serialize it back to a JSON string, and — after the
same canonicalization pass — produce **byte-identical output**.

That is: the SDKs agree on the wire shape. Field set, field
ordering after canonicalization, optional-vs-omitted semantics, empty
collections, number / boolean / unicode representation — all identical
across Go, TypeScript, Python, Rust, and PHP.

## What this does NOT prove

- Behavioral equivalence (runtime, provider adapters, tool dispatch).
- HTTP cassette fingerprints (a separate, known mismatch tracked in
  each SDK's README).
- Performance, allocation, or threading parity.
- Validate / error-message equivalence — validation is covered by
  each SDK's negative-fixture tests, not here.

This is a **wire-format conformance** harness, period.

## How to run

From the repo root:

```sh
make test-parity
```

Per-SDK debugging:

```sh
make test-parity-go
make test-parity-ts
make test-parity-py
make test-parity-rs
make test-parity-php
```

Or invoke the runner directly:

```sh
python3 tools/parity/runner.py            # all SDKs
python3 tools/parity/runner.py go py      # subset
```

The runner expects `jq` on `$PATH` and per-SDK toolchains (`go`,
`node`, `python3`, `cargo`, `php`). The TS helper consumes the
compiled `ts/dist/` — `make test-parity` arranges that automatically
via `pnpm build`; the direct `runner.py` invocation does not.

## SDKs covered

| SDK | Helper | Public API used |
|-----|--------|-----------------|
| Go | `go/cmd/parity-roundtrip/main.go` | `json.Unmarshal` into `stem.Session` + `json.Marshal` |
| TypeScript | `ts/tools/parity-roundtrip.js` | `parseEnvelope` + `serializeEnvelope` |
| Python | `py/tools/parity_roundtrip.py` | `parse_envelope` + `serialize_envelope` |
| Rust | `rs/examples/parity-roundtrip.rs` | `parse_envelope` + `serialize_envelope` |
| PHP | `php/tools/parity-roundtrip.php` | `HopTop\Stem\parseEnvelope` + `HopTop\Stem\serializeEnvelope` |

Each helper takes one argument (path to a crtx envelope JSON), reads
the file, calls the public parse, calls the public serialize, and
writes the result to stdout. Helpers exit nonzero with an error on
stderr on any failure.

## Fixtures

3 vendored crtx v0.1 examples. Source of truth lives in the upstream
`crtx` repo under `specs/v0.1/examples/`; each SDK keeps a private
copy under its own `testdata` directory so SDK tests never reach
across SDK boundaries.

| Fixture | Exercises | Per-SDK paths |
|---------|-----------|---------------|
| `minimal.json` | Bare envelope: required fields, two text turns | `go/testdata/crtx_v0.1/`, `ts/testdata/crtx_v0.1/`, `py/tests/testdata/crtx_v0.1/`, `rs/tests/testdata/crtx_v0.1/`, `php/tests/testdata/crtx_v0.1/` |
| `tool-call.json` | `tool_call` + `tool_result` content parts; nested objects in `input` / `output`; optional `source.instance` |  same layout |
| `fork.json` | `parent_id` + `fork_point` + `metadata`; demonstrates the fork-pointer pair |  same layout |

## Canonicalization

Each helper emits its native serialization. The runner pipes that
through `jq -cS .`:

- `-c` — compact (no whitespace)
- `-S` — sort object keys recursively

The canonical form is what the runner compares byte-for-byte across
SDKs. Trailing newlines are stripped by `jq -cS` (it writes a single
trailing `\n` which all 5 outputs share equally, so it does not affect
the comparison).

## Reading a failure

The matrix shows one cell per `(SDK, fixture)`:

| Cell | Meaning |
|------|---------|
| `ok` | Helper ran, output canonicalized, matched the reference SDK |
| `diff` | Helper ran but canonical output diverges from the reference SDK for this fixture — a real parity violation |
| `FAIL` | Helper exited nonzero; stderr is printed below the matrix |
| `BADJSON` | Helper succeeded but produced non-JSON output that `jq` refused |

The "reference SDK" for a given fixture is the first SDK in the run
list that produced `ok`. The runner then prints a unified diff
between the reference canonical form and each divergent SDK's
canonical form.

**A divergence is always either an SDK bug or a spec ambiguity.**
Do NOT paper over it by changing the canonicalization — fix the
non-conformant SDK, or escalate the ambiguity to the `crtx` spec.

Known shapes to watch when triaging:

- empty object vs empty array (`{}` vs `[]` for `tool_call.input`,
  `metadata`)
- integer vs float for `fork_point`
- unicode escaping (`é` vs `é`) — `jq -cS` normalizes to raw
  UTF-8 so this should not matter, but verify
- `metadata: null` vs omitted — the spec marks it optional; omitted is
  canonical
- map key ordering — `jq -cS` sorts, so this never matters

## Adding a new SDK

1. Vendor the 3 fixtures into the SDK's testdata directory.
2. Add a `parity-roundtrip` helper that reads `argv[1]`, calls the
   SDK's public parse + serialize, writes to stdout.
3. Register the SDK in:
   - `tools/parity/runner.py` — add to `ALL_SDKS`, `testdata_dir`,
     and `helper_command`.
   - `Makefile` — add `test-parity-<lang>` and any prerequisite step
     to `_parity-build`.
   - `.github/workflows/ci.yml` — extend the `parity` job's setup
     steps with the new toolchain.

## Adding a new fixture

1. Land the fixture upstream in `crtx/specs/v0.1/examples/`.
2. Copy it into each SDK's testdata directory using the existing names.
3. Add it to the `FIXTURES` list in `tools/parity/runner.py`.

No other code change required — the harness loops over `FIXTURES ×
SDKS` automatically.
