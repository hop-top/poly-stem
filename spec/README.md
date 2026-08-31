# spec

`stem` implements the language-agnostic
[`crtx`](https://github.com/hop-top/spec-crtx) v0.1 envelope spec for
sessions, turns, and content parts. `crtx` is the on-the-wire and
on-disk contract; this repository does not own the spec.

## Source of truth

- Spec repo: <https://github.com/hop-top/spec-crtx>
- Versioned: `crtx` v0.1 → first `stem` minor (`0.1.x`)
- Sibling implementers: [`vein`](https://github.com/hop-top/vein),
  [`nerv`](https://github.com/hop-top/nerv)

Any change to envelope shape (session, turn, content-part schemas)
belongs in `crtx` first. `stem` follows; it does not lead.

## What lives here

Nothing yet. This directory is a placeholder so the polyglot layout
matches sibling repos.

A future release may vendor conformance fixtures from `crtx` under
`spec/fixtures/` for offline parity testing across the language SDKs.
Until then, run conformance against the upstream `crtx` repo directly.

## What does not live here

- Envelope schemas (live in `crtx`)
- Per-language SDK type definitions (live under `go/`, `ts/`, `py/`,
  `rs/`, `php/`)
- Runtime-internal types — depth limits, supervisor topics, tool
  registry shapes (live in the SDK package they belong to)
