import { describe, expect, it } from "vitest";
import {
  EnvelopeParseError,
  parseEnvelope,
  serializeEnvelope,
} from "../src";
import type { Envelope } from "../src";

function baseEnv(overrides: Partial<Envelope> = {}): Envelope {
  return {
    crtx_version: "0.1",
    id: "01J",
    created_at: "2026-05-28T14:00:00Z",
    updated_at: "2026-05-28T14:00:01Z",
    source: { kind: "stem", version: "0.1.0" },
    turns: [
      {
        id: "t-0",
        role: "user",
        created_at: "2026-05-28T14:00:00Z",
        content: [{ type: "text", text: "hello" }],
      },
    ],
    ...overrides,
  };
}

describe("parseEnvelope", () => {
  it("accepts a minimal valid envelope (string)", () => {
    const env = parseEnvelope(JSON.stringify(baseEnv()));
    expect(env.id).toBe("01J");
    expect(env.turns).toHaveLength(1);
  });

  it("accepts a minimal valid envelope (Uint8Array)", () => {
    const bytes = new TextEncoder().encode(JSON.stringify(baseEnv()));
    const env = parseEnvelope(bytes);
    expect(env.id).toBe("01J");
  });

  it("rejects malformed JSON with EnvelopeParseError", () => {
    expect(() => parseEnvelope("{not json")).toThrow(EnvelopeParseError);
  });

  it("rejects unknown top-level fields", () => {
    const env: Record<string, unknown> = { ...baseEnv(), extraneous: 1 };
    expect(() => parseEnvelope(JSON.stringify(env))).toThrow(EnvelopeParseError);
  });

  it("rejects unknown text-part fields", () => {
    const env = baseEnv();
    env.turns[0].content = [
      { type: "text", text: "hi", unknown_field: 1 } as never,
    ];
    expect(() => parseEnvelope(JSON.stringify(env))).toThrow(EnvelopeParseError);
  });

  it("accepts extension parts with arbitrary fields", () => {
    const env = baseEnv();
    env.turns[0].content = [
      { type: "x-io.jadb.custom", foo: "bar", count: 3 } as never,
    ];
    const parsed = parseEnvelope(JSON.stringify(env));
    expect(parsed.turns[0].content[0]).toMatchObject({
      type: "x-io.jadb.custom",
      foo: "bar",
      count: 3,
    });
  });

  it("rejects non-object top-level JSON", () => {
    expect(() => parseEnvelope("[]")).toThrow(EnvelopeParseError);
    expect(() => parseEnvelope("42")).toThrow(EnvelopeParseError);
    expect(() => parseEnvelope("null")).toThrow(EnvelopeParseError);
    expect(() => parseEnvelope("\"hi\"")).toThrow(EnvelopeParseError);
  });

  // The boundary: parseEnvelope is decode-only. Structurally invalid
  // but decode-clean envelopes MUST parse successfully — structural
  // checks live in validate() / validateBytes(). Mirrors py / go / rs
  // / php reference behavior.

  it("accepts a structurally invalid but decode-clean envelope (parent_id without fork_point)", () => {
    const env = { ...baseEnv(), parent_id: "p-x" };
    expect(() => parseEnvelope(JSON.stringify(env))).not.toThrow();
  });

  it("accepts a structurally invalid but decode-clean envelope (dispatched_from + parent_id mutex)", () => {
    const env = {
      ...baseEnv(),
      parent_id: "p",
      fork_point: 0,
      dispatched_from: { envelope_id: "e", call_id: "c" },
    };
    expect(() => parseEnvelope(JSON.stringify(env))).not.toThrow();
  });

  it("accepts a tool_result with no preceding tool_call at parse time", () => {
    const env = baseEnv({
      turns: [
        {
          id: "t-t",
          role: "tool",
          created_at: "2026-05-28T14:00:00Z",
          content: [{ type: "tool_result", call_id: "orphan", output: "ok" }],
        },
      ],
    });
    // Structurally invalid (no preceding open tool_call) but
    // decode-clean — parseEnvelope MUST NOT throw.
    expect(() => parseEnvelope(JSON.stringify(env))).not.toThrow();
  });

  it("accepts an unknown role at parse time (structural concern)", () => {
    const env = baseEnv();
    (env.turns[0] as { role: string }).role = "bogus";
    expect(() => parseEnvelope(JSON.stringify(env))).not.toThrow();
  });

  it("accepts a malformed extension type at parse time (structural concern)", () => {
    const env = baseEnv();
    env.turns[0].content = [{ type: "x-bad space", foo: 1 } as never];
    // Extension fields are decode-allowed; the regex check is
    // structural and surfaces via validate() only.
    expect(() => parseEnvelope(JSON.stringify(env))).not.toThrow();
  });

  it("accepts in_reply_to_call_id with no open call at parse time", () => {
    const env = baseEnv();
    env.turns[0].in_reply_to_call_id = "missing";
    expect(() => parseEnvelope(JSON.stringify(env))).not.toThrow();
  });

  it("rejects unknown dispatched_from fields", () => {
    const env = {
      ...baseEnv(),
      dispatched_from: { envelope_id: "p", call_id: "c", spy: 1 },
    };
    expect(() => parseEnvelope(JSON.stringify(env))).toThrow(EnvelopeParseError);
  });

  it("rejects unknown injected_turns fields", () => {
    const env = {
      ...baseEnv(),
      injected_turns: [
        { envelope_id: "src", start_turn_id: "a", end_turn_id: "b", spy: 1 },
      ],
    };
    expect(() => parseEnvelope(JSON.stringify(env))).toThrow(EnvelopeParseError);
  });

  it("rejects unknown source fields", () => {
    const env = baseEnv();
    (env.source as { spy?: boolean }).spy = true;
    expect(() => parseEnvelope(JSON.stringify(env))).toThrow(EnvelopeParseError);
  });

  it("rejects unknown turn fields", () => {
    const env = baseEnv();
    (env.turns[0] as { surprise?: unknown }).surprise = 1;
    expect(() => parseEnvelope(JSON.stringify(env))).toThrow(EnvelopeParseError);
  });
});

