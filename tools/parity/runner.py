#!/usr/bin/env python3
"""tools/parity/runner.py — cross-SDK parity harness.

For each crtx v0.1 fixture, invoke every SDK's parity-roundtrip helper
(parse + serialize), canonicalize the output via ``jq -cS``, and assert
byte-identity across the 5 polyglot SDKs.

Exit 0 on full parity, nonzero on any divergence or helper failure.

Usage:
    tools/parity/runner.py            # run all SDKs
    tools/parity/runner.py go ts      # restrict to a subset

Required on ``$PATH``: ``jq``, ``go``, ``node``, ``python3``,
``cargo``, ``php``. The TS helper expects ``ts/dist/`` to already be
built — the Makefile target ``test-parity`` arranges that.
"""
from __future__ import annotations

import argparse
import os
import shutil
import subprocess
import sys
import tempfile
from pathlib import Path
from typing import Iterable

REPO_ROOT = Path(__file__).resolve().parents[2]

FIXTURES = ["minimal", "tool-call", "fork"]
ALL_SDKS = ["go", "ts", "py", "rs", "php"]


def testdata_dir(sdk: str) -> Path:
    """Each SDK vendors its own copy of the 3 fixtures. Helpers are
    pointed at their SDK-local copy so nothing reads across SDK
    boundaries.
    """
    if sdk == "go":
        return REPO_ROOT / "go" / "testdata" / "crtx_v0.1"
    if sdk == "ts":
        return REPO_ROOT / "ts" / "testdata" / "crtx_v0.1"
    if sdk == "py":
        return REPO_ROOT / "py" / "tests" / "testdata" / "crtx_v0.1"
    if sdk == "rs":
        return REPO_ROOT / "rs" / "tests" / "testdata" / "crtx_v0.1"
    if sdk == "php":
        return REPO_ROOT / "php" / "tests" / "testdata" / "crtx_v0.1"
    raise ValueError(f"unknown SDK {sdk!r}")


def helper_command(sdk: str, fixture_path: Path) -> tuple[list[str], Path, dict[str, str]]:
    """Return (argv, cwd, env_overrides) for one (sdk, fixture) run."""
    if sdk == "go":
        env = {"GOCACHE": os.environ.get("GOCACHE", "/tmp/stem-go-build")}
        return (
            ["env", "-u", "GOROOT", "go", "run", "./cmd/parity-roundtrip", str(fixture_path)],
            REPO_ROOT / "go",
            env,
        )
    if sdk == "ts":
        return (
            ["node", "tools/parity-roundtrip.js", str(fixture_path)],
            REPO_ROOT / "ts",
            {},
        )
    if sdk == "py":
        python = os.environ.get("PYTHON", sys.executable)
        return (
            [python, "tools/parity_roundtrip.py", str(fixture_path)],
            REPO_ROOT / "py",
            {"PYTHONPATH": str(REPO_ROOT / "py" / "src")},
        )
    if sdk == "rs":
        return (
            ["cargo", "run", "--example", "parity-roundtrip", "--quiet", "--", str(fixture_path)],
            REPO_ROOT / "rs",
            {},
        )
    if sdk == "php":
        return (
            ["php", "tools/parity-roundtrip.php", str(fixture_path)],
            REPO_ROOT / "php",
            {},
        )
    raise ValueError(f"unknown SDK {sdk!r}")


def run_helper(sdk: str, fixture: str) -> tuple[int, bytes, bytes]:
    """Invoke the SDK helper for one fixture; return (returncode, stdout, stderr)."""
    fixture_path = testdata_dir(sdk) / f"{fixture}.json"
    argv, cwd, env_overrides = helper_command(sdk, fixture_path)
    env = os.environ.copy()
    env.update(env_overrides)
    proc = subprocess.run(
        argv,
        cwd=str(cwd),
        env=env,
        stdout=subprocess.PIPE,
        stderr=subprocess.PIPE,
        check=False,
    )
    return proc.returncode, proc.stdout, proc.stderr


def canonicalize(raw: bytes) -> tuple[int, bytes, bytes]:
    """Pipe raw JSON through ``jq -cS .``; return (returncode, stdout, stderr)."""
    proc = subprocess.run(
        ["jq", "-cS", "."],
        input=raw,
        stdout=subprocess.PIPE,
        stderr=subprocess.PIPE,
        check=False,
    )
    return proc.returncode, proc.stdout, proc.stderr


