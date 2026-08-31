# Contributing to stem

`stem` is a polyglot package shipping five SDKs from one repo. The
envelope shape (sessions, turns, content parts) is owned by the
upstream [`crtx`](https://github.com/hop-top/spec-crtx) spec — `stem`
follows it; it does not lead. Go is the reference runtime; the four
other SDKs (TS, Py, Rs, PHP) ship the envelope I/O layer only at
v0.1.

## Find what you're changing

| Path | What |
|------|------|
| [`go/`](go/) | Go reference runtime — agent loop, tool registry, supervisor, memory / JSONL / SQLite stores |
| [`ts/`](ts/) | TypeScript envelope SDK (parse / serialize / validate) |
| [`py/`](py/) | Python envelope SDK |
| [`rs/`](rs/) | Rust envelope SDK |
| [`php/`](php/) | PHP envelope SDK |
| [`spec/`](spec/) | Pointer to upstream [`crtx`](https://github.com/hop-top/spec-crtx) v0.1 envelope spec |
| [`tools/parity/`](tools/parity/) | Cross-SDK envelope-conformance harness |
| [`docs/`](docs/) | Product + engineering docs |
| [`.github/`](.github/) | CI, release-please config, publish flow |

Each SDK directory owns its build, test, and packaging tooling.

## Getting started

1. Fork the repository.
2. Clone your fork locally.
3. Create a feature branch: `git checkout -b feat/my-change`.
4. Make focused changes inside ONE SDK when possible — cross-SDK
   changes start in `crtx` (see [Adding a feature](#adding-a-feature)).
5. Run the SDK's tests + the parity harness.
6. Commit using Conventional Commits.
7. Push and open a pull request.

## Run tests for your SDK

```sh
# Go
cd go && go test ./...

# TypeScript
cd ts && pnpm install && pnpm test

# Python
cd py && uv sync && uv run pytest

# Rust
cd rs && cargo test --workspace

# PHP
cd php && composer install && ./vendor/bin/phpunit
```

Each SDK's README documents its full lint / build / package surface.

## Verify cross-SDK parity before you push

All five SDKs must round-trip the same crtx v0.1 envelopes to
byte-identical canonical JSON. Run the harness from the repo root
**before opening any PR** that touches envelope code, types, or
serialization:

```sh
make test-parity
```

15 cells (3 fixtures × 5 SDKs); all must be green. Per-SDK debugging:

```sh
make test-parity-go
make test-parity-ts
make test-parity-py
make test-parity-rs
make test-parity-php
```

See [`tools/parity/`](tools/parity/) for what the harness proves
(wire format only — not behavioral equivalence, not HTTP cassette
parity).

## Add a feature

`crtx` is upstream of `stem`. Spec changes propagate one direction:
`crtx` → Go reference → other SDKs.

If a change affects envelope shape, persistence layout, supervisor
topics, or public API semantics:

1. Land the envelope change in [`crtx`](https://github.com/hop-top/spec-crtx) first.
2. Implement the behavior in the Go reference runtime.
3. Propagate to every other SDK that needs to consume or emit the
   new shape. For v0.1 polyglot scope, this is parse / serialize /
   validate only — runtime features stay Go-only until polyglot
   runtime parity lands.
4. Add or update tests in each affected SDK.
5. Re-run `make test-parity` and update fixtures under
   [`tools/parity/`](tools/parity/) if the canonical form changed.
6. Update docs under [`docs/`](docs/).

Do not let one SDK define behavior the others cannot reproduce on
the wire.

## Record cassettes for samples

The sample apps under `<lang>/examples/{current-time,web-search}/`
(Python uses `current_time` / `web_search`) wrap their HTTP transport
with [xrr](https://github.com/hop-top/xrr) for record / replay.

Cassettes live **per-language, per-sample** at
`<lang>/examples/<sample>/cassettes/` and are not shared across SDKs —
xrr fingerprints requests by body bytes and each SDK's JSON
serialization differs in formatting.

To record cassettes locally (provider API key required):

```sh
cd <lang>/examples/<sample>
XRR_MODE=record OPENAI_API_KEY=sk-... <run command>     # current-time
XRR_MODE=record TAVILY_API_KEY=tvly-... <run command>   # web-search
```

The exact run command is in each sample's README. Cassettes
recorded in `record` mode are committed alongside the sample so
replay works without keys.

## Verify before you push

Run, in order, the SDK tests for every directory you touched plus the
parity harness:

```sh
cd go && go test ./...
cd ts && pnpm install && pnpm test
cd py && uv sync && uv run pytest
cd rs && cargo test --workspace
cd php && composer install && ./vendor/bin/phpunit
make test-parity         # from repo root
```

Expected: all SDK suites green, `make test-parity` reports `15/15 ok`.
A red parity cell is a real cross-SDK wire-format divergence — fix the
non-conformant SDK rather than skipping the cell.

## Write docs under `docs/`

Markdown files under `docs/` must use the `.ops` frontmatter keys:

- `doc_type`, `subtype`, `status`, `title`, `summary`, `owner`,
  `created`, `updated`, `audience`, `confidentiality`, `tags`

Frontmatter formatting is strict:

- Opening `---` is the first line of the file.
- No blank line immediately after the opening `---`.
- One blank line after the closing `---` before any text or header.

Use controlled values already present in `docs/` unless a new
option is explicitly added to the business / project documentation
configuration.

## Match the SDK's style

- Follow existing conventions in each SDK directory.
- Keep files small; split when behavior boundaries blur (~500 LOC
  is a soft cap).
- Keep public facades stable; hide runtime / store / provider
  dependencies behind replaceable adapters.
- Do not commit generated build artifacts, dependency caches, or
  local tool output.

## Write a Conventional Commit

Use [Conventional Commits](https://www.conventionalcommits.org/).
Scopes match top-level directory names so release-please can route
to the right component:

```text
feat(go): add JSONL store rotation
fix(ts): correct timestamp serialization
feat(py): validate source.kind strictly
fix(rs): reject unknown roles at decode
feat(php): list-shaped metadata reject
chore(tools): bump parity fixture count
docs: clarify cassette recording flow
ci: pin gofumpt version
```

Allowed types: `feat`, `fix`, `refactor`, `build`, `ci`, `chore`,
`docs`, `style`, `perf`, `test`. Breaking changes use `!` or a
`BREAKING CHANGE:` trailer.

Do not downgrade a user-visible feature to `chore:` to avoid a
release bump.

## Open a PR that will merge

- Reference related issues, specs, or ADRs.
- Keep PRs small and reviewable.
- Ensure CI passes — both the per-SDK matrix and the parity job —
  before requesting review.
- Update docs when behavior, API, or workflow changes.
- Do not edit `CHANGELOG.md` by hand. release-please owns it.

## Follow the code of conduct

Be respectful, specific, and constructive. Focus reviews on
correctness, maintainability, and cross-SDK parity.
