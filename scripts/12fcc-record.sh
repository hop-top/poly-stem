#!/usr/bin/env bash
# Record 12fcc conformance cassettes from real stem runs.
#
# For every scenario under e2e/conformance/scenarios/stem/, drives
# `kit conformance harness record` against a freshly built binary:
# each step executes as a real subprocess; exit code, stdout, stderr,
# and duration are captured verbatim into the svc upload layout the
# grading service consumes:
#
#   e2e/conformance/cassettes/<scenario-id>/
#     manifest.yaml
#     story.yaml
#     steps/<step-id>/{result.json,stdout.txt,stderr.txt}
#
# Captures are never edited after the fact; re-running re-records
# everything from scratch. The recorder refuses to proceed when the
# story bytes do not hash to the scenario's declared content_hash.
#
# Scan roots are pinned to a fixed work dir seeded from the committed
# fixture store (e2e/conformance/fixtures/) so the store paths that
# leak into recorded output stay deterministic across re-records.
#
# Requirements:
#   - KIT_BIN: path to a kit binary that ships `conformance harness
#     record` (build from hop.top/kit cmd/kit). Defaults to `kit` on
#     PATH.
#
# Usage: [KIT_BIN=/path/to/kit] scripts/12fcc-record.sh
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
CONF="$REPO_ROOT/e2e/conformance"
BIN="$REPO_ROOT/bin/stem"
KIT_BIN="${KIT_BIN:-kit}"
WORK="${STEM_12FCC_WORK:-/tmp/stem-12fcc-record}"

# The probe requires real help output (not just exit 0) so a wrong
# binary can never masquerade as a conformance-capable kit.
probe="$("$KIT_BIN" conformance harness record --help 2>/dev/null || true)"
case "$probe" in
  *--scenario*) ;;
  *)
    echo "ERROR: '$KIT_BIN' does not ship 'conformance harness record'. Set KIT_BIN to a kit binary built from hop.top/kit cmd/kit." >&2
    exit 1
    ;;
esac

echo "==> building $BIN"
(cd "$REPO_ROOT/go" && go build -buildvcs=false -o "$BIN" ./cmd/stem)
BINARY_VERSION="$(cd "$REPO_ROOT" && git rev-parse --short HEAD 2>/dev/null || echo dev)"

echo "==> seeding fixture store at $WORK"
rm -rf "$WORK"
mkdir -p "$WORK/empty"
cp -R "$CONF/fixtures/crtx" "$WORK/crtx"

# Every step subprocess inherits this environment: scans hit only the
# seeded fixture store, never a real user store.
export STEM_CRTX_DIRS="$WORK/crtx/stem/sessions:$WORK/crtx/nerv/sessions"
export STEM_CLAUDECODE_DIRS="$WORK/empty"
export STEM_CODEX_DIRS="$WORK/empty"
export STEM_COPILOT_DIRS="$WORK/empty"

for scenario in "$CONF"/scenarios/stem/*/*/scenario.yaml; do
  sid="$(basename "$(dirname "$(dirname "$scenario")")")"
  echo "==> recording $sid"
  rm -rf "$CONF/cassettes/$sid"
  "$KIT_BIN" conformance harness record \
    --scenario "$scenario" \
    --binary "$BIN" \
    --binary-version "$BINARY_VERSION" \
    --out "$CONF/cassettes/$sid" \
    --workdir "$WORK" \
    --no-hints >/dev/null
done

echo "==> cassettes written to $CONF/cassettes"
