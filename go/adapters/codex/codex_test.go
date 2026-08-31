package codex_test

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	stem "hop.top/stem"
	"hop.top/stem/adapters/codex"
)

const (
	modernPath   = "testdata/store/2026/08/01/rollout-2026-08-01T10-00-00-01900000-0000-7000-8000-00000000000a.jsonl"
	legacyPath   = "testdata/store/2025/10/04/rollout-2025-10-04T07-00-25-01900000-0000-7000-8000-00000000000b.jsonl"
	forkedPath   = "testdata/store/2026/04/22/rollout-2026-04-22T05-36-32-01900000-0000-7000-8000-00000000000c.jsonl"
	subagentPath = "testdata/store/2026/07/18/rollout-2026-07-18T20-57-15-01900000-0000-7000-8000-00000000000d.jsonl"
	driftPath    = "testdata/store/2026/08/02/rollout-2026-08-02T09-00-00-01900000-0000-7000-8000-00000000000e.jsonl"
	noMetaPath   = "testdata/store/2026/08/03/rollout-2026-08-03T00-00-00-01900000-0000-7000-8000-00000000000f.jsonl"
)

func ts(t *testing.T, s string) time.Time {
	t.Helper()
	v, err := time.Parse(time.RFC3339Nano, s)
	require.NoError(t, err)
	return v
}

// validateEnvelope asserts the session passes both the structural
// validator and the strict-decode byte validator after a marshal
// round-trip, proving the adapter emits schema-shaped envelopes.
func validateEnvelope(t *testing.T, s *stem.Session) {
	t.Helper()
	require.NoError(t, stem.Validate(s))
	b, err := json.Marshal(s)
	require.NoError(t, err)
	require.NoError(t, stem.ValidateBytes(b))
}

func TestDiscover(t *testing.T) {
	files, err := codex.Discover("testdata/store")
	require.NoError(t, err)
	require.Equal(t, []string{
		filepath.FromSlash(legacyPath),
		filepath.FromSlash(forkedPath),
		filepath.FromSlash(subagentPath),
		filepath.FromSlash(modernPath),
		filepath.FromSlash(driftPath),
		filepath.FromSlash(noMetaPath),
	}, files)
}

func TestDiscoverMissingRoot(t *testing.T) {
	_, err := codex.Discover("testdata/no-such-root")
	require.Error(t, err)
}

