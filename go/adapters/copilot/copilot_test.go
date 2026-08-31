package copilot_test

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	stem "hop.top/stem"
	"hop.top/stem/adapters"
	"hop.top/stem/adapters/copilot"
)

const (
	modernPath     = "testdata/store/01990000-aaaa-7bbb-8ccc-000000000001/events.jsonl"
	resumePath     = "testdata/store/01990000-aaaa-7bbb-8ccc-000000000003/events.jsonl"
	subagentPath   = "testdata/store/01990000-aaaa-7bbb-8ccc-000000000004/events.jsonl"
	driftPath      = "testdata/store/01990000-aaaa-7bbb-8ccc-000000000005/events.jsonl"
	noStartPath    = "testdata/store/01990000-aaaa-7bbb-8ccc-000000000006/events.jsonl"
	dupModernPath  = "testdata/store/01990000-aaaa-7bbb-8ccc-000000000007/events.jsonl"
	legacyFlatPath = "testdata/store/8fb47a2e-1111-4aaa-9bbb-000000000002.jsonl"
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
	files, err := copilot.Discover("testdata/store")
	require.NoError(t, err)
	require.Equal(t, []string{
		filepath.FromSlash(modernPath),
		filepath.FromSlash(resumePath),
		filepath.FromSlash(subagentPath),
		filepath.FromSlash(driftPath),
		filepath.FromSlash(noStartPath),
		filepath.FromSlash(dupModernPath),
		filepath.FromSlash(legacyFlatPath),
	}, files)
}

func TestDiscoverMissingRoot(t *testing.T) {
	_, err := copilot.Discover("testdata/no-such-root")
	require.Error(t, err)
}

func TestParseFileModern(t *testing.T) {
	s, rep, err := copilot.ParseFile(modernPath)
	require.NoError(t, err)
	require.NotNil(t, rep)

	require.Equal(t, "0.1", s.CrtxVersion)
	require.Equal(t, "01990000-aaaa-7bbb-8ccc-000000000001", s.ID)
	require.Equal(t, ts(t, "2026-08-01T09:00:00.000Z"), s.CreatedAt)
	require.Equal(t, ts(t, "2026-08-01T09:00:10.000Z"), s.UpdatedAt)
	require.Equal(t, stem.Source{Kind: "copilot", Version: "1.0.15"}, s.Source)
	require.Empty(t, s.ParentID)
	require.Nil(t, s.ForkPoint)
	require.Nil(t, s.DispatchedFrom)

	abs, err := filepath.Abs(modernPath)
	require.NoError(t, err)
	require.Equal(t, "/home/dev/acme", s.Metadata[copilot.MetaCwd])
	require.Equal(t, "main", s.Metadata[copilot.MetaGitBranch])
	require.Equal(t, "deadbeefdeadbeefdeadbeefdeadbeefdeadbeef", s.Metadata[copilot.MetaGitCommit])
	require.Equal(t, abs, s.Metadata[copilot.MetaNativePath])
	require.Equal(t, "claude-sonnet-4.5", s.Metadata[copilot.MetaModel])
	require.Equal(t, "acme/api", s.Metadata[copilot.MetaRepository])

	require.Len(t, s.Turns, 5)

	wantRoles := []stem.Role{
		stem.RoleUser, stem.RoleAssistant, stem.RoleAssistant,
		stem.RoleTool, stem.RoleAssistant,
	}
	for i, r := range wantRoles {
		require.Equal(t, r, s.Turns[i].Role, "turn %d role", i)
	}

	// Turn ids reuse the native event ids verbatim.
	require.Equal(t, "a1000000-0000-4000-8000-000000000002", s.Turns[0].ID)
	require.Equal(t, "a1000000-0000-4000-8000-000000000008", s.Turns[4].ID)

	// User turn: displayed content, not transformedContent.
	require.Len(t, s.Turns[0].Content, 1)
	require.Equal(t, stem.PartTypeText, s.Turns[0].Content[0].Type)
	require.Equal(t, "add rate limiting to the api", s.Turns[0].Content[0].Text)
	require.Equal(t, ts(t, "2026-08-01T09:00:02.000Z"), s.Turns[0].CreatedAt)

	// assistant.reasoning becomes a thinking part.
	require.Equal(t, stem.PartTypeThinking, s.Turns[1].Content[0].Type)
	require.Equal(t, "Plan the middleware.", s.Turns[1].Content[0].Text)

	// assistant.message with toolRequests: text part then tool_call.
	require.Len(t, s.Turns[2].Content, 2)
	require.Equal(t, stem.PartTypeText, s.Turns[2].Content[0].Type)
	require.Equal(t, "Adding rate limiting now.", s.Turns[2].Content[0].Text)
	tc := s.Turns[2].Content[1]
	require.Equal(t, stem.PartTypeToolCall, tc.Type)
	require.Equal(t, "call_001", tc.CallID)
	require.Equal(t, "bash", tc.Name)
	require.JSONEq(t, `{"cmd":"ls"}`, string(tc.Input))

	// tool.execution_complete: concise result content as JSON string.
	tr := s.Turns[3].Content[0]
	require.Equal(t, stem.PartTypeToolResult, tr.Type)
	require.Equal(t, "call_001", tr.CallID)
	require.JSONEq(t, `"main.go"`, string(tr.Output))
	require.False(t, tr.IsError)

	require.Equal(t, "Rate limiting added.", s.Turns[4].Content[0].Text)

	// Accounting: the echoing execution_start is a duplicate, host
	// bookkeeping events are counted by type.
	require.Equal(t, 9, rep.Lines)
	require.Equal(t, 5, rep.Turns)
	require.Equal(t, 1, rep.Skips[copilot.SkipDuplicateToolCall])
	require.Equal(t, 1, rep.Skips["event:session.usage_info"])
	require.Equal(t, 1, rep.Skips["event:session.shutdown"])

	validateEnvelope(t, s)
}

