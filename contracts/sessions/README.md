# contracts/sessions

Eva contracts pinning the `--format json` output shapes of the
`stem sessions` command group. The prose spec is
[`docs/specs/sessions-cli.md`](../../docs/specs/sessions-cli.md);
these contracts are its machine-checkable half.

## Contents

| File | Pins |
|------|------|
| [`pack.yaml`](pack.yaml) | Pack manifest composing the five contracts |
| [`list.yaml`](list.yaml) | `sessions list` document + the shared SessionSummary shape |
| [`search.yaml`](search.yaml) | `sessions search` document (matches + hits) |
| [`show.yaml`](show.yaml) | `sessions show` = crtx v0.1 Envelope invariant subset |
| [`lineage.yaml`](lineage.yaml) | `sessions lineage` flat node array |
| [`error.yaml`](error.yaml) | Structured error document (stderr, JSON mode) |

## Run

Each contract validates one captured stdout (or, for `error.yaml`,
stderr) document:

```sh
stem sessions list --format json > /tmp/list.json
eva run --contract contracts/sessions/list.yaml --input /tmp/list.json
```

All evaluators are deterministic (`json_schema_valid`, binary mode) —
no LLM calls, no network.

## Stability

Shapes follow the spec's 0.1 evolution rule: fields may be added,
never removed or renamed; consumers ignore unknown fields. The
schemas therefore do not set `additionalProperties: false`. The
authoritative schema for `show` output is the upstream crtx
`envelope.schema.json`; `show.yaml` pins only the invariant subset.
