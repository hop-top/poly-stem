/**
 * Envelope JSON parsing and serialization.
 *
 * parseEnvelope decodes a crtx envelope from a JSON string or byte
 * buffer. Strict-decode only: rejects malformed JSON, non-object top
 * levels, and unknown fields at the envelope / source / dispatched_from
 * / injected_turns / turn / known content-part levels. Extension parts
 * (`x-…`) round-trip verbatim. Structural invariants (open-call
 * lifetime, parent_id ↔ fork_point mutual dependency, tool_result
 * linkage, etc.) live in validate(), NOT here — a structurally
 * questionable but decode-clean envelope parses successfully.
 *
 * serializeEnvelope renders a structurally-typed Envelope back to a
 * canonical JSON string. Empty turns arrays emit `[]`, not `null`.
 * Image parts with both-set or neither-set data/url are rejected at
 * serialize time so producers cannot silently emit invalid JSON.
 */

import {
  DISPATCHED_FROM_FIELDS,
  ENVELOPE_FIELDS,
  INJECTED_TURN_FIELDS,
  KNOWN_PART_FIELDS,
  SOURCE_FIELDS,
  TURN_FIELDS,
  isExtension,
} from "./content";
import { EnvelopeParseError } from "./errors";
import type { EnvelopeError } from "./errors";
import type { ContentPart, Envelope, ImagePart } from "./types";

/**
 * parseEnvelope decodes a crtx v0.1 envelope from a JSON string or
 * Uint8Array buffer.
 *
 * Strict-decode only: throws EnvelopeParseError on malformed JSON,
 * non-object top-level, or unknown fields at the envelope / source /
 * dispatched_from / injected_turns / turn / known content-part levels.
 * Extension parts (`type` starts with `x-`) keep their unknown fields
 * verbatim.
 *
 * Structural invariants (open-call lifetime, parent_id ↔ fork_point,
 * tool_result linkage, mutual exclusion of dispatched_from with the
 * fork pair, etc.) are NOT checked here. Use validate() on the
 * returned Envelope to surface those as a non-throwing error list, or
 * validateBytes() to combine both steps.
 *
 * Throws EnvelopeParseError; the first ~10 issues appear in the
 * message and the full list is on `.cause` as EnvelopeError[].
 *
 * Migration (from <= 0.1.x): callers that relied on parseEnvelope to
 * throw on structural issues (open-call lifetime, mutex, linkage)
 * MUST call validate() afterward:
 *
 *   const env = parseEnvelope(s);
 *   const errs = validate(env);
 *   if (errs.length) { ... }
 */
export function parseEnvelope(input: string | Uint8Array): Envelope {
  const text =
    typeof input === "string" ? input : new TextDecoder("utf-8").decode(input);

  let raw: unknown;
  try {
    raw = JSON.parse(text);
  } catch (err) {
    const msg = `invalid JSON: ${err instanceof Error ? err.message : String(err)}`;
    throw new EnvelopeParseError(msg, { path: "", message: msg });
  }

  if (raw === null || typeof raw !== "object" || Array.isArray(raw)) {
    const e: EnvelopeError = {
      path: "",
      message: "envelope must be a JSON object",
    };
    throw new EnvelopeParseError(`envelope invalid: ${e.message}`, e);
  }

  const errs: EnvelopeError[] = [];
  rejectUnknownFields(raw, errs);
  if (errs.length > 0) {
    const preview = errs
      .slice(0, 10)
      .map((e) => (e.path ? `${e.path}: ${e.message}` : e.message))
      .join("; ");
    const more = errs.length > 10 ? ` (and ${errs.length - 10} more)` : "";
    throw new EnvelopeParseError(`envelope invalid: ${preview}${more}`, errs);
  }

  return raw as Envelope;
}

/**
 * rejectUnknownFields walks a parsed envelope and pushes one error per
 * unknown field at the envelope / source / dispatched_from /
 * injected_turns / turn / known-content-part levels. Extension parts
 * (type prefixed `x-`) are allowed any fields.
 *
 * File-local helper; not exported to keep it unreachable via
 * deep-imports.
 */
