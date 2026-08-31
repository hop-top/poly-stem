# Read and write crtx envelopes in TS, Py, Rs, PHP

Parse a crtx v0.1 envelope from JSON, build one from scratch, and
validate it — using whichever polyglot SDK matches your runtime. No
LLM provider, no runtime loop, no storage backends. Just envelope
I/O.

## When to use this

- You are recording AI sessions in a TypeScript, Python, Rust, or
  PHP service and want them readable by other crtx-aware tools.
- You are converting Claude Code, Codex, or another CLI's JSONL
  transcripts into crtx so they can flow through downstream indexers.
- You are inspecting or rewriting envelope files outside Go and want
  strict-decode + structured validation without hand-rolling the
  schema.

If your goal is to drive an LLM provider, dispatch tools, or persist
a session through a `Store`, you need the Go SDK —
see [quickstart-go](./quickstart-go.md). The polyglot SDKs do not
ship a runtime at v0.1.

## Before you begin

You need a recent runtime for your language and the matching SDK.

| SDK | Runtime | Install |
|-----|---------|---------|
| TypeScript | Node ≥ 22 | `pnpm add @hop-top/stem` |
| Python | CPython ≥ 3.12 | `pip install hop-top-stem` |
| Rust | edition 2021 | `cargo add hop-top-stem` |
| PHP | PHP ≥ 8.2 | `composer require hop-top/stem` |

## Outcome

After this guide you will be able to round-trip a 2-turn envelope
(user prompt + assistant reply) through parse → mutate → serialize
→ validate in whichever language your service speaks.

## Quick path

Every SDK exposes the same three operations:

| Operation | TS | Py | Rs | PHP |
|-----------|----|----|----|-----|
| Parse from JSON | `parseEnvelope(json)` | `parse_envelope(json)` | `parse_envelope(&json)` | `HopTop\Stem\parseEnvelope($json)` |
| Serialize to JSON | `serializeEnvelope(env)` | `serialize_envelope(env)` | `serialize_envelope(&env)` | `HopTop\Stem\serializeEnvelope($env)` |
| Validate (structured errors) | `validate(env)` | `validate(env)` | `validate(&env)` | `HopTop\Stem\validate($env)` |
| Strict-decode + validate | `validateBytes(json)` | `validate_bytes(json)` | `validate_bytes(&bytes)` | `HopTop\Stem\validateBytes($json)` |

`validate*` returns a list of structured errors; an empty list means
the envelope is valid. `parseEnvelope` / `parse_envelope` is strict
— unknown top-level or content-part fields cause a decode error.

## The 3 operations

### 1. Parse an envelope from JSON

The input is the canonical JSON shape produced by any crtx v0.1
producer. Unknown fields are rejected at parse time.

```ts
// TypeScript
import { parseEnvelope } from "@hop-top/stem";
import type { Envelope } from "@hop-top/stem";

const env: Envelope = parseEnvelope(jsonString);
console.log(env.id, env.turns.length);
```

```python
# Python
from stem import parse_envelope

env = parse_envelope(json_string)
print(env.id, len(env.turns))
```

```rust
// Rust
use hop_top_stem::{parse_envelope, Envelope};

let env: Envelope = parse_envelope(&json_string)?;
println!("{} {}", env.id, env.turns.len());
```

```php
// PHP
use function HopTop\Stem\parseEnvelope;

$env = parseEnvelope($jsonString);
echo $env->id, ' ', count($env->turns), PHP_EOL;
```

Expected behavior: a malformed envelope throws / returns an error
that names the first offending field, e.g.
`unknown envelope field foo` or
`turns[0].content[1]: tool_call missing non-empty call_id`.

### 2. Build an envelope from scratch and serialize

Each SDK ships typed constructors for the five known ContentPart
variants. Extension parts (`x-…`) round-trip verbatim.

```ts
// TypeScript
import {
  CrtxVersion,
  serializeEnvelope,
  SourceKind,
  Version,
} from "@hop-top/stem";
import type { Envelope } from "@hop-top/stem";

const env: Envelope = {
  crtx_version: CrtxVersion,
  id: "demo-1",
  created_at: "2026-05-28T14:00:00Z",
  updated_at: "2026-05-28T14:00:01Z",
  source: { kind: SourceKind, version: Version },
  turns: [
    {
      id: "t-0",
      role: "user",
      created_at: "2026-05-28T14:00:00Z",
      content: [{ type: "text", text: "What time is it?" }],
    },
    {
      id: "t-1",
      role: "assistant",
      created_at: "2026-05-28T14:00:01Z",
      content: [{ type: "text", text: "2026-05-28T14:00:01Z" }],
    },
  ],
};

const wire: string = serializeEnvelope(env);
```