func TestParseFileLegacyFlat(t *testing.T) {
	s, rep, err := copilot.ParseFile(legacyFlatPath)
	require.NoError(t, err)

	require.Equal(t, "8fb47a2e-1111-4aaa-9bbb-000000000002", s.ID)
	require.Equal(t, stem.Source{Kind: "copilot", Version: "0.0.339"}, s.Source)
	require.Equal(t, ts(t, "2025-11-10T14:30:00.000Z"), s.CreatedAt)
	require.Equal(t, ts(t, "2025-11-10T14:30:08.000Z"), s.UpdatedAt)

	// Context without git yields no git keys; no model selected.
	require.Equal(t, "/home/dev/legacy", s.Metadata[copilot.MetaCwd])
	require.NotContains(t, s.Metadata, copilot.MetaGitBranch)
	require.NotContains(t, s.Metadata, copilot.MetaGitCommit)
	require.NotContains(t, s.Metadata, copilot.MetaModel)
	require.NotContains(t, s.Metadata, copilot.MetaRepository)

	require.Len(t, s.Turns, 2)
	require.Equal(t, 2, rep.Turns)
	validateEnvelope(t, s)
}

func TestParseFileResume(t *testing.T) {
	s, rep, err := copilot.ParseFile(resumePath)
	require.NoError(t, err)

	// Resume appends to the same file under the same id: one envelope,
	// no fork linkage, updated_at advances past the resume.
	require.Equal(t, "01990000-aaaa-7bbb-8ccc-000000000003", s.ID)
	require.Empty(t, s.ParentID)
	require.Nil(t, s.ForkPoint)
	require.Nil(t, s.DispatchedFrom)
	require.Equal(t, ts(t, "2026-08-02T08:00:00.000Z"), s.CreatedAt)
	require.Equal(t, ts(t, "2026-08-03T10:00:06.000Z"), s.UpdatedAt)

	require.Len(t, s.Turns, 4)
	require.Equal(t, 1, rep.Skips["event:session.resume"])

	// Metadata reflects session start; the resume-time context (which
	// carries a branch) is not folded in.
	require.Equal(t, "/home/dev/resume", s.Metadata[copilot.MetaCwd])
	require.NotContains(t, s.Metadata, copilot.MetaGitBranch)
	require.Equal(t, "gpt-5", s.Metadata[copilot.MetaModel])

	validateEnvelope(t, s)
}

func TestParseFileSubagentInline(t *testing.T) {
	s, rep, err := copilot.ParseFile(subagentPath)
	require.NoError(t, err)

	// Sub-agent activity is recorded inline in the parent's event log;
	// no child envelope, no dispatch linkage.
	require.Equal(t, "01990000-aaaa-7bbb-8ccc-000000000004", s.ID)
	require.Nil(t, s.DispatchedFrom)
	require.Len(t, s.Turns, 6)

	// The dispatching tool_call comes from the assistant message.
	require.Equal(t, stem.PartTypeToolCall, s.Turns[1].Content[1].Type)
	require.Equal(t, "call_sub", s.Turns[1].Content[1].CallID)

	// The sub-agent's own tool call has no assistant.message carrier;
	// its execution_start opens the call (assistant role).
	require.Equal(t, stem.RoleAssistant, s.Turns[2].Role)
	st := s.Turns[2].Content[0]
	require.Equal(t, stem.PartTypeToolCall, st.Type)
	require.Equal(t, "call_t1", st.CallID)
	require.Equal(t, "grep", st.Name)

	require.Equal(t, stem.RoleTool, s.Turns[3].Role)
	require.Equal(t, "call_t1", s.Turns[3].Content[0].CallID)
	require.Equal(t, stem.RoleTool, s.Turns[4].Role)
	require.Equal(t, "call_sub", s.Turns[4].Content[0].CallID)

	require.Equal(t, 1, rep.Skips["event:subagent.started"])
	require.Equal(t, 1, rep.Skips["event:subagent.completed"])

	validateEnvelope(t, s)
}

