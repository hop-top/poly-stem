# poly-stem

[![Release](https://img.shields.io/github/v/release/hop-top/poly-stem?include_prereleases&sort=semver)](https://github.com/hop-top/poly-stem/releases)
[![CI](https://github.com/hop-top/poly-stem/actions/workflows/ci.yml/badge.svg)](https://github.com/hop-top/poly-stem/actions/workflows/ci.yml)
[![12fc](https://img.shields.io/endpoint?url=https://raw.githubusercontent.com/hop-top/poly-stem/main/.12fc.json)](https://github.com/hop-top/poly-stem/actions/workflows/12fcc.yaml)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](LICENSE)

> [!WARNING]
> **Alpha — pre-1.0.** API surface and tag history may break. Pin to
> exact tags, not ranges. See [`CHANGELOG.md`](CHANGELOG.md).

## What stem is

`stem` is the AI agent runtime for the
[`crtx`](https://github.com/hop-top/spec-crtx) envelope format — session
lifecycle, turn execution, tool dispatch, multi-session supervision.
The Go SDK is the **reference runtime**: it drives the LLM, routes
turns, executes tools, persists state, forks children. The four other
language SDKs (TS, Py, Rs, PHP) ship at v0.1 as the
**envelope I/O layer only** — they parse, serialize, and validate
[crtx](https://github.com/hop-top/spec-crtx) (pronounced "cortex")
envelopes so non-Go services can consume sessions produced by stem.

## What ships in v0.1

| Lang | Package | Install | v0.1 scope |
|------|---------|---------|------------|
| Go | `hop.top/stem` | `go get hop.top/stem` | Runtime + tool registry + memory / JSONL / SQLite stores + multi-agent supervisor + kit adapter |
| TypeScript | `@hop-top/stem` | `pnpm add @hop-top/stem` | Envelope parse + serialize + validate (Tier A) |
| Python | `hop-top-stem` | `pip install hop-top-stem` | Envelope parse + serialize + validate (Tier A) |
| Rust | `hop-top-stem` | `cargo add hop-top-stem` | Envelope parse + serialize + validate (Tier A) |
| PHP | `hop-top/stem` | `composer require hop-top/stem` | Envelope parse + serialize + validate (Tier A) |

All five SDKs round-trip the same crtx v0.1 envelopes to byte-identical
canonical JSON — see [Parity](#parity).

## Use stem when

- You are building an agent in Go and want Provider-abstracted LLM,
  tool registry, persistent store (memory / JSONL / SQLite), and a
  supervisor for child sessions.
- You are recording or replaying agent sessions in TS / Py / Rs / PHP
  and need them readable by other crtx-aware tools.
- You are converting Claude Code / Codex / other CLI session logs into
  crtx so they flow through downstream indexers (vein, planned).
- You want to find and reread past agent sessions across CLI tools —
  see [Session recall CLI](#session-recall-cli).
- You want deterministic agent tests — record a real LLM round-trip
  once, replay in CI forever with no key.

## Do not use stem when

- You need a full agent runtime in TS, Py, Rs, or PHP today. Only Go
  ships a runtime at v0.1.
- You need prompt management, retrieval pipelines, evaluation
  harnesses, or fine-tuning workflows.
- You need cross-language behavioral parity. Wire format is identical
  across all five SDKs; runtime behavior is Go-only.
- You need a managed service. stem is a library; you run it.

## What is NOT in v0.1 polyglot

The TS, Py, Rs, and PHP SDKs deliberately exclude every runtime
concern. None of these exist outside Go at v0.1:

- `Provider` interface (LLM driver abstraction)
- Agent loop / turn execution
- Tool registry + dispatch
- Multi-session supervisor + topics
- Session stores (memory, JSONL, SQLite)
- Fork semantics enforcement (the envelope carries `parent_id` /
  `fork_point`; runtime logic for managing children is Go-only)

Polyglot runtime parity is post-v0.1. The wire format has to settle
across all five SDKs before runtime contracts can follow. No dates
promised.

## Quickstart

### Go — full runtime

```sh
mkdir stem-quickstart && cd stem-quickstart
go mod init example.com/stem-quickstart
go get hop.top/stem
# write main.go (see docs/quickstart-go.md for the full program)
go run .
```

Full runnable program with mock Provider + tool registry + memory
store: [`docs/quickstart-go.md`](docs/quickstart-go.md). The Go SDK
surface (Runtime, Supervisor, JSONL / SQLite stores, fork / dispatch)
lives in [`go/README.md`](go/README.md).

### TypeScript / Python / Rust / PHP — envelope I/O only

```sh
pnpm add @hop-top/stem     # TypeScript
pip install hop-top-stem   # Python
cargo add hop-top-stem     # Rust
composer require hop-top/stem  # PHP
```

Every SDK exposes the same three operations: `parseEnvelope`,
`serializeEnvelope`, `validate`. Side-by-side examples in all four
languages: [`docs/quickstart-polyglot.md`](docs/quickstart-polyglot.md).

The polyglot SDKs do not drive LLMs themselves — they consume
envelopes produced upstream (typically by stem-go) and emit envelopes
to be consumed downstream. Per-SDK READMEs:
[`ts/`](ts/README.md), [`py/`](py/README.md), [`rs/`](rs/README.md),
[`php/`](php/README.md).

## Session recall CLI

`stem sessions` restores cross-CLI recall of past agent sessions: one
read-only command group that queries every session on this machine,
whichever CLI produced it, on the crtx envelope layer.

| Command | Use it to |
|---------|-----------|
| `stem sessions list` | Enumerate sessions across every store, newest first |
| `stem sessions search <query>` | Find sessions by content or metadata |
| `stem sessions show <id>` | Reread one session turn by turn — or emit its crtx envelope verbatim with `--format json` |
| `stem sessions lineage <id>` | Trace the fork / dispatch chain around a session, across CLIs |

Sources covered: the Claude Code, Codex, and Copilot native stores
(read-only, normalized to crtx envelopes at read time) plus
crtx-native producers (stem, nerv) under
`$XDG_STATE_HOME/<producer>/sessions/`. Duplicate discoveries collapse
to one session; adapters never write to a native store.

```sh
go install hop.top/stem/cmd/stem@latest

stem sessions list --cwd ~/code/acme --since 7d    # this project, this week
stem sessions search "rate limiting"               # which session touched this?
stem sessions show 0198f2d4                        # any unique id prefix (>= 4 chars)
stem sessions show 0198f2d4 --format json | jq .   # verbatim crtx v0.1 envelope
stem sessions lineage 0198f2d4                     # forks + sub-agents, across CLIs
```

`list` output (synthetic):

```text
ID        SOURCE       UPDATED           TURNS  CWD                TITLE
0198f2d4  codex        2026-08-07 09:14     31  ~/code/acme        fix the flaky auth test
7c9a11b0  claude-code  2026-08-06 22:41    118  ~/code/acme        add rate limiting to the api
a44e02c9  stem         2026-08-06 18:02     12  ~/experiments/rag  compare retrieval settings
```

Every leaf takes `--format text|json` (default `text`). Text output is
for humans and not contractual; the JSON document shapes are pinned by
the eva contracts under [`contracts/sessions/`](contracts/sessions/)
and evolve additively. Full command semantics — filters, search
scopes, id addressing, exit codes — live in the
[sessions CLI spec](docs/specs/sessions-cli.md).

## Verify

```sh
make test-parity
```

Expected: `15/15 cells ok` — all 5 SDKs round-trip the 3 vendored
fixtures (`minimal`, `tool-call`, `fork`) to byte-identical canonical
JSON. The matrix is the conformance contract; a `diff` cell is a real
parity violation.

## Common issues

| Symptom | Cause | Fix |
|---------|-------|-----|
| `go: hop.top/stem@... unknown module` | Corporate proxy or stale GOPROXY. | Set `GOPROXY=https://proxy.golang.org,direct`, or pull via mirror at `github.com/hop-top/poly-stem/tree/main/go`. |
| `unknown envelope field <name>` on parse | Producer wrote a field crtx v0.1 doesn't allow. | Move the field under `metadata`, or remove it. |
| Looking for `Provider` / `Runtime` / `Store` in TS / Py / Rs / PHP | These ship Go-only at v0.1. | Use Go for the agent loop; consume envelopes from your other-language code. See [What is NOT in v0.1](#what-is-not-in-v01-polyglot). |
| `xrr: cassette miss for http-XXXXXXXX` on sample run | Request body changed since cassette was recorded. | Rerun with `XRR_MODE=record` + the relevant API key. See [`docs/record-replay.md`](docs/record-replay.md). |
| `make test-parity` shows a `diff` cell | One SDK's canonical JSON diverges. | Inspect the unified diff the runner prints; fix the non-conformant SDK, don't change canonicalization. |

For per-symptom playbooks across SDKs, see [`docs/troubleshooting.md`](docs/troubleshooting.md).

## Triad context

stem is one of three sibling tools that speak the same on-the-wire
envelope:

| Tool | Role |
|------|------|
| `stem` | Runtime — produces sessions (agent loop, tool dispatch, fork) |
| [`vein`](https://github.com/hop-top/vein) _(planned)_ | Indexer — carries sessions between tools |
| [`nerv`](https://github.com/hop-top/nerv) _(planned)_ | Router — signal routing across agents |

The shared contract is the language-agnostic
[`crtx`](https://github.com/hop-top/spec-crtx) envelope spec. crtx owns
the shape of sessions, turns, and content parts; stem, vein, and
nerv all consume it as equals. Any envelope change lands in crtx
first.

## Sample apps + cassettes

Each polyglot SDK ships two end-to-end sample apps under
`<lang>/examples/{current-time,web-search}/` (Python uses
`current_time` / `web_search` to satisfy import-path conventions).
Each sample wires an envelope by hand through one tool-use round-trip
against a real LLM provider, with the HTTP transport wrapped by
[xrr](https://github.com/hop-top/xrr) for record / replay.

Cassettes live **per-language, per-sample** at
`<lang>/examples/<sample>/cassettes/`. They are not shared across
SDKs: xrr fingerprints requests by body bytes, and each SDK's JSON
serialization differs in formatting (key order, whitespace, escape
choices). A Go cassette can't replay a TS request and vice versa.

v0.1 cassette state:

| SDK | current-time | web-search |
|-----|--------------|------------|
| Go | recorded — replays without keys | recorded — replays without keys |
| Rust | recorded — replays without keys | recorded — replays without keys |
| TS | recording is post-v0.1 | recording is post-v0.1 |
| Python | recording is post-v0.1 | recording is post-v0.1 |
| PHP | recording is post-v0.1 | recording is post-v0.1 |

Each sample documents its recording path under a `TODO(post-cassettes)`
note.

## Parity

`make test-parity` proves all five SDKs round-trip three vendored
crtx v0.1 fixtures (`minimal`, `tool-call`, `fork`) to byte-identical
canonical JSON after `jq -cS` normalization. 15 cells in the matrix,
all green at v0.1. Details and the runner live under
[`tools/parity/`](tools/parity/).

This is a **wire-format conformance** harness only. It does not
prove behavioral equivalence (none expected — only Go has runtime),
HTTP cassette parity (the per-language-cassettes constraint is a
known mismatch), or validate / error-message parity.

## Repo layout

| Path | What |
|------|------|
| [`go/`](go/) | Reference runtime (Tier A + Tier B) |
| [`ts/`](ts/) | TypeScript envelope SDK (Tier A) |
| [`py/`](py/) | Python envelope SDK (Tier A) |
| [`rs/`](rs/) | Rust envelope SDK (Tier A) |
| [`php/`](php/) | PHP envelope SDK (Tier A) |
| [`spec/`](spec/) | Pointer to upstream [`crtx`](https://github.com/hop-top/spec-crtx) spec |
| [`contracts/`](contracts/) | Eva contracts pinning the sessions CLI JSON output shapes |
| [`e2e/stories/`](e2e/stories/) | Kit stories — plain-English intents behind the CLI surface |
| [`tools/parity/`](tools/parity/) | Cross-SDK envelope-conformance harness |
| [`docs/`](docs/) | Product + engineering docs |

## License

MIT — see [`LICENSE`](LICENSE).
