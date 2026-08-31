# web_search (py)

Stem agent loop with a real upstream tool: the model decides to call
`web_search`, the example POSTs to the Tavily search API, and the
results flow back to the model for a final answer. Mirrors
`../../../go/examples/web-search/` wire-for-wire.

## What it demonstrates

- Same envelope-wiring shape as `../current_time/` — see that example
  for the step-by-step explanation.
- Two upstream services in one envelope: Anthropic (assistant) +
  Tavily (tool execution).
- Both HTTP calls share one xrr session, so both calls record into the
  same cassette directory. Each cassette is keyed by a hash of
  method + URL + body, so request fingerprints never collide across
  services.
- The Tavily request body always carries a stable placeholder for
  `api_key` (`"replay-mode-no-key-required"`) so the shipped cassettes
  replay deterministically regardless of who recorded them. Real keys
  never enter the request body or the cassette.

## Run it

Default mode is replay. Cassettes ship under
`./cassettes/`; no API keys required:

```
uv sync
uv run python main.py
```

Expected output:

```
wrote .../session.jsonl (4 turns)
envelope round-trip + validate: ok
```

`session.jsonl` is gitignored — runtime output, not source.

## Re-record cassettes

Recording requires both keys:

```
XRR_MODE=record ANTHROPIC_API_KEY=sk-ant-... TAVILY_API_KEY=tvly-... \
    uv run python main.py
```

(The Tavily key is currently NOT injected into the request body — the
placeholder discipline keeps the cassette stable. See the Go example
README for the rationale.)

## Verify

- Process exits 0.
- `./session.jsonl` exists and is valid JSON.
- The printed line ends in `validate: ok`.

## Common issues

| Symptom | Cause | Fix |
|---------|-------|-----|
| `xrr: cassette miss` for Tavily / Anthropic | Request body changed since recording. | Rerun with `XRR_MODE=record` + both keys exported. |
| Py-side cassettes not yet recorded | Py recording is post-v0.1. | Wait for the recording pass, or record locally. |

## Runtime layer note

Like `../current_time/`, this example deliberately avoids any runtime
sugar so polyglot SDKs in Rs / PHP can mirror this exact wire-level
flow without language-specific helpers.
