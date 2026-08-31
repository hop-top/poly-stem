/**
 * crtx v0.1 envelope/turn/content-part TypeScript types.
 *
 * The wire shape matches envelope.schema.json. ContentPart is a
 * discriminated union on `type`. Unknown / extension parts (type
 * prefixed `x-`) are preserved verbatim as an opaque record.
 */

/**
 * Role identifies the author of a Turn. Mirrors crtx v0.1 enum.
 */
export type Role = "user" | "assistant" | "tool" | "system" | "developer";

/**
 * Source identifies the runtime that produced the Envelope.
 */
export interface Source {
  kind: string;
  version: string;
  instance?: string;
}

/**
 * TextPart — plain text payload.
 */
export interface TextPart {
  type: "text";
  text: string;
  metadata?: Record<string, unknown>;
}

/**
 * ToolCallPart — assistant-issued tool invocation.
 *
 * `input` is canonically a JSON object; a raw string form is permitted
 * by the spec for legacy / streaming chunks.
 *
 * `parent_call_id` (crtx v0.1 envelope.md §6.2) glues a nested call to
 * an outer call still open at the position of this part. Used for
 * in-band sub-tool / dispatch chains.
 */
export interface ToolCallPart {
  type: "tool_call";
  call_id: string;
  name: string;
  input: Record<string, unknown> | string;
  parent_call_id?: string;
  metadata?: Record<string, unknown>;
}

/**
 * ToolResultPart — tool-role response to a preceding ToolCallPart.
 *
 * `child_envelope_id` (crtx v0.1 envelope.md §6.3) points at the
 * dispatch-nest child Envelope when the call was executed out-of-band
 * in a fresh session. Consumers MAY follow it to the full child
 * transcript.
 */
export interface ToolResultPart {
  type: "tool_result";
  call_id: string;
  output: unknown;
  is_error?: boolean;
  child_envelope_id?: string;
  metadata?: Record<string, unknown>;
}

/**
 * ImagePart — image payload. Exactly one of `data` (base64) or `url`
 * MUST be set.
 */
export interface ImagePart {
  type: "image";
  mime: string;
  data?: string;
  url?: string;
  alt?: string;
  metadata?: Record<string, unknown>;
}

/**
 * ThinkingPart — reasoning trace emitted by models that expose them.
 */
export interface ThinkingPart {
  type: "thinking";
  text: string;
  signature?: string;
  metadata?: Record<string, unknown>;
}

/**
 * ExtensionPart — opaque content part whose `type` is prefixed `x-`.
 * Round-trips verbatim; consumers MUST preserve unknown fields.
 */
export interface ExtensionPart {
  type: string;
  [field: string]: unknown;
}

/**
 * ContentPart — discriminated union on `type`. Known variants match
 * the crtx v0.1 enum; extension parts (`x-…`) fall through to
 * ExtensionPart.
 */
export type ContentPart =
  | TextPart
  | ToolCallPart
  | ToolResultPart
  | ImagePart
  | ThinkingPart
  | ExtensionPart;

/**
 * Turn — one contribution within an Envelope. content MUST be
 * non-empty; ordering is by array position, not by created_at.
 *
 * `agent_id` (crtx v0.1 envelope.md §3.1) attributes the Turn to a
 * specific producing agent when multiple assistants contribute. Opaque
 * to the spec; stability within an Envelope is REQUIRED.
 *
 * `in_reply_to_call_id` (crtx v0.1 envelope.md §3.2) marks the Turn as
 * a mid-flight message into the lifetime of an open tool_call.
 * Forbidden on tool-role Turns (tool_result IS the resolution).
 */
export interface Turn {
  id: string;
  role: Role;
  created_at: string;
  content: ContentPart[];
  agent_id?: string;
  in_reply_to_call_id?: string;
  metadata?: Record<string, unknown>;
}

/**
 * DispatchedFrom marks an Envelope as a dispatch nest spawned by an
 * open tool_call in another Envelope. Both members are REQUIRED by the
 * schema; mutually exclusive with parent_id / fork_point.
 *
 * See envelope.md §7.2.
 */
export interface DispatchedFrom {
  envelope_id: string;
  call_id: string;
}

/**
 * InjectedTurnRef references a slice of Turns in another Envelope to
 * include as part of this Envelope's context. Append-only.
 *
 * `injected_after_turn_id`, when set, MUST reference a Turn already
 * present in this Envelope's `turns` at the time the entry is
 * appended (forward references forbidden). Absence means
 * "creation-time injection," conceptually prepended before turns[0].
 *
 * See envelope.md §7.3.
 */
export interface InjectedTurnRef {
  envelope_id: string;
  start_turn_id: string;
  end_turn_id: string;
  injected_after_turn_id?: string;
}

/**
 * Envelope — top-level crtx v0.1 container.
 *
 * parent_id / fork_point are mutually dependent: both set or both
 * omitted. `dispatched_from` is mutually exclusive with the fork
 * fields (envelope.md §7.2). `injected_turns` is orthogonal and MAY
 * appear with either relationship or on a standalone Envelope.
 */
export interface Envelope {
  crtx_version: string;
  id: string;
  created_at: string;
  updated_at: string;
  source: Source;
  turns: Turn[];
  parent_id?: string;
  fork_point?: number;
  dispatched_from?: DispatchedFrom;
  injected_turns?: InjectedTurnRef[];
  metadata?: Record<string, unknown>;
}
