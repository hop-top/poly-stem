import { describe, expect, it } from "vitest";
import {
  CrtxVersion,
  PartType,
  SourceKind,
  Version,
  isExtension,
  isImage,
  isKnownPartType,
  isText,
  isThinking,
  isToolCall,
  isToolResult,
  isValidExtensionType,
} from "../src";
import type { ContentPart } from "../src";

describe("version constants", () => {
  it("emits the crtx v0.1 spec version literal", () => {
    expect(CrtxVersion).toBe("0.1");
  });
  it("identifies stem as the producing runtime", () => {
    expect(SourceKind).toBe("stem");
  });
  it("ships a stem semver", () => {
    expect(Version).toMatch(/^\d+\.\d+\.\d+/);
  });
});

describe("PartType discriminator values", () => {
  it("covers the crtx v0.1 enum", () => {
    expect(Object.values(PartType).sort()).toEqual(
      ["image", "text", "thinking", "tool_call", "tool_result"].sort()
    );
  });
});

describe("type guards", () => {
  const cases: Array<{ part: ContentPart; guard: (p: ContentPart) => boolean; name: string }> = [
    { name: "text", guard: isText, part: { type: "text", text: "hi" } },
    {
      name: "tool_call",
      guard: isToolCall,
      part: { type: "tool_call", call_id: "c1", name: "n", input: {} },
    },
    {
      name: "tool_result",
      guard: isToolResult,
      part: { type: "tool_result", call_id: "c1", output: "ok" },
    },
    {
      name: "image (data)",
      guard: isImage,
      part: { type: "image", mime: "image/png", data: "AAA=" },
    },
    {
      name: "thinking",
      guard: isThinking,
      part: { type: "thinking", text: "reasoning" },
    },
    {
      name: "extension",
      guard: isExtension,
      part: { type: "x-io.jadb.custom", foo: "bar" },
    },
  ];

  for (const { name, guard, part } of cases) {
    it(`recognises ${name}`, () => {
      expect(guard(part)).toBe(true);
    });
  }

  it("isKnownPartType accepts each built-in", () => {
    for (const v of Object.values(PartType)) {
      expect(isKnownPartType(v)).toBe(true);
    }
  });

  it("isKnownPartType rejects extension types", () => {
    expect(isKnownPartType("x-io.jadb.custom")).toBe(false);
  });

  it("isValidExtensionType enforces the regex", () => {
    expect(isValidExtensionType("x-io.jadb.custom")).toBe(true);
    expect(isValidExtensionType("x-a")).toBe(true);
    expect(isValidExtensionType("x-")).toBe(false);
    expect(isValidExtensionType("xfoo")).toBe(false);
    expect(isValidExtensionType("x-bad space")).toBe(false);
    expect(isValidExtensionType("x-bad/slash")).toBe(false);
  });
});