func TestParseFileModern(t *testing.T) {
	s, rep, err := codex.ParseFile(modernPath)
	require.NoError(t, err)
	require.NotNil(t, rep)

	require.Equal(t, "0.1", s.CrtxVersion)
	require.Equal(t, "01900000-0000-7000-8000-00000000000a", s.ID)
	require.Equal(t, ts(t, "2026-08-01T10:00:00.000Z"), s.CreatedAt)
	require.Equal(t, ts(t, "2026-08-01T10:00:11.000Z"), s.UpdatedAt)
	require.Equal(t, stem.Source{Kind: "codex", Version: "0.145.0"}, s.Source)
	require.Empty(t, s.ParentID)
	require.Nil(t, s.ForkPoint)
	require.Nil(t, s.DispatchedFrom)

	abs, err := filepath.Abs(modernPath)
	require.NoError(t, err)
	require.Equal(t, "/home/dev/acme", s.Metadata[codex.MetaCwd])
	require.Equal(t, "main", s.Metadata[codex.MetaGitBranch])
	require.Equal(t, "deadbeefdeadbeefdeadbeefdeadbeefdeadbeef", s.Metadata[codex.MetaGitCommit])
	require.Equal(t, abs, s.Metadata[codex.MetaNativePath])
	require.Equal(t, "You are a coding agent.", s.Metadata[codex.MetaInstructions])

	require.Len(t, s.Turns, 8)

	// Roles and part types, in order.
	wantRoles := []stem.Role{
		stem.RoleDeveloper, stem.RoleUser, stem.RoleAssistant,
		stem.RoleAssistant, stem.RoleTool, stem.RoleAssistant,
		stem.RoleTool, stem.RoleAssistant,
	}
	for i, r := range wantRoles {
		require.Equal(t, r, s.Turns[i].Role, "turn %d role", i)
	}

	// Deterministic turn ids.
	require.Equal(t, "t_000001", s.Turns[0].ID)
	require.Equal(t, "t_000008", s.Turns[7].ID)

	// Developer preamble.
	require.Len(t, s.Turns[0].Content, 1)
	require.Equal(t, stem.PartTypeText, s.Turns[0].Content[0].Type)

	// User turn keeps both text parts.
	require.Len(t, s.Turns[1].Content, 2)
	require.Equal(t, "add rate limiting to the api", s.Turns[1].Content[0].Text)
	require.Equal(t, ts(t, "2026-08-01T10:00:02.000Z"), s.Turns[1].CreatedAt)

	// Reasoning summary becomes a thinking part.
	require.Equal(t, stem.PartTypeThinking, s.Turns[2].Content[0].Type)
	require.Equal(t, "Plan the middleware.", s.Turns[2].Content[0].Text)

	// function_call: parsed JSON arguments ride verbatim.
	fc := s.Turns[3].Content[0]
	require.Equal(t, stem.PartTypeToolCall, fc.Type)
	require.Equal(t, "call_001", fc.CallID)
	require.Equal(t, "exec_command", fc.Name)
	require.JSONEq(t, `{"cmd":"ls"}`, string(fc.Input))

	// function_call_output: string output rides as a JSON string.
	fr := s.Turns[4].Content[0]
	require.Equal(t, stem.PartTypeToolResult, fr.Type)
	require.Equal(t, "call_001", fr.CallID)
	require.JSONEq(t, `"Process exited with code 0\nmain.go"`, string(fr.Output))

	// custom_tool_call: plain-string input rides as a JSON string.
	cc := s.Turns[5].Content[0]
	require.Equal(t, stem.PartTypeToolCall, cc.Type)
	require.Equal(t, "call_002", cc.CallID)
	require.Equal(t, "apply_patch", cc.Name)
	require.JSONEq(t, `"*** Begin Patch synthetic *** End Patch"`, string(cc.Input))

	cr := s.Turns[6].Content[0]
	require.Equal(t, stem.PartTypeToolResult, cr.Type)
	require.Equal(t, "call_002", cr.CallID)

	require.Equal(t, "Rate limiting added.", s.Turns[7].Content[0].Text)

	// Accounting: noise records tracked, nothing fatal.
	require.Equal(t, 15, rep.Lines)
	require.Equal(t, 8, rep.Turns)
	require.Equal(t, 4, rep.Skips[codex.SkipEventRecord])
	require.Equal(t, 1, rep.Skips[codex.SkipTurnContext])
	require.Equal(t, 1, rep.Skips[codex.SkipWorldState])

	validateEnvelope(t, s)
}

func TestParseFileLegacy(t *testing.T) {
	s, rep, err := codex.ParseFile(legacyPath)
	require.NoError(t, err)

	require.Equal(t, "01900000-0000-7000-8000-00000000000b", s.ID)
	require.Equal(t, stem.Source{Kind: "codex", Version: "0.44.0"}, s.Source)
	require.Equal(t, ts(t, "2025-10-04T07:00:25.681Z"), s.CreatedAt)
	require.Equal(t, ts(t, "2025-10-04T07:00:26.000Z"), s.UpdatedAt)

	// Legacy string `instructions` key mapped; empty git object emits
	// no branch/commit keys.
	require.Equal(t, "/home/dev/legacy", s.Metadata[codex.MetaCwd])
	require.Equal(t, "Be terse.", s.Metadata[codex.MetaInstructions])
	require.NotContains(t, s.Metadata, codex.MetaGitBranch)
	require.NotContains(t, s.Metadata, codex.MetaGitCommit)

	require.Len(t, s.Turns, 2)
	require.Equal(t, 2, rep.Turns)
	validateEnvelope(t, s)
}