def print_matrix(sdks: list[str], status: dict[tuple[str, str], str]) -> None:
    """Render the SDK × fixture matrix with one cell per pair."""
    col_w = max(8, max((len(f) for f in FIXTURES), default=0) + 2)
    print()
    print("        " + "".join(f"{f:<{col_w}}" for f in FIXTURES))
    for sdk in sdks:
        cells = []
        for fx in FIXTURES:
            cells.append(f"{status[(sdk, fx)]:<{col_w}}")
        print(f"{sdk:<8}" + "".join(cells))
    print()


def main(argv: list[str]) -> int:
    parser = argparse.ArgumentParser(description=__doc__.splitlines()[0])
    parser.add_argument(
        "sdks",
        nargs="*",
        help=f"SDKs to test (default: {' '.join(ALL_SDKS)})",
    )
    args = parser.parse_args(argv)
    sdks: list[str] = args.sdks or list(ALL_SDKS)
    unknown = [s for s in sdks if s not in ALL_SDKS]
    if unknown:
        print(f"parity: unknown SDK(s): {' '.join(unknown)}", file=sys.stderr)
        return 2
    if shutil.which("jq") is None:
        print("parity: jq is required on PATH", file=sys.stderr)
        return 2

    workdir = Path(tempfile.mkdtemp(prefix="stem-parity."))
    try:
        # Slot 0 of each value is the status keyword, slot 1 the canonical bytes
        # (only set when status is 'ok'), slot 2 the captured stderr.
        status: dict[tuple[str, str], str] = {}
        canon: dict[tuple[str, str], bytes] = {}
        errs: dict[tuple[str, str], bytes] = {}

        for fixture in FIXTURES:
            for sdk in sdks:
                rc, out, err = run_helper(sdk, fixture)
                if rc != 0:
                    status[(sdk, fixture)] = "FAIL"
                    errs[(sdk, fixture)] = err
                    continue
                rc2, cout, cerr = canonicalize(out)
                if rc2 != 0:
                    status[(sdk, fixture)] = "BADJSON"
                    errs[(sdk, fixture)] = err + cerr
                    continue
                status[(sdk, fixture)] = "ok"
                canon[(sdk, fixture)] = cout

        # Cross-SDK comparison per fixture.
        diverged: set[tuple[str, str]] = set()
        for fixture in FIXTURES:
            ref_sdk: str | None = next(
                (s for s in sdks if status[(s, fixture)] == "ok"), None
            )
            if ref_sdk is None:
                for sdk in sdks:
                    diverged.add((sdk, fixture))
                continue
            ref_bytes = canon[(ref_sdk, fixture)]
            for sdk in sdks:
                if status[(sdk, fixture)] != "ok":
                    diverged.add((sdk, fixture))
                    continue
                if canon[(sdk, fixture)] != ref_bytes:
                    diverged.add((sdk, fixture))

        # Mark divergent-but-ok cells as 'diff' in the matrix.
        display_status = dict(status)
        for key in diverged:
            if display_status.get(key) == "ok":
                display_status[key] = "diff"

        print_matrix(sdks, display_status)

        if diverged:
            print("parity: divergence detected")
            for fixture in FIXTURES:
                ref_sdk = next(
                    (s for s in sdks if status[(s, fixture)] == "ok"), None
                )
                for sdk in sdks:
                    if (sdk, fixture) not in diverged:
                        continue
                    print()
                    print(f"--- {sdk} × {fixture} ---")
                    st = status[(sdk, fixture)]
                    if st in ("FAIL", "BADJSON"):
                        print(f"helper status: {st}")
                        err = errs.get((sdk, fixture), b"")
                        if err:
                            print("stderr:")
                            for line in err.decode("utf-8", "replace").splitlines():
                                print(f"  {line}")
                    elif st == "ok" and ref_sdk is not None and sdk != ref_sdk:
                        print(f"canonical diff vs reference SDK ({ref_sdk}):")
                        ref_text = canon[(ref_sdk, fixture)].decode("utf-8", "replace")
                        cur_text = canon[(sdk, fixture)].decode("utf-8", "replace")
                        import difflib
                        for line in difflib.unified_diff(
                            ref_text.splitlines(),
                            cur_text.splitlines(),
                            fromfile=ref_sdk,
                            tofile=sdk,
                            lineterm="",
                        ):
                            print(f"  {line}")
            return 1

        cases = len(FIXTURES) * len(sdks)
        print(
            f"parity: ok {' '.join(sdks)} ({cases} cases across {len(FIXTURES)} fixtures)"
        )
        return 0
    finally:
        shutil.rmtree(workdir, ignore_errors=True)


if __name__ == "__main__":
    raise SystemExit(main(sys.argv[1:]))
