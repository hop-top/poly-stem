package stem_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"hop.top/stem"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// crtxExamplesDir resolves to the vendored copy of crtx v0.1 example
// envelopes under testdata/. Vendored deliberately so tests run with
// no external repo dependencies.
const crtxExamplesDir = "testdata/crtx_v0.1"

func TestCrtxExamples_RoundTrip(t *testing.T) {
	for _, name := range []string{
		"minimal.json",
		"tool-call.json",
		"fork.json",
		"dispatch.json",
		"dispatch-nested.json",
		"injection.json",
	} {
		t.Run(name, func(t *testing.T) {
			data, err := os.ReadFile(filepath.Join(crtxExamplesDir, name))
			require.NoError(t, err)

			var env stem.Session
			require.NoError(t, json.Unmarshal(data, &env))

			// fork.json names a parent that isn't loaded; the tool_call
			// example is self-contained; minimal has no tool calls.
			// Validate runs in-envelope only, so all three should pass.
			require.NoError(t, stem.Validate(&env))

			// Round-trip back to JSON and reparse — equality at the
			// crtx-level fields.
			out, err := json.Marshal(&env)
			require.NoError(t, err)

			var got stem.Session
			require.NoError(t, json.Unmarshal(out, &got))
			assert.Equal(t, env.ID, got.ID)
			assert.Equal(t, env.CrtxVersion, got.CrtxVersion)
			assert.Equal(t, env.Source, got.Source)
			assert.Equal(t, env.ParentID, got.ParentID)
			if env.ForkPoint != nil {
				require.NotNil(t, got.ForkPoint)
				assert.Equal(t, *env.ForkPoint, *got.ForkPoint)
			}
			require.Equal(t, len(env.Turns), len(got.Turns))
			for i := range env.Turns {
				assert.Equal(t, env.Turns[i].ID, got.Turns[i].ID)
				assert.Equal(t, env.Turns[i].Role, got.Turns[i].Role)
				require.Equal(t, len(env.Turns[i].Content), len(got.Turns[i].Content))
				for j := range env.Turns[i].Content {
					assert.Equal(t, env.Turns[i].Content[j].Type,
						got.Turns[i].Content[j].Type)
				}
			}

			// Deep-equal: re-marshal the parsed envelope and compare
			// canonical JSON shapes by parsing both into generic maps.
			// Catches bugs in text / tool_call.input / metadata
			// round-tripping that the spot-check above misses, plus
			// silent drops of new optional fields (agent_id,
			// parent_call_id, child_envelope_id, in_reply_to_call_id,
			// dispatched_from, injected_turns) when those land in a
			// fixture.
			var orig, again map[string]any
			require.NoError(t, json.Unmarshal(data, &orig))
			require.NoError(t, json.Unmarshal(out, &again))
			if !reflect.DeepEqual(orig, again) {
				t.Fatalf("canonical round-trip mismatch\norig: %#v\nagain: %#v", orig, again)
			}
		})
	}
}

// TestCrtxExamples_DispatchProvenanceFieldsRoundTrip pins the new
// envelope.md §3.1 / §3.2 / §6.2 / §7.2 fields against the canonical
// dispatch example. Decoupled from the generic deep-equal so a drop
// of one specific new field surfaces with a targeted message rather
// than as a mystery map diff.
func TestCrtxExamples_DispatchProvenanceFieldsRoundTrip(t *testing.T) {
	data, err := os.ReadFile(filepath.Join(crtxExamplesDir, "dispatch.json"))
	require.NoError(t, err)

	var env stem.Session
	require.NoError(t, json.Unmarshal(data, &env))

	// turns[1]: assistant agent_id=C, holds tool_call call_C_dispatch_A.
	assert.Equal(t, "C", env.Turns[1].AgentID)
	require.Len(t, env.Turns[1].Content, 1)
	tc := env.Turns[1].Content[0]
	assert.Equal(t, stem.PartTypeToolCall, tc.Type)
	assert.Equal(t, "call_C_dispatch_A", tc.CallID)

	// turns[3]: user role with in_reply_to_call_id set (interjection).
	assert.Equal(t, "call_C_dispatch_A", env.Turns[3].InReplyToCallID)

	// turns[4]: nested tool_call with parent_call_id pointing at the
	// outer call.
	require.Len(t, env.Turns[4].Content, 1)
	nested := env.Turns[4].Content[0]
	assert.Equal(t, "call_C_dispatch_A", nested.ParentCallID)

	// Re-encode and confirm the fields survive verbatim.
	out, err := json.Marshal(&env)
	require.NoError(t, err)
	assert.Contains(t, string(out), `"agent_id":"C"`)
	assert.Contains(t, string(out), `"in_reply_to_call_id":"call_C_dispatch_A"`)
	assert.Contains(t, string(out), `"parent_call_id":"call_C_dispatch_A"`)
}

// TestCrtxExamples_DispatchNestedFieldsRoundTrip pins the
// envelope.md §7.2 dispatch-nest signal (Envelope.dispatched_from).
func TestCrtxExamples_DispatchNestedFieldsRoundTrip(t *testing.T) {
	data, err := os.ReadFile(filepath.Join(crtxExamplesDir, "dispatch-nested.json"))
	require.NoError(t, err)

	var env stem.Session
	require.NoError(t, json.Unmarshal(data, &env))

	require.NotNil(t, env.DispatchedFrom)
	assert.NotEmpty(t, env.DispatchedFrom.EnvelopeID)
	assert.NotEmpty(t, env.DispatchedFrom.CallID)
	assert.Empty(t, env.ParentID, "dispatch nest MUST NOT set parent_id")
	assert.Nil(t, env.ForkPoint, "dispatch nest MUST NOT set fork_point")

	// Re-encode preserves dispatched_from.
	out, err := json.Marshal(&env)
	require.NoError(t, err)
	assert.Contains(t, string(out), `"dispatched_from":`)
}

// TestCrtxExamples_InjectionFieldsRoundTrip pins envelope.md §7.3
// injected_turns.
func TestCrtxExamples_InjectionFieldsRoundTrip(t *testing.T) {
	data, err := os.ReadFile(filepath.Join(crtxExamplesDir, "injection.json"))
	require.NoError(t, err)

	var env stem.Session
	require.NoError(t, json.Unmarshal(data, &env))

	require.NotEmpty(t, env.InjectedTurns)
	for i, ref := range env.InjectedTurns {
		assert.NotEmptyf(t, ref.EnvelopeID, "injected_turns[%d].envelope_id", i)
		assert.NotEmptyf(t, ref.StartTurnID, "injected_turns[%d].start_turn_id", i)
		assert.NotEmptyf(t, ref.EndTurnID, "injected_turns[%d].end_turn_id", i)
	}

	out, err := json.Marshal(&env)
	require.NoError(t, err)
	assert.Contains(t, string(out), `"injected_turns":`)
}
