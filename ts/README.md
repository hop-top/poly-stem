# stem (ts)

TypeScript SDK for `stem`, the polyglot AI agent runtime envelope format.

> This package is a read-only language mirror of the canonical
> [`hop-top/poly-stem`](https://github.com/hop-top/poly-stem) repo.
> Issues and PRs belong in poly-stem.

This v0.x SDK ships **Tier A** scope only: types, parse, serialize, and
validate for the [crtx v0.1 envelope](https://spec.hop.top/crtx/v0.1/envelope.md).
There is no provider abstraction, no tool registry, no session store,
and no runtime supervisor — Tier B will add those once the wire format
has settled across all five polyglot SDKs.

## Use this when

- You are recording an AI session in TypeScript and want it readable
  by other crtx-aware tools.
- You are converting a CLI's JSONL transcript into crtx for downstream
  indexing.
- You are inspecting or rewriting envelope files outside Go without
  hand-rolling the schema.
- You need strict decode (rejects unknown fields) and structured
  validation errors.

If you need an agent loop, tool dispatch, or session persistence, use
the Go SDK — those are Go-only at v0.1.

## Before you begin

- Node `>=22.0.0`. Zero runtime dependencies.
- pnpm / npm / yarn for install.
- TypeScript optional — types ship with the package.

## Outcome

After installing, you can parse a crtx v0.1 envelope from JSON, build
one from scratch with typed constructors, and validate either against
the same structural rules the Go reference enforces.

## Install

```bash
pnpm add @hop-top/stem
# or: npm install @hop-top/stem
# or: yarn add @hop-top/stem
```

Node `>=22.0.0`. Zero runtime dependencies.

## Quickstart

Build an envelope by hand:

```ts
import {
  CrtxVersion,
  SourceKind,
  Version,
  serializeEnvelope,
  validate,
} from "@hop-top/stem";
import type { Envelope } from "@hop-top/stem";

const env: Envelope = {
  crtx_version: CrtxVersion,
  id: "01JCRTX0DEMO",
  created_at: "2026-05-28T14:00:00Z",
  updated_at: "2026-05-28T14:00:01Z",
  source: { kind: SourceKind, version: Version },
  turns: [
    {
      id: "t-0",
      role: "user",
      created_at: "2026-05-28T14:00:00Z",
      content: [{ type: "text", text: "Hello." }],
    },
    {
      id: "t-1",
      role: "assistant",
      created_at: "2026-05-28T14:00:01Z",
      content: [{ type: "text", text: "Hi! How can I help?" }],
    },
  ],
};

const errs = validate(env);
if (errs.length > 0) {
  throw new Error(`invalid: ${errs.map((e) => e.message).join("; ")}`);
}

const jsonl = serializeEnvelope(env); // one envelope per JSONL line
```

Parse strictly from disk or the network:

```ts
import { parseEnvelope, EnvelopeParseError } from "@hop-top/stem";

try {
  const env = parseEnvelope(rawBytes); // string | Uint8Array
} catch (err) {
  if (err instanceof EnvelopeParseError) {
    // err.cause is the EnvelopeError[] from strict-decode + validate
  }
  throw err;
}
```

Use type guards to narrow content parts:

```ts
import { isText, isToolCall, isToolResult } from "@hop-top/stem";

for (const turn of env.turns) {
  for (const part of turn.content) {
    if (isText(part)) console.log(part.text);
    else if (isToolCall(part)) console.log("call", part.name, part.input);
    else if (isToolResult(part)) console.log("result", part.call_id, part.output);
  }
}
```

## Public API surface

- Types: `Envelope`, `Turn`, `ContentPart`, `Role`, `Source`,
  `TextPart`, `ToolCallPart`, `ToolResultPart`, `ImagePart`,
  `ThinkingPart`, `ExtensionPart`, `EnvelopeError`.
- Constants: `CrtxVersion`, `Version`, `SourceKind`, `PartType`.
- Parse / serialize: `parseEnvelope`, `serializeEnvelope`.
- Validate: `validate`, `validateBytes`.
- Type guards: `isText`, `isToolCall`, `isToolResult`, `isImage`,
  `isThinking`, `isExtension`, `isKnownPartType`,
  `isValidExtensionType`.

## Examples

The repo ships two sample apps that wire an envelope through one
Anthropic Messages tool-use round-trip, replaying recorded cassettes
so they run with no API key and no network:

- [`examples/current-time`](./examples/current-time)
- [`examples/web-search`](./examples/web-search)

See the polyglot overview in
[`../README.md`](https://github.com/hop-top/poly-stem/blob/main/README.md).

## Verify

```sh
cd ts && pnpm install && pnpm test
```

Expected: all tests pass. To prove this SDK still matches the wire
spec, run the cross-SDK harness from the repo root:

```sh
make test-parity-ts
```

Expected: `ts: 3/3 ok` against the `minimal`, `tool-call`, and `fork`
fixtures.

## Common issues

| Symptom | Cause | Fix |
|---------|-------|-----|
| `unknown envelope field <name>` | Producer wrote a field crtx v0.1 doesn't allow. | Move the field under `metadata`, or remove it. |
| `EnvelopeParseError` thrown from `parseEnvelope` | Strict decode rejects unknown fields. | Inspect `err.cause` (an `EnvelopeError[]`) for the offending path. |
| `unknown ContentPart type "my-thing"` | Custom content part missing the `x-` prefix. | Rename `type` to `x-<reverse-dns>` so it round-trips as an extension. |
| Looking for `Provider`, `Runtime`, or `Store` | Tier B (runtime) is Go-only at v0.1. | Run `stem-go` as a sidecar and consume the envelopes from TS. |
| Tests pass but `make test-parity` shows `diff` for `ts` | Canonical JSON diverged from the reference SDK. | Don't mutate canonicalization — fix the serializer. |

For cross-SDK issues (parity diffs, envelope parse errors, xrr
cassette pitfalls), see [`../docs/troubleshooting.md`](../docs/troubleshooting.md).

## Next steps

- Side-by-side polyglot examples: [`../docs/quickstart-polyglot.md`](../docs/quickstart-polyglot.md).
- Record / replay LLM round-trips: [`../docs/record-replay.md`](../docs/record-replay.md).
- crtx v0.1 wire spec: [envelope.md](https://github.com/hop-top/spec-crtx/blob/main/specs/v0.1/envelope.md).

## License

MIT. See the
[`hop-top/poly-stem` LICENSE](https://github.com/hop-top/poly-stem/blob/main/LICENSE).
