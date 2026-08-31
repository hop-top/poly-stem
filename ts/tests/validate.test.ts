import { describe, expect, it } from "vitest";
import { validate, validateBytes } from "../src";
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

describe("validate (structural)", () => {
  it("accepts a minimal envelope", () => {
    expect(validate(baseEnv())).toEqual([]);
  });

  it("rejects missing crtx_version", () => {
    const env = baseEnv();
    (env as Partial<Envelope>).crtx_version = undefined as unknown as string;
    const errs = validate(env);
    expect(errs.some((e) => e.path === "crtx_version")).toBe(true);
  });

  it("rejects an unsupported crtx_version", () => {
    const env = baseEnv({ crtx_version: "1.0" });
    expect(validate(env).some((e) => e.path === "crtx_version")).toBe(true);
  });

  it("rejects unknown roles", () => {
    const env = baseEnv();
    env.turns[0].role = "bogus" as never;
    expect(validate(env).some((e) => e.path === "turns[0].role")).toBe(true);
  });

  it("rejects missing content[]", () => {
    const env = baseEnv();
    env.turns[0].content = [];
    expect(validate(env).some((e) => e.path === "turns[0].content")).toBe(true);
  });

  it("rejects image with both data and url", () => {
    const env = baseEnv();
    env.turns[0].content = [
      {
        type: "image",
        mime: "image/png",
        data: "AAA=",
        url: "https://example.com/x.png",
      },
    ];
    expect(validate(env).some((e) => /data\/url/.test(e.message))).toBe(true);
  });

  it("rejects image with neither data nor url", () => {
    const env = baseEnv();
    env.turns[0].content = [{ type: "image", mime: "image/png" }];
    expect(validate(env).some((e) => /data\/url/.test(e.message))).toBe(true);
  });

  it("rejects tool_result without a preceding tool_call", () => {
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
    expect(
      validate(env).some((e) => /no preceding open tool_call/.test(e.message))
    ).toBe(true);
  });

  it("accepts tool_result that follows its tool_call", () => {
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
            { type: "tool_result", call_id: "c1", output: { ok: true } },
          ],
        },
      ],
    });
    expect(validate(env)).toEqual([]);
  });

  it("rejects parent_id without fork_point", () => {
    const env = baseEnv({ parent_id: "parent-x" });
    expect(validate(env).some((e) => /parent_id and fork_point/.test(e.message))).toBe(true);
  });

  it("rejects fork_point without parent_id", () => {
    const env = baseEnv({ fork_point: 0 });
    expect(validate(env).some((e) => /parent_id and fork_point/.test(e.message))).toBe(true);
  });

  it("accepts parent_id + fork_point together", () => {
    const env = baseEnv({ parent_id: "parent-x", fork_point: 2 });
    expect(validate(env)).toEqual([]);
  });

  it("rejects malformed extension types", () => {
    const env = baseEnv();
    env.turns[0].content = [{ type: "x-bad space", foo: 1 } as never];
    expect(validate(env).some((e) => /does not match/.test(e.message))).toBe(true);
  });

  it("accepts empty thinking.text — schema permits empty string", () => {
    const env = baseEnv();
    env.turns[0].content = [{ type: "thinking", text: "" }];
    expect(validate(env)).toEqual([]);
  });

  it("rejects image with non-conformant mime per crtx schema regex", () => {
    const env = baseEnv();
    env.turns[0].content = [
      { type: "image", mime: "NOT_A_MIME", data: "AAA=" },
    ];
    const errs = validate(env);
    expect(
      errs.some((e) => e.path === "turns[0].content[0].mime")
    ).toBe(true);
  });

  it("accepts empty tool_call.input ({}) — tools may take no args", () => {
    const env = baseEnv({
      turns: [
        {
          id: "t-a",
          role: "assistant",
          created_at: "2026-05-28T14:00:00Z",
          content: [
            { type: "tool_call", call_id: "c1", name: "current_time", input: {} },
          ],
        },
        {
          id: "t-t",
          role: "tool",
          created_at: "2026-05-28T14:00:01Z",
          content: [
            { type: "tool_result", call_id: "c1", output: { now: "2026-05-28T14:00:01Z" } },
          ],
        },
      ],
    });
    expect(validate(env)).toEqual([]);
  });

  it("accepts a well-formed extension type", () => {
    const env = baseEnv();
    env.turns[0].content = [
      { type: "x-io.jadb.custom", foo: 1 } as never,
    ];
    expect(validate(env)).toEqual([]);
  });
});

