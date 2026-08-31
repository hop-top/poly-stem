// web-search — second stem (TS) agent example.
//
// Same envelope-wiring shape as ../current-time/, but the tool is
// `web_search`, backed by the Tavily search API. The same xrr
// session captures both Anthropic and Tavily calls — each HTTP
// request has a distinct method+URL+body fingerprint so they
// co-exist in one cassette directory.
//
// Steps:
//
//   1. Construct a fresh Envelope by hand.
//   2. Append a user turn asking for the latest stable Go release.
//   3. Call Anthropic with a `web_search(query)` tool.
//   4. Convert the assistant's tool_use block into a stem ToolCallPart.
//   5. Execute the tool: POST to api.tavily.com via the xrr-wrapped fetch.
//   6. Append a tool-role turn carrying a ToolResultPart.
//   7. Call Anthropic again with the tool result echoed back.
//   8. Append the final assistant text.
//   9. Serialize to ./session.jsonl, reload, and validate.
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
const USER_PROMPT = "What's the latest stable Go release?";
const TOOL_NAME = "web_search";
const TAVILY_URL = "https://api.tavily.com/search";
const MAX_TAVILY_RESULTS = 3;
const JSONL_PATH = resolve(__dirname, "../session.jsonl");
const FIXED_TS = "2026-01-01T00:00:00Z";

interface TavilyResult {
  title: string;
  url: string;
  snippet: string;
}

async function main(): Promise<void> {
  // --- 1. xrr session.
  await mkdir(CASSETTE_DIR, { recursive: true });
  const xrr = newXrrSession(CASSETTE_DIR, modeFromEnv(process.env.XRR_MODE));
  const xrrFetch = makeXrrFetch(xrr);

  const apiKey = process.env.ANTHROPIC_API_KEY ?? "replay-mode-no-key-required";
  const tavilyKey = process.env.TAVILY_API_KEY ?? "replay-mode-no-key-required";

  const anthropic = new Anthropic({ apiKey, fetch: xrrFetch });

  // --- 2. fresh Envelope + initial user turn.
  const envelope: Envelope = {
    crtx_version: CrtxVersion,
    id: "web-search-demo",
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
    description: "Run a web search and return the top results.",
    input_schema: {
      type: "object",
      properties: {
        query: { type: "string", description: "Search query string." },
      },
      required: ["query"],
    },
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

  // --- 5. execute tool against Tavily through the xrr-wrapped fetch.
  const query = typeof toolInput.query === "string" ? toolInput.query : USER_PROMPT;
  const results = await callTavily(xrrFetch, tavilyKey, query);

  const toolPayload = { query, results };
  envelope.turns.push({
    id: "t-tool-2",
    role: "tool",
    created_at: FIXED_TS,
    content: [
      {
        type: "tool_result",
        call_id: toolUseId,
        output: toolPayload,
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
            content: JSON.stringify(toolPayload),
          },
        ],
      },
    ],
  });

  // --- 7. append final assistant text.
  const { turn: finalTurn } = assistantTurnFromMessage(second, "t-assistant-3");
  envelope.turns.push(finalTurn);

  // --- 8. write ./session.jsonl.
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

async function callTavily(
  xrrFetch: (input: RequestInfo | URL, init?: RequestInit) => Promise<Response>,
  apiKey: string,
  query: string
): Promise<TavilyResult[]> {
  const body = JSON.stringify({
    api_key: apiKey,
    query,
    max_results: MAX_TAVILY_RESULTS,
  });
  const resp = await xrrFetch(TAVILY_URL, {
    method: "POST",
    headers: { "content-type": "application/json" },
    body,
  });
  const text = await resp.text();
  if (resp.status >= 300) {
    throw new Error(`tavily: status ${resp.status}: ${text}`);
  }
  const raw = JSON.parse(text) as {
    results?: Array<{ title?: string; url?: string; content?: string }>;
  };
  const results: TavilyResult[] = [];
  for (const r of raw.results ?? []) {
    results.push({
      title: r.title ?? "",
      url: r.url ?? "",
      snippet: r.content ?? "",
    });
  }
  return results;
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
  console.error("web-search:", err);
  process.exitCode = 1;
});
