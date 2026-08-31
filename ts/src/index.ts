/**
 * stem — TypeScript SDK for the crtx v0.1 conversation envelope.
 *
 * Tier A surface: types + parse + serialize + validate. No runtime,
 * no provider abstraction, no storage backends.
 *
 * See https://spec.hop.top/crtx/v0.1/envelope.schema.json for the spec.
 */

export {
  DISPATCHED_FROM_FIELDS,
  ENVELOPE_FIELDS,
  INJECTED_TURN_FIELDS,
  KNOWN_PART_FIELDS,
  PartType,
  SOURCE_FIELDS,
  TURN_FIELDS,
  isExtension,
  isImage,
  isKnownPartType,
  isText,
  isThinking,
  isToolCall,
  isToolResult,
  isValidExtensionType,
} from "./content";
export { EnvelopeParseError } from "./errors";
export type { EnvelopeError } from "./errors";
export { parseEnvelope, serializeEnvelope } from "./parse";
export type {
  ContentPart,
  DispatchedFrom,
  Envelope,
  ExtensionPart,
  ImagePart,
  InjectedTurnRef,
  Role,
  Source,
  TextPart,
  ThinkingPart,
  ToolCallPart,
  ToolResultPart,
  Turn,
} from "./types";
export { validate, validateBytes } from "./validate";
export { CrtxVersion, SourceKind, Version } from "./version";