function rejectUnknownFields(
  raw: unknown,
  errs: EnvelopeError[]
): void {
  if (!raw || typeof raw !== "object" || Array.isArray(raw)) return;
  const env = raw as Record<string, unknown>;

  for (const k of Object.keys(env)) {
    if (!ENVELOPE_FIELDS.has(k)) {
      errs.push({ path: k, message: `unknown envelope field "${k}"` });
    }
  }

  if (env.source && typeof env.source === "object" && !Array.isArray(env.source)) {
    for (const k of Object.keys(env.source as Record<string, unknown>)) {
      if (!SOURCE_FIELDS.has(k)) {
        errs.push({ path: `source.${k}`, message: `unknown source field "${k}"` });
      }
    }
  }

  if (
    env.dispatched_from &&
    typeof env.dispatched_from === "object" &&
    !Array.isArray(env.dispatched_from)
  ) {
    for (const k of Object.keys(env.dispatched_from as Record<string, unknown>)) {
      if (!DISPATCHED_FROM_FIELDS.has(k)) {
        errs.push({
          path: `dispatched_from.${k}`,
          message: `unknown dispatched_from field "${k}"`,
        });
      }
    }
  }

  if (Array.isArray(env.injected_turns)) {
    for (let i = 0; i < env.injected_turns.length; i++) {
      const r = env.injected_turns[i];
      if (!r || typeof r !== "object" || Array.isArray(r)) continue;
      for (const k of Object.keys(r as Record<string, unknown>)) {
        if (!INJECTED_TURN_FIELDS.has(k)) {
          errs.push({
            path: `injected_turns[${i}].${k}`,
            message: `unknown injected_turns field "${k}"`,
          });
        }
      }
    }
  }

  if (Array.isArray(env.turns)) {
    for (let i = 0; i < env.turns.length; i++) {
      const t = env.turns[i];
      if (!t || typeof t !== "object" || Array.isArray(t)) continue;
      const turn = t as Record<string, unknown>;
      for (const k of Object.keys(turn)) {
        if (!TURN_FIELDS.has(k)) {
          errs.push({
            path: `turns[${i}].${k}`,
            message: `unknown turn field "${k}"`,
          });
        }
      }
      if (Array.isArray(turn.content)) {
        for (let j = 0; j < turn.content.length; j++) {
          const p = turn.content[j];
          if (!p || typeof p !== "object" || Array.isArray(p)) continue;
          const part = p as Record<string, unknown>;
          const t2 = part.type;
          if (typeof t2 !== "string") continue;
          if (t2.startsWith("x-")) continue; // extensions: any fields allowed
          const allowed = KNOWN_PART_FIELDS[t2];
          if (!allowed) continue; // unknown discriminator caught by validate()
          for (const k of Object.keys(part)) {
            if (!allowed.has(k)) {
              errs.push({
                path: `turns[${i}].content[${j}].${k}`,
                message: `unknown ${t2} part field "${k}"`,
              });
            }
          }
        }
      }
    }
  }
}

/**
 * serializeEnvelope renders an Envelope to crtx-canonical JSON.
 *
 * - Empty turns emit `[]` (never `null`).
 * - Image parts MUST set exactly one of data/url; both-set or
 *   neither-set rejects with an Error.
 * - Extension parts (`x-…`) emit verbatim.
 * - Optional fields are omitted when undefined.
 */
export function serializeEnvelope(env: Envelope): string {
  return JSON.stringify(canonicalEnvelope(env));
}

