package stem_test

import (
	"encoding/json"
	"testing"
	"time"

	"hop.top/stem"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestValidate_RejectsNilEnvelope(t *testing.T) {
	err := stem.Validate(nil)
	assert.ErrorIs(t, err, stem.ErrInvalidEnvelope)
}

func TestValidate_RequiresCrtxVersion(t *testing.T) {
	s := &stem.Session{
		ID:        "s-1",
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
		Source:    stem.DefaultSource(),
	}
	err := stem.Validate(s)
	require.ErrorIs(t, err, stem.ErrInvalidEnvelope)
	assert.Contains(t, err.Error(), "crtx_version")
}

func TestValidate_RejectsBadCrtxVersion(t *testing.T) {
	s := &stem.Session{
		CrtxVersion: "1.0",
		ID:          "s-1",
		CreatedAt:   time.Now().UTC(),
		UpdatedAt:   time.Now().UTC(),
		Source:      stem.DefaultSource(),
	}
	err := stem.Validate(s)
	require.ErrorIs(t, err, stem.ErrInvalidEnvelope)
	assert.Contains(t, err.Error(), "crtx_version")
}

// crtx_version must be "major.minor"; bare "1" is malformed.
func TestValidate_RejectsMalformedCrtxVersion(t *testing.T) {
	s := &stem.Session{
		CrtxVersion: "1",
		ID:          "s-1",
		CreatedAt:   time.Now().UTC(),
		UpdatedAt:   time.Now().UTC(),
		Source:      stem.DefaultSource(),
	}
	err := stem.Validate(s)
	require.ErrorIs(t, err, stem.ErrInvalidEnvelope)
	assert.Contains(t, err.Error(), "crtx_version")
}

func TestValidate_RejectsSourceKindOnly(t *testing.T) {
	// Source.Version missing.
	s := &stem.Session{
		CrtxVersion: stem.CrtxVersion,
		ID:          "s-1",
		CreatedAt:   time.Now().UTC(),
		UpdatedAt:   time.Now().UTC(),
		Source:      stem.Source{Kind: "stem"},
	}
	err := stem.Validate(s)
	require.ErrorIs(t, err, stem.ErrInvalidEnvelope)
	assert.Contains(t, err.Error(), "source")
}

func TestValidate_RejectsSourceVersionOnly(t *testing.T) {
	// Source.Kind missing.
	s := &stem.Session{
		CrtxVersion: stem.CrtxVersion,
		ID:          "s-1",
		CreatedAt:   time.Now().UTC(),
		UpdatedAt:   time.Now().UTC(),
		Source:      stem.Source{Version: "0.1.0"},
	}
	err := stem.Validate(s)
	require.ErrorIs(t, err, stem.ErrInvalidEnvelope)
	assert.Contains(t, err.Error(), "source")
}

// image part with neither data nor url is rejected by structural
// Validate.
func TestValidate_RejectsImagePartWithoutDataOrURL(t *testing.T) {
	s := stem.NewSession("s-1")
	s.Turns = []stem.Turn{{
		ID:        "t-1",
		Role:      stem.RoleUser,
		CreatedAt: time.Now().UTC(),
		Content: []stem.ContentPart{{
			Type: stem.PartTypeImage,
			Mime: "image/png",
		}},
	}}
	err := stem.Validate(s)
	require.ErrorIs(t, err, stem.ErrInvalidEnvelope)
	assert.Contains(t, err.Error(), "exactly one")
}

func TestValidate_RejectsTurnWithEmptyID(t *testing.T) {
	s := stem.NewSession("s-1")
	s.Turns = []stem.Turn{{
		ID:        "",
		Role:      stem.RoleUser,
		CreatedAt: time.Now().UTC(),
		Content:   []stem.ContentPart{stem.TextPart("hi")},
	}}
	err := stem.Validate(s)
	require.ErrorIs(t, err, stem.ErrInvalidEnvelope)
	assert.Contains(t, err.Error(), "missing id")
}

// Extension part Type matching the bare "x-" prefix (zero chars
// after) is malformed per crtx v0.1 schema regex
// `^x-[a-zA-Z0-9._-]+$`. Validate must reject it.
func TestValidate_RejectsBareExtensionPrefix(t *testing.T) {
	raw := json.RawMessage(`{"type":"x-"}`)
	var p stem.ContentPart
	require.NoError(t, json.Unmarshal(raw, &p))

	s := stem.NewSession("s-1")
	s.Turns = []stem.Turn{{
		ID:        "t-1",
		Role:      stem.RoleUser,
		CreatedAt: time.Now().UTC(),
		Content:   []stem.ContentPart{p},
	}}
	err := stem.Validate(s)
	require.ErrorIs(t, err, stem.ErrInvalidEnvelope)
	assert.Contains(t, err.Error(), "extension type")
}

// Extension type with valid regex-conformant body must validate.
func TestValidate_AcceptsValidExtensionType(t *testing.T) {
	raw := json.RawMessage(`{"type":"x-io.jadb.foo"}`)
	var p stem.ContentPart
	require.NoError(t, json.Unmarshal(raw, &p))

	s := stem.NewSession("s-1")
	s.Turns = []stem.Turn{{
		ID:        "t-1",
		Role:      stem.RoleUser,
		CreatedAt: time.Now().UTC(),
		Content:   []stem.ContentPart{p},
	}}
	assert.NoError(t, stem.Validate(s))
}

// Extension type containing a character outside the spec class must
// fail validation. Space is the canonical out-of-class example.
func TestValidate_RejectsExtensionTypeWithBadChar(t *testing.T) {
	raw := json.RawMessage(`{"type":"x-bad space"}`)
	var p stem.ContentPart
	require.NoError(t, json.Unmarshal(raw, &p))

	s := stem.NewSession("s-1")
	s.Turns = []stem.Turn{{
		ID:        "t-1",
		Role:      stem.RoleUser,
		CreatedAt: time.Now().UTC(),
		Content:   []stem.ContentPart{p},
	}}
	err := stem.Validate(s)
	require.ErrorIs(t, err, stem.ErrInvalidEnvelope)
	assert.Contains(t, err.Error(), "extension type")
}

func TestValidate_RejectsUnknownRole(t *testing.T) {
	s := stem.NewSession("s-1")
	s.Turns = []stem.Turn{
		{ID: "t", Role: "wizard", CreatedAt: time.Now().UTC(),
			Content: []stem.ContentPart{stem.TextPart("hi")}},
	}
	err := stem.Validate(s)
	require.ErrorIs(t, err, stem.ErrInvalidEnvelope)
	assert.Contains(t, err.Error(), "unknown role")
}

func TestValidate_RejectsEmptyContent(t *testing.T) {
	s := stem.NewSession("s-1")
	s.Turns = []stem.Turn{
		{ID: "t", Role: stem.RoleUser, CreatedAt: time.Now().UTC()},
	}
	err := stem.Validate(s)
	require.ErrorIs(t, err, stem.ErrInvalidEnvelope)
	assert.Contains(t, err.Error(), "content")
}

func TestValidate_AllRolesAccepted(t *testing.T) {
	roles := []stem.Role{stem.RoleUser, stem.RoleAssistant, stem.RoleTool, stem.RoleSystem, stem.RoleDeveloper}
	for _, r := range roles {
		s := stem.NewSession("s-" + string(r))
		now := time.Now().UTC()
		if r == stem.RoleTool {
			// Need a preceding tool_call so tool_result validates.
			tc, _ := stem.ToolCallPart("c1", "x", map[string]any{})
			s.Turns = append(s.Turns, stem.Turn{
				ID: "t-call", Role: stem.RoleAssistant, CreatedAt: now,
				Content: []stem.ContentPart{tc},
			})
			tr, _ := stem.ToolResultPart("c1", map[string]any{"ok": true}, false)
			s.Turns = append(s.Turns, stem.Turn{
				ID: "t-res", Role: r, CreatedAt: now,
				Content: []stem.ContentPart{tr},
			})
		} else {
			s.Turns = append(s.Turns, stem.Turn{
				ID: "t-1", Role: r, CreatedAt: now,
				Content: []stem.ContentPart{stem.TextPart("ok")},
			})
		}
		assert.NoError(t, stem.Validate(s), "role %q must validate", r)
	}
}

func TestValidate_ForkPointRequiresParentID(t *testing.T) {
	s := stem.NewSession("s-1")
	fp := 0
	s.ForkPoint = &fp
	err := stem.Validate(s)
	require.ErrorIs(t, err, stem.ErrInvalidEnvelope)
}

func TestValidate_ParentIDRequiresForkPoint(t *testing.T) {
	s := stem.NewSession("s-1")
	s.ParentID = "p"
	err := stem.Validate(s)
	require.ErrorIs(t, err, stem.ErrInvalidEnvelope)
}

func TestValidate_ToolResultRequiresPriorToolCall(t *testing.T) {
	s := stem.NewSession("s-1")
	tr, err := stem.ToolResultPart("orphan", map[string]any{}, false)
	require.NoError(t, err)
	s.Turns = []stem.Turn{
		{ID: "t-1", Role: stem.RoleTool, CreatedAt: time.Now().UTC(),
			Content: []stem.ContentPart{tr}},
	}
	verr := stem.Validate(s)
	require.ErrorIs(t, verr, stem.ErrInvalidEnvelope)
	assert.Contains(t, verr.Error(), "no preceding open tool_call")
}

func TestValidate_ToolResultMatchesPriorToolCall(t *testing.T) {
	s := stem.NewSession("s-1")
	tc, _ := stem.ToolCallPart("c-1", "echo", map[string]any{})
	tr, _ := stem.ToolResultPart("c-1", map[string]any{"ok": true}, false)
	now := time.Now().UTC()
	s.Turns = []stem.Turn{
		{ID: "u-1", Role: stem.RoleUser, CreatedAt: now,
			Content: []stem.ContentPart{stem.TextPart("hi")}},
		{ID: "a-1", Role: stem.RoleAssistant, CreatedAt: now,
			Content: []stem.ContentPart{tc}},
		{ID: "t-1", Role: stem.RoleTool, CreatedAt: now,
			Content: []stem.ContentPart{tr}},
	}
	assert.NoError(t, stem.Validate(s))
}

func TestValidate_ImageVariantData(t *testing.T) {
	s := stem.NewSession("s-1")
	s.Turns = []stem.Turn{
		{ID: "t", Role: stem.RoleUser, CreatedAt: time.Now().UTC(),
			Content: []stem.ContentPart{{
				Type: stem.PartTypeImage,
				Mime: "image/png",
				Data: "aGVsbG8=",
			}}},
	}
	assert.NoError(t, stem.Validate(s))

	s.Turns[0].Content[0].URL = "https://example.com/x.png"
	err := stem.Validate(s)
	require.ErrorIs(t, err, stem.ErrInvalidEnvelope)
	assert.Contains(t, err.Error(), "exactly one")
}

func TestValidateBytes_RejectsUnknownTopLevelFields(t *testing.T) {
	// Structurally valid envelope, but with an unknown top-level
	// field. Structural Validate accepts it (json.Unmarshal silently
	// drops unknown fields); ValidateBytes must reject.
	raw := []byte(`{
		"crtx_version": "0.1",
		"id": "s-vb-1",
		"created_at": "2026-05-28T14:00:00Z",
		"updated_at": "2026-05-28T14:00:00Z",
		"source": {"kind": "stem", "version": "0.1.0"},
		"turns": [],
		"bogus_unknown_top_level": "drift"
	}`)

	var s stem.Session
	require.NoError(t, json.Unmarshal(raw, &s))
	assert.NoError(t, stem.Validate(&s), "structural Validate must accept (lenient)")

	vbErr := stem.ValidateBytes(raw)
	require.Error(t, vbErr, "ValidateBytes must reject unknown field")
	assert.ErrorIs(t, vbErr, stem.ErrInvalidEnvelope)
}

func TestValidateBytes_AcceptsClean(t *testing.T) {
	raw := []byte(`{
		"crtx_version": "0.1",
		"id": "s-vb-2",
		"created_at": "2026-05-28T14:00:00Z",
		"updated_at": "2026-05-28T14:00:00Z",
		"source": {"kind": "stem", "version": "0.1.0"},
		"turns": []
	}`)
	assert.NoError(t, stem.ValidateBytes(raw))
}

// Empty thinking.text is permitted by schema; only the key is required.
func TestValidate_AcceptsEmptyThinkingText(t *testing.T) {
	s := stem.NewSession("s-1")
	s.Turns = []stem.Turn{{
		ID: "t", Role: stem.RoleAssistant, CreatedAt: time.Now().UTC(),
		Content: []stem.ContentPart{{Type: stem.PartTypeThinking, Text: ""}},
	}}
	assert.NoError(t, stem.Validate(s))
}

// crtx v0.1 image.mime schema regex ^[a-z]+/[a-zA-Z0-9.+-]+$ — Validate
// must reject a non-conformant mime literal even when data/url is set.
func TestValidate_RejectsBadImageMime(t *testing.T) {
	s := stem.NewSession("s-1")
	s.Turns = []stem.Turn{{
		ID: "t", Role: stem.RoleUser, CreatedAt: time.Now().UTC(),
		Content: []stem.ContentPart{{
			Type: stem.PartTypeImage,
			Mime: "NOT_A_MIME",
			Data: "aGk=",
		}},
	}}
	err := stem.Validate(s)
	require.ErrorIs(t, err, stem.ErrInvalidEnvelope)
	assert.Contains(t, err.Error(), "mime")
}

// Empty tool_call.input ({}) is valid: tools genuinely take no args
// (current_time, etc.). Schema is silent (no minProperties); per
// crtx v0.1 the SDK must accept.
func TestValidate_AcceptsEmptyToolCallInput(t *testing.T) {
	tc, err := stem.ToolCallPart("c1", "current_time", map[string]any{})
	require.NoError(t, err)
	tr, err := stem.ToolResultPart("c1", map[string]any{"now": "2026-05-28T00:00:00Z"}, false)
	require.NoError(t, err)

	s := stem.NewSession("s-1")
	now := time.Now().UTC()
	s.Turns = []stem.Turn{
		{ID: "a", Role: stem.RoleAssistant, CreatedAt: now, Content: []stem.ContentPart{tc}},
		{ID: "t", Role: stem.RoleTool, CreatedAt: now, Content: []stem.ContentPart{tr}},
	}
	assert.NoError(t, stem.Validate(s))
}

func TestValidate_AcceptsExtensionPart(t *testing.T) {
	raw := json.RawMessage(`{"type":"x-io.jadb.custom","note":"keep me"}`)
	var p stem.ContentPart
	require.NoError(t, json.Unmarshal(raw, &p))

	s := stem.NewSession("s-1")
	s.Turns = []stem.Turn{
		{ID: "t", Role: stem.RoleUser, CreatedAt: time.Now().UTC(),
			Content: []stem.ContentPart{p}},
	}
	assert.NoError(t, stem.Validate(s))
}

// --- Multi-agent provenance (crtx envelope.md §3.1, §3.2, §6.2, §6.3, §7.2, §7.3) ---

func TestValidate_DispatchedFromMutuallyExclusiveWithForkFields(t *testing.T) {
	fp := 0
	s := stem.NewSession("s-1")
	s.ParentID = "parent"
	s.ForkPoint = &fp
	s.DispatchedFrom = &stem.DispatchedFrom{EnvelopeID: "p", CallID: "c"}
	err := stem.Validate(s)
	require.ErrorIs(t, err, stem.ErrInvalidEnvelope)
	assert.Contains(t, err.Error(), "mutually exclusive")
}

func TestValidate_DispatchedFromRequiresBothMembers(t *testing.T) {
	s := stem.NewSession("s-1")
	s.DispatchedFrom = &stem.DispatchedFrom{EnvelopeID: "p"} // missing call_id
	err := stem.Validate(s)
	require.ErrorIs(t, err, stem.ErrInvalidEnvelope)
	assert.Contains(t, err.Error(), "envelope_id and call_id")
}

func TestValidate_DispatchedFromAlone(t *testing.T) {
	s := stem.NewSession("s-1")
	s.DispatchedFrom = &stem.DispatchedFrom{EnvelopeID: "parent-env", CallID: "call-1"}
	assert.NoError(t, stem.Validate(s))
}

func TestValidate_ParentCallIDRequiresOpenOuterCall(t *testing.T) {
	inner, err := stem.ToolCallPart("inner", "echo", map[string]any{})
	require.NoError(t, err)
	inner.ParentCallID = "outer" // outer not seen
	s := stem.NewSession("s-1")
	s.Turns = []stem.Turn{
		{ID: "a", Role: stem.RoleAssistant, CreatedAt: time.Now().UTC(),
			Content: []stem.ContentPart{inner}},
	}
	err = stem.Validate(s)
	require.ErrorIs(t, err, stem.ErrInvalidEnvelope)
	assert.Contains(t, err.Error(), "parent_call_id")
}

func TestValidate_ParentCallIDAcceptedWhenOuterOpen(t *testing.T) {
	outer, _ := stem.ToolCallPart("outer", "dispatch", map[string]any{})
	inner, _ := stem.ToolCallPart("inner", "sub", map[string]any{})
	inner.ParentCallID = "outer"
	now := time.Now().UTC()
	s := stem.NewSession("s-1")
	s.Turns = []stem.Turn{
		{ID: "a", Role: stem.RoleAssistant, CreatedAt: now,
			Content: []stem.ContentPart{outer}},
		{ID: "b", Role: stem.RoleAssistant, CreatedAt: now,
			Content: []stem.ContentPart{inner}},
	}
	assert.NoError(t, stem.Validate(s))
}

func TestValidate_ParentCallIDRejectedAfterOuterResolved(t *testing.T) {
	outer, _ := stem.ToolCallPart("outer", "dispatch", map[string]any{})
	outerResult, _ := stem.ToolResultPart("outer", "done", false)
	inner, _ := stem.ToolCallPart("inner", "sub", map[string]any{})
	inner.ParentCallID = "outer"
	now := time.Now().UTC()
	s := stem.NewSession("s-1")
	s.Turns = []stem.Turn{
		{ID: "a", Role: stem.RoleAssistant, CreatedAt: now, Content: []stem.ContentPart{outer}},
		{ID: "t", Role: stem.RoleTool, CreatedAt: now, Content: []stem.ContentPart{outerResult}},
		{ID: "b", Role: stem.RoleAssistant, CreatedAt: now, Content: []stem.ContentPart{inner}},
	}
	err := stem.Validate(s)
	require.ErrorIs(t, err, stem.ErrInvalidEnvelope)
	assert.Contains(t, err.Error(), "parent_call_id")
}

func TestValidate_InReplyToCallIDRequiresOpenCall(t *testing.T) {
	now := time.Now().UTC()
	s := stem.NewSession("s-1")
	s.Turns = []stem.Turn{
		{
			ID: "u", Role: stem.RoleUser, CreatedAt: now,
			InReplyToCallID: "missing",
			Content:         []stem.ContentPart{stem.TextPart("interjection")},
		},
	}
	err := stem.Validate(s)
	require.ErrorIs(t, err, stem.ErrInvalidEnvelope)
	assert.Contains(t, err.Error(), "in_reply_to_call_id")
}

func TestValidate_InReplyToCallIDForbiddenOnToolRole(t *testing.T) {
	tc, _ := stem.ToolCallPart("c-1", "x", map[string]any{})
	tr, _ := stem.ToolResultPart("c-1", "out", false)
	now := time.Now().UTC()
	s := stem.NewSession("s-1")
	s.Turns = []stem.Turn{
		{ID: "a", Role: stem.RoleAssistant, CreatedAt: now, Content: []stem.ContentPart{tc}},
		{
			ID: "t", Role: stem.RoleTool, CreatedAt: now,
			InReplyToCallID: "c-1",
			Content:         []stem.ContentPart{tr},
		},
	}
	err := stem.Validate(s)
	require.ErrorIs(t, err, stem.ErrInvalidEnvelope)
	assert.Contains(t, err.Error(), "in_reply_to_call_id")
}

func TestValidate_InReplyToCallIDAcceptedWhileCallOpen(t *testing.T) {
	tc, _ := stem.ToolCallPart("c-1", "dispatch", map[string]any{})
	tr, _ := stem.ToolResultPart("c-1", "out", false)
	now := time.Now().UTC()
	s := stem.NewSession("s-1")
	s.Turns = []stem.Turn{
		{ID: "a", Role: stem.RoleAssistant, CreatedAt: now, Content: []stem.ContentPart{tc}},
		{
			ID: "u", Role: stem.RoleUser, CreatedAt: now,
			InReplyToCallID: "c-1",
			Content:         []stem.ContentPart{stem.TextPart("update")},
		},
		{ID: "t", Role: stem.RoleTool, CreatedAt: now, Content: []stem.ContentPart{tr}},
	}
	assert.NoError(t, stem.Validate(s))
}

func TestValidate_AcceptsAgentIDOnApplicableRoles(t *testing.T) {
	now := time.Now().UTC()
	s := stem.NewSession("s-1")
	s.Turns = []stem.Turn{
		{ID: "a", Role: stem.RoleAssistant, CreatedAt: now, AgentID: "C",
			Content: []stem.ContentPart{stem.TextPart("hi")}},
	}
	assert.NoError(t, stem.Validate(s))
}

func TestValidate_AcceptsChildEnvelopeIDOnToolResult(t *testing.T) {
	tc, _ := stem.ToolCallPart("c-1", "dispatch", map[string]any{})
	tr, _ := stem.ToolResultPart("c-1", "done", false)
	tr.ChildEnvelopeID = "child-env"
	now := time.Now().UTC()
	s := stem.NewSession("s-1")
	s.Turns = []stem.Turn{
		{ID: "a", Role: stem.RoleAssistant, CreatedAt: now, Content: []stem.ContentPart{tc}},
		{ID: "t", Role: stem.RoleTool, CreatedAt: now, Content: []stem.ContentPart{tr}},
	}
	assert.NoError(t, stem.Validate(s))

	// Round-trip preserves it.
	b, err := json.Marshal(s)
	require.NoError(t, err)
	assert.Contains(t, string(b), `"child_envelope_id":"child-env"`)
}

func TestValidate_InjectedTurnsRequireAllThreeIDs(t *testing.T) {
	s := stem.NewSession("s-1")
	s.InjectedTurns = []stem.InjectedTurnRef{{EnvelopeID: "src"}} // missing start/end
	err := stem.Validate(s)
	require.ErrorIs(t, err, stem.ErrInvalidEnvelope)
	assert.Contains(t, err.Error(), "injected_turns")
}

func TestValidate_InjectedAfterTurnIDMustExistInEnvelope(t *testing.T) {
	now := time.Now().UTC()
	s := stem.NewSession("s-1")
	s.Turns = []stem.Turn{
		{ID: "t-1", Role: stem.RoleUser, CreatedAt: now,
			Content: []stem.ContentPart{stem.TextPart("hi")}},
	}
	s.InjectedTurns = []stem.InjectedTurnRef{{
		EnvelopeID: "src", StartTurnID: "s", EndTurnID: "s",
		InjectedAfterTurnID: "does-not-exist",
	}}
	err := stem.Validate(s)
	require.ErrorIs(t, err, stem.ErrInvalidEnvelope)
	assert.Contains(t, err.Error(), "injected_after_turn_id")
}

func TestValidate_InjectedTurnsCreationTimeAccepted(t *testing.T) {
	s := stem.NewSession("s-1")
	s.InjectedTurns = []stem.InjectedTurnRef{{
		EnvelopeID: "src", StartTurnID: "a", EndTurnID: "b",
	}}
	assert.NoError(t, stem.Validate(s))
}
