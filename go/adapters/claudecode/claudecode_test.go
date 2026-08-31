package claudecode

import (
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	stem "hop.top/stem"
)

const (
	acmeSessionID  = "9a1f0e2c-3b4d-4a5e-8f60-712233445566"
	ragSessionID   = "7cc04d10-99aa-4bcd-8eef-334455667788"
	asyncSessionID = "88d7e6f0-1122-4333-8444-99aabbccddee"
	driftID        = "1b7e4f00-2266-4c1a-9d3e-aabbccddeeff"
)

func acmeMainPath(t *testing.T) string {
	t.Helper()
	return filepath.Join("testdata", "projects", "-home-dev-code-acme", acmeSessionID+".jsonl")
}

// requireCrtxClean asserts the envelope passes structural validation
// and that its JSON form strict-decodes as a crtx v0.1 envelope.
func requireCrtxClean(t *testing.T, env *stem.Session) {
	t.Helper()
	require.NoError(t, stem.Validate(env))
	b, err := json.Marshal(env)
	require.NoError(t, err)
	require.NoError(t, stem.ValidateBytes(b))
}

func TestParseFileMainSession(t *testing.T) {
	res, err := ParseFile(acmeMainPath(t), nil)
	require.NoError(t, err)
	require.NotNil(t, res.Envelope)
	env := res.Envelope

	assert.Equal(t, acmeSessionID, env.ID)
	assert.Equal(t, stem.CrtxVersion, env.CrtxVersion)
	assert.Equal(t, SourceKind, env.Source.Kind)
	assert.Equal(t, "2.1.220", env.Source.Version)

	require.Len(t, env.Turns, 7)
	roles := make([]stem.Role, 0, len(env.Turns))
	for _, tn := range env.Turns {
		roles = append(roles, tn.Role)
	}
	assert.Equal(t, []stem.Role{
		stem.RoleUser, stem.RoleAssistant, stem.RoleAssistant,
		stem.RoleTool, stem.RoleAssistant, stem.RoleTool,
		stem.RoleAssistant,
	}, roles)

	// Turn ids are the native record uuids.
	assert.Equal(t, "11111111-1111-4111-8111-000000000001", env.Turns[0].ID)

	// Envelope timestamps bracket the turn timestamps.
	assert.Equal(t, time.Date(2026, 1, 15, 10, 0, 0, 0, time.UTC), env.CreatedAt)
	assert.Equal(t, time.Date(2026, 1, 15, 10, 5, 10, 0, time.UTC), env.UpdatedAt)

	// Content mapping: thinking + text in one assistant turn.
	require.Len(t, env.Turns[1].Content, 2)
	assert.Equal(t, stem.PartTypeThinking, env.Turns[1].Content[0].Type)
	assert.Equal(t, "rate limiter: token bucket per client", env.Turns[1].Content[0].Text)
	assert.Equal(t, "sig001", env.Turns[1].Content[0].Signature)
	assert.Equal(t, stem.PartTypeText, env.Turns[1].Content[1].Type)

	// tool_use -> tool_call.
	require.Len(t, env.Turns[2].Content, 1)
	call := env.Turns[2].Content[0]
	assert.Equal(t, stem.PartTypeToolCall, call.Type)
	assert.Equal(t, "toolu_01AAA", call.CallID)
	assert.Equal(t, "Read", call.Name)
	assert.JSONEq(t, `{"file_path":"/home/dev/code/acme/api/server.go"}`, string(call.Input))

	// string tool_result content -> JSON string output.
	res1 := env.Turns[3].Content[0]
	assert.Equal(t, stem.PartTypeToolResult, res1.Type)
	assert.Equal(t, "toolu_01AAA", res1.CallID)
	var s string
	require.NoError(t, json.Unmarshal(res1.Output, &s))
	assert.Contains(t, s, "package api")

	// Agent dispatch result: array output kept verbatim, child
	// envelope id set from toolUseResult.agentId.
	res2 := env.Turns[5].Content[0]
	assert.Equal(t, stem.PartTypeToolResult, res2.Type)
	assert.Equal(t, "toolu_01BBB", res2.CallID)
	assert.Equal(t, "a1b2c3d4e5f6a7b8c", res2.ChildEnvelopeID)
	assert.JSONEq(t, `[{"type":"text","text":"token bucket via x/time/rate recommended"}]`, string(res2.Output))

	// Adapter metadata contract.
	assert.Equal(t, "/home/dev/code/acme", env.Metadata[MetaCWD])
	assert.Equal(t, "main", env.Metadata[MetaGitBranch])
	abs, err := filepath.Abs(acmeMainPath(t))
	require.NoError(t, err)
	assert.Equal(t, abs, env.Metadata[MetaNativePath])

	// Dispatch refs extracted for sidechain resolution.
	assert.Equal(t, DispatchIndex{
		"a1b2c3d4e5f6a7b8c": {EnvelopeID: acmeSessionID, CallID: "toolu_01BBB"},
	}, res.DispatchRefs)

	// Non-turn records accounted, nothing dropped or malformed.
	assert.Equal(t, map[string]int{
		"mode": 1, "permission-mode": 1, "system": 1,
		"ai-title": 1, "file-history-snapshot": 1, "attachment": 1,
	}, res.Account.SkippedRecords)
	assert.Zero(t, res.Account.MalformedLines)
	assert.Zero(t, res.Account.DroppedParts)
	assert.Zero(t, res.Account.OrphanToolResults)
	assert.Zero(t, res.Account.SkippedTurnRecords)
	assert.False(t, res.Account.DispatchUnresolved)

	requireCrtxClean(t, env)
}

