/**
 * Structural validation for crtx v0.1 envelopes.
 *
 * Hand-rolled: no JSON-Schema runtime dependency. Mirrors the Go
 * reference (go/validate.go) — required fields, role enum,
 * ContentPart discriminator, parent_id/fork_point linkage,
 * tool_result→tool_call linkage, image data/url XOR, extension type
 * regex. The authoritative spec is envelope.schema.json; for full
 * schema conformance use an external JSON Schema validator.
 */

import {
  PartType,
  isExtension,
  isKnownPartType,
  isValidExtensionType,
} from "./content";
import { EnvelopeParseError } from "./errors";
import type { EnvelopeError } from "./errors";
import { parseEnvelope } from "./parse";
import type { ContentPart, Envelope, Role } from "./types";
import { CrtxVersion } from "./version";

const ROLES = new Set<string>(["user", "assistant", "tool", "system", "developer"]);

const RFC3339_RE =
  /^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(\.\d+)?(Z|[+-]\d{2}:\d{2})$/;

// crtx v0.1 image.mime regex — schema-required (spec §9 "schema wins").
const MIME_RE = /^[a-z]+\/[a-zA-Z0-9.+-]+$/;

/**
 * Validate checks an Envelope against crtx v0.1 structural rules.
 * Returns an array of EnvelopeError; empty array means valid.
 *
 * Strict-decode (rejecting unknown top-level fields) is handled by
 * validateBytes / parseEnvelope, NOT here — Validate accepts the
 * structural shape of an already-parsed Envelope object.
 */
