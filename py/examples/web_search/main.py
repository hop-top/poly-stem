"""web_search — second stem agent example, Python edition.

Same envelope-wiring shape as ../current_time/, but the tool is
``web_search``, executed by POSTing to api.tavily.com. One
``stem.Envelope`` captures the full transcript; two upstream services
(Anthropic + Tavily) are recorded into the same xrr cassette directory
because each HTTP call has a distinct method+URL+body fingerprint.

Steps:

 1. Construct a fresh ``stem.Envelope``.
 2. Append a user turn asking for the latest stable Go release.
 3. Call Anthropic with a ``web_search(query)`` tool.
 4. Project the assistant ``tool_use`` block into a ``ToolCallPart``.
 5. Execute the tool: POST to api.tavily.com (xrr-wrapped client).
 6. Append a ``tool``-role turn carrying a ``ToolResultPart``.
 7. Call Anthropic again with the tool result echoed back.
 8. Append the model's final text response.
 9. Serialize to ``./session.jsonl`` and reload + ``validate``.
"""
from __future__ import annotations

import json
import os
from pathlib import Path

import anthropic
import httpx
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
USER_PROMPT = "What's the latest stable Go release?"
TOOL_NAME = "web_search"
TAVILY_URL = "https://api.tavily.com/search"
MAX_TAVILY_RESULTS = 3
JSONL_PATH = Path(__file__).resolve().parent / "session.jsonl"
FIXED_TS = "2026-01-01T00:00:00Z"
TAVILY_PLACEHOLDER_KEY = "replay-mode-no-key-required"


def main() -> None:
    # TODO(post-cassettes): verify replay end-to-end once Py-shaped cassettes
    # land under ./cassettes/. Go-recorded cassettes use
    # the Go SDK's request body shape, so fingerprints differ.

    # --- 1. xrr-wrapped httpx client (shared between Anthropic + Tavily).
    http_client = new_http_client(CASSETTE_DIR)
    api_key = os.environ.get("ANTHROPIC_API_KEY", "replay-mode-no-key-required")
    client = anthropic.Anthropic(api_key=api_key, http_client=http_client)

    # --- 2. fresh stem envelope + user turn.
    env = Envelope(
        crtx_version=CRTX_VERSION,
        id="web-search-demo",
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
        "description": "Run a web search and return the top results.",
        "input_schema": {
            "type": "object",
            "properties": {
                "query": {
                    "type": "string",
                    "description": "Search query string.",
                }
            },
            "required": ["query"],
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

    # --- 5. execute tool against Tavily over the shared xrr-wrapped client.
    if isinstance(tool_input, dict):
        query = tool_input.get("query") or USER_PROMPT
    else:
        query = USER_PROMPT
    results = call_tavily(http_client, query)
    tool_result_payload = {"query": query, "results": results}

    env.turns.append(
        Turn(
            id="t-tool-2",
            role=Role.TOOL,
            created_at=FIXED_TS,
            content=[ToolResultPart(call_id=tool_use_id, output=tool_result_payload)],
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
                        "content": json.dumps(tool_result_payload),
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

    # --- 8. write session.jsonl.
    JSONL_PATH.write_text(serialize_envelope(env) + "\n")
    print(f"wrote {JSONL_PATH} ({len(env.turns)} turns)")

    # --- 9. reload + validate.
    reloaded = parse_envelope(JSONL_PATH.read_text().strip())
    errs = validate(reloaded)
    if errs:
        raise RuntimeError(f"validate reloaded envelope: {[str(e) for e in errs]}")
    print("envelope round-trip + validate: ok")


def call_tavily(http_client: httpx.Client, query: str) -> list[dict]:
    """POST the query to api.tavily.com via the xrr-wrapped client.

    The request body always carries the placeholder ``api_key`` so the
    shipped cassettes replay deterministically regardless of who
    recorded them. Real keys never enter the request body or the
    cassette.
    """
    body = {
        "api_key": TAVILY_PLACEHOLDER_KEY,
        "query": query,
        "max_results": MAX_TAVILY_RESULTS,
    }
    resp = http_client.post(
        TAVILY_URL,
        content=json.dumps(body).encode("utf-8"),
        headers={"Content-Type": "application/json"},
    )
    if resp.status_code >= 300:
        raise RuntimeError(f"tavily: status {resp.status_code}: {resp.text}")
    raw = resp.json()
    out: list[dict] = []
    for r in raw.get("results", []):
        out.append(
            {
                "title": r.get("title", ""),
                "url": r.get("url", ""),
                "snippet": r.get("content", ""),
            }
        )
    return out


def _assistant_blocks_for_echo(message) -> list:
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
