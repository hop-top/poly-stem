# stem (php)

PHP SDK for `stem`, the polyglot AI agent runtime. Implements the
**crtx v0.1** envelope on the wire — the same shape produced by the
Go, TypeScript, Python, and Rust SDKs.

> This Tier A release covers **envelope I/O only**: types, parsing,
> serialization, and validation. The runtime layer (`Provider`,
> `Supervisor`, storage backends) is intentionally out of scope. Build
> your loop with manual envelope wiring; the polyglot examples in
> `examples/` show the exact pattern.

## Use this when

- You are recording an AI session in PHP and want it readable by
  other crtx-aware tools.
- You are wiring a stem envelope through a PHP service that consumes
  output from stem-go upstream.
- You need typed (readonly) value objects plus strict decode for
  envelope I/O.

If you need an agent loop, tool dispatch, or session persistence, use
the Go SDK — those are Go-only at v0.1.

## Before you begin

- PHP 8.2 or newer (readonly properties + first-class enums).
- `composer` for install.
- No runtime dependencies; `ext-json` only.

## Outcome

After installing, you can construct an envelope with typed readonly
value objects, append turns immutably with `Envelope::appendTurn`,
serialize to canonical JSON, and validate against the same structural
rules the Go reference enforces.

## Install

```bash
composer require hop-top/stem
```

Requires PHP 8.2+.

## Quickstart

```php
<?php

require 'vendor/autoload.php';

use HopTop\Stem\Content\TextPart;
use HopTop\Stem\Envelope;
use HopTop\Stem\RoleEnum;
use HopTop\Stem\Turn;
use function HopTop\Stem\serializeEnvelope;
use function HopTop\Stem\validate;

$env = Envelope::fresh('demo-1');

$env = $env->appendTurn(new Turn(
    id:        't0',
    role:      RoleEnum::User,
    createdAt: '2026-05-28T14:00:00Z',
    content:   [new TextPart('Hello.')],
));

$env = $env->appendTurn(new Turn(
    id:        't1',
    role:      RoleEnum::Assistant,
    createdAt: '2026-05-28T14:00:01Z',
    content:   [new TextPart('Hi! How can I help?')],
));

$errors = validate($env);
assert($errors === []);

echo serializeEnvelope($env), PHP_EOL;
```

## Public surface

Namespace: `HopTop\Stem\…`.

| Symbol                         | Purpose                                                              |
| ------------------------------ | -------------------------------------------------------------------- |
| `Stem::CRTX_VERSION`           | crtx spec version (`"0.1"`).                                         |
| `Stem::VERSION`                | stem semver, emitted as `source.version`.                            |
| `Stem::SOURCE_KIND`            | Canonical kind (`"stem"`).                                           |
| `RoleEnum`                     | String-backed enum: `User`/`Assistant`/`Tool`/`System`/`Developer`.  |
| `Source`                       | `{kind, version, instance?}` — readonly value type.                  |
| `Envelope`                     | Top-level container. `Envelope::fresh($id)` + `appendTurn($turn)`.   |
| `Turn`                         | One contribution. Holds an ordered `ContentPart[]`.                  |
| `Content\ContentPart`          | Common interface for all parts.                                      |
| `Content\TextPart`             | `{text}`.                                                            |
| `Content\ToolCallPart`         | `{callId, name, input}` — `input` may be object or string.           |
| `Content\ToolResultPart`       | `{callId, output, isError?}`.                                        |
| `Content\ImagePart`            | `{mime, data XOR url, alt?}` — constructor enforces the XOR.         |
| `Content\ThinkingPart`         | `{text, signature?}` — provider-supplied chain-of-thought trace.     |
| `Content\ExtensionPart`        | Verbatim round-trip for `x-*` ContentParts.                          |
| `parseEnvelope(string)`        | Strict decode → `Envelope`. Rejects unknown fields.                  |
| `serializeEnvelope(Envelope)`  | Canonical JSON string. Empty turns serialize to `[]`, not `null`.    |
| `validate(Envelope)`           | Structural validation → `EnvelopeError[]`.                           |
| `validateBytes(string)`        | Strict decode + structural validate.                                 |
| `EnvelopeError`                | `{path, message}`.                                                   |
| `EnvelopeParseException`       | Thrown on decode-time failure.                                       |

## Samples

Two end-to-end examples live under `examples/`:

- `examples/current-time/` — single-tool round-trip against OpenAI.
- `examples/web-search/`   — OpenAI + Tavily, two tools across one envelope.

Both replay deterministically from each sample's own `cassettes/` dir
via [xrr][xrr]; no API keys required for the default `XRR_MODE=replay`
path.

```bash
cd examples/current-time
composer install
php index.php
```

## Verify

```sh
cd php && composer install && ./vendor/bin/phpunit
```

Expected: all tests pass. To prove this SDK still matches the wire
spec, run the cross-SDK harness from the repo root:

```sh
make test-parity-php
```

Expected: `php: 3/3 ok` against the `minimal`, `tool-call`, and `fork`
fixtures.

## Common issues

| Symptom | Cause | Fix |
|---------|-------|-----|
| `EnvelopeParseException: unknown envelope field "<name>"` | Producer wrote a field crtx v0.1 doesn't allow. | Move the field under `metadata`, or remove it. |
| `EnvelopeError[]` non-empty from `validate($env)` | Structural rule failed (role enum, image data/url XOR, orphan tool_result, ...). | Iterate `$errors` — each carries `path` and `message`. |
| `ImagePart` constructor throws | `mime` + exactly one of `data` / `url` required; both or neither violates the XOR. | Pick one — base64 in `data` or remote URL in `url`. |
| Looking for `Provider` / `Runtime` / `Supervisor` | Tier B (runtime) is Go-only at v0.1. | Run `stem-go` as a sidecar and consume the envelopes from PHP. |
| `make test-parity` shows `diff` for `php` | Canonical JSON diverged from the reference SDK. | Don't mutate canonicalization — fix the serializer. |

For cross-SDK issues (parity diffs, envelope parse errors, xrr
cassette pitfalls), see [`../docs/troubleshooting.md`](../docs/troubleshooting.md).

## Next steps

- Side-by-side polyglot examples: [`../docs/quickstart-polyglot.md`](../docs/quickstart-polyglot.md).
- Record / replay LLM round-trips: [`../docs/record-replay.md`](../docs/record-replay.md).

## Spec

[crtx v0.1 envelope spec][spec] is the authoritative wire contract.

## License

MIT. See the [`hop-top/poly-stem` LICENSE](https://github.com/hop-top/poly-stem/blob/main/LICENSE).

[xrr]: https://github.com/hop-top/poly-xrr
[spec]: https://spec.hop.top/crtx/v0.1/envelope.md
