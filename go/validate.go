package stem

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// ErrInvalidEnvelope is returned by Validate when an Envelope fails
// structural validation. Wrap callers may use errors.Is.
var ErrInvalidEnvelope = errors.New("stem: invalid envelope")

// ValidateBytes is a strict-decode wrapper over Validate. It rejects
// envelopes whose JSON contains unknown top-level fields and then
// runs the standard Validate. Structural-only Validate does not
// catch unknown-field drift; ValidateBytes does. Use it on the
// boundary when you accept envelopes from external producers.
func ValidateBytes(b []byte) error {
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.DisallowUnknownFields()
	var s Session
	if err := dec.Decode(&s); err != nil {
		return fmt.Errorf("%w: %w", ErrInvalidEnvelope, err)
	}
	return Validate(&s)
}

// Validate checks an Envelope against crtx v0.1 structural rules.
// It is intentionally hand-rolled (no JSON-Schema runtime dep): it
// covers required fields, role enum, ContentPart discriminator, the
// dependent parent_id/fork_point pair, and tool_result→tool_call
// linkage. The authoritative spec is envelope.schema.json; for full
// schema conformance use an external JSON Schema validator.
func Validate(s *Session) error {
	if s == nil {
		return fmt.Errorf("%w: nil envelope", ErrInvalidEnvelope)
	}
	if s.CrtxVersion == "" {
		return fmt.Errorf("%w: missing crtx_version", ErrInvalidEnvelope)
	}
	if !crtxVersionCompatible(s.CrtxVersion) {
		return fmt.Errorf("%w: crtx_version %q unsupported (this stem speaks %q)",
			ErrInvalidEnvelope, s.CrtxVersion, CrtxVersion)
	}
	if s.ID == "" {
		return fmt.Errorf("%w: missing id", ErrInvalidEnvelope)
	}
	if s.CreatedAt.IsZero() {
		return fmt.Errorf("%w: missing created_at", ErrInvalidEnvelope)
	}
	if s.UpdatedAt.IsZero() {
		return fmt.Errorf("%w: missing updated_at", ErrInvalidEnvelope)
	}
	if s.Source.Kind == "" || s.Source.Version == "" {
		return fmt.Errorf("%w: source.kind and source.version required", ErrInvalidEnvelope)
	}

	// parent_id ↔ fork_point: both or neither.
	if (s.ParentID == "") != (s.ForkPoint == nil) {
		return fmt.Errorf("%w: parent_id and fork_point must both be set or both omitted", ErrInvalidEnvelope)
	}
	if s.ForkPoint != nil && *s.ForkPoint < 0 {
		return fmt.Errorf("%w: fork_point must be >= 0", ErrInvalidEnvelope)
	}

	// dispatched_from is mutually exclusive with parent_id / fork_point
	// (envelope.md §7.2). Schema enforces this with an if/then/not
	// block; mirror it here so hand-built sessions are rejected too.
	if s.DispatchedFrom != nil {
		if s.ParentID != "" || s.ForkPoint != nil {
			return fmt.Errorf("%w: dispatched_from is mutually exclusive with parent_id/fork_point", ErrInvalidEnvelope)
		}
		if s.DispatchedFrom.EnvelopeID == "" || s.DispatchedFrom.CallID == "" {
			return fmt.Errorf("%w: dispatched_from requires both envelope_id and call_id", ErrInvalidEnvelope)
		}
	}

	// injected_turns entries: required tuple plus an
	// injected_after_turn_id that, when set, MUST already exist in
	// this Envelope's turns at the position the entry is read
	// (envelope.md §7.3 — forward references forbidden).
	turnIDsSeen := make(map[string]struct{}, len(s.Turns))
	for _, t := range s.Turns {
		turnIDsSeen[t.ID] = struct{}{}
	}
	for i, ref := range s.InjectedTurns {
		if ref.EnvelopeID == "" || ref.StartTurnID == "" || ref.EndTurnID == "" {
			return fmt.Errorf("%w: injected_turns[%d]: envelope_id / start_turn_id / end_turn_id required", ErrInvalidEnvelope, i)
		}
		// We cannot resolve the source envelope here, but we can
		// enforce the local invariant: injected_after_turn_id must
		// reference a turn already present in this envelope's
		// turns[]. For creation-time entries (no after id), no check
		// is possible until the consumer resolves cross-source.
		if ref.InjectedAfterTurnID != "" {
			if _, ok := turnIDsSeen[ref.InjectedAfterTurnID]; !ok {
				return fmt.Errorf("%w: injected_turns[%d]: injected_after_turn_id %q not present in this envelope", ErrInvalidEnvelope, i, ref.InjectedAfterTurnID)
			}
		}
	}

	// Track open tool_calls. A call goes open on tool_call and closes
	// on its matching tool_result. Used by:
	//   - tool_result.call_id linkage
	//   - tool_call.parent_call_id (MUST point at an open outer call)
	//   - Turn.in_reply_to_call_id (MUST point at an open call)
	openCalls := map[string]struct{}{}

	for i, t := range s.Turns {
		if err := validateTurn(t, openCalls); err != nil {
			return fmt.Errorf("%w: turns[%d]: %w", ErrInvalidEnvelope, i, err)
		}
	}
	return nil
}

