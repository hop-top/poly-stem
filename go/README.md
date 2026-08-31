# stem (go)

Go SDK for `stem`, the polyglot AI agent runtime — session lifecycle,
turn execution, tool dispatch, multi-session supervision.

Envelopes produced and consumed by this SDK follow the
language-agnostic [`crtx` v0.1 spec](https://github.com/hop-top/spec-crtx).

See [`../README.md`](../README.md) for the polyglot overview and triad
positioning (`vein` / `nerv` / `stem`).

> This repository is a read-only language mirror. Open issues and pull
> requests in [`hop-top/poly-stem`](https://github.com/hop-top/poly-stem).

## Use this when

- You are building a Go agent and want Provider-abstracted LLM, a tool
  registry, persistent stores (memory / JSONL / SQLite), and a
  supervisor for child sessions.
- You need fork / dispatch semantics for what-if branches and
  sub-agent nests.
- You want lifecycle events on a bus (`crtx.turn.*`,
  `crtx.session.envelope.*`) via a configurable Publisher.

## Before you begin

- Go 1.26.1 or newer.
- For SQLite store: CGO not required — uses `modernc.org/sqlite`.
- For the runtime loop tutorial: no LLM key needed (mock Provider).
- For the sample apps: no key needed in replay mode (default).

## Outcome

After installing, you can wire a `Session`, register tools on a
`Runtime`, drive turns through a `Provider`, and persist the
resulting envelope through a `Store` — all conforming to the crtx
v0.1 wire format.

## Install

```sh
go get hop.top/stem
go get hop.top/stem/sqlite   # optional SQLite store
```

## Examples

Two end-to-end sample apps live under [`examples/`](./examples). Both
construct stem envelopes manually (no runtime layer) and exercise one
tool-use round-trip against the Anthropic Messages API, with HTTP
transports wrapped by [xrr](https://github.com/hop-top/xrr) so they
replay from vendored cassettes without API keys:

| Example | Tool | Upstreams |
|---------|------|-----------|
| [`examples/current-time`](./examples/current-time) | `current_time` → ISO-8601 timestamp | Anthropic |
| [`examples/web-search`](./examples/web-search) | `web_search` → Tavily search results | Anthropic + Tavily |

```sh
cd go/examples/current-time && go run .   # default: replay, no keys
```

Cassettes live next to each sample at `examples/<sample>/cassettes/`.
Each polyglot SDK records its own per-language cassettes — xrr fingerprints
HTTP requests by body bytes, so SDK serialization differences across
languages defeat shared cassettes.

## Quick start

```go
import (
    "context"
    "fmt"
    "time"

    "hop.top/stem"
)

ctx := context.Background()
store := stem.NewMemoryStore()

s := stem.NewSession("chat-001", stem.WithStore(store))
if err := store.Create(ctx, stem.SessionMeta{ID: s.ID, Source: s.Source}); err != nil {
    return fmt.Errorf("create session: %w", err)
}

if err := store.AppendTurn(ctx, s.ID, stem.Turn{
    ID:        "t-0",
    Role:      stem.RoleUser,
    CreatedAt: time.Now().UTC(),
    Content:   []stem.ContentPart{stem.TextPart("Hello")},
}); err != nil {
    return fmt.Errorf("append turn: %w", err)
}
```

## Envelope shape

A `stem.Session` IS a crtx Envelope on the wire. JSON output is
schema-conformant:

```json
{
  "crtx_version": "0.1",
  "id": "chat-001",
  "created_at": "...",
  "updated_at": "...",
  "source": {"kind": "stem", "version": "0.1.0"},
  "turns": [
    {
      "id": "t-0",
      "role": "user",
      "created_at": "...",
      "content": [{"type": "text", "text": "Hello"}]
    }
  ]
}
```

## Multi-agent

Coordinate multiple sessions: forks for what-if branches, dispatch
nests for parallel sub-agents, a router for inter-session messaging,
and a supervisor for lifecycle observation.

### Fork — speculative branch

`Session.Fork(id)` returns a child envelope that inherits the parent
conceptually up to `fork_point = len(parent.Turns)`. The child starts
empty; consumers reconstruct by concatenating `parent.Turns[:ForkPoint]`
with `child.Turns`.

```go
child := parent.Fork("what-if-1")
// child.ParentID == parent.ID; *child.ForkPoint == len(parent.Turns)
```

### Dispatch — sub-agent nest

`Session.Dispatch(ctx, id, callID, opts...)` creates a child envelope
spawned by an open `tool_call` in the parent. The child does NOT inherit
the parent's transcript — the curator pulls in context via the first
turn or `injected_turns`. `callID` MUST refer to an open `tool_call` in
the parent or `Dispatch` panics.

```go
child := parent.Dispatch(ctx, "child-1", "call-search-1")
// run the sub-agent loop on child here…
_ = parent.ReturnDispatch(ctx, child, "call-search-1", result, false)
```

`ReturnDispatch` appends a `tool_result` to the parent closing the open
call (with `child_envelope_id` set), and publishes
`crtx.session.envelope.returned`. Returns `ErrNotDispatchChild` /
`ErrCallNotOpen` on misuse.

### Router — inter-session messaging

`NewDirectRouter()` plus `Session.SendTo(ctx, targetID, content)`
deliver a `RoleUser` turn to another registered session, tagged with
`Turn.Metadata[MetaKeyFromSession] = sender.ID`.

### Supervisor — lifecycle observation

`NewSupervisor(pub)` tracks watched children via `Watch` / `Done` /
`Cancel` / `WaitAll` and fires `x.io.jadb.stem.child.done` when each
completes. Useful when the runtime wants to await all children before
persisting a parent close.

See [envelope.md §7](https://github.com/hop-top/spec-crtx/blob/main/specs/v0.1/envelope.md#7-child-envelopes)
for wire-format invariants and
[events.md §3](https://github.com/hop-top/spec-crtx/blob/main/specs/v0.1/events.md#3-canonical-topics--v01)
for topic payloads.

## Packages

| Path | Purpose |
|------|---------|
| `hop.top/stem` | Core types (Envelope/Turn/ContentPart), runtime, in-memory and JSONL stores, multi-agent supervisor. |
| `hop.top/stem/sqlite` | SQLite-backed Store via `modernc.org/sqlite`. |
| `hop.top/stem/kitadapter` | Documentation-only mirror of the kit `llm.Client` ↔ `stem.Provider` mapping. The real adapter lives in `hop.top/kit` (which depends on stem). |

## Validation

`stem.Validate(env *Session) error` runs the structural checks from
the crtx v0.1 spec (required fields, role enum, ContentPart
discriminator, tool_result → tool_call linkage). For full
schema conformance use any external JSON Schema validator against
[`envelope.schema.json`](https://spec.hop.top/crtx/v0.1/envelope.schema.json).

## Verify

```sh
cd go && go test ./...
```

Expected: all packages pass. To prove the SDK still matches the wire
spec, run the cross-SDK harness from the repo root:

```sh
make test-parity-go
```

Expected: `go: 3/3 ok` against the `minimal`, `tool-call`, and `fork`
fixtures.

## Common issues

| Symptom | Cause | Fix |
|---------|-------|-----|
| `stem: no provider configured` | Forgot `RuntimeWithProvider` on `NewRuntime`. | Pass `stem.RuntimeWithProvider(provider)`. |
| `stem: max tool depth exceeded` | Provider keeps emitting `tool_call` parts. | Raise the cap with `stem.RuntimeWithMaxToolRounds(n)`, or fix the Provider. |
| `stem: invalid envelope: tool_result: call_id "..." has no preceding tool_call` | Tool result's `call_id` doesn't match a prior `tool_call`. | Reuse the assistant's `call_id` verbatim when constructing the tool turn. |
| `go: hop.top/stem unknown module` | Corporate GOPROXY blocks `hop.top/*`. | Set `GOPROXY=https://proxy.golang.org,direct` or pull via `github.com/hop-top/poly-stem/tree/main/go`. |
| `kitadapter` won't compile against `hop.top/kit` | Adapter is a documentation-only mirror. | Use the real adapter in `hop.top/kit`; `kitadapter` here exists only to show the surface. |

For cross-SDK issues (parity diffs, envelope parse errors, xrr
cassette pitfalls), see [`../docs/troubleshooting.md`](../docs/troubleshooting.md).

## Status

v0.1.0. The on-disk JSONL format is **one crtx Envelope per line**;
the file holds exactly one envelope and `AppendTurn` rewrites it.

## Next steps

- Full tutorial with mock Provider: [`../docs/quickstart-go.md`](../docs/quickstart-go.md).
- Record and replay LLM round-trips deterministically: [`../docs/record-replay.md`](../docs/record-replay.md).
- End-to-end Anthropic sample with cassettes: [`examples/current-time`](./examples/current-time).
- crtx v0.1 wire spec: [envelope.md](https://github.com/hop-top/spec-crtx/blob/main/specs/v0.1/envelope.md).

## License

MIT — see [`../LICENSE`](../LICENSE).