func TestParseFileLegacyStringContentAndMeta(t *testing.T) {
	path := filepath.Join("testdata", "projects", "-home-dev-experiments-rag", ragSessionID+".jsonl")
	res, err := ParseFile(path, nil)
	require.NoError(t, err)
	require.NotNil(t, res.Envelope)
	env := res.Envelope

	assert.Equal(t, ragSessionID, env.ID)
	assert.Equal(t, "2.0.77", env.Source.Version)

	// isMeta caveat record skipped; five real turns remain.
	require.Len(t, env.Turns, 5)
	assert.Equal(t, map[string]int{"user:meta": 1}, res.Account.SkippedRecords)

	// Legacy string message.content -> one text part.
	first := env.Turns[0]
	assert.Equal(t, stem.RoleUser, first.Role)
	require.Len(t, first.Content, 1)
	assert.Equal(t, stem.PartTypeText, first.Content[0].Type)
	assert.Equal(t, "compare retrieval settings", first.Content[0].Text)

	// Empty gitBranch never emits the metadata key.
	_, ok := env.Metadata[MetaGitBranch]
	assert.False(t, ok)
	assert.Equal(t, "/home/dev/experiments/rag", env.Metadata[MetaCWD])

	assert.Equal(t, DispatchIndex{
		"ab12cd3": {EnvelopeID: ragSessionID, CallID: "toolu_03AAA"},
	}, res.DispatchRefs)

	requireCrtxClean(t, env)
}

func TestParseFileSidechainResolved(t *testing.T) {
	path := filepath.Join("testdata", "projects", "-home-dev-code-acme",
		acmeSessionID, "subagents", "agent-a1b2c3d4e5f6a7b8c.jsonl")
	idx := DispatchIndex{
		"a1b2c3d4e5f6a7b8c": {EnvelopeID: acmeSessionID, CallID: "toolu_01BBB"},
	}
	res, err := ParseFile(path, idx)
	require.NoError(t, err)
	require.NotNil(t, res.Envelope)
	env := res.Envelope

	// Envelope id is the native agent id, verbatim.
	assert.Equal(t, "a1b2c3d4e5f6a7b8c", env.ID)
	require.NotNil(t, env.DispatchedFrom)
	assert.Equal(t, acmeSessionID, env.DispatchedFrom.EnvelopeID)
	assert.Equal(t, "toolu_01BBB", env.DispatchedFrom.CallID)
	assert.Empty(t, env.ParentID)
	assert.Nil(t, env.ForkPoint)
	assert.False(t, res.Account.DispatchUnresolved)

	require.Len(t, env.Turns, 4)
	for _, tn := range env.Turns {
		assert.Equal(t, "a1b2c3d4e5f6a7b8c", tn.AgentID)
	}

	requireCrtxClean(t, env)
}

