/**
 * Version constants for stem (TS) and the crtx spec it speaks.
 */

/** crtx spec version emitted on every Envelope. */
export const CrtxVersion = "0.1" as const;

/** stem-ts semver. Surfaced as Source.version on stem-produced envelopes. */
export const Version = "0.1.0" as const;

/** Source.kind value identifying stem as the producing runtime. */
export const SourceKind = "stem" as const;