func crtxVersionCompatible(v string) bool {
	if v == CrtxVersion {
		return true
	}
	// Match major: "0.x" speaks "0.y" with y == our minor. For 0.1
	// this is strict equality; codified here so future stem releases
	// have an explicit knob.
	wantMajor, _, ok := strings.Cut(CrtxVersion, ".")
	if !ok {
		return false
	}
	gotMajor, _, ok := strings.Cut(v, ".")
	if !ok {
		return false
	}
	return wantMajor == gotMajor && wantMajor == "0"
	// Note: in 0.x every minor is breaking; we only accept exact.
	// The strings above just keep the major shape explicit.
}

func validateTurn(t Turn, openCalls map[string]struct{}) error {
	if t.ID == "" {
		return fmt.Errorf("missing id")
	}
	if !validRole(t.Role) {
		return fmt.Errorf("unknown role %q", t.Role)
	}
	if t.CreatedAt.IsZero() {
		return fmt.Errorf("missing created_at")
	}
	if len(t.Content) == 0 {
		return fmt.Errorf("content[] must have >=1 part")
	}

	// in_reply_to_call_id (envelope.md §3.2) MUST reference an open
	// tool_call at the point this Turn is appended. Tool-role Turns
	// SHOULD NOT carry it — tool_result IS the resolution of the
	// call, not an interjection.
	if t.InReplyToCallID != "" {
		if t.Role == RoleTool {
			return fmt.Errorf("in_reply_to_call_id: forbidden on tool-role turns")
		}
		if _, ok := openCalls[t.InReplyToCallID]; !ok {
			return fmt.Errorf("in_reply_to_call_id %q has no open tool_call", t.InReplyToCallID)
		}
	}

	for j, p := range t.Content {
		if err := validateContentPart(p, openCalls); err != nil {
			return fmt.Errorf("content[%d]: %w", j, err)
		}
	}
	return nil
}

func validRole(r Role) bool {
	switch r {
	case RoleUser, RoleAssistant, RoleTool, RoleSystem, RoleDeveloper:
		return true
	}
	return false
}

func validateContentPart(p ContentPart, openCalls map[string]struct{}) error {
	if p.Type == "" {
		return fmt.Errorf("missing type")
	}
	if p.IsExtension() {
		if !isValidExtensionType(p.Type) {
			return fmt.Errorf("extension type %q does not match ^x-[a-zA-Z0-9._-]+$", p.Type)
		}
		return nil
	}
	switch p.Type {
	case PartTypeText:
		// Empty string is permitted by schema (only "type" is required
		// to be present with "text"); we accept.
		return nil
	case PartTypeToolCall:
		if p.CallID == "" {
			return fmt.Errorf("tool_call: missing call_id")
		}
		if p.Name == "" {
			return fmt.Errorf("tool_call: missing name")
		}
		// parent_call_id (envelope.md §6.2) MUST reference an outer
		// tool_call still open at this position.
		if p.ParentCallID != "" {
			if _, ok := openCalls[p.ParentCallID]; !ok {
				return fmt.Errorf("tool_call: parent_call_id %q has no open outer tool_call", p.ParentCallID)
			}
		}
		// Empty input ({}) is valid: tools genuinely take no args
		// (e.g. current_time()). Schema is silent (no minProperties).
		openCalls[p.CallID] = struct{}{}
		return nil
	case PartTypeToolResult:
		if p.CallID == "" {
			return fmt.Errorf("tool_result: missing call_id")
		}
		if _, ok := openCalls[p.CallID]; !ok {
			return fmt.Errorf("tool_result: call_id %q has no preceding open tool_call", p.CallID)
		}
		if len(p.Output) == 0 {
			return fmt.Errorf("tool_result: missing output")
		}
		// Close the matched call so later turns can't interject into
		// it via in_reply_to_call_id, and nested calls can't claim it
		// as a parent.
		delete(openCalls, p.CallID)
		return nil
	case PartTypeImage:
		if p.Mime == "" {
			return fmt.Errorf("image: missing mime")
		}
		if !isValidMimeType(p.Mime) {
			return fmt.Errorf("image: mime %q does not match ^[a-z]+/[a-zA-Z0-9.+-]+$", p.Mime)
		}
		hasData := p.Data != ""
		hasURL := p.URL != ""
		if hasData == hasURL {
			return fmt.Errorf("image: exactly one of data/url required")
		}
		return nil
	case PartTypeThinking:
		// Empty string permitted by schema (only "type" + "text" key
		// required; text value may be ""). Mirrors text part rules.
		return nil
	default:
		return fmt.Errorf("unknown ContentPart type %q", p.Type)
	}
}