func TestParseFileSidechainUnresolved(t *testing.T) {
	path := filepath.Join("testdata", "projects", "-home-dev-experiments-rag", "agent-0fa11ba.jsonl")
	res, err := ParseFile(path, nil)
	require.NoError(t, err)
	require.NotNil(t, res.Envelope)
	env := res.Envelope

	assert.Equal(t, "0fa11ba", env.ID)
	assert.Nil(t, env.DispatchedFrom)
	assert.True(t, res.Account.DispatchUnresolved)
	require.Len(t, env.Turns, 2)

	requireCrtxClean(t, env)
}

// Background agents leave no toolUseResult.agentId in the parent;
// the linkage arrives later as a <task-notification> naming both the
// agent id and the spawning tool call — injected as a user record
// when the session was live, or stranded in a queue-operation record
// when it ended first. Both encodings must resolve.
func TestParseFileAsyncTaskNotification(t *testing.T) {
	path := filepath.Join("testdata", "projects", "-home-dev-svc-async", asyncSessionID+".jsonl")
	res, err := ParseFile(path, nil)
	require.NoError(t, err)
	require.NotNil(t, res.Envelope)

	assert.Equal(t, DispatchIndex{
		"acafe12345678beef": {EnvelopeID: asyncSessionID, CallID: "toolu_04AAA"},
		"a0ddba11feed0123f": {EnvelopeID: asyncSessionID, CallID: "toolu_04BBB"},
	}, res.DispatchRefs)

	// The injected notification stays in the transcript as a text
	// turn; the queued one is a non-turn record.
	require.Len(t, res.Envelope.Turns, 7)
	assert.Equal(t, stem.RoleUser, res.Envelope.Turns[5].Role)
	assert.Contains(t, res.Envelope.Turns[5].Content[0].Text, "<task-notification>")
	assert.Equal(t, map[string]int{"queue-operation": 1}, res.Account.SkippedRecords)

	requireCrtxClean(t, res.Envelope)
}

func TestParseFileDrift(t *testing.T) {
	path := filepath.Join("testdata", "drift", driftID+".jsonl")
	res, err := ParseFile(path, nil)
	require.NoError(t, err)
	require.NotNil(t, res.Envelope)
	env := res.Envelope

	assert.Equal(t, driftID, env.ID)
	// No native version field anywhere -> placeholder keeps the
	// envelope schema-valid (source.version is required).
	assert.Equal(t, "unknown", env.Source.Version)

	// Survivors: good text, unknown-block-with-text, orphan-with-text,
	// future-top-level-field, image, thinking.
	require.Len(t, env.Turns, 6)

	acct := res.Account
	assert.Equal(t, 1, acct.MalformedLines)
	assert.Equal(t, map[string]int{"quantum-checkpoint": 1}, acct.SkippedRecords)
	// missing uuid + bad timestamp + orphan-only + empty content.
	assert.Equal(t, 4, acct.SkippedTurnRecords)
	assert.Equal(t, 1, acct.DroppedParts)
	assert.Equal(t, 2, acct.OrphanToolResults)

	// Unknown sibling block dropped, text kept.
	require.Len(t, env.Turns[1].Content, 1)
	assert.Equal(t, "kept text next to unknown block", env.Turns[1].Content[0].Text)

	// Orphan tool_result dropped, sibling text kept, role stays user.
	require.Len(t, env.Turns[2].Content, 1)
	assert.Equal(t, stem.RoleUser, env.Turns[2].Role)
	assert.Equal(t, "also said this", env.Turns[2].Content[0].Text)

	// Image block mapped.
	img := env.Turns[4].Content[0]
	assert.Equal(t, stem.PartTypeImage, img.Type)
	assert.Equal(t, "image/png", img.Mime)
	assert.Equal(t, "aGVsbG8=", img.Data)

	// Thinking-only assistant turn mapped.
	think := env.Turns[5].Content[0]
	assert.Equal(t, stem.PartTypeThinking, think.Type)
	assert.Equal(t, "quiet consideration", think.Text)
	assert.Equal(t, "sigZ", think.Signature)

	assert.Equal(t, time.Date(2026, 3, 1, 8, 0, 0, 0, time.UTC), env.CreatedAt)
	assert.Equal(t, time.Date(2026, 3, 1, 8, 0, 12, 0, time.UTC), env.UpdatedAt)

	requireCrtxClean(t, env)
}