func TestParseFileDrift(t *testing.T) {
	s, rep, err := copilot.ParseFile(driftPath)
	require.NoError(t, err)

	require.Equal(t, "01990000-aaaa-7bbb-8ccc-000000000005", s.ID)
	require.Equal(t, stem.Source{Kind: "copilot", Version: "9.9.9"}, s.Source)
	require.Equal(t, ts(t, "2026-08-05T09:00:00.000Z"), s.CreatedAt)
	require.Equal(t, ts(t, "2026-08-05T09:00:14.000Z"), s.UpdatedAt)

	// Survivors, in order: user message with attachment, assistant
	// message with readable reasoning, user-requested tool call, its
	// failed result, assistant text next to an invalid tool request,
	// timestamp-less user message, closing assistant message.
	require.Len(t, s.Turns, 7)

	require.Equal(t, stem.RoleUser, s.Turns[0].Role)
	require.Equal(t, "look at this", s.Turns[0].Content[0].Text)

	require.Len(t, s.Turns[1].Content, 2)
	require.Equal(t, stem.PartTypeThinking, s.Turns[1].Content[0].Type)
	require.Equal(t, "thinking aloud", s.Turns[1].Content[0].Text)
	require.Equal(t, "resp", s.Turns[1].Content[1].Text)

	// tool.user_requested maps to a user-role turn carrying the call.
	require.Equal(t, stem.RoleUser, s.Turns[2].Role)
	uc := s.Turns[2].Content[0]
	require.Equal(t, stem.PartTypeToolCall, uc.Type)
	require.Equal(t, "call_u1", uc.CallID)
	require.Equal(t, "shell", uc.Name)
	require.JSONEq(t, `"ls -la"`, string(uc.Input))

	// Failed execution surfaces is_error.
	fr := s.Turns[3].Content[0]
	require.Equal(t, stem.PartTypeToolResult, fr.Type)
	require.Equal(t, "call_u1", fr.CallID)
	require.True(t, fr.IsError)
	require.JSONEq(t, `"boom"`, string(fr.Output))

	// Invalid toolRequests entry dropped; text part survives.
	require.Len(t, s.Turns[4].Content, 1)
	require.Equal(t, "call below", s.Turns[4].Content[0].Text)

	// Timestamp-less event inherits the last seen timestamp.
	require.Equal(t, "timeless", s.Turns[5].Content[0].Text)
	require.Equal(t, ts(t, "2026-08-05T09:00:12.000Z"), s.Turns[5].CreatedAt)

	require.Equal(t, "survived the drift", s.Turns[6].Content[0].Text)

	// Accounting per skip class.
	require.Equal(t, 15, rep.Lines)
	require.Equal(t, 7, rep.Turns)
	require.Equal(t, 1, rep.Skips[copilot.SkipMalformedLine])
	require.Equal(t, 1, rep.Skips["event:holo.sync"])
	require.Equal(t, 1, rep.Skips[copilot.SkipDuplicateStart])
	require.Equal(t, 1, rep.Skips[copilot.SkipAttachment])
	require.Equal(t, 2, rep.Skips[copilot.SkipEmptyMessage])
	require.Equal(t, 1, rep.Skips[copilot.SkipOpaqueReasoning])
	require.Equal(t, 1, rep.Skips[copilot.SkipEmptyReasoning])
	require.Equal(t, 1, rep.Skips[copilot.SkipOrphanToolResult])
	require.Equal(t, 1, rep.Skips[copilot.SkipInvalidToolCall])

	validateEnvelope(t, s)
}

func TestParseFileNoStart(t *testing.T) {
	s, rep, err := copilot.ParseFile(noStartPath)
	require.NoError(t, err)

	// Identity recovered from the store path; version unknown.
	require.Equal(t, "01990000-aaaa-7bbb-8ccc-000000000006", s.ID)
	require.Equal(t, stem.Source{Kind: "copilot", Version: "unknown"}, s.Source)
	require.Equal(t, ts(t, "2026-08-06T11:00:00.000Z"), s.CreatedAt)
	require.Equal(t, ts(t, "2026-08-06T11:00:01.000Z"), s.UpdatedAt)
	require.NotContains(t, s.Metadata, copilot.MetaCwd)
	require.Len(t, s.Turns, 2)
	require.Equal(t, 2, rep.Turns)
	validateEnvelope(t, s)
}

