# poly-stem — agent steering

This repository is the polyglot home for `stem`, the AI agent
runtime for the [`crtx`](https://github.com/hop-top/spec-crtx) envelope
format. Five SDKs ship from one repo under language-named
subdirectories (`go/`, `ts/`, `py/`, `rs/`, `php/`).

## Conventions

- **Spec compliance**: `crtx` is the source of truth for envelope
  shape (sessions, turns, content parts). Any change to that shape
  belongs in the `crtx` repo first. `stem` follows; it does not
  lead.
- **Reference impl**: the Go SDK is the reference runtime — agent
  loop, tool registry, supervisor, stores. Other SDKs (TS, Py, Rs,
  PHP) ship envelope I/O only at v0.1. When adding a feature that
  affects the wire, land it in Go first, then propagate to every
  other SDK that needs it.
- **Naming**: `stem` is the runtime. Use `crtx` types for anything
  that crosses a wire or hits disk. Internal-only types (depth
  limits, supervisor topics) may stay in `stem`.
- **Parity**: run `make test-parity` before touching envelope code
  in any SDK. All 15 cells (3 fixtures × 5 SDKs) must stay green.
- **Commits**: Conventional Commits. Scope = top-level dir name
  (`go`, `ts`, `py`, `rs`, `php`, `tools`, `docs`, `ci`).
  Telegraphese in commit bodies.