func TestParseFileNoTurns(t *testing.T) {
	path := filepath.Join("testdata", "projects", "-home-dev-experiments-rag",
		"11e0aa22-bb33-4cc4-8dd5-556677889900.jsonl")
	res, err := ParseFile(path, nil)
	require.NoError(t, err)
	assert.Nil(t, res.Envelope)
	assert.Equal(t, map[string]int{"mode": 1, "last-prompt": 1}, res.Account.SkippedRecords)
}

func TestScanStore(t *testing.T) {
	root := filepath.Join("testdata", "projects")
	got := map[string]*Result{}
	for res, err := range Scan(root) {
		require.NoError(t, err)
		require.NotNil(t, res.Envelope)
		got[res.Envelope.ID] = res
	}

	// Three mains, four resolved sidechains, one orphan sidechain;
	// the zero-turn file is skipped.
	require.Len(t, got, 8)
	for id, res := range got {
		requireCrtxClean(t, res.Envelope)
		assert.Equal(t, SourceKind, res.Envelope.Source.Kind, id)
	}

	// Nested-layout sidechain resolved via the acme main's dispatch refs.
	nested := got["a1b2c3d4e5f6a7b8c"].Envelope
	require.NotNil(t, nested.DispatchedFrom)
	assert.Equal(t, acmeSessionID, nested.DispatchedFrom.EnvelopeID)
	assert.Equal(t, "toolu_01BBB", nested.DispatchedFrom.CallID)

	// Flat-layout sidechain resolved via the rag main.
	flat := got["ab12cd3"].Envelope
	require.NotNil(t, flat.DispatchedFrom)
	assert.Equal(t, ragSessionID, flat.DispatchedFrom.EnvelopeID)
	assert.Equal(t, "toolu_03AAA", flat.DispatchedFrom.CallID)

	// Async sidechains resolved via task-notifications: one injected
	// as a user turn, one stranded in a queue-operation record.
	async := got["acafe12345678beef"].Envelope
	require.NotNil(t, async.DispatchedFrom)
	assert.Equal(t, asyncSessionID, async.DispatchedFrom.EnvelopeID)
	assert.Equal(t, "toolu_04AAA", async.DispatchedFrom.CallID)
	queued := got["a0ddba11feed0123f"].Envelope
	require.NotNil(t, queued.DispatchedFrom)
	assert.Equal(t, asyncSessionID, queued.DispatchedFrom.EnvelopeID)
	assert.Equal(t, "toolu_04BBB", queued.DispatchedFrom.CallID)

	// Orphan sidechain yields an envelope without linkage.
	orphan := got["0fa11ba"]
	assert.Nil(t, orphan.Envelope.DispatchedFrom)
	assert.True(t, orphan.Account.DispatchUnresolved)

	// Parent-side forward pointer to the dispatch nest survives Scan.
	acme := got[acmeSessionID].Envelope
	assert.Equal(t, "a1b2c3d4e5f6a7b8c", acme.Turns[5].Content[0].ChildEnvelopeID)
}

func TestScanMissingRoot(t *testing.T) {
	n := 0
	for _, err := range Scan(filepath.Join("testdata", "does-not-exist")) {
		require.NoError(t, err)
		n++
	}
	assert.Zero(t, n)
}
