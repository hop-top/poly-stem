# current-time (PHP)

Smallest viable stem PHP agent loop. Mirrors
[`go/examples/current-time`](../../../go/examples/current-time/) — same
9-step flow, same envelope shape, OpenAI Chat Completions in place of
the Anthropic Messages API.

## What it shows

- Construct an `Envelope::fresh($id)`.
- Append user / assistant / tool `Turn`s with typed `ContentPart`s.
- Project an OpenAI tool_call into a `ToolCallPart`.
- Execute a local handler, feed the result back to the model.
- Serialize → reload → `validate` the round-trip.

The OpenAI HTTP transport is wrapped with [xrr] via a small PSR-18
client, so the same code records cassettes once and replays them
deterministically forever after.

## Run it

```bash
composer install
php index.php
```

Default mode is **replay**; no API key required if a cassette is
present at `./cassettes/`.

> NOTE: at the time of writing the shared cassette directory is being
> populated by a parallel Go-SDK agent against Anthropic. The OpenAI
> request fingerprint is distinct (different URL + body shape), so a
> separate cassette set is required to make this PHP example replay
> end-to-end. Pull-side verification is tracked under
> `TODO(post-cassettes)` in `index.php`.

## Re-record cassettes

```bash
XRR_MODE=record OPENAI_API_KEY=sk-... php index.php
```

xrr persists method, URL, body, status, headers, and response body —
no `Authorization` or other secret header material.

## Verify

- Process exits 0.
- `./session.jsonl` exists and is valid JSON.
- The printed line ends in `validate: ok` once the OpenAI cassette
  set lands (see note above).

## Common issues

| Symptom | Cause | Fix |
|---------|-------|-----|
| `xrr: cassette miss` | OpenAI request body changed, or no OpenAI cassette recorded yet. | Run with `XRR_MODE=record` + `OPENAI_API_KEY`. |
| PHP-side cassettes not yet recorded | Tracked under `TODO(post-cassettes)` in `index.php`. | Record locally with a real OpenAI key. |

[xrr]: https://github.com/hop-top/poly-xrr