func TestParseReaderWithoutIdentity(t *testing.T) {
	// No session.start and no store path to fall back on: not a session.
	in := `{"id":"e1","timestamp":"2026-08-06T11:00:00.000Z","type":"user.message","data":{"content":"hi"}}`
	_, _, err := copilot.Parse(strings.NewReader(in))
	require.Error(t, err)
}

func TestParseEmptyInput(t *testing.T) {
	_, _, err := copilot.Parse(strings.NewReader(""))
	require.Error(t, err)
}

func TestLoadAll(t *testing.T) {
	entries, err := copilot.LoadAll("testdata/store")
	require.NoError(t, err)
	require.Len(t, entries, 7)

	for _, e := range entries {
		require.NoError(t, e.Err, "entry %s", e.Path)
		require.NotNil(t, e.Session, "entry %s", e.Path)
		require.NotNil(t, e.Report, "entry %s", e.Path)
		abs, aerr := filepath.Abs(e.Path)
		require.NoError(t, aerr)
		require.Equal(t, abs, e.Session.Metadata[copilot.MetaNativePath])
		validateEnvelope(t, e.Session)
	}

	// Native ids reused verbatim, one per store session; the flat
	// duplicate of 0007 is shadowed by the directory form.
	ids := make([]string, 0, len(entries))
	for _, e := range entries {
		ids = append(ids, e.Session.ID)
	}
	require.ElementsMatch(t, []string{
		"01990000-aaaa-7bbb-8ccc-000000000001",
		"01990000-aaaa-7bbb-8ccc-000000000003",
		"01990000-aaaa-7bbb-8ccc-000000000004",
		"01990000-aaaa-7bbb-8ccc-000000000005",
		"01990000-aaaa-7bbb-8ccc-000000000006",
		"01990000-aaaa-7bbb-8ccc-000000000007",
		"8fb47a2e-1111-4aaa-9bbb-000000000002",
	}, ids)
}

func TestLoadAllMissingRoot(t *testing.T) {
	// A machine without a Copilot store is a normal condition: empty
	// result, no error.
	entries, err := copilot.LoadAll("testdata/no-such-root")
	require.NoError(t, err)
	require.Empty(t, entries)
}

func TestAdapterSeam(t *testing.T) {
	var a adapters.Adapter = copilot.Adapter{}
	require.Equal(t, "copilot", a.Kind())

	var results []*adapters.Result
	for res, err := range a.Scan("testdata/store") {
		require.NoError(t, err)
		require.NotNil(t, res.Envelope)
		results = append(results, res)
	}
	require.Len(t, results, 7)

	for _, res := range results {
		require.NotNil(t, res.Account, "accounting must ride every result")
		require.Equal(t, "copilot", res.Envelope.Source.Kind)
		require.Contains(t, res.Envelope.Metadata, adapters.MetaNativePath)
		require.Nil(t, res.DispatchRefs, "copilot never contributes dispatch refs")
		require.Nil(t, res.Envelope.DispatchedFrom)
		require.Empty(t, res.Envelope.ParentID)
	}
}

func TestAdapterSeamMissingRoot(t *testing.T) {
	var a adapters.Adapter = copilot.Adapter{}
	for res, err := range a.Scan(filepath.Join(t.TempDir(), "nope")) {
		t.Fatalf("expected no yields, got (%v, %v)", res, err)
	}
}

func TestAdapterSkippedCopies(t *testing.T) {
	_, rep, err := copilot.ParseFile(driftPath)
	require.NoError(t, err)

	skips := rep.Skipped()
	require.NotEmpty(t, skips)
	for k := range skips {
		skips[k] = -1
	}
	again := rep.Skipped()
	for reason, n := range again {
		require.Positive(t, n, "reason %s", reason)
	}
}

func TestDefaultRoots(t *testing.T) {
	home := t.TempDir()
	t.Setenv("COPILOT_HOME", home)
	require.Equal(t, []string{filepath.Join(home, "session-state")},
		copilot.Adapter{}.DefaultRoots())

	t.Setenv("COPILOT_HOME", "")
	roots := copilot.Adapter{}.DefaultRoots()
	require.Len(t, roots, 1)
	require.True(t, filepath.IsAbs(roots[0]))
	require.Equal(t, filepath.Join(".copilot", "session-state"),
		filepath.Join(filepath.Base(filepath.Dir(roots[0])), filepath.Base(roots[0])))
}