func TestParseFileForked(t *testing.T) {
	s, rep, err := codex.ParseFile(forkedPath)
	require.NoError(t, err)

	// forked_from_id maps to parent_id; Codex materializes the full
	// history in the fork file, so fork_point is 0.
	require.Equal(t, "01900000-0000-7000-8000-00000000000c", s.ID)
	require.Equal(t, "01900000-0000-7000-8000-0000000000aa", s.ParentID)
	require.NotNil(t, s.ForkPoint)
	require.Equal(t, 0, *s.ForkPoint)

	// The copied parent session_meta line is skipped, not merged.
	require.Equal(t, 1, rep.Skips[codex.SkipDuplicateMeta])
	require.Len(t, s.Turns, 2)
	validateEnvelope(t, s)
}

func TestParseFileSubagent(t *testing.T) {
	s, _, err := codex.ParseFile(subagentPath)
	require.NoError(t, err)

	// session_id differing from id marks a sub-thread; the root
	// session becomes the fork parent (no call_id exists in the
	// native store, so dispatched_from cannot be built).
	require.Equal(t, "01900000-0000-7000-8000-00000000000d", s.ID)
	require.Equal(t, "01900000-0000-7000-8000-0000000000bb", s.ParentID)
	require.NotNil(t, s.ForkPoint)
	require.Equal(t, 0, *s.ForkPoint)
	require.Nil(t, s.DispatchedFrom)

	// Null base_instructions and null git tolerated.
	require.NotContains(t, s.Metadata, codex.MetaInstructions)
	require.NotContains(t, s.Metadata, codex.MetaGitBranch)
	validateEnvelope(t, s)
}

func TestParseFileDrift(t *testing.T) {
	s, rep, err := codex.ParseFile(driftPath)
	require.NoError(t, err)

	require.Equal(t, "01900000-0000-7000-8000-00000000000e", s.ID)
	require.Equal(t, stem.Source{Kind: "codex", Version: "9.9.9"}, s.Source)
	require.Equal(t, ts(t, "2026-08-02T09:00:00.000Z"), s.CreatedAt)
	require.Equal(t, ts(t, "2026-08-02T09:00:16.000Z"), s.UpdatedAt)

	// Survivors, in order: mixed-part user message, tool_call with
	// unparseable arguments, its result, reasoning content text,
	// image message, closing assistant message.
	require.Len(t, s.Turns, 6)

	require.Equal(t, stem.RoleUser, s.Turns[0].Role)
	require.Len(t, s.Turns[0].Content, 1)
	require.Equal(t, "mixed parts survive", s.Turns[0].Content[0].Text)

	fc := s.Turns[1].Content[0]
	require.Equal(t, stem.PartTypeToolCall, fc.Type)
	require.Equal(t, "call_005", fc.CallID)
	require.JSONEq(t, `"not json {"`, string(fc.Input))

	fr := s.Turns[2].Content[0]
	require.Equal(t, stem.PartTypeToolResult, fr.Type)
	require.Equal(t, "call_005", fr.CallID)

	th := s.Turns[3].Content[0]
	require.Equal(t, stem.PartTypeThinking, th.Type)
	require.Equal(t, "raw chain of thought", th.Text)

	img := s.Turns[4].Content[0]
	require.Equal(t, stem.PartTypeImage, img.Type)
	require.Equal(t, "image/png", img.Mime)
	require.Equal(t, "QUJD", img.Data)
	require.Empty(t, img.URL)

	require.Equal(t, "survived the drift", s.Turns[5].Content[0].Text)

	// Accounting per skip class.
	require.Equal(t, 17, rep.Lines) // blank line not counted
	require.Equal(t, 6, rep.Turns)
	require.Equal(t, 1, rep.Skips[codex.SkipMalformedLine])
	require.Equal(t, 1, rep.Skips[codex.SkipUnknownRecord])
	require.Equal(t, 1, rep.Skips[codex.SkipUnknownItem])
	require.Equal(t, 2, rep.Skips[codex.SkipUnknownPart])
	require.Equal(t, 1, rep.Skips[codex.SkipUnknownRole])
	require.Equal(t, 1, rep.Skips[codex.SkipEmptyMessage])
	require.Equal(t, 1, rep.Skips[codex.SkipOrphanToolResult])
	require.Equal(t, 1, rep.Skips[codex.SkipEmptyReasoning])
	require.Equal(t, 1, rep.Skips[codex.SkipWebSearchCall])
	require.Equal(t, 1, rep.Skips[codex.SkipCompacted])
	require.Equal(t, 1, rep.Skips[codex.SkipInvalidToolCall])

	validateEnvelope(t, s)
}