export function validate(env: Envelope): EnvelopeError[] {
  const errs: EnvelopeError[] = [];

  if (env === null || env === undefined) {
    errs.push({ path: "", message: "envelope is nil" });
    return errs;
  }
  if (typeof env !== "object") {
    errs.push({ path: "", message: "envelope must be an object" });
    return errs;
  }

  if (typeof env.crtx_version !== "string" || env.crtx_version.length === 0) {
    errs.push({ path: "crtx_version", message: "missing crtx_version" });
  } else if (!crtxVersionCompatible(env.crtx_version)) {
    errs.push({
      path: "crtx_version",
      message: `crtx_version "${env.crtx_version}" unsupported (this stem speaks "${CrtxVersion}")`,
    });
  }

  if (typeof env.id !== "string" || env.id.length === 0) {
    errs.push({ path: "id", message: "missing id" });
  }
  if (!isRfc3339(env.created_at)) {
    errs.push({ path: "created_at", message: "missing or invalid created_at" });
  }
  if (!isRfc3339(env.updated_at)) {
    errs.push({ path: "updated_at", message: "missing or invalid updated_at" });
  }

  // Source.
  if (!env.source || typeof env.source !== "object") {
    errs.push({ path: "source", message: "missing source" });
  } else {
    if (typeof env.source.kind !== "string" || env.source.kind.length === 0) {
      errs.push({ path: "source.kind", message: "source.kind required" });
    }
    if (typeof env.source.version !== "string" || env.source.version.length === 0) {
      errs.push({ path: "source.version", message: "source.version required" });
    }
  }

  // parent_id ↔ fork_point mutual dependency.
  const hasParent = env.parent_id !== undefined && env.parent_id !== "";
  const hasFork = env.fork_point !== undefined && env.fork_point !== null;
  if (hasParent !== hasFork) {
    errs.push({
      path: "parent_id",
      message: "parent_id and fork_point must both be set or both omitted",
    });
  }
  if (hasFork) {
    if (typeof env.fork_point !== "number" || !Number.isInteger(env.fork_point)) {
      errs.push({ path: "fork_point", message: "fork_point must be an integer" });
    } else if (env.fork_point < 0) {
      errs.push({ path: "fork_point", message: "fork_point must be >= 0" });
    }
  }

  // dispatched_from (envelope.md §7.2): both members required; mutually
  // exclusive with parent_id / fork_point.
  if (env.dispatched_from !== undefined && env.dispatched_from !== null) {
    if (hasParent || hasFork) {
      errs.push({
        path: "dispatched_from",
        message:
          "dispatched_from is mutually exclusive with parent_id/fork_point",
      });
    }
    const df = env.dispatched_from;
    if (typeof df !== "object" || Array.isArray(df)) {
      errs.push({
        path: "dispatched_from",
        message: "dispatched_from must be an object",
      });
    } else {
      if (typeof df.envelope_id !== "string" || df.envelope_id.length === 0) {
        errs.push({
          path: "dispatched_from.envelope_id",
          message: "dispatched_from requires both envelope_id and call_id",
        });
      }
      if (typeof df.call_id !== "string" || df.call_id.length === 0) {
        errs.push({
          path: "dispatched_from.call_id",
          message: "dispatched_from requires both envelope_id and call_id",
        });
      }
    }
  }

  if (!Array.isArray(env.turns)) {
    errs.push({ path: "turns", message: "turns must be an array" });
    return errs;
  }

  // injected_turns (envelope.md §7.3): require the (envelope_id,
  // start_turn_id, end_turn_id) tuple; when injected_after_turn_id is
  // set, it MUST already exist in this envelope's turns[] (forward
  // references forbidden).
  if (env.injected_turns !== undefined && env.injected_turns !== null) {
    if (!Array.isArray(env.injected_turns)) {
      errs.push({
        path: "injected_turns",
        message: "injected_turns must be an array",
      });
    } else {
      const localTurnIDs = new Set<string>();
      for (const t of env.turns) {
        if (t && typeof t === "object" && typeof (t as { id?: unknown }).id === "string") {
          localTurnIDs.add((t as { id: string }).id);
        }
      }
      for (let i = 0; i < env.injected_turns.length; i++) {
        const ref = env.injected_turns[i] as unknown;
        const base = `injected_turns[${i}]`;
        if (!ref || typeof ref !== "object" || Array.isArray(ref)) {
          errs.push({ path: base, message: "injected_turns entry must be an object" });
          continue;
        }
        const r = ref as Record<string, unknown>;
        if (typeof r.envelope_id !== "string" || r.envelope_id.length === 0) {
          errs.push({
            path: `${base}.envelope_id`,
            message:
              "injected_turns: envelope_id / start_turn_id / end_turn_id required",
          });
        }
        if (typeof r.start_turn_id !== "string" || r.start_turn_id.length === 0) {
          errs.push({
            path: `${base}.start_turn_id`,
            message:
              "injected_turns: envelope_id / start_turn_id / end_turn_id required",
          });
        }
        if (typeof r.end_turn_id !== "string" || r.end_turn_id.length === 0) {
          errs.push({
            path: `${base}.end_turn_id`,
            message:
              "injected_turns: envelope_id / start_turn_id / end_turn_id required",
          });
        }
        if (r.injected_after_turn_id !== undefined) {
          if (typeof r.injected_after_turn_id !== "string") {
            errs.push({
              path: `${base}.injected_after_turn_id`,
              message: "injected_after_turn_id must be a string",
            });
          } else if (r.injected_after_turn_id.length > 0 &&
                     !localTurnIDs.has(r.injected_after_turn_id)) {
            errs.push({
              path: `${base}.injected_after_turn_id`,
              message: `injected_after_turn_id "${r.injected_after_turn_id}" not present in this envelope`,
            });
          }
        }
      }
    }
  }

  // Track open tool_calls. A call goes open on tool_call and closes on
  // its matching tool_result. Used by:
  //   - tool_result.call_id linkage
  //   - tool_call.parent_call_id (MUST point at an open outer call)
  //   - Turn.in_reply_to_call_id (MUST point at an open call)
  const openCalls = new Set<string>();
  for (let i = 0; i < env.turns.length; i++) {
    validateTurn(env.turns[i], `turns[${i}]`, openCalls, errs);
  }

  return errs;
}

function crtxVersionCompatible(v: string): boolean {
  if (v === CrtxVersion) return true;
  // 0.x: every minor is breaking; accept exact only. Keep the check
  // explicit so future stem versions have an obvious knob.
  const [wantMajor] = CrtxVersion.split(".");
  const [gotMajor] = v.split(".");
  return wantMajor === gotMajor && wantMajor === "0" && v === CrtxVersion;
}

