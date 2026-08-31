# Build your first stem agent in Go

Wire a `stem.Session`, register one tool, drive a Provider through
the runtime loop, and print the final envelope as JSON. No LLM
account, no network call, no API key.

## When to use this

- You are starting a new Go agent and want a runnable scaffold.
- You want to see how `Session`, `Runtime`, `Provider`, and
  `ToolRegistry` fit together without reading the test suite.
- You need a self-contained example to copy into a fresh module.

## Before you begin

You need:

- Go 1.26.1 or newer.
- An empty directory and a network connection for the initial
  `go get`.
- No Anthropic key, no OpenAI key — the tutorial uses a mock
  Provider.

## Outcome

After this guide you will have a single-file program that prints a
multi-turn crtx v0.1 envelope to stdout — user prompt, assistant
tool call, tool result, final assistant text — and exits 0.

## Quick path

```sh
mkdir stem-quickstart && cd stem-quickstart
go mod init example.com/stem-quickstart
go get hop.top/stem
# paste the Steps below into main.go, then:
go run .
```

Expected last line: `envelope: { ... "turns": [ ... 4 entries ... ] }`.

## Steps

### 1. Create the module and pull the SDK

```sh
mkdir stem-quickstart && cd stem-quickstart
go mod init example.com/stem-quickstart
go get hop.top/stem
```

### 2. Write `main.go`

The program below uses three real stem symbols:

- `stem.NewSession` — constructs a root crtx envelope with a default
  `Source` of `{kind: "stem", version: "0.1.0"}`.
- `stem.NewRuntime` with `RuntimeWithProvider`, `RuntimeWithStore`,
  `RuntimeWithToolRegistry` — wraps the session and drives the
  tool-call loop.
- `stem.ToolHandlerFunc` — adapts a plain function to the
  `ToolHandler` interface used by `ToolRegistry.Register`.

The mock `Provider` returns canned `Turn` values: the first call
issues a `tool_call` for `current_time`; the second returns a
text-only assistant turn that ends the loop.

```go
// main.go
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sync"
	"time"

	"hop.top/stem"
)

// cannedProvider implements stem.Provider with a fixed list of
// responses returned in order from Complete().
type cannedProvider struct {
	mu    sync.Mutex
	turns []stem.Turn
	idx   int
}

func (p *cannedProvider) Complete(_ context.Context, _ []stem.Turn) (stem.Turn, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.idx >= len(p.turns) {
		return stem.Turn{
			Role:    stem.RoleAssistant,
			Content: []stem.ContentPart{stem.TextPart("(fallback)")},
		}, nil
	}
	t := p.turns[p.idx]
	p.idx++
	return t, nil
}

func (p *cannedProvider) Stream(_ context.Context, _ []stem.Turn) (stem.TurnStream, error) {
	return nil, fmt.Errorf("not implemented")
}

func (p *cannedProvider) CallWithTools(
	ctx context.Context, turns []stem.Turn, _ []stem.ToolDef,
) (stem.Turn, error) {
	return p.Complete(ctx, turns)
}

func main() {
	ctx := context.Background()

	// 1. Mock provider: first response asks for the tool, second answers.
	toolCall, err := stem.ToolCallPart("call-0", "current_time", map[string]any{})
	if err != nil {
		fail(err)
	}
	provider := &cannedProvider{turns: []stem.Turn{
		{Role: stem.RoleAssistant, Content: []stem.ContentPart{toolCall}},
		{Role: stem.RoleAssistant, Content: []stem.ContentPart{
			stem.TextPart("The time is now."),
		}},
	}}

	// 2. Tool registry with one handler. Handler returns a JSON-encoded
	//    timestamp; stem wraps it in a tool_result ContentPart.
	registry := stem.NewToolRegistry()
	registry.Register("current_time", stem.ToolHandlerFunc(
		func(_ context.Context, _ string, _ json.RawMessage) (json.RawMessage, error) {
			out := map[string]string{"time": time.Now().UTC().Format(time.RFC3339)}
			return json.Marshal(out)
		},
	))

	// 3. In-memory store. Runtime persists every turn through it.
	store := stem.NewMemoryStore()
	session := stem.NewSession("quickstart-1")
	if err := store.Create(ctx, stem.SessionMeta{
		ID:     session.ID,
		Source: session.Source,
	}); err != nil {
		fail(err)
	}

	// 4. Runtime wires the four pieces together.
	rt := stem.NewRuntime(session,
		stem.RuntimeWithProvider(provider),
		stem.RuntimeWithStore(store),
		stem.RuntimeWithToolRegistry(registry),
	)

	// 5. Drive one round. Send() appends the user turn, calls the
	//    provider, dispatches the tool, calls the provider again, and
	//    returns the final assistant turn.
	if _, err := rt.Send(ctx, "What time is it?"); err != nil {
		fail(err)
	}

	// 6. Print the full envelope.
	out, err := session.MarshalIndent("", "  ")
	if err != nil {
		fail(err)
	}
	fmt.Printf("envelope: %s\n", out)
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, "error:", err)
	os.Exit(1)
}
```