describe("validateBytes (strict decode)", () => {
  it("accepts a clean envelope JSON", () => {
    expect(validateBytes(JSON.stringify({
      crtx_version: "0.1",
      id: "01J",
      created_at: "2026-05-28T14:00:00Z",
      updated_at: "2026-05-28T14:00:01Z",
      source: { kind: "stem", version: "0.1.0" },
      turns: [],
    }))).toEqual([]);
  });

  it("rejects unknown top-level fields", () => {
    const json = JSON.stringify({
      crtx_version: "0.1",
      id: "01J",
      created_at: "2026-05-28T14:00:00Z",
      updated_at: "2026-05-28T14:00:01Z",
      source: { kind: "stem", version: "0.1.0" },
      turns: [],
      surprise: "extra",
    });
    const errs = validateBytes(json);
    expect(errs.some((e) => /unknown envelope field/.test(e.message))).toBe(true);
  });

  it("rejects unknown source fields", () => {
    const json = JSON.stringify({
      crtx_version: "0.1",
      id: "01J",
      created_at: "2026-05-28T14:00:00Z",
      updated_at: "2026-05-28T14:00:01Z",
      source: { kind: "stem", version: "0.1.0", spy: true },
      turns: [],
    });
    expect(
      validateBytes(json).some((e) => /unknown source field/.test(e.message))
    ).toBe(true);
  });

  it("rejects unknown turn fields", () => {
    const json = JSON.stringify({
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
          content: [{ type: "text", text: "hi" }],
          surprise: 1,
        },
      ],
    });
    expect(
      validateBytes(json).some((e) => /unknown turn field/.test(e.message))
    ).toBe(true);
  });

  it("rejects unknown text-part fields", () => {
    const json = JSON.stringify({
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
          content: [{ type: "text", text: "hi", surprise: 1 }],
        },
      ],
    });
    expect(
      validateBytes(json).some((e) => /unknown text part field/.test(e.message))
    ).toBe(true);
  });

  it("accepts extension parts with arbitrary fields", () => {
    const json = JSON.stringify({
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
          content: [{ type: "x-io.jadb.custom", whatever: 7 }],
        },
      ],
    });
    expect(validateBytes(json)).toEqual([]);
  });
});

// --- Multi-agent provenance (envelope.md §3.1, §3.2, §6.2, §6.3, §7.2, §7.3) ---

