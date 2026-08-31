# Releasing

Releases run via [release-please](https://github.com/googleapis/release-please).
The manifest lives at `.github/.release-please-manifest.json`; the
config at `.github/release-please-config.json`. Six components are
managed in lockstep — one umbrella + one per SDK.

## How releases run

1. Conventional Commits on `main` trigger release-please.
2. release-please opens **one release PR per component** (six total
   at most per cycle) with bumped versions and changelog entries.
3. Merging a release PR creates a GitHub release + tag formatted
   `<component>/v<version>` (e.g. `stem/v0.1.0-alpha.0`,
   `stem-ts/v0.1.0-alpha.0`).
4. `.github/workflows/publish.yml` fires on each tag and calls the
   org-wide reusable workflow
   [`hop-top/.github/.github/workflows/publish-on-tag.yml@v0`](https://github.com/hop-top/.github/blob/main/.github/workflows/publish-on-tag.yml),
   which parses `<component>/v<version>` from the tag, looks up the
   `ecosystems` entry in `publish.yml`, and dispatches to the
   per-language publish + mirror reusable workflows
   (`publish-ts.yml`, `publish-py.yml`, `publish-rs.yml`,
   `mirror-subtree.yml`).

The Go module publishes by tag presence alone — `proxy.golang.org`
resolves `hop.top/stem` from the `stem/v<version>` tag, no mirror
required at the polyglot-repo layer.

## Know the six components

| Component | Path | Release type | Package name | Prerelease policy |
|-----------|------|--------------|--------------|-------------------|
| `poly-stem` | `.` | `go` (umbrella) | — | `alpha.0`, `versioning: prerelease` |
| `stem` | `go/` | `go` | `hop.top/stem` | `alpha.0`, `versioning: prerelease` |
| `stem-ts` | `ts/` | `node` | `@hop-top/stem` | `alpha.0`, `versioning: prerelease` |
| `stem-py` | `py/` | `python` | `hop-top-stem` | `alpha.0`, `versioning: prerelease` |
| `stem-rs` | `rs/` | `rust` | `hop-top-stem` (crates.io) | `alpha.0`, `versioning: prerelease` |
| `stem-php` | `php/` | `php` | `hop-top/stem` | `alpha.0`, `versioning: prerelease` |

Commit scopes use the **path name** (`feat(ts): ...`,
`feat(go): ...`), not the component name. release-please routes
each commit to the right component by path-prefix match.

## Remember v0.1 scope

All six packages ship `0.1.0-alpha.0` as their first release.

- The `stem` Go package is the **reference runtime** — full agent
  loop, tool registry, supervisor, memory / JSONL / SQLite stores.
- `stem-ts`, `stem-py`, `stem-rs`, `stem-php` are **envelope I/O
  only** at v0.1 — parse, serialize, validate. No runtime, no
  Provider interface, no storage.

Polyglot runtime parity is post-v0.1 and is not promised on any
specific milestone. See [`README.md`](README.md) for the full v0.1
scope statement.

## Choose a version bump

Pre-1.0 (current):

- `feat:` / `fix:` / `perf:` → minor (`0.x → 0.x+1`).
- `feat!:` / `BREAKING CHANGE` → minor (downgraded from major via
  `bump-minor-pre-major: true`).

Post-1.0:

- `feat:` → minor.
- `fix:` / `perf:` → patch.
- `feat!:` / `BREAKING CHANGE` → major.

`bump-minor-pre-major: true` retires at `1.0.0` per component.

## Cut a prerelease

While the API surface stabilises, every component stays on the
alpha track. The manifest seeds each component at `0.0.0` with
`prerelease-type: alpha.0` and `versioning: prerelease`, so the
first release-please run computes `0.1.0-alpha.0` for each.

The trailing `.0` on `alpha.0` is intentional — without it
release-please skips `alpha.0` and starts at `alpha.1`.

## Ship the first release

On the first release-please pass after merging the v0.1
scaffolding:

- Up to six release PRs open simultaneously (one per component
  with merged unreleased commits).
- Each PR targets `0.1.0-alpha.0` for its component.
- Merging a release PR creates the tag and triggers
  `publish.yml` → org-wide `publish-on-tag.yml`.

Merge order does not matter — components publish independently —
but merge the umbrella `poly-stem` PR last so its changelog
references the per-SDK tags that already exist.

## Verify before merging a release PR

Before merging a release PR (or any PR that touches envelope
code):

- [ ] `make test-parity` green on `main` — 15 / 15 cells.
- [ ] CI matrix green for every SDK that has tests in the diff.
- [ ] `CHANGELOG.md` entries are release-please-generated, not
      hand-authored. Do not edit `CHANGELOG.md` manually.
- [ ] No `T-NNNN` or other internal refs in commit titles, PR
      titles, or release notes.

## Ship a hotfix

For an alpha-channel hotfix:

1. Land the `fix:` commit on `main` with the correct scope.
2. release-please opens a release PR bumping the alpha counter
   (`0.1.0-alpha.N → 0.1.0-alpha.N+1`).
3. Merge as usual.

There is no separate release branch while on the alpha track.
