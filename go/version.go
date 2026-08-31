package stem

// CrtxVersion is the crtx spec version emitted on every Envelope.
const CrtxVersion = "0.1"

// Version is the stem semver, surfaced as Source.version on
// stem-produced Envelopes.
const Version = "0.1.0"

// SourceKind identifies stem as the producing runtime on every
// envelope created via NewSession with the default Source.
const SourceKind = "stem"
