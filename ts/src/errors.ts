/**
 * Error types surfaced by the stem envelope SDK.
 */

/**
 * EnvelopeParseError is thrown by parseEnvelope when the input is not
 * a valid crtx envelope (malformed JSON or unknown fields).
 */
export class EnvelopeParseError extends Error {
  constructor(message: string, public readonly cause?: unknown) {
    super(message);
    this.name = "EnvelopeParseError";
  }
}

/**
 * EnvelopeError describes one structural validation failure surfaced
 * by validate() or validateBytes().
 *
 * path is a slash-joined JSON path from the envelope root, using []
 * for array indices (e.g. "turns[2].content[0].call_id").
 */
export interface EnvelopeError {
  /** Slash-joined JSON path to the offending location. */
  path: string;
  /** Human-readable explanation. */
  message: string;
}
