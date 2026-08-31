// current-time — smallest viable stem (TS) agent loop.
//
// Demonstrates manual wiring of a crtx envelope through one Anthropic
// Messages tool-use round-trip:
//
//   1. Construct a fresh Envelope by hand.
//   2. Append a user turn asking the time.
//   3. Call Anthropic with a single `current_time` tool definition.
//   4. Convert the assistant's tool_use block into a stem ToolCallPart
//      and append an assistant turn.
//   5. Execute the tool locally (fixed timestamp for cassette stability).
//   6. Append a tool-role turn carrying a ToolResultPart.
//   7. Call Anthropic again with the updated conversation.
//   8. Append the model's final text response.
//   9. Serialize to ./session.jsonl, reload, and validate.
//
// The Anthropic HTTP transport is wrapped with xrr so the same code
// records cassettes (XRR_MODE=record + ANTHROPIC_API_KEY) and replays
// them deterministically (default mode, no key needed).
//
// TODO(post-cassettes): verify replay end-to-end once cassettes land
// in ./cassettes/.

import { fileURLToPath } from "node:url";
import { dirname, resolve } from "node:path";
import { mkdir, readFile, writeFile } from "node:fs/promises";
import Anthropic from "@anthropic-ai/sdk";
import {
  CrtxVersion,
  SourceKind,
  Version,
  parseEnvelope,
  serializeEnvelope,
  validate,
} from "@hop-top/stem";
import type { ContentPart, Envelope, Turn } from "@hop-top/stem";

import { makeXrrFetch, modeFromEnv, newXrrSession } from "./xrrhttp.js";

const __filename = fileURLToPath(import.meta.url);
const __dirname = dirname(__filename);

const CASSETTE_DIR = resolve(__dirname, "../cassettes");
const MODEL = "claude-sonnet-4-6";
const MAX_TOKENS = 1024;
const USER_PROMPT = "What time is it?";
const TOOL_NAME = "current_time";
const JSONL_PATH = resolve(__dirname, "../session.jsonl");
const FIXED_TS = "2026-01-01T00:00:00Z";

async function main(): Promise<void> {
  // --- 1. xrr session.
  await mkdir(CASSETTE_DIR, { recursive: true });
  const xrr = newXrrSession(CASSETTE_DIR, modeFromEnv(process.env.XRR_MODE));

  // Anthropic SDK requires *something* in apiKey even in replay mode.
  const apiKey = process.env.ANTHROPIC_API_KEY ?? "replay-mode-no-key-required";
  const anthropic = new Anthropic({ apiKey, fetch: makeXrrFetch(xrr) });

  // --- 2. fresh Envelope + initial user turn.
  const envelope: Envelope = {
    crtx_version: CrtxVersion,
    id: "current-time-demo",
    created_at: FIXED_TS,
    updated_at: FIXED_TS,
    source: { kind: SourceKind, version: Version },
    turns: [
      {
        id: "t-user-0",
        role: "user",
        created_at: FIXED_TS,
        content: [{ type: "text", text: USER_PROMPT }],
      },
    ],
  };

  // --- 3. tool definition + first Anthropic call.
  const tool: Anthropic.Tool = {
    name: TOOL_NAME,
    description: "Returns the current UTC time as an ISO 8601 string.",
    input_schema: { type: "object", properties: {}, required: [] },
  };

  const first = await anthropic.messages.create({
    model: MODEL,
    max_tokens: MAX_TOKENS,
    tools: [tool],
    messages: [{ role: "user", content: USER_PROMPT }],
  });

  // --- 4. project assistant turn.
  const { turn: assistantTurn, toolUseId, toolInput } = assistantTurnFromMessage(
    first,
    "t-assistant-1"
  );
  envelope.turns.push(assistantTurn);

  if (!toolUseId) {
    throw new Error("model returned no tool_use; cassette/model out of sync");
  }

  // --- 5. execute tool locally + append tool-role turn.
  const toolResult = executeCurrentTime(toolInput);
  envelope.turns.push({
    id: "t-tool-2",
    role: "tool",
    created_at: FIXED_TS,
    content: [
      {
        type: "tool_result",
        call_id: toolUseId,
        output: toolResult,
      },
    ],
  });

  // --- 6. second Anthropic call with tool result echoed back.
  const second = await anthropic.messages.create({
    model: MODEL,
    max_tokens: MAX_TOKENS,
    tools: [tool],
    messages: [
      { role: "user", content: USER_PROMPT },
      { role: "assistant", content: first.content },
      {
        role: "user",
        content: [
          {
            type: "tool_result",
            tool_use_id: toolUseId,
            content: JSON.stringify(toolResult),
          },
        ],
      },
    ],
  });

  // --- 7. append final assistant text.
  const { turn: finalTurn } = assistantTurnFromMessage(second, "t-assistant-3");
  envelope.turns.push(finalTurn);

  // --- 8. write ./session.jsonl (one envelope per line).
  await writeFile(JSONL_PATH, serializeEnvelope(envelope) + "\n", "utf8");
  console.log(`wrote ${JSONL_PATH} (${envelope.turns.length} turns)`);

  // --- 9. reload + validate.
  const reloadedRaw = await readFile(JSONL_PATH, "utf8");
  const reloaded = parseEnvelope(reloadedRaw.trim());
  const errs = validate(reloaded);
  if (errs.length > 0) {
    throw new Error(
      `validate reloaded envelope: ${errs.map((e) => `${e.path}: ${e.message}`).join("; ")}`
    );
  }
  console.log("envelope round-trip + validate: ok");
}

function executeCurrentTime(_input: Record<string, unknown>): Record<string, unknown> {
  // Fixed value keeps the recorded cassette stable across re-records.
  // Production code would use `new Date().toISOString()`.
  return { time: FIXED_TS, zone: "UTC" };
}

function assistantTurnFromMessage(
  m: Anthropic.Message,
  turnId: string
): { turn: Turn; toolUseId?: string; toolInput: Record<string, unknown> } {
  const parts: ContentPart[] = [];
  let toolUseId: string | undefined;
  let toolInput: Record<string, unknown> = {};

  for (const block of m.content) {
    if (block.type === "text") {
      parts.push({ type: "text", text: block.text });
    } else if (block.type === "tool_use") {
      const input = (block.input ?? {}) as Record<string, unknown>;
      parts.push({
        type: "tool_call",
        call_id: block.id,
        name: block.name,
        input,
      });
      if (!toolUseId) {
        toolUseId = block.id;
        toolInput = input;
      }
    }
  }

  if (parts.length === 0) {
    throw new Error("assistant message had no usable content blocks");
  }
  return {
    toolInput,
    toolUseId,
    turn: {
      id: turnId,
      role: "assistant",
      created_at: FIXED_TS,
      content: parts,
    },
  };
}

main().catch((err) => {
  console.error("current-time:", err);
  process.exitCode = 1;
});