function isRfc3339(s: unknown): boolean {
  return typeof s === "string" && RFC3339_RE.test(s);
}

function validRole(r: unknown): r is Role {
  return typeof r === "string" && ROLES.has(r);
}

function validateTurn(
  t: unknown,
  base: string,
  openCalls: Set<string>,
  errs: EnvelopeError[]
): void {
  if (!t || typeof t !== "object") {
    errs.push({ path: base, message: "turn must be an object" });
    return;
  }
  const turn = t as Record<string, unknown>;
  if (typeof turn.id !== "string" || turn.id.length === 0) {
    errs.push({ path: `${base}.id`, message: "missing id" });
  }
  if (!validRole(turn.role)) {
    errs.push({
      path: `${base}.role`,
      message: `unknown role "${String(turn.role)}"`,
    });
  }
  if (!isRfc3339(turn.created_at)) {
    errs.push({
      path: `${base}.created_at`,
      message: "missing or invalid created_at",
    });
  }

  // in_reply_to_call_id (envelope.md §3.2): MUST reference an open
  // tool_call at the point this Turn is appended. Forbidden on
  // tool-role Turns — tool_result IS the resolution, not an
  // interjection.
  if (turn.in_reply_to_call_id !== undefined) {
    if (typeof turn.in_reply_to_call_id !== "string") {
      errs.push({
        path: `${base}.in_reply_to_call_id`,
        message: "in_reply_to_call_id must be a string",
      });
    } else if (turn.in_reply_to_call_id.length > 0) {
      if (turn.role === "tool") {
        errs.push({
          path: `${base}.in_reply_to_call_id`,
          message: "in_reply_to_call_id: forbidden on tool-role turns",
        });
      } else if (!openCalls.has(turn.in_reply_to_call_id)) {
        errs.push({
          path: `${base}.in_reply_to_call_id`,
          message: `in_reply_to_call_id "${turn.in_reply_to_call_id}" has no open tool_call`,
        });
      }
    }
  }

  if (!Array.isArray(turn.content) || turn.content.length === 0) {
    errs.push({
      path: `${base}.content`,
      message: "content[] must have >=1 part",
    });
    return;
  }
  for (let j = 0; j < turn.content.length; j++) {
    validateContentPart(
      turn.content[j] as ContentPart,
      `${base}.content[${j}]`,
      openCalls,
      errs
    );
  }
}

