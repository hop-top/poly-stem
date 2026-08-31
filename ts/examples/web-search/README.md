# web-search

Second stem (TS) agent example. Same envelope-wiring shape as
[`../current-time`](../current-time), but the tool is `web_search`,
backed by the Tavily search API. One stem `Envelope` captures the
full transcript; two upstream services (Anthropic + Tavily) are
recorded into the same xrr cassette directory because each HTTP call
has a distinct method+URL+body fingerprint.

## What it demonstrates

- Two upstream services through a single xrr `FileSession`.
- The same TS SDK shape (`parseEnvelope`, `serializeEnvelope`,
  `validate`) handling a multi-tool transcript.
- A real-world POST body (Tavily JSON) round-tripping through the
  recorded fingerprint without any custom adapter wiring.

## Install

```bash
pnpm install
```

(`@hop-top/stem` is consumed via a local `link:` to the sibling
package so changes flow through without a publish step.)

## Run it (replay — default)

No API keys required. Cassettes live at
`./cassettes/`:

```bash
pnpm start
```

You should see:

```
wrote .../session.jsonl (4 turns)
envelope round-trip + validate: ok
```

`session.jsonl` is gitignored.

## Re-record cassettes

Both keys required:

```bash
XRR_MODE=record \
  ANTHROPIC_API_KEY=sk-ant-... \
  TAVILY_API_KEY=tvly-... \
  pnpm start
```

Cassettes will be (re-)written under
`./cassettes/`. xrr only persists the
request method, URL, body, and the response status / headers / body —
no auth headers or other secret material is captured.

## Verify

- Process exits 0.
- `./session.jsonl` exists and is valid JSON.
- The printed line ends in `validate: ok`.

## Common issues

| Symptom | Cause | Fix |
|---------|-------|-----|
| `xrr: cassette miss` for Tavily / Anthropic | Request body changed (query, model, tool schema). | Rerun with `XRR_MODE=record` + both keys exported. |
| TS-side cassettes not yet recorded | TS recording is post-v0.1. | Wait for the TS recording pass, or record locally with both keys. |