describe("serializeEnvelope", () => {
  it("emits empty turns as [] (never null)", () => {
    const env = baseEnv({ turns: [] });
    const json = serializeEnvelope(env);
    expect(JSON.parse(json).turns).toEqual([]);
  });

  it("rejects image parts with both data and url", () => {
    const env = baseEnv();
    env.turns[0].content = [
      {
        type: "image",
        mime: "image/png",
        data: "AAA=",
        url: "https://example.com/x.png",
      },
    ];
    expect(() => serializeEnvelope(env)).toThrow(/exactly one of data\/url/);
  });

  it("throws on undefined tool_result.output (required by spec)", () => {
    const env = baseEnv({
      turns: [
        {
          id: "t-a",
          role: "assistant",
          created_at: "2026-05-28T14:00:00Z",
          content: [
            { type: "tool_call", call_id: "c1", name: "n", input: {} },
          ],
        },
        {
          id: "t-t",
          role: "tool",
          created_at: "2026-05-28T14:00:01Z",
          content: [
            { type: "tool_result", call_id: "c1", output: undefined as unknown as null },
          ],
        },
      ],
    });
    expect(() => serializeEnvelope(env)).toThrow(/output is required/);
  });

  it("rejects image parts with neither data nor url", () => {
    const env = baseEnv();
    env.turns[0].content = [{ type: "image", mime: "image/png" }];
    expect(() => serializeEnvelope(env)).toThrow(/exactly one of data\/url/);
  });

  it("preserves extension parts verbatim through round-trip", () => {
    const env = baseEnv();
    env.turns[0].content = [
      { type: "x-io.jadb.custom", foo: "bar", nested: { n: 1 } } as never,
    ];
    const serialized = serializeEnvelope(env);
    const reparsed = parseEnvelope(serialized);
    expect(reparsed.turns[0].content[0]).toEqual({
      type: "x-io.jadb.custom",
      foo: "bar",
      nested: { n: 1 },
    });
  });

  it("omits is_error when false", () => {
    const env = baseEnv({
      turns: [
        {
          id: "t-a",
          role: "assistant",
          created_at: "2026-05-28T14:00:00Z",
          content: [
            { type: "tool_call", call_id: "c1", name: "n", input: {} },
          ],
        },
        {
          id: "t-t",
          role: "tool",
          created_at: "2026-05-28T14:00:01Z",
          content: [
            { type: "tool_result", call_id: "c1", output: "ok", is_error: false },
          ],
        },
      ],
    });
    const json = serializeEnvelope(env);
    const back = JSON.parse(json);
    expect(back.turns[1].content[0]).not.toHaveProperty("is_error");
  });

  it("retains is_error when true", () => {
    const env = baseEnv({
      turns: [
        {
          id: "t-a",
          role: "assistant",
          created_at: "2026-05-28T14:00:00Z",
          content: [
            { type: "tool_call", call_id: "c1", name: "n", input: {} },
          ],
        },
        {
          id: "t-t",
          role: "tool",
          created_at: "2026-05-28T14:00:01Z",
          content: [
            { type: "tool_result", call_id: "c1", output: "fail", is_error: true },
          ],
        },
      ],
    });
    const back = JSON.parse(serializeEnvelope(env));
    expect(back.turns[1].content[0].is_error).toBe(true);
  });

  it("omits optional source.instance when absent", () => {
    const env = baseEnv();
    const back = JSON.parse(serializeEnvelope(env));
    expect(back.source).toEqual({ kind: "stem", version: "0.1.0" });
  });
});
