---
doc_type: spec
subtype: cli
status: draft
title: stem sessions command group
summary: CLI UX and crtx query semantics for cross-CLI session lookup (list / search / show / lineage).
owner: jadb
created: 2026-08-07
updated: 2026-08-07
audience: contributors
confidentiality: public
tags: [cli, sessions, crtx, adapters]
---

# `stem sessions` — cross-CLI session lookup

Spec for the `stem sessions` command group: `list`, `search`, `show`,
`lineage`. It restores cross-CLI recall of past agent sessions on the
[crtx](https://github.com/hop-top/spec-crtx) envelope layer, where all
CLIs normalize.

**Status: Draft.** Targets crtx v0.1 (itself Draft/alpha). The JSON
output shapes in this document are the contract that the session
adapters and CLI implementations build against; they are pinned by
the eva contracts under [`contracts/sessions/`](../../contracts/sessions/).

"MUST", "SHOULD", "MAY" follow
[RFC 2119](https://www.rfc-editor.org/rfc/rfc2119).

## Scope

- In: read-only lookup over sessions from native CLI stores
  (Claude Code, Codex, Copilot) and crtx-native producers; substring /
  regex search with time-window and cwd filters; fork / dispatch
  lineage resolution; text + JSON output on every leaf.
- Out: semantic / embedding search, session editing, live session
  attach, non-CLI sources. Adapters never write to native stores.

## 1. Model

Every session the group operates on is a crtx v0.1 Envelope. Two
paths produce them:

| Path | Producers | How |
|------|-----------|-----|
| crtx-native | stem runtime, nerv, any conformant tool | Read as-is from session roots (below) |
| Adapter-normalized | claude-code, codex, copilot | Native store files converted to Envelopes at read time, in memory |

Adapters are **read-only** views over native stores. Normalization is
lossy-tolerant: records an adapter cannot map are skipped, never
fatal. The Envelope's `source.kind` carries the producing tool
(`claude-code`, `codex`, `copilot`, `stem`, `nerv`); `source.version`
carries the producing tool's version when the native store exposes
it.

### 1.1 Session roots

crtx-native envelopes are discovered under
`$XDG_STATE_HOME/<producer>/sessions/` (default
`~/.local/state/<producer>/sessions/`) for producers `crtx`, `stem`,
and `nerv`. One file per session, named `<session-id>.jsonl`, one
Envelope JSON document per line; the **last non-empty line is
authoritative** (tolerates streaming appenders; matches the Go SDK's
`JSONLStore` convention). Subdirectories one level deep are also
scanned; deeper nesting is not.

`STEM_CRTX_DIRS` (colon-separated, empty entries dropped) overrides
the default root list entirely. Highest priority.

Native stores live where each CLI writes them; each adapter owns its
discovery paths:

| Adapter | Store |
|---------|-------|
| claude-code | `~/.claude/projects/<project-dir>/*.jsonl` |
| codex | `~/.codex/sessions/YYYY/MM/DD/rollout-*.jsonl` |
| copilot | Defined by the copilot adapter |

Envelopes whose `crtx_version` is not `0.1` are skipped with a
warning on stderr.

### 1.2 Identity and dedup

The session id is the Envelope `id`. Adapters MUST reuse the native
store's session identifier verbatim (Claude Code `sessionId`, Codex
`session_meta.payload.id`) — never re-mint, so ids remain stable
across re-normalization and match what users see in the native tools.

When the same id is discovered via multiple paths, it is **one**
session: a crtx-native envelope wins over an adapter-normalized one;
among crtx-native duplicates the most recently `updated_at` wins.

### 1.3 Adapter metadata contract

Adapters (and crtx-native producers, where applicable) SHOULD set
these reverse-DNS keys in `Envelope.metadata`. The CLI's cwd filter
and summary output read them; absence degrades to `null` in output,
never to an error:

| Key | Value |
|-----|-------|
| `top.hop.stem.cwd` | Absolute working directory the session ran in |
| `top.hop.stem.git_branch` | Git branch at session start, when known |
| `top.hop.stem.git_commit` | Git commit hash at session start, when known |
| `top.hop.stem.native_path` | Absolute path of the native store file the envelope was normalized from (adapter-normalized only) |

### 1.4 Lineage mapping

Native stores encode parent/child structure in tool-specific ways;
adapters map them to the two crtx child-envelope relationships:

- **Fork** (`parent_id` + `fork_point`): a session that resumes or
  branches another session's history.
- **Dispatch nest** (`dispatched_from: {envelope_id, call_id}`): a
  sub-agent transcript spawned by an open `tool_call` in a parent
  session (e.g. Claude Code sidechain files).

The exact per-store extraction is each adapter's concern; the
requirement here is only that whatever linkage the native store does
encode surfaces as these crtx fields, because `lineage` (§2.5)
resolves exclusively through them.

## 2. Command surface

```text
stem sessions list                 # enumerate sessions, newest first
stem sessions search <query>       # find sessions by content/metadata
stem sessions show <id>            # one session, turn by turn
stem sessions lineage <id>         # fork/dispatch chain around a session
```

All four leaves are **read-only** (safety class: read). None prompt,
none mutate any store, all are idempotent.

Every leaf takes an explicit format contract: `--format text|json`,
default `text`. Kit-family global flags (`-C/--chdir`, `-c/--config`,
color/verbosity controls) apply as on every hop.top CLI and are not
re-specified here.

Output discipline, both formats:

- stdout carries only the command's result document.
- Warnings, progress, and skip notices go to stderr.
- In `--format json`, stdout is exactly one UTF-8 JSON document with
  a trailing newline, conforming to the shapes in §3.

### 2.1 Id addressing

`show` and `lineage` take a session id argument. **Full id or
case-insensitive literal prefix**, minimum 4 characters. The prefix
is matched against the full id string (hyphens included, as stored).

- Exactly one session matches → resolved.
- No session matches → exit 3, not-found error.
- More than one matches → exit 4, ambiguous-id error listing up to
  10 candidate summaries so the caller can retry with a longer
  prefix. In `--format json` the candidates are structured (§3.6).

Rationale: native ids are UUIDs (Claude Code UUIDv4, Codex UUIDv7);
crtx recommends ULID/UUIDv7. Full 36-char ids are hostile to human
recall; git-style unique-prefix addressing is the established CLI
norm and is cheap to resolve over the scanned id set. Text output
renders ids as 8-character prefixes; JSON always carries full ids.

### 2.2 Shared filter flags — `list` and `search`

The two proven recall axes — time window and working directory — are
first-class on both leaves:

| Flag | Value | Semantics |
|------|-------|-----------|
| `--since <when>` | RFC 3339 timestamp, `YYYY-MM-DD` date, or relative `<N>h`/`<N>d`/`<N>w` | Keep sessions whose `[created_at, updated_at]` interval intersects `[since, until]` |
| `--until <when>` | same | Upper bound of the window; default now |
| `--cwd <path>` | path (relative resolved against the invocation cwd) | Keep sessions whose recorded cwd (§1.3) equals `path` or lies under it (path-segment-boundary prefix) |
| `--source <kind>` | repeatable | Keep sessions whose `source.kind` matches any given value |
| `--limit <n>` | int, `0` = unlimited | Cap result count after sorting; default 20 |

Interval-intersection (not point-in-window) is deliberate: a
long-running session touched inside the window is a recall hit even
if it started before the window opened.

Zero results is success (exit 0) with an empty result set.

### 2.3 `list`

Enumerates sessions across all roots and adapters, ordered by
`updated_at` descending (newest first). Ties break by `id` ascending
for deterministic output.

Dispatch-nest children (`dispatched_from` set) are **excluded by
default** — sub-agent transcripts drown the top-level list. Fork
children are included by default (they are real user-facing
sessions). `--nested` includes dispatch nests.

| Flag | Semantics |
|------|-----------|
| shared filters (§2.2) | |
| `--nested` | Include dispatch-nest children |

Text output: one row per session — id prefix, source kind, updated
timestamp, turn count, cwd, derived title (first `user` text part,
whitespace-collapsed, truncated). Example (synthetic):

```text
ID        SOURCE       UPDATED           TURNS  CWD                TITLE
0198f2d4  codex        2026-08-07 09:14     31  ~/code/acme        fix the flaky auth test
7c9a11b0  claude-code  2026-08-06 22:41    118  ~/code/acme        add rate limiting to the api
a44e02c9  stem         2026-08-06 18:02     12  ~/experiments/rag  compare retrieval settings
```

When `--limit` truncates, a one-line notice goes to stderr and the
JSON document sets `truncated: true`.

### 2.4 `search`

`search <query>` finds sessions matching a query, subject to the
shared filters. The query positional is REQUIRED (filter-only
enumeration is `list`'s job; missing query is a usage error, exit 2).

Matching:

- Default: case-insensitive fixed-string containment.
- `--regex`: query is an RE2 regular expression.
- `--case-sensitive`: literal-case matching (either mode).

Scope — what the query runs against:

| `--scope` | Searches |
|-----------|----------|
| `content` | `text` and `thinking` ContentParts of every turn, all roles |
| `metadata` | Envelope `metadata` values and the derived title |
| `all` (default) | Both |

`tool_call.input` and `tool_result.output` are excluded from
`content` by default (high noise, unbounded size); `--tools` extends
the content scope to include them.

Unlike `list`, search includes dispatch-nest children by default —
recall must see sub-agent transcripts.

| Flag | Semantics |
|------|-----------|
| shared filters (§2.2) | |
| `--regex` | Treat query as RE2 |
| `--case-sensitive` | Disable case folding |
| `--scope content\|metadata\|all` | Default `all` |
| `--role <role>` | Restrict content matches to turns of this role |
| `--tools` | Include tool_call input / tool_result output in content scope |

Results are grouped per session (sessions ordered `updated_at`
descending; hits within a session in turn order). Each hit carries a
snippet: the matched region with surrounding context, whitespace
collapsed, capped at 200 characters. Text output is grep-like
(synthetic):

```text
0198f2d4  codex  2026-08-07  ~/code/acme
  [t12 user]      …the auth test is flaky on CI, retry logic maybe…
  [t14 assistant] …made the auth test deterministic by freezing the clock…
```

### 2.5 `lineage`

`lineage <id>` resolves the continuation chain around a session,
across CLIs: ancestors via `parent_id`/`fork_point` and
`dispatched_from.envelope_id`, descendants by scanning all known
sessions for envelopes whose `parent_id` or
`dispatched_from.envelope_id` references a chain member. Because
resolution runs over the full scanned set (§1.1), a chain may cross
sources — e.g. a Claude Code session forked and continued under
stem.

Semantics:

- Ancestors: walk parent references transitively to the root.
- Descendants: transitive closure of children of the target (not of
  its ancestors — siblings of the target are out of scope).
- A parent reference that resolves to no known envelope produces an
  **unresolved placeholder node** (chain truncates there); it is
  reported, never fatal. Registry-level resolution of foreign
  envelope ids is out of scope per crtx §7.3.
- Cycle guard: resolution MUST terminate by never revisiting an id.

Text output renders the chain as a tree, root first, the queried
session marked (synthetic):

```text
7c9a11b0  claude-code  2026-08-05  add rate limiting to the api
└─ fork @t42
   0198f2d4  codex  2026-08-07  fix the flaky auth test   ◀ target
   └─ dispatch (call tc_0091)
      3d1e01b1  claude-code  2026-08-07  [sidechain] run the test matrix
```

### 2.6 `show`

`show <id>` prints one session turn by turn. Text output: a header
(id, source, timestamps, cwd, branch, lineage pointers when set)
followed by each turn's role and content; `thinking` parts render
collapsed to a marker, `tool_call`/`tool_result` render name and a
truncated payload preview.

In `--format json` the output is the **normalized crtx Envelope
itself, verbatim** — no wrapper. The schema of record is upstream
[`envelope.schema.json`](https://github.com/hop-top/spec-crtx/blob/main/specs/v0.1/envelope.schema.json);
the eva contract (§4) pins the invariant subset. This makes `show
--format json` the universal escape hatch: anything crtx-aware can
consume it directly.

### 2.7 Exit codes

| Code | Meaning |
|------|---------|
| 0 | Success (including empty result sets) |
| 1 | Runtime failure (I/O, corrupt root, internal error) |
| 2 | Usage error (unknown flag, missing query, bad `--since` value) |
| 3 | Session id not found (`show`, `lineage`) |
| 4 | Ambiguous id prefix (`show`, `lineage`) |

## 3. JSON output contracts

Stability rules (mirrors crtx versioning): within this spec's 0.1
line, fields MAY be added to any shape but MUST NOT be removed,
renamed, or change meaning. Consumers MUST ignore unknown fields.
Text output is **not** contractual and may change freely; automation
MUST use `--format json`.

Nullable-but-present beats absent: every field listed as required
below is always emitted, with `null` when unknown.

### 3.1 SessionSummary (shared shape)

```json
{
  "id": "0198f2d4-7b1c-7e02-9c40-2f6a8d1e5b77",
  "source": { "kind": "codex", "version": "0.145.0" },
  "created_at": "2026-08-07T08:02:11Z",
  "updated_at": "2026-08-07T09:14:56Z",
  "turn_count": 31,
  "cwd": "/home/dev/code/acme",
  "git_branch": "main",
  "title": "fix the flaky auth test",
  "parent_id": null,
  "fork_point": null,
  "dispatched_from": null,
  "store": {
    "adapter": "codex",
    "path": "/home/dev/.codex/sessions/2026/08/07/rollout-….jsonl"
  }
}
```

`store` is provenance: which adapter produced the summary and from
which file (`adapter` is `crtx` for crtx-native envelopes; `path` is
the envelope file itself in that case). `cwd`, `git_branch`, and
`title` are denormalized from §1.3 metadata / turn content for
direct `jq` ergonomics.

### 3.2 `list --format json`

```json
{
  "sessions": [ SessionSummary, … ],
  "truncated": false
}
```

### 3.3 `search --format json`

```json
{
  "query": "auth test",
  "matches": [
    {
      "session": SessionSummary,
      "hits": [
        {
          "scope": "content",
          "turn_index": 12,
          "turn_id": "t_0c2f",
          "role": "user",
          "snippet": "…the auth test is flaky on CI…"
        }
      ]
    }
  ],
  "truncated": false
}
```

Metadata-scope hits carry `"scope": "metadata"` with `turn_index`,
`turn_id`, and `role` set to `null`.

### 3.4 `show --format json`

The crtx v0.1 Envelope, verbatim (§2.6).

### 3.5 `lineage --format json`

```json
{
  "target": "0198f2d4-7b1c-7e02-9c40-2f6a8d1e5b77",
  "nodes": [
    {
      "id": "7c9a11b0-…",
      "parent": null,
      "relation": null,
      "fork_point": null,
      "call_id": null,
      "resolved": true,
      "summary": SessionSummary
    },
    {
      "id": "0198f2d4-…",
      "parent": "7c9a11b0-…",
      "relation": "fork",
      "fork_point": 42,
      "call_id": null,
      "resolved": true,
      "summary": SessionSummary
    },
    {
      "id": "3d1e01b1-…",
      "parent": "0198f2d4-…",
      "relation": "dispatch",
      "fork_point": null,
      "call_id": "tc_0091",
      "resolved": true,
      "summary": SessionSummary
    }
  ]
}
```

Flat node array, root first, then breadth-first. `relation` is how a
node attaches to its `parent` (`"fork"` or `"dispatch"`; `null` for
the root). Unresolved placeholders set `"resolved": false` with
`"summary": null`. `target` names the queried node's full id.

### 3.6 Errors

On failure with `--format json`, stdout stays empty and stderr
carries exactly one JSON document:

```json
{
  "error": {
    "code": "ambiguous_id",
    "message": "prefix \"0198\" matches 3 sessions",
    "hint": "retry with a longer prefix",
    "details": { "candidates": [ SessionSummary, … ] }
  }
}
```

`code` values: `not_found` (exit 3), `ambiguous_id` (exit 4),
`usage` (exit 2), `internal` (exit 1). `hint` and `details` are
optional; `candidates` is capped at 10.

## 4. Contracts

The JSON shapes above are pinned by eva contracts under
[`contracts/sessions/`](../../contracts/sessions/) — one contract
per leaf plus the error shape, composed by `pack.yaml`. Each
contract validates a captured `--format json` stdout document with
the `json_schema_valid` evaluator in binary mode. CI for the CLI
implementation MUST run each leaf and validate its output against
the matching contract.

## 5. Conformance notes

Alignment with the 12-factor AI-CLI expectations that the kit
strict gate enforces:

- Every leaf declares its format contract explicitly (`text|json`,
  default `text`); no leaf inherits a bare `--format`.
- All four leaves are side-effect-free reads; safe to retry, safe to
  delegate.
- Errors are structured and corrective (§3.6): classification code,
  message, hint, machine-readable candidates.
- Provenance rides every summary (`store.adapter` + `store.path`)
  and every shown envelope (`top.hop.stem.native_path`).
- Schemas evolve additively within 0.1; breaking shape changes
  require a new spec version.

## 6. Open questions (deferred to implementation tasks)

- Claude Code sidechain files (`agent-*.jsonl`) → dispatch-nest
  `call_id` extraction: which native field identifies the spawning
  tool call.
- Codex resume/branch metadata → `parent_id`/`fork_point` mapping.
- Copilot store location and shape (its adapter defines both).
- Repo-identity matching (same repo across worktrees / clones via
  remote URL) as a complement to path-subtree `--cwd`.
- Archived / rotated native stores beyond the live directories.
- A `latest` convenience selector for `show`.
