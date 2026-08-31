/**
 * ContentPart type guards and extension-type helpers.
 */

import type {
  ContentPart,
  ExtensionPart,
  ImagePart,
  TextPart,
  ThinkingPart,
  ToolCallPart,
  ToolResultPart,
} from "./types";

/** Built-in crtx v0.1 ContentPart `type` discriminator values. */
export const PartType = {
  Text: "text",
  ToolCall: "tool_call",
  ToolResult: "tool_result",
  Image: "image",
  Thinking: "thinking",
} as const;

const KNOWN_PART_TYPES = new Set<string>(Object.values(PartType));

/** Allowed top-level Envelope field names. */
export const ENVELOPE_FIELDS = new Set<string>([
  "crtx_version",
  "id",
  "created_at",
  "updated_at",
  "source",
  "turns",
  "parent_id",
  "fork_point",
  "dispatched_from",
  "injected_turns",
  "metadata",
]);

/** Allowed Turn field names. */
export const TURN_FIELDS = new Set<string>([
  "id",
  "role",
  "created_at",
  "content",
  "agent_id",
  "in_reply_to_call_id",
  "metadata",
]);

/** Allowed Source field names. */
export const SOURCE_FIELDS = new Set<string>(["kind", "version", "instance"]);

/** Allowed DispatchedFrom field names. */
export const DISPATCHED_FROM_FIELDS = new Set<string>([
  "envelope_id",
  "call_id",
]);

/** Allowed InjectedTurnRef field names. */
export const INJECTED_TURN_FIELDS = new Set<string>([
  "envelope_id",
  "start_turn_id",
  "end_turn_id",
  "injected_after_turn_id",
]);

/** Per-variant allowed ContentPart field names (excluding extension). */
export const KNOWN_PART_FIELDS: Record<string, Set<string>> = {
  text: new Set(["type", "text", "metadata"]),
  tool_call: new Set(["type", "call_id", "name", "input", "parent_call_id", "metadata"]),
  tool_result: new Set([
    "type",
    "call_id",
    "output",
    "is_error",
    "child_envelope_id",
    "metadata",
  ]),
  image: new Set(["type", "mime", "data", "url", "alt", "metadata"]),
  thinking: new Set(["type", "text", "signature", "metadata"]),
};

/** True when `t` is a known built-in ContentPart discriminator. */
export function isKnownPartType(t: string): boolean {
  return KNOWN_PART_TYPES.has(t);
}

/**
 * isValidExtensionType matches the crtx v0.1 extension regex
 * `^x-[a-zA-Z0-9._-]+$`. Requires ≥1 trailing character.
 */
export function isValidExtensionType(t: string): boolean {
  return /^x-[a-zA-Z0-9._-]+$/.test(t);
}

/** True when the ContentPart `type` is an `x-…` extension. */
export function isExtension(p: ContentPart): p is ExtensionPart {
  return typeof p.type === "string" && p.type.startsWith("x-");
}

/** True when the ContentPart is a TextPart. */
export function isText(p: ContentPart): p is TextPart {
  return p.type === PartType.Text;
}

/** True when the ContentPart is a ToolCallPart. */
export function isToolCall(p: ContentPart): p is ToolCallPart {
  return p.type === PartType.ToolCall;
}

/** True when the ContentPart is a ToolResultPart. */
export function isToolResult(p: ContentPart): p is ToolResultPart {
  return p.type === PartType.ToolResult;
}

/** True when the ContentPart is an ImagePart. */
export function isImage(p: ContentPart): p is ImagePart {
  return p.type === PartType.Image;
}

/** True when the ContentPart is a ThinkingPart. */
export function isThinking(p: ContentPart): p is ThinkingPart {
  return p.type === PartType.Thinking;
}
