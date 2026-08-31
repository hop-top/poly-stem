# stem documentation

Documentation index for `stem`, the polyglot AI agent runtime.

## Pick your starting point by intent

| I want to… | Read |
|------------|------|
| Decide whether stem fits my stack | [overview.md](overview.md) |
| Build my first Go agent | [quickstart-go.md](quickstart-go.md) |
| Read or write envelopes in TS / Py / Rs / PHP | [quickstart-polyglot.md](quickstart-polyglot.md) |
| Make my LLM-driven tests deterministic | [record-replay.md](record-replay.md) |
| Diagnose an error that spans SDKs | [troubleshooting.md](troubleshooting.md) |
| Look up past CLI sessions (spec) | [specs/sessions-cli.md](specs/sessions-cli.md) |
| Verify cross-SDK wire-format parity | [`../tools/parity/README.md`](../tools/parity/README.md) |
| Read the wire spec | [crtx v0.1 envelope](https://github.com/hop-top/spec-crtx/blob/main/specs/v0.1/envelope.md) |
| Cut a release | [`../RELEASING.md`](../RELEASING.md) |
| Contribute changes | [`../CONTRIBUTING.md`](../CONTRIBUTING.md) |
| Report a vulnerability | [`../SECURITY.md`](../SECURITY.md) |

## Disclosure ladder

The pages above are ordered from "decide" → "do" → "operate":

1. **Decide** — [overview.md](overview.md) tells you whether stem
   belongs in your stack and which SDK covers what.
2. **Do** — pick the language: [quickstart-go.md](quickstart-go.md)
   for the full runtime, [quickstart-polyglot.md](quickstart-polyglot.md)
   for the four envelope-only SDKs.
3. **Operate** — [record-replay.md](record-replay.md) makes your tests
   deterministic; [`../tools/parity/README.md`](../tools/parity/README.md)
   proves cross-SDK conformance.
4. **Reference** — the [crtx spec](https://github.com/hop-top/spec-crtx)
   is the wire contract; the per-SDK READMEs under `../{go,ts,py,rs,php}/`
   document each language's surface.

## Future layout

The folder is mostly flat. As the project grows, deeper content
lands under:

| Path | Purpose |
|------|---------|
| `design/` | ADRs and design notes for stem-internal decisions |
| `specs/` | Local specs — CLI surfaces and extensions over `crtx` (first entry: [sessions-cli.md](specs/sessions-cli.md)) |
| `personas/` | Target users — agent builders, runtime operators |
| `stories/` | User-facing behavior stories |

Until the rest lands, the flat layout above is the canonical entry
point.