describe("validate (multi-agent provenance)", () => {
  it("rejects dispatched_from with parent_id/fork_point", () => {
    const env = baseEnv({
      parent_id: "parent",
      fork_point: 0,
      dispatched_from: { envelope_id: "p", call_id: "c" },
    });
    expect(
      validate(env).some((e) => /mutually exclusive/.test(e.message))
    ).toBe(true);
  });

  it("rejects dispatched_from missing call_id", () => {
    const env = baseEnv({
      dispatched_from: { envelope_id: "p" } as never,
    });
    expect(
      validate(env).some((e) => /envelope_id and call_id/.test(e.message))
    ).toBe(true);
  });

  it("rejects dispatched_from missing envelope_id", () => {
    const env = baseEnv({
      dispatched_from: { call_id: "c" } as never,
    });
    expect(
      validate(env).some((e) => /envelope_id and call_id/.test(e.message))
    ).toBe(true);
  });

  it("accepts dispatched_from on its own", () => {
    const env = baseEnv({
      dispatched_from: { envelope_id: "parent-env", call_id: "call-1" },
    });
    expect(validate(env)).toEqual([]);
  });

  it("rejects tool_call.parent_call_id with no open outer call", () => {
    const env = baseEnv({
      turns: [
        {
          id: "a",
          role: "assistant",
          created_at: "2026-05-28T14:00:00Z",
          content: [
            {
              type: "tool_call",
              call_id: "inner",
              name: "sub",
              input: {},
              parent_call_id: "outer",
            },
          ],
        },
      ],
    });
    expect(
      validate(env).some((e) => /parent_call_id/.test(e.message))
    ).toBe(true);
  });

  it("accepts tool_call.parent_call_id while outer call is open", () => {
    const env = baseEnv({
      turns: [
        {
          id: "a",
          role: "assistant",
          created_at: "2026-05-28T14:00:00Z",
          content: [
            { type: "tool_call", call_id: "outer", name: "dispatch", input: {} },
          ],
        },
        {
          id: "b",
          role: "assistant",
          created_at: "2026-05-28T14:00:01Z",
          content: [
            {
              type: "tool_call",
              call_id: "inner",
              name: "sub",
              input: {},
              parent_call_id: "outer",
            },
          ],
        },
      ],
    });
    expect(validate(env)).toEqual([]);
  });

  it("rejects tool_call.parent_call_id after outer call resolved", () => {
    const env = baseEnv({
      turns: [
        {
          id: "a",
          role: "assistant",
          created_at: "2026-05-28T14:00:00Z",
          content: [
            { type: "tool_call", call_id: "outer", name: "dispatch", input: {} },
          ],
        },
        {
          id: "t",
          role: "tool",
          created_at: "2026-05-28T14:00:01Z",
          content: [{ type: "tool_result", call_id: "outer", output: "done" }],
        },
        {
          id: "b",
          role: "assistant",
          created_at: "2026-05-28T14:00:02Z",
          content: [
            {
              type: "tool_call",
              call_id: "inner",
              name: "sub",
              input: {},
              parent_call_id: "outer",
            },
          ],
        },
      ],
    });
    expect(
      validate(env).some((e) => /parent_call_id/.test(e.message))
    ).toBe(true);
  });

  it("rejects Turn.in_reply_to_call_id with no open call", () => {
    const env = baseEnv({
      turns: [
        {
          id: "u",
          role: "user",
          created_at: "2026-05-28T14:00:00Z",
          in_reply_to_call_id: "missing",
          content: [{ type: "text", text: "interjection" }],
        },
      ],
    });
    expect(
      validate(env).some((e) => /in_reply_to_call_id/.test(e.message))
    ).toBe(true);
  });

  it("rejects Turn.in_reply_to_call_id on tool-role turns", () => {
    const env = baseEnv({
      turns: [
        {
          id: "a",
          role: "assistant",
          created_at: "2026-05-28T14:00:00Z",
          content: [{ type: "tool_call", call_id: "c-1", name: "x", input: {} }],
        },
        {
          id: "t",
          role: "tool",
          created_at: "2026-05-28T14:00:01Z",
          in_reply_to_call_id: "c-1",
          content: [{ type: "tool_result", call_id: "c-1", output: "out" }],
        },
      ],
    });
    expect(
      validate(env).some((e) => /forbidden on tool-role/.test(e.message))
    ).toBe(true);
  });

  it("accepts Turn.in_reply_to_call_id while the call is open", () => {
    const env = baseEnv({
      turns: [
        {
          id: "a",
          role: "assistant",
          created_at: "2026-05-28T14:00:00Z",
          content: [
            { type: "tool_call", call_id: "c-1", name: "dispatch", input: {} },
          ],
        },
        {
          id: "u",
          role: "user",
          created_at: "2026-05-28T14:00:01Z",
          in_reply_to_call_id: "c-1",
          content: [{ type: "text", text: "update" }],
        },
        {
          id: "t",
          role: "tool",
          created_at: "2026-05-28T14:00:02Z",
          content: [{ type: "tool_result", call_id: "c-1", output: "out" }],
        },
      ],
    });
    expect(validate(env)).toEqual([]);
  });

  it("accepts agent_id on assistant turns", () => {
    const env = baseEnv({
      turns: [
        {
          id: "a",
          role: "assistant",
          created_at: "2026-05-28T14:00:00Z",
          agent_id: "C",
          content: [{ type: "text", text: "hi" }],
        },
      ],
    });
    expect(validate(env)).toEqual([]);
  });

  it("accepts tool_result.child_envelope_id", () => {
    const env = baseEnv({
      turns: [
        {
          id: "a",
          role: "assistant",
          created_at: "2026-05-28T14:00:00Z",
          content: [
            { type: "tool_call", call_id: "c-1", name: "dispatch", input: {} },
          ],
        },
        {
          id: "t",
          role: "tool",
          created_at: "2026-05-28T14:00:01Z",
          content: [
            {
              type: "tool_result",
              call_id: "c-1",
              output: "done",
              child_envelope_id: "child-env",
            },
          ],
        },
      ],
    });
    expect(validate(env)).toEqual([]);
  });

  it("rejects injected_turns missing start/end", () => {
    const env = baseEnv({
      injected_turns: [{ envelope_id: "src" } as never],
    });
    expect(
      validate(env).some((e) => /injected_turns/.test(e.message))
    ).toBe(true);
  });

  it("rejects injected_after_turn_id not present in this envelope", () => {
    const env = baseEnv({
      injected_turns: [
        {
          envelope_id: "src",
          start_turn_id: "s",
          end_turn_id: "s",
          injected_after_turn_id: "does-not-exist",
        },
      ],
    });
    expect(
      validate(env).some((e) => /injected_after_turn_id/.test(e.message))
    ).toBe(true);
  });

  it("accepts creation-time injected_turns entries", () => {
    const env = baseEnv({
      injected_turns: [
        { envelope_id: "src", start_turn_id: "a", end_turn_id: "b" },
      ],
    });
    expect(validate(env)).toEqual([]);
  });

  it("accepts injected_after_turn_id when target turn exists locally", () => {
    const env = baseEnv({
      turns: [
        {
          id: "t-anchor",
          role: "user",
          created_at: "2026-05-28T14:00:00Z",
          content: [{ type: "text", text: "anchor" }],
        },
      ],
      injected_turns: [
        {
          envelope_id: "src",
          start_turn_id: "s",
          end_turn_id: "s",
          injected_after_turn_id: "t-anchor",
        },
      ],
    });
    expect(validate(env)).toEqual([]);
  });
});

