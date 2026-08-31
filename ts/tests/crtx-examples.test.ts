import { readFileSync } from "node:fs";
import { join } from "node:path";
import { describe, expect, it } from "vitest";
import { parseEnvelope, serializeEnvelope, validate, validateBytes } from "../src";

const EXAMPLES_DIR = join(__dirname, "..", "testdata", "crtx_v0.1");
const EXAMPLES = [
  "minimal.json",
  "tool-call.json",
  "fork.json",
  "dispatch.json",
  "dispatch-nested.json",
  "injection.json",
] as const;

describe("crtx v0.1 example envelopes", () => {
  for (const name of EXAMPLES) {
    describe(name, () => {
      const raw = readFileSync(join(EXAMPLES_DIR, name), "utf8");
      const originalParsed = JSON.parse(raw);

      it("validates with validateBytes (strict)", () => {
        expect(validateBytes(raw)).toEqual([]);
      });

      it("parses without errors", () => {
        const env = parseEnvelope(raw);
        expect(env.crtx_version).toBe("0.1");
      });

      it("round-trips parse → serialize → reparse to deep-equal", () => {
        const env = parseEnvelope(raw);
        const re = serializeEnvelope(env);
        const env2 = parseEnvelope(re);
        expect(JSON.parse(serializeEnvelope(env2))).toEqual(
          JSON.parse(serializeEnvelope(env))
        );
      });

      it("re-serialized canonical form deep-equals the original input", () => {
        const env = parseEnvelope(raw);
        const re = JSON.parse(serializeEnvelope(env));
        expect(re).toEqual(originalParsed);
      });

      it("validate() on the parsed envelope is empty", () => {
        const env = parseEnvelope(raw);
        expect(validate(env)).toEqual([]);
      });
    });
  }
});