### 3. Run it

```sh
go run .
```

Expected output (timestamps and `created_at` will differ):

```text
envelope: {
  "crtx_version": "0.1",
  "id": "quickstart-1",
  "created_at": "2026-05-28T14:00:00Z",
  "updated_at": "2026-05-28T14:00:00Z",
  "source": {
    "kind": "stem",
    "version": "0.1.0"
  },
  "turns": [
    {"id": "u-0", "role": "user", ...},
    {"id": "a-1", "role": "assistant", "content": [{"type": "tool_call", ...}]},
    {"id": "tool-2", "role": "tool", "content": [{"type": "tool_result", ...}]},
    {"id": "a-3", "role": "assistant", "content": [{"type": "text", ...}]}
  ]
}
```

The envelope holds four turns: user prompt, assistant `tool_call`,
tool `tool_result`, and the assistant's final text answer.

## Verify

- Process exits 0.
- The printed envelope is valid JSON. Pipe it through `jq` to confirm:
  `go run . 2>&1 | sed -n 's/^envelope: //p' | jq .`
- The envelope passes `stem.Validate` — replace the print step with:
  ```go
  if err := stem.Validate(session); err != nil {
      fail(err)
  }
  ```

## Common issues

| Symptom | Cause | Fix |
|---------|-------|-----|
| `stem: no provider configured` | Forgot `RuntimeWithProvider` on `NewRuntime`. | Pass `stem.RuntimeWithProvider(provider)`. |
| `stem: max tool depth exceeded` | Mock provider keeps issuing `tool_call` parts. | Make sure the second canned turn is text-only, or raise `stem.RuntimeWithMaxToolRounds(n)`. |
| `stem: invalid envelope: turns[X]: tool_result: call_id "..." has no preceding tool_call` | `call_id` on the tool turn doesn't match the assistant's `tool_call.call_id`. | Reuse the same `"call-0"` string in both `ToolCallPart` and the tool registry response — the runtime handles this for you when the provider issues real calls. |
| `go: hop.top/stem@... unknown module` | Behind a corporate proxy. | Configure `GOPROXY` or pull via the mirror at `github.com/hop-top/poly-stem/tree/main/go`. |

## How it works

`Runtime.Send` runs a fixed loop:

1. Append the user message and persist it. A provider failure here
   never loses the user turn.
2. Call `Provider.Complete` with the current turns slice.
3. If the response contains any `tool_call` parts, append the
   assistant turn, dispatch each call through the `ToolRegistry`,
   append a tool-role turn carrying the `tool_result`, and loop.
4. If the response is tool-call-free, append it as the final
   assistant turn and return.

The loop is bounded by `RuntimeWithMaxToolRounds` (default 10). Each
appended turn flows through the configured `Store` so the on-disk
view never trails the in-memory transcript by more than one operation.
A configured `Publisher` (not used in this tutorial) emits crtx v0.1
lifecycle events: `crtx.turn.user.received`,
`crtx.turn.assistant.emitted`, `crtx.turn.tool.called`, and
`crtx.turn.tool.completed`. See
[events.md §3](https://github.com/hop-top/spec-crtx/blob/main/specs/v0.1/events.md#3-canonical-topics--v01)
for the full topic taxonomy and payload shapes.

## Options

| Option | Use when |
|--------|----------|
| `stem.NewJSONLStore(path)` | You want the session persisted to a one-envelope-per-line file. |
| `sqlite.New(...)` (`hop.top/stem/sqlite`) | You want indexed lookup across many sessions. |
| `stem.RuntimeWithPublisher(p)` | You want lifecycle events on a bus (e.g. nats, redis). |
| `stem.RuntimeWithMaxToolRounds(n)` | The agent legitimately needs more than 10 tool rounds. |
| `stem.WithSource(stem.Source{Kind: "my-cli", Version: "1.2.3"})` on `NewSession` | The producing runtime is not stem itself. |

## Next steps

- Wire a real LLM and record the round-trip:
  [record-replay](./record-replay.md).
- See an end-to-end Anthropic example with cassette replay:
  [`go/examples/current-time`](../go/examples/current-time/README.md).
- Understand where stem fits in the wider triad:
  [overview](./overview.md).
- Read or write envelopes from non-Go services:
  [quickstart-polyglot](./quickstart-polyglot.md).
