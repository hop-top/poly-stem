# Record and replay LLM sessions for tests

Wrap the LLM client's HTTP transport with [xrr](https://github.com/hop-top/xrr)
once, record the round-trip, and replay it forever in CI without
network access or an API key. Result: deterministic agent tests
that don't burn provider credits.

## When to use this

- Your test suite hits a real LLM and you want to remove the network
  hop without rewriting the test.
- Your CI shouldn't depend on an API key being available.
- You want to reproduce a specific model response (a hallucination,
  a tool-call sequence, a refusal) and assert on it across runs.
- You are developing offline and need the agent loop to keep working.

## Before you begin

You need:

- [xrr](https://github.com/hop-top/xrr) — the cross-runtime recorder.
  The Go SDK and Rust SDK in `poly-stem` already depend on it
  (`hop.top/xrr v0.1.0-alpha.4` and the corresponding Rust crate).
- An LLM client SDK in your language (Anthropic, OpenAI, etc.).
- The stem SDK in your language for envelope I/O. Not strictly
  required for record/replay; useful when the test asserts on the
  resulting envelope.
- For first-time recording: a real API key for your LLM provider.

## Outcome

After this guide you will have an agent test that runs offline,
exits 0 in well under a second, and asserts on a stem envelope
constructed from a recorded provider response.

## Quick path

1. Add an `xrr` HTTP wrapper to your LLM client.
2. Run once with `XRR_MODE=record` and a real API key.
3. Commit the generated cassette directory.
4. Run forever afterwards with no environment variables and no key.

## Steps

### 1. Wrap the LLM client's HTTP transport

The pattern is identical across LLM SDKs: build an HTTP client whose
`Do` method routes through `xrr.Session.Record`, then pass it as
`option.WithHTTPClient` (or your SDK's equivalent).

The Go reference is at
[`go/examples/current-time/xrrhttp.go`](../go/examples/current-time/xrrhttp.go).
The relevant shape:

```go
session, _ := xrr.NewSession(
    xrr.ModeReplay, // default; record / passthrough also valid
    xrr.NewFileCassette("./cassettes"),
)

httpClient := newXRRClient(session, nil)
client := anthropic.NewClient(
    option.WithAPIKey(apiKey),
    option.WithHTTPClient(httpClient),
)
```

`session.Record(ctx, adapter, req, do)` runs `do()` once in record
mode and writes the response to a cassette file. In replay mode it
short-circuits `do()` entirely and returns the saved response.

### 2. Record cassettes once

Set `XRR_MODE=record` and supply the real API key:

```sh
cd go/examples/current-time
XRR_MODE=record ANTHROPIC_API_KEY=sk-ant-... go run .
```

Expected output:

```text
wrote ./session.jsonl (4 turns)
envelope round-trip + validate: ok
```

After this completes, `./cassettes/` holds a pair of YAML files per
HTTP call:

```text
cassettes/
  http-5017acb7.req.yaml
  http-5017acb7.resp.yaml
  http-d5e33672.req.yaml
  http-d5e33672.resp.yaml
```

The 8-char suffix is the request fingerprint (method + path + first
8 hex chars of the body SHA-256). Commit the directory.

### 3. Replay everywhere afterwards

Default mode is replay. No environment variables, no key, no network:

```sh
go run .
```

Expected output: identical to step 2. The recorded responses serve
without ever opening a socket.

## Verify

- Process exits 0 with no `ANTHROPIC_API_KEY` set.
- `./session.jsonl` is written and decodes through `stem.Validate`
  (or the polyglot equivalent) without errors.
- Network monitoring tools (`tcpdump`, `lsof -i`) show zero
  connections during replay.

## Common issues

| Symptom | Cause | Fix |
|---------|-------|-----|
| `xrr: cassette miss for http-XXXXXXXX` | Request body changed since recording (model name, max_tokens, prompt, any client-side default). | Rerun with `XRR_MODE=record`. The fingerprint hashes the full request body — even reordered JSON fields produce a different hash. |
| `xrr: cassette dir does not exist` | Wrapper points at a directory that's never been recorded into. | `mkdir -p ./cassettes` before instantiating the `FileCassette`, or commit the existing directory from another machine. |
| Recorded cassette contains real API key | An `Authorization` or `X-Api-Key` header leaked into the request capture. | xrr's HTTP adapter records the **response** headers, not the request headers. If your wrapper logs request headers itself, scrub them before writing. Audit captured files before committing. |
| Replay works in Go but not in TS | Cassettes are per-language. The Anthropic Go SDK and Anthropic TS SDK serialize the request body differently — different bytes, different SHA-256, different fingerprint. | Record the cassette in the language that will replay it. Do not share cassette directories across SDKs. |
| `xrr: unknown XRR_MODE "..."` | Environment variable contains a typo. | Use one of `record`, `replay`, `passthrough`, or unset (defaults to replay). |

## How it works

xrr intercepts at a wrapper seam inside the process that makes the
call. For HTTP, the seam is an `http.Client.Do` shim that:

1. Reads `req.Body` and computes a fingerprint:
   `method + path+query + sha256(body)[:8]` for the HTTP adapter
   (per the xrr cassette format spec).
2. In **record** mode, calls the wrapped transport, captures the
   response (status + headers + body), and writes a YAML cassette
   pair `http-<fingerprint>.req.yaml` / `http-<fingerprint>.resp.yaml`.
3. In **replay** mode, looks up the cassette by fingerprint and
   returns the saved response without calling the transport.
4. In **passthrough** mode, calls the transport and leaves the
   cassette directory untouched.

Cassettes are YAML so they diff cleanly in code review. The format
is language-agnostic — any xrr port can read a cassette another port
wrote, **as long as the request fingerprints match**.

## Per-language fingerprint pitfall

Different LLM SDKs in different languages produce different request
bodies for the same logical call. The Anthropic Go SDK and the
Anthropic TS SDK both POST to `/v1/messages`, but they emit JSON
with different key ordering, different whitespace, different number
formatting, different optional-field defaults. SHA-256 of those
bodies is different. The fingerprints don't match. Cross-language
cassette reuse is therefore infeasible at v0.1 — each SDK records
and replays its own cassettes.

This is a property of the LLM clients, not a limitation in xrr.
Treat cassette directories as private to the SDK that produced
them.

## Where cassettes live

Per-sample, per-language:

```text
go/examples/current-time/cassettes/      # recorded, replay-ready
go/examples/web-search/cassettes/        # recorded, replay-ready
rs/examples/current-time/cassettes/      # recorded, replay-ready
rs/examples/web-search/cassettes/        # recorded, replay-ready
ts/examples/current-time/cassettes/      # dir exists; recording is post-v0.1
ts/examples/web-search/cassettes/        # dir exists; recording is post-v0.1
py/examples/current_time/cassettes/      # dir exists; recording is post-v0.1
py/examples/web_search/cassettes/        # dir exists; recording is post-v0.1
php/examples/current-time/cassettes/     # dir exists; recording is post-v0.1
php/examples/web-search/cassettes/       # dir exists; recording is post-v0.1
```

Each sample's `README.md` documents its recording path (look for the
`TODO(post-cassettes)` note in the TS / Py / PHP cases).

## Next steps

- Browse the xrr cassette spec, modes, and adapter list:
  [xrr README](https://github.com/hop-top/xrr).
- See the full Go agent example with cassettes already recorded:
  [`go/examples/current-time`](../go/examples/current-time/README.md).
- Use the polyglot SDKs to inspect or assert on the resulting
  envelopes from your test code:
  [quickstart-polyglot](./quickstart-polyglot.md).
- Drive a stem agent from scratch in Go:
  [quickstart-go](./quickstart-go.md).
