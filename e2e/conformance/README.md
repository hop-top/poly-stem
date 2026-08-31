# Conformance grading (12fcc service tier)

Scenario library, recorded cassettes, and captured verdicts for the
service-graded factors of the 12-Factor AI-CLI conformance contract:
F3 Structured I/O, F4 Corrective Error Model, F5 Explicit Contracts,
F6 Previewability, F7 Idempotency, F8 State Transparency, and F11
Exit Code Semantics. The story-graded factors (F1, F2, F9, F12) and
leak scanning (F10) are covered by the `12fcc` workflow over
`e2e/stories/`.

## Layout

```
scenarios/stem/<id>/1.0.0/scenario.yaml   graded rubric, one per scenario
stories/<id>.yaml                         user story each scenario is bound to
fixtures/crtx/<producer>/sessions/        committed crtx v0.1 envelopes the
                                          recorder seeds into the scan roots
cassettes/<id>/                           captures from real binary runs
  manifest.yaml                           upload manifest (story hash, steps)
  story.yaml                              byte-exact copy of the story
  steps/<step-id>/result.json             exit code + duration as observed
  steps/<step-id>/stdout.txt              stdout as observed
  steps/<step-id>/stderr.txt              stderr as observed
verdicts/<id>.json                        grading service verdict, tier 3
```

Cassettes are recorded, never authored: every stdout/stderr byte and
exit code comes from executing the actual binary via `kit conformance
harness record`. Scenarios encode the spec-correct expectation even
where current behavior violates it — a failing verdict is the honest
outcome, not a broken pipeline. Re-records churn only `recorded_at`,
`binary_version`, and `duration_ms`; captures are byte-stable.

## Re-running

Record fresh cassettes (builds the binary, seeds the fixture store
into a fixed work dir, runs every scenario step as a real
subprocess):

```sh
KIT_BIN=/path/to/kit make 12fcc-record
```

Grade them against a locally served scenario library (requires a
`kit` binary that ships the `conformance` command group; point
`KIT_BIN` at one built from `hop.top/kit` `cmd/kit`):

```sh
KIT_BIN=/path/to/kit make 12fcc-grade
```

The grade target boots `kit conformance svc serve` on a loopback port
with `--scenarios-root e2e/conformance`, mints a `grade:stem` token
into a throwaway claims DB, uploads every cassette at tier 3,
rewrites `verdicts/`, and prints per-scenario verdicts plus a
per-factor rollup. Grading is measurement, not a gate: the target
only fails when a cassette cannot be graded at all. Set `STRICT=1` to
also fail on `fail` verdicts.

## Badge

`.12fc.json` at the repo root — the shields.io endpoint behind the
README badge — is committed and is the source of truth for all 12
factors. CI never rewrites it: the `12fcc` workflow can only run the
story and leak leaves (F1, F2, F9, F10, F12), and the badge verdict
counts measured factors only, so a CI refresh would degrade the
truthful `12/12 pass` badge to a red `5/5 pass` one. The workflow
keeps its badge commit disabled until a released `kit` can genuinely
grade cassettes in CI; the enable checklist lives in
`.github/workflows/12fcc.yaml`.

Refresh the badge locally, from measurements only:

```sh
KIT_BIN=/path/to/kit make 12fcc-record   # re-record cassettes from the real binary
KIT_BIN=/path/to/kit make 12fcc-grade    # tier-3 verdicts into verdicts/
KIT_BIN=/path/to/kit make 12fcc-badge    # verify leaves + verdicts -> .12fc.json
```

The badge target re-runs `verify-no-leak` and `verify-stories`
against `e2e/stories/` (the same scope CI checks), folds in the
per-factor statuses from `verdicts/`, and renders the badge with
`kit conformance badge` — the same verdict and colour rules CI uses.
Commit the resulting `.12fc.json` diff together with the re-recorded
cassettes and verdicts.

## Current verdicts

As of the captures in `cassettes/` (see `verdicts/` for full facets):

| Scenario | Verdict | Factors |
|---|---|---|
| `sessions-list-json` | pass | F3 F8 F11 |
| `sessions-show-envelope` | pass | F3 F8 F11 |
| `search-repeat-read` | pass | F3 F7 |
| `error-envelope-correction` | pass | F4 F11 |
| `contract-surfaces` | pass | F5 F11 |
| `read-only-surface` | pass | F5 F6 |

Factor rollup: every graded factor (F3, F4, F5, F6, F7, F8, F11)
passes. The behaviors that carry the verdicts:

- **F3/F8** — every sessions leaf renders one JSON document on
  stdout under `--format json`; session state (source, recency,
  backing store), fork lineage, and turn-level envelope content are
  inspectable as typed fields.
- **F4** — error documents carry `code` + `hint` (spec §3.6):
  not-found names the recovery command, ambiguous prefixes list
  structured candidates, usage rejections point at the failing
  leaf's own `--help`.
- **F5** — `stem capabilities` serves the machine-readable command
  surface built on kit's toolspec walker; `--api-version` pins are
  honored; formats outside the declared text|json contract are
  refused loudly.
- **F6** — previewability holds vacuously and verifiably: the
  capability document declares zero mutating leaves and zero
  dry-run requirements, pinned as measured counts. Adding a write
  leaf without preview support flips the `read-only-surface`
  scenario.
- **F7** — repeated reads converge: the identical search exits
  clean twice with the same match set.
- **F11** — the process honors envelope-declared exit codes and the
  kit exit-class table (USAGE=2, NOT_FOUND=3, CONFLICT=4); unknown
  commands are usage errors instead of help + exit 0.

If a regression re-introduces a defect, re-record and re-grade; the
scenario library encodes the spec-correct expectation, so the verdict
flips without rubric changes.