// --- Strict-decode coverage for new fields ---

describe("validateBytes (parse-vs-validate boundary)", () => {
  // Structural errors surface as EnvelopeError entries, NOT throws —
  // parseEnvelope decodes successfully and validate() catches the
  // structural issues. This is the boundary parseEnvelope upholds:
  // decode-only at parse, structural via validate.

  it("returns structural errors via the list, not via throw (parent_id without fork_point)", () => {
    const json = JSON.stringify({
      ...baseEnv(),
      parent_id: "p-x",
    });
    const errs = validateBytes(json);
    expect(errs.some((e) => /parent_id and fork_point/.test(e.message))).toBe(true);
  });

  it("returns structural errors via the list, not via throw (orphan tool_result)", () => {
    const json = JSON.stringify(baseEnv({
      turns: [
        {
          id: "t-t",
          role: "tool",
          created_at: "2026-05-28T14:00:00Z",
          content: [{ type: "tool_result", call_id: "orphan", output: "ok" }],
        },
      ],
    }));
    const errs = validateBytes(json);
    expect(errs.some((e) => /no preceding open tool_call/.test(e.message))).toBe(true);
  });

  it("returns structural errors via the list, not via throw (dispatched_from + fork mutex)", () => {
    const json = JSON.stringify({
      ...baseEnv(),
      parent_id: "p",
      fork_point: 0,
      dispatched_from: { envelope_id: "e", call_id: "c" },
    });
    const errs = validateBytes(json);
    expect(errs.some((e) => /mutually exclusive/.test(e.message))).toBe(true);
  });
});

describe("validateBytes (multi-agent provenance strict decode)", () => {
  it("rejects unknown dispatched_from fields", () => {
    const json = JSON.stringify({
      crtx_version: "0.1",
      id: "01J",
      created_at: "2026-05-28T14:00:00Z",
      updated_at: "2026-05-28T14:00:01Z",
      source: { kind: "stem", version: "0.1.0" },
      dispatched_from: { envelope_id: "p", call_id: "c", spy: true },
      turns: [],
    });
    expect(
      validateBytes(json).some((e) => /unknown dispatched_from field/.test(e.message))
    ).toBe(true);
  });

  it("rejects unknown injected_turns fields", () => {
    const json = JSON.stringify({
      crtx_version: "0.1",
      id: "01J",
      created_at: "2026-05-28T14:00:00Z",
      updated_at: "2026-05-28T14:00:01Z",
      source: { kind: "stem", version: "0.1.0" },
      injected_turns: [
        {
          envelope_id: "src",
          start_turn_id: "a",
          end_turn_id: "b",
          spy: 1,
        },
      ],
      turns: [],
    });
    expect(
      validateBytes(json).some((e) => /unknown injected_turns field/.test(e.message))
    ).toBe(true);
  });

  it("accepts an envelope with dispatched_from + injected_turns clean", () => {
    const json = JSON.stringify({
      crtx_version: "0.1",
      id: "01J",
      created_at: "2026-05-28T14:00:00Z",
      updated_at: "2026-05-28T14:00:01Z",
      source: { kind: "stem", version: "0.1.0" },
      dispatched_from: { envelope_id: "parent-env", call_id: "c-1" },
      injected_turns: [
        { envelope_id: "src", start_turn_id: "a", end_turn_id: "b" },
      ],
      turns: [],
    });
    expect(validateBytes(json)).toEqual([]);
  });
});
