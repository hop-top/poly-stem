"""current_time — smallest viable stem agent loop, Python edition.

Mirrors the Go example wire-for-wire. Manual envelope wiring through
one tool-use round-trip against the Anthropic Messages API:

 1. Build the xrr session (defaults to replay against vendored cassettes).
 2. Construct a fresh stem ``Envelope`` and append a user turn.
 3. Call Anthropic with a single ``current_time`` tool definition.
 4. Project the assistant ``tool_use`` block into a stem ``ToolCallPart``
    and append an assistant turn.
 5. Execute the tool locally and append a ``tool``-role turn carrying a
    ``ToolResultPart``.
 6. Call Anthropic again with the updated transcript echoed back.
 7. Append the model's final text response.
 8. Serialize the envelope to ``./session.jsonl`` (one envelope per
    line — matches the crtx JSONL convention).
 9. Reload + ``validate`` the round-trip.

The Anthropic HTTP transport is wrapped with xrr, so the same code
records cassettes (``XRR_MODE=record ANTHROPIC_API_KEY=... uv run python
main.py``) and replays them deterministically (default mode, no key).
"""
from __future__ import annotations

import json
import os
from pathlib import Path

import anthropic
from xrr_httpx import new_http_client

from stem import (
    CRTX_VERSION,
    Envelope,
    Role,
    Source,
    TextPart,
    ToolCallPart,
    ToolResultPart,
    Turn,
    parse_envelope,
    serialize_envelope,
    validate,
)

CASSETTE_DIR = str((Path(__file__).resolve().parent / "cassettes").resolve())
MODEL = "claude-sonnet-4-6"
MAX_TOKENS = 1024
USER_PROMPT = "What time is it?"
TOOL_NAME = "current_time"
JSONL_PATH = Path(__file__).resolve().parent / "session.jsonl"
FIXED_TS = "2026-01-01T00:00:00Z"


def main() -> None:
    # TODO(post-cassettes): verify replay end-to-end once Py-shaped cassettes
    # land under ./cassettes/. Go-recorded cassettes
    # use the Go SDK's request body shape, so fingerprints differ.
    # --- 1. xrr-wrapped httpx client.
    http_client = new_http_client(CASSETTE_DIR)
    api_key = os.environ.get("ANTHROPIC_API_KEY", "replay-mode-no-key-required")
    client = anthropic.Anthropic(api_key=api_key, http_client=http_client)

    # --- 2. fresh stem envelope + user turn.
    env = Envelope(
        crtx_version=CRTX_VERSION,
        id="current-time-demo",
        created_at=FIXED_TS,
        updated_at=FIXED_TS,
        source=Source(kind="stem", version="0.1.0"),
        turns=[
            Turn(
                id="t-user-0",
                role=Role.USER,
                created_at=FIXED_TS,
                content=[TextPart(text=USER_PROMPT)],
            )
        ],
    )

    # --- 3. tool definition + first Anthropic call.
    tool_def: dict = {
        "name": TOOL_NAME,
        "description": "Returns the current UTC time as an ISO 8601 string.",
        "input_schema": {
            "type": "object",
            "properties": {},
            "required": [],
        },
    }

    first = client.messages.create(
        model=MODEL,
        max_tokens=MAX_TOKENS,
        messages=[{"role": "user", "content": USER_PROMPT}],
        tools=[tool_def],
    )

    # --- 4. project assistant message into a stem turn.
    assistant_parts: list = []
    tool_use_id = ""
    tool_input: dict = {}
    for block in first.content:
        if block.type == "text":
            assistant_parts.append(TextPart(text=block.text))
        elif block.type == "tool_use":
            assistant_parts.append(
                ToolCallPart(call_id=block.id, name=block.name, input=block.input or {})
            )
            if not tool_use_id:
                tool_use_id = block.id
                tool_input = block.input or {}

    if not assistant_parts:
        raise RuntimeError("assistant returned no usable content blocks")
    if not tool_use_id:
        raise RuntimeError("model returned no tool_use; cassette/model out of sync")

    env.turns.append(
        Turn(
            id="t-assistant-1",
            role=Role.ASSISTANT,
            created_at=FIXED_TS,
            content=assistant_parts,
        )
    )

    # --- 5. execute tool locally + append tool-role turn.
    tool_result = execute_current_time(tool_input)
    env.turns.append(
        Turn(
            id="t-tool-2",
            role=Role.TOOL,
            created_at=FIXED_TS,
            content=[ToolResultPart(call_id=tool_use_id, output=tool_result)],
        )
    )

    # --- 6. second Anthropic call with tool result echoed back.
    assistant_blocks = _assistant_blocks_for_echo(first)
    second = client.messages.create(
        model=MODEL,
        max_tokens=MAX_TOKENS,
        messages=[
            {"role": "user", "content": USER_PROMPT},
            {"role": "assistant", "content": assistant_blocks},
            {
                "role": "user",
                "content": [
                    {
                        "type": "tool_result",
                        "tool_use_id": tool_use_id,
                        "content": json.dumps(tool_result),
                    }
                ],
            },
        ],
        tools=[tool_def],
    )

    # --- 7. append final assistant text.
    final_parts: list = []
    for block in second.content:
        if block.type == "text":
            final_parts.append(TextPart(text=block.text))
    if not final_parts:
        raise RuntimeError("final assistant message had no text blocks")
    env.turns.append(
        Turn(
            id="t-assistant-3",
            role=Role.ASSISTANT,
            created_at=FIXED_TS,
            content=final_parts,
        )
    )

    # --- 8. write session.jsonl (one envelope per line).
    JSONL_PATH.write_text(serialize_envelope(env) + "\n")
    print(f"wrote {JSONL_PATH} ({len(env.turns)} turns)")

    # --- 9. reload + validate.
    reloaded = parse_envelope(JSONL_PATH.read_text().strip())
    errs = validate(reloaded)
    if errs:
        raise RuntimeError(f"validate reloaded envelope: {[str(e) for e in errs]}")
    print("envelope round-trip + validate: ok")


def execute_current_time(_input: dict) -> dict:
    """Local handler for the current_time tool.

    Fixed timestamp keeps the recorded cassette stable across re-records;
    production code would call ``datetime.now(UTC)``.
    """
    return {"time": FIXED_TS, "zone": "UTC"}


def _assistant_blocks_for_echo(message) -> list:
    """Convert the model's response back into the param-shaped blocks
    needed to echo the assistant turn on the next call.
    """
    out: list = []
    for block in message.content:
        if block.type == "text":
            out.append({"type": "text", "text": block.text})
        elif block.type == "tool_use":
            out.append(
                {
                    "type": "tool_use",
                    "id": block.id,
                    "name": block.name,
                    "input": block.input or {},
                }
            )
    return out


if __name__ == "__main__":
    main()