```python
# Python
from stem import (
    CRTX_VERSION,
    SOURCE_KIND,
    VERSION,
    Envelope,
    Role,
    Source,
    TextPart,
    Turn,
    serialize_envelope,
)

env = Envelope(
    crtx_version=CRTX_VERSION,
    id="demo-1",
    created_at="2026-05-28T14:00:00Z",
    updated_at="2026-05-28T14:00:01Z",
    source=Source(kind=SOURCE_KIND, version=VERSION),
    turns=[
        Turn(
            id="t-0",
            role=Role.USER,
            created_at="2026-05-28T14:00:00Z",
            content=[TextPart(text="What time is it?")],
        ),
        Turn(
            id="t-1",
            role=Role.ASSISTANT,
            created_at="2026-05-28T14:00:01Z",
            content=[TextPart(text="2026-05-28T14:00:01Z")],
        ),
    ],
)

wire = serialize_envelope(env)
```

```rust
// Rust
use hop_top_stem::{
    serialize_envelope, ContentPart, Envelope, Role, Source, Turn,
};

let env = Envelope {
    crtx_version: "0.1".to_string(),
    id: "demo-1".to_string(),
    created_at: "2026-05-28T14:00:00Z".to_string(),
    updated_at: "2026-05-28T14:00:01Z".to_string(),
    source: Source::default_for_stem(),
    turns: vec![
        Turn {
            id: "t-0".to_string(),
            role: Role::User,
            created_at: "2026-05-28T14:00:00Z".to_string(),
            content: vec![ContentPart::text("What time is it?")],
            metadata: None,
        },
        Turn {
            id: "t-1".to_string(),
            role: Role::Assistant,
            created_at: "2026-05-28T14:00:01Z".to_string(),
            content: vec![ContentPart::text("2026-05-28T14:00:01Z")],
            metadata: None,
        },
    ],
    parent_id: None,
    fork_point: None,
    metadata: None,
};

let wire: String = serialize_envelope(&env)?;
```

```php
// PHP
use HopTop\Stem\Envelope;
use HopTop\Stem\RoleEnum;
use HopTop\Stem\Source;
use HopTop\Stem\Turn;
use HopTop\Stem\Content\TextPart;
use function HopTop\Stem\serializeEnvelope;

$env = new Envelope(
    crtxVersion: '0.1',
    id: 'demo-1',
    createdAt: '2026-05-28T14:00:00Z',
    updatedAt: '2026-05-28T14:00:01Z',
    source: Source::default(),
    turns: [
        new Turn(
            id: 't-0',
            role: RoleEnum::User,
            createdAt: '2026-05-28T14:00:00Z',
            content: [new TextPart('What time is it?')],
        ),
        new Turn(
            id: 't-1',
            role: RoleEnum::Assistant,
            createdAt: '2026-05-28T14:00:01Z',
            content: [new TextPart('2026-05-28T14:00:01Z')],
        ),
    ],
);

$wire = serializeEnvelope($env);
```

Expected output (canonical JSON, identical across all four SDKs after
`jq -cS`):

```json
{"crtx_version":"0.1","id":"demo-1","created_at":"2026-05-28T14:00:00Z","updated_at":"2026-05-28T14:00:01Z","source":{"kind":"stem","version":"0.1.0"},"turns":[{"id":"t-0","role":"user","created_at":"2026-05-28T14:00:00Z","content":[{"type":"text","text":"What time is it?"}]},{"id":"t-1","role":"assistant","created_at":"2026-05-28T14:00:01Z","content":[{"type":"text","text":"2026-05-28T14:00:01Z"}]}]}
```

### 3. Validate an envelope

`validate` runs the same structural checks as the Go reference: role
enum, ContentPart discriminator, `parent_id` ↔ `fork_point`
dependency, `tool_result.call_id` linkage. It returns a list of
structured errors; an empty list means the envelope is valid.

