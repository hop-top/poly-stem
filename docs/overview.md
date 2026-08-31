# stem at a glance

stem is the AI agent runtime for the
[crtx](https://github.com/hop-top/spec-crtx) conversation envelope. Use
this page to decide whether stem belongs in your stack and which SDK
covers what.

## Outcome

After reading this page you will know: (1) whether stem fits your use
case, (2) which SDK to install (Go for runtime, TS / Py / Rs / PHP
for envelope I/O), and (3) where stem sits in the wider `crtx` /
`vein` / `nerv` triad.

## What stem is

stem packages an agent loop, tool dispatch, session persistence, and
multi-session supervision around a single wire format: the crtx v0.1
envelope. The Go SDK is the reference runtime — it drives the LLM
provider, runs the tool-call loop, writes turns to a store, and forks
or spawns child sessions. The four other SDKs (TypeScript, Python,
Rust, PHP) ship at v0.1 as the **envelope I/O layer only**: they
parse, serialize, and validate envelopes produced upstream by Go (or
by any other crtx-conformant producer).

The wire format is the contract. Anything that can read or write a
crtx envelope can participate in a stem-produced session — the
runtime layer is an implementation detail of the producing service.

## Use stem when

- You are building an agent in Go and want a Provider-abstracted LLM
  client, a tool registry, a persistent session store (memory, JSONL,
  or SQLite), and a supervisor for child sessions out of the box.
- You are recording agent sessions in TypeScript, Python, Rust, or
  PHP and need them readable by other tools without re-implementing
  the envelope shape.
- You are converting Claude Code / Codex / other CLI session logs
  into crtx so they can flow through vein (planned indexer) or be
  diffed against a stem-produced baseline.
- You want a deterministic test harness — record an LLM round-trip
  once with [xrr](https://github.com/hop-top/xrr), replay it forever.

## Do not use stem when

- You need a full agent runtime in TypeScript, Python, Rust, or PHP
  today. Only Go ships a runtime at v0.1. The other SDKs cannot drive
  a Provider, dispatch a tool, or persist a session.
- You want prompt management, retrieval pipelines, evaluation
  harnesses, or fine-tuning workflows. stem does not do any of these.
- You need cross-language behavioral parity. The wire format is
  identical across all five SDKs; runtime behavior is Go-only.
- You need a managed service. stem is a library; you run it.

## The triad: stem, vein, nerv, crtx

| Tool | Role | v0.1 status |
|------|------|-------------|
| [crtx](https://github.com/hop-top/spec-crtx) | Spec — Envelope, Turn, ContentPart, events taxonomy | Shipped |
| stem | Runtime — produces sessions (agent loop, tool dispatch, fork) | This release |
| [vein](https://github.com/hop-top/vein) | Indexer — cross-CLI session catalog | Planned |
| [nerv](https://github.com/hop-top/nerv) | Router — signal routing across agents | Planned |

crtx owns the shape of sessions, turns, and content parts. stem,
vein, and nerv each consume it as equals. Any envelope change lands
in crtx first and ripples out to consumers.

## What ships in v0.1

| SDK | Package | Install | Scope |
|-----|---------|---------|-------|
| Go | `hop.top/stem` | `go get hop.top/stem` | Runtime + tool registry + memory / JSONL / SQLite stores + multi-agent supervisor |
| TypeScript | `@hop-top/stem` | `pnpm add @hop-top/stem` | Envelope parse + serialize + validate |
| Python | `hop-top-stem` | `pip install hop-top-stem` | Envelope parse + serialize + validate |
| Rust | `hop-top-stem` | `cargo add hop-top-stem` | Envelope parse + serialize + validate |
| PHP | `hop-top/stem` | `composer require hop-top/stem` | Envelope parse + serialize + validate |

All five SDKs round-trip the same three crtx v0.1 fixtures to
byte-identical canonical JSON. `make test-parity` asserts this on
every push.

The polyglot SDKs deliberately exclude every runtime concern. None of
these exist outside Go at v0.1: Provider interface, agent loop, tool
dispatch, session stores, multi-session supervisor, fork semantics
enforcement. Polyglot runtime parity is post-v0.1; the wire format
has to settle across five SDKs before runtime contracts can follow.

## Wire format invariants

Every SDK produces JSON that satisfies the crtx v0.1 schema:

- `crtx_version` is `"0.1"`.
- Empty `turns` serialize as `[]`, never `null` or absent.
- `parent_id` and `fork_point` appear together or not at all.
- `dispatched_from` is mutually exclusive with `parent_id`/`fork_point`:
  a child envelope is either a what-if fork or a dispatch nest, never
  both. See [envelope.md §7.2](https://github.com/hop-top/spec-crtx/blob/main/specs/v0.1/envelope.md#72-dispatch-nests).
- `ContentPart` is a discriminated union on `type`; unknown variants
  must use the `x-` prefix and round-trip verbatim.
- `tool_result.call_id` must reference a preceding `tool_call.call_id`
  in the same envelope.
- Image parts carry exactly one of `data` or `url`.

The Go validator is reference; the polyglot validators mirror its
checks and return structured error lists.

## Where to go next

- Build your first agent in Go: [quickstart-go](./quickstart-go.md).
- Read or write envelopes in TS / Py / Rs / PHP:
  [quickstart-polyglot](./quickstart-polyglot.md).
- Record and replay LLM sessions for tests:
  [record-replay](./record-replay.md).
- Spec reference:
  [crtx v0.1 envelope](https://github.com/hop-top/spec-crtx/blob/main/specs/v0.1/envelope.md).