func TestParseFileNoMeta(t *testing.T) {
	s, rep, err := codex.ParseFile(noMetaPath)
	require.NoError(t, err)

	// Identity recovered from the rollout filename; timestamps from
	// the record lines; version unknown.
	require.Equal(t, "01900000-0000-7000-8000-00000000000f", s.ID)
	require.Equal(t, stem.Source{Kind: "codex", Version: "unknown"}, s.Source)
	require.Equal(t, ts(t, "2026-08-03T00:00:01.000Z"), s.CreatedAt)
	require.Equal(t, ts(t, "2026-08-03T00:00:02.000Z"), s.UpdatedAt)
	require.NotContains(t, s.Metadata, codex.MetaCwd)
	require.Len(t, s.Turns, 2)
	require.Equal(t, 2, rep.Turns)
	validateEnvelope(t, s)
}

func TestParseReaderWithoutIdentity(t *testing.T) {
	// No session_meta and no filename to fall back on: not a session.
	in := `{"timestamp":"2026-08-03T00:00:01.000Z","type":"response_item","payload":{"type":"message","role":"user","content":[{"type":"input_text","text":"hi"}]}}`
	_, _, err := codex.Parse(strings.NewReader(in))
	require.Error(t, err)
}

func TestParseEmptyInput(t *testing.T) {
	_, _, err := codex.Parse(strings.NewReader(""))
	require.Error(t, err)
}

func TestLoadAll(t *testing.T) {
	entries, err := codex.LoadAll("testdata/store")
	require.NoError(t, err)
	require.Len(t, entries, 6)

	for _, e := range entries {
		require.NoError(t, e.Err, "entry %s", e.Path)
		require.NotNil(t, e.Session, "entry %s", e.Path)
		require.NotNil(t, e.Report, "entry %s", e.Path)
		abs, aerr := filepath.Abs(e.Path)
		require.NoError(t, aerr)
		require.Equal(t, abs, e.Session.Metadata[codex.MetaNativePath])
		validateEnvelope(t, e.Session)
	}

	// Native ids reused verbatim, one per store file.
	ids := make([]string, 0, len(entries))
	for _, e := range entries {
		ids = append(ids, e.Session.ID)
	}
	require.ElementsMatch(t, []string{
		"01900000-0000-7000-8000-00000000000a",
		"01900000-0000-7000-8000-00000000000b",
		"01900000-0000-7000-8000-00000000000c",
		"01900000-0000-7000-8000-00000000000d",
		"01900000-0000-7000-8000-00000000000e",
		"01900000-0000-7000-8000-00000000000f",
	}, ids)
}

func TestLoadAllMissingRoot(t *testing.T) {
	// A machine without a Codex store is a normal condition: empty
	// result, no error.
	entries, err := codex.LoadAll("testdata/no-such-root")
	require.NoError(t, err)
	require.Empty(t, entries)
}