function canonicalEnvelope(env: Envelope): Record<string, unknown> {
  const out: Record<string, unknown> = {
    crtx_version: env.crtx_version,
    id: env.id,
    created_at: env.created_at,
    updated_at: env.updated_at,
    source: canonicalSource(env.source),
  };
  if (env.parent_id !== undefined) out.parent_id = env.parent_id;
  if (env.fork_point !== undefined) out.fork_point = env.fork_point;
  if (env.dispatched_from !== undefined) {
    out.dispatched_from = {
      envelope_id: env.dispatched_from.envelope_id,
      call_id: env.dispatched_from.call_id,
    };
  }
  if (env.injected_turns !== undefined) {
    out.injected_turns = env.injected_turns.map((r) =>
      omitUndefined({
        envelope_id: r.envelope_id,
        start_turn_id: r.start_turn_id,
        end_turn_id: r.end_turn_id,
        injected_after_turn_id: r.injected_after_turn_id,
      })
    );
  }
  // turns is required and MUST be an array (never null) on the wire.
  out.turns = (env.turns ?? []).map(canonicalTurn);
  if (env.metadata !== undefined) out.metadata = env.metadata;
  return out;
}

function canonicalSource(s: Envelope["source"]): Record<string, unknown> {
  const out: Record<string, unknown> = { kind: s.kind, version: s.version };
  if (s.instance !== undefined) out.instance = s.instance;
  return out;
}

function canonicalTurn(t: Envelope["turns"][number]): Record<string, unknown> {
  const out: Record<string, unknown> = {
    id: t.id,
    role: t.role,
    created_at: t.created_at,
    content: t.content.map(canonicalPart),
  };
  if (t.agent_id !== undefined) out.agent_id = t.agent_id;
  if (t.in_reply_to_call_id !== undefined) {
    out.in_reply_to_call_id = t.in_reply_to_call_id;
  }
  if (t.metadata !== undefined) out.metadata = t.metadata;
  return out;
}

function canonicalPart(p: ContentPart): Record<string, unknown> {
  if (isExtension(p)) {
    // Round-trip verbatim. We shallow-copy so callers can't mutate
    // through the returned object.
    return { ...p };
  }
  switch (p.type) {
    case "text":
      return omitUndefined({
        type: p.type,
        text: p.text,
        metadata: p.metadata,
      });
    case "tool_call":
      return omitUndefined({
        type: p.type,
        call_id: p.call_id,
        name: p.name,
        input: p.input ?? {},
        parent_call_id: p.parent_call_id,
        metadata: p.metadata,
      });
    case "tool_result": {
      if (p.output === undefined) {
        // Spec marks output as required (any). undefined would
        // silently coerce to null on serialize — reject instead, same
        // contract as the image-XOR check below.
        throw new Error("stem: tool_result.output is required (undefined not allowed)");
      }
      const out: Record<string, unknown> = {
        type: p.type,
        call_id: p.call_id,
        output: p.output,
      };
      // Omit is_error when false (matches Go reference).
      if (p.is_error === true) out.is_error = true;
      if (p.child_envelope_id !== undefined) {
        out.child_envelope_id = p.child_envelope_id;
      }
      if (p.metadata !== undefined) out.metadata = p.metadata;
      return out;
    }
    case "image":
      return canonicalImage(p);
    case "thinking":
      return omitUndefined({
        type: p.type,
        text: p.text,
        signature: p.signature,
        metadata: p.metadata,
      });
    default: {
      // Unknown non-extension type at serialize time is a producer bug.
      const t = (p as { type?: unknown }).type;
      throw new Error(
        `stem: unknown ContentPart type ${JSON.stringify(t)} (extension parts MUST use x- prefix)`
      );
    }
  }
}

function canonicalImage(p: ImagePart): Record<string, unknown> {
  const hasData = typeof p.data === "string" && p.data.length > 0;
  const hasUrl = typeof p.url === "string" && p.url.length > 0;
  if (hasData === hasUrl) {
    throw new Error("stem: image part: exactly one of data/url required");
  }
  return omitUndefined({
    type: p.type,
    mime: p.mime,
    data: p.data,
    url: p.url,
    alt: p.alt,
    metadata: p.metadata,
  });
}

function omitUndefined(o: Record<string, unknown>): Record<string, unknown> {
  const out: Record<string, unknown> = {};
  for (const k of Object.keys(o)) {
    if (o[k] !== undefined) out[k] = o[k];
  }
  return out;
}
