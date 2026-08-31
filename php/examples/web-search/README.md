# web-search (PHP)

Second stem PHP agent example. Mirrors
[`go/examples/web-search`](../../../go/examples/web-search/) — same
9-step flow, OpenAI Chat Completions + Tavily Search instead of
Anthropic + Tavily.

## What it shows

- One `Envelope` carries the full transcript across two upstream
  services (OpenAI + Tavily). Both HTTP calls go through the same
  xrr-wrapped PSR-18 client; each call has a distinct
  method+URL+body fingerprint, so they coexist in the same cassette
  directory.
- `ToolCallPart` carries the `query` argument; `ToolResultPart` carries
  the normalised result shape.

## Run it

```bash
composer install
php index.php
```

Default mode is **replay**. Same caveat as `current-time/`: a parallel
Go agent is recording Anthropic cassettes; an OpenAI cassette set has
to land before this PHP example replays end-to-end. The
`TODO(post-cassettes)` line in `index.php` is the verification hook.

## Re-record cassettes

```bash
XRR_MODE=record OPENAI_API_KEY=sk-... TAVILY_API_KEY=tvly-... php index.php
```

xrr persists method, URL, body, status, headers, and response body —
no `Authorization` or other secret header material.

## Verify

- Process exits 0.
- `./session.jsonl` exists and is valid JSON.
- The printed line ends in `validate: ok` once OpenAI cassettes land.

## Common issues

| Symptom | Cause | Fix |
|---------|-------|-----|
| `xrr: cassette miss` for Tavily / OpenAI | Request body changed, or no cassette recorded yet. | Run with `XRR_MODE=record` + both keys. |
| PHP-side cassettes not yet recorded | Tracked under `TODO(post-cassettes)` in `index.php`. | Record locally with both keys. |

[xrr]: https://github.com/hop-top/poly-xrr