`validate_bytes` (Py / Rs) and `validateBytes` (TS / PHP) combine
strict decode plus structural validation in one call — the parse
step rejects unknown fields the validator alone wouldn't catch.

```ts
// TypeScript
import { validate, validateBytes } from "@hop-top/stem";

const errs = validate(env);
if (errs.length > 0) {
  for (const e of errs) console.error(`${e.path}: ${e.message}`);
}

// Or in one shot from raw JSON:
const errsFromBytes = validateBytes(jsonString);
```

```python
# Python
from stem import validate, validate_bytes

errors = validate(env)
for e in errors:
    print(f"{e.path}: {e.message}")

# Or in one shot:
errors_from_bytes = validate_bytes(json_string)
```

```rust
// Rust
use hop_top_stem::{validate, validate_bytes};

let errs = validate(&env);
for e in &errs {
    eprintln!("{e}");
}

let errs_from_bytes = validate_bytes(json_string.as_bytes());
```

```php
// PHP
use function HopTop\Stem\validate;
use function HopTop\Stem\validateBytes;

$errors = validate($env);
foreach ($errors as $e) {
    fwrite(STDERR, sprintf("%s: %s\n", $e->path, $e->message));
}

$errorsFromBytes = validateBytes($jsonString);
```

Expected behavior on the demo envelope: the returned list is empty.
Mutate any required field (delete `id`, drop `created_at`, switch
`role` to `"agent"`) and the same call returns at least one
`EnvelopeError` naming the failing path.

## Verify

Serialize the same logical envelope in every SDK you use, then
normalize through `jq -cS` and `diff`:

```sh
ts-cmd  | jq -cS . > /tmp/env.ts.json
py-cmd  | jq -cS . > /tmp/env.py.json
rs-cmd  | jq -cS . > /tmp/env.rs.json
php-cmd | jq -cS . > /tmp/env.php.json
diff /tmp/env.ts.json /tmp/env.py.json
diff /tmp/env.ts.json /tmp/env.rs.json
diff /tmp/env.ts.json /tmp/env.php.json
```

All four files must be byte-identical. `make test-parity` at the
repo root asserts this across the three vendored fixtures
(`minimal`, `tool-call`, `fork`) on every push.

## Common issues

| Symptom | Cause | Fix |
|---------|-------|-----|
| `unknown envelope field <name>` | Producer wrote a field crtx v0.1 doesn't allow. | Remove the field or move it under `metadata`. |
| `turns[N].content[M]: tool_result: call_id "X" has no preceding tool_call` | Tool result's `call_id` doesn't match an earlier `tool_call` in the same envelope. | Issue the `tool_call` first; reuse its `call_id` verbatim. |
| `image part: exactly one of data/url required` | An `image` ContentPart sets both `data` and `url` (or neither). | Pick one — base64 in `data` or remote in `url`. |
| `crtx_version "0.0" unsupported` | Wrong spec version on the wire. | This SDK speaks `0.1`; bump the producer or pin an older SDK release. |
| `unknown ContentPart type "my-thing"` | Non-standard content part missing the `x-` prefix. | Rename `type` to `x-my-thing` so it round-trips as an extension. |

## What is NOT available in polyglot SDKs

At v0.1, TS / Py / Rs / PHP ship envelope I/O only. The following
exist only in the Go SDK:

- `Provider` interface (LLM driver abstraction)
- `Runtime.Send` / agent loop / tool dispatch
- `ToolRegistry`
- `Store` interface and the in-memory / JSONL / SQLite backends
- `Supervisor` and `DirectRouter` for multi-session work
- `Session.Fork` / `Session.Dispatch` / `Session.ReturnDispatch` runtime semantics

If your service needs any of these, the recommended pattern is:
run stem-go as a sidecar / subprocess and consume the envelopes it
emits from your TS / Py / Rs / PHP code via the parse + validate
calls above. Polyglot runtime parity is post-v0.1.

## Next steps

- Understand where this layer fits: [overview](./overview.md).
- Record and replay LLM round-trips deterministically:
  [record-replay](./record-replay.md).
- Read the wire spec:
  [crtx v0.1 envelope](https://github.com/hop-top/spec-crtx/blob/main/specs/v0.1/envelope.md).
- See end-to-end Anthropic / OpenAI samples per SDK under
  `<lang>/examples/{current-time,web-search}/`.