function validateContentPart(
  p: unknown,
  base: string,
  openCalls: Set<string>,
  errs: EnvelopeError[]
): void {
  if (!p || typeof p !== "object") {
    errs.push({ path: base, message: "content part must be an object" });
    return;
  }
  const part = p as Record<string, unknown>;
  if (typeof part.type !== "string" || part.type.length === 0) {
    errs.push({ path: `${base}.type`, message: "missing type" });
    return;
  }

  if (isExtension(part as ContentPart)) {
    if (!isValidExtensionType(part.type)) {
      errs.push({
        path: `${base}.type`,
        message: `extension type "${part.type}" does not match ^x-[a-zA-Z0-9._-]+$`,
      });
    }
    return;
  }

  if (!isKnownPartType(part.type)) {
    errs.push({
      path: `${base}.type`,
      message: `unknown ContentPart type "${part.type}" (extension parts MUST use x- prefix)`,
    });
    return;
  }

  switch (part.type) {
    case PartType.Text:
      // Empty text string is permitted by spec.
      return;

    case PartType.ToolCall: {
      if (typeof part.call_id !== "string" || part.call_id.length === 0) {
        errs.push({ path: `${base}.call_id`, message: "tool_call: missing call_id" });
      }
      if (typeof part.name !== "string" || part.name.length === 0) {
        errs.push({ path: `${base}.name`, message: "tool_call: missing name" });
      }
      if (part.input === undefined || part.input === null) {
        errs.push({ path: `${base}.input`, message: "tool_call: missing input" });
      } else if (
        typeof part.input !== "string" &&
        (typeof part.input !== "object" || Array.isArray(part.input))
      ) {
        errs.push({
          path: `${base}.input`,
          message: "tool_call: input must be object or string",
        });
      }
      // parent_call_id (envelope.md §6.2): MUST reference an outer
      // tool_call still open at this position.
      if (part.parent_call_id !== undefined) {
        if (typeof part.parent_call_id !== "string") {
          errs.push({
            path: `${base}.parent_call_id`,
            message: "tool_call: parent_call_id must be a string",
          });
        } else if (part.parent_call_id.length > 0 && !openCalls.has(part.parent_call_id)) {
          errs.push({
            path: `${base}.parent_call_id`,
            message: `tool_call: parent_call_id "${part.parent_call_id}" has no open outer tool_call`,
          });
        }
      }
      if (typeof part.call_id === "string" && part.call_id.length > 0) {
        openCalls.add(part.call_id);
      }
      return;
    }

    case PartType.ToolResult: {
      let matched = false;
      if (typeof part.call_id !== "string" || part.call_id.length === 0) {
        errs.push({
          path: `${base}.call_id`,
          message: "tool_result: missing call_id",
        });
      } else if (!openCalls.has(part.call_id)) {
        errs.push({
          path: `${base}.call_id`,
          message: `tool_result: call_id "${part.call_id}" has no preceding open tool_call`,
        });
      } else {
        matched = true;
      }
      if (!("output" in part)) {
        errs.push({ path: `${base}.output`, message: "tool_result: missing output" });
      }
      if ("is_error" in part && typeof part.is_error !== "boolean") {
        errs.push({
          path: `${base}.is_error`,
          message: "tool_result: is_error must be boolean",
        });
      }
      if ("child_envelope_id" in part && typeof part.child_envelope_id !== "string") {
        errs.push({
          path: `${base}.child_envelope_id`,
          message: "tool_result: child_envelope_id must be a string",
        });
      }
      // Close the matched call so later turns can't interject into it
      // via in_reply_to_call_id and nested calls can't claim it as a
      // parent (mirrors envelope.md §3.2 / §6.2 lifetime invariants).
      if (matched && typeof part.call_id === "string") {
        openCalls.delete(part.call_id);
      }
      return;
    }

    case PartType.Image: {
      if (typeof part.mime !== "string" || part.mime.length === 0) {
        errs.push({ path: `${base}.mime`, message: "image: missing mime" });
      } else if (!MIME_RE.test(part.mime)) {
        errs.push({
          path: `${base}.mime`,
          message: `image: mime "${part.mime}" does not match ^[a-z]+/[a-zA-Z0-9.+-]+$`,
        });
      }
      const hasData = typeof part.data === "string" && part.data.length > 0;
      const hasUrl = typeof part.url === "string" && part.url.length > 0;
      if (hasData === hasUrl) {
        errs.push({
          path: base,
          message: "image: exactly one of data/url required",
        });
      }
      return;
    }

    case PartType.Thinking: {
      // Empty thinking.text permitted by schema (text value may be "");
      // only the key itself is required. Mirrors text part rules.
      if (typeof part.text !== "string") {
        errs.push({ path: `${base}.text`, message: "thinking: missing text" });
      }
      return;
    }
  }
}

/**
 * validateBytes is the convenience function for the trust boundary:
 * strict-decode an envelope (parseEnvelope) and then run structural
 * validate, returning the combined EnvelopeError list.
 *
 * Throws are caught and converted to entries in the returned list so
 * callers always get the same error shape regardless of whether the
 * input failed decode or structural checks. A non-empty result means
 * the input is not crtx-conformant.
 *
 * Use this on trust boundaries (loading from disk, network, or third
 * parties); use validate() directly for in-process objects you built.
 */
export function validateBytes(input: string | Uint8Array): EnvelopeError[] {
  let env: Envelope;
  try {
    env = parseEnvelope(input);
  } catch (err) {
    if (err instanceof EnvelopeParseError) {
      const cause = err.cause;
      if (Array.isArray(cause)) return cause as EnvelopeError[];
      if (cause && typeof cause === "object" && "path" in cause && "message" in cause) {
        return [cause as EnvelopeError];
      }
      return [{ path: "", message: err.message }];
    }
    throw err;
  }
  return validate(env);
}
