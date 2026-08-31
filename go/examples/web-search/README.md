# web-search

Stem agent loop with a real upstream tool: the model decides to call
`web_search`, the example POSTs to the Tavily search API, and the
results flow back to the model for a final answer.

## What it demonstrates

- Same envelope-wiring shape as `../current-time/` — see that example
  for the step-by-step explanation.
- Two upstream services in one envelope: Anthropic (assistant) +
  Tavily (tool execution).
- Both HTTP clients share one xrr session, so both calls record into
  the same cassette directory. Each cassette is keyed by a hash of
  method + URL + body, so request fingerprints never collide across
  services.
- The Tavily request body always carries a stable placeholder for
  `api_key` (`"replay-mode-no-key-required"`) so the shipped
  cassettes replay deterministically regardless of who recorded them.
  Real keys never enter the request body or the cassette.

## Run it

Default mode is replay. Cassettes ship under
`./cassettes/`; no API keys required:

```
go run .
```

Expected output:

```
wrote ./session.jsonl (4 turns)
envelope round-trip + validate: ok
```

`session.jsonl` is gitignored — runtime output, not source.

## Re-record cassettes

Recording requires both keys. The Tavily key is sent over the wire
during recording, but the cassette ships with the placeholder string
because the example always serializes
`"api_key": "replay-mode-no-key-required"` into the body it hands to
the SDK (real auth goes through a different path in a future revision;
for now the example is "record once with the placeholder key against
a real Tavily endpoint and copy the response into the cassette" or
swap in your own placeholder discipline).

```
XRR_MODE=record ANTHROPIC_API_KEY=sk-ant-... TAVILY_API_KEY=tvly-... go run .
```

## Verify

- Process exits 0.
- `./session.jsonl` exists and is valid JSON.
- The printed line ends in `validate: ok`.

## Common issues

| Symptom | Cause | Fix |
|---------|-------|-----|
| `xrr: cassette miss` for the Tavily fingerprint | Tavily request body changed (query, search depth, top-k). | Rerun with `XRR_MODE=record` + both keys. |
| `xrr: cassette miss` for the Anthropic fingerprint | Anthropic request body changed (model, system, tool schema). | Rerun with `XRR_MODE=record` + `ANTHROPIC_API_KEY`. |

## Runtime layer note

Like `../current-time/`, this example deliberately avoids stem's
runtime layer (`stem.NewSession(..., WithProvider(...), WithStore(...),
WithToolRegistry(...))` and `runtime.go`) so the same wire-level flow
ports cleanly to TS / Py / Rs / PHP. The runtime layer automates this
loop in production Go code.
