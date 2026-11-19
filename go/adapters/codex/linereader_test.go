package codex_test

import (
	"bufio"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	stem "hop.top/stem"
	"hop.top/stem/adapters"
	"hop.top/stem/adapters/codex"
)

const appThreadID = "019a0000-0000-7000-8000-0000000000aa"

var _ adapters.LineReader = (*codex.LineReader)(nil)

func fileLines(t *testing.T, path string) [][]byte {
	t.Helper()
	f, err := os.Open(path)
	require.NoError(t, err)
	defer f.Close()
	var out [][]byte
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 1<<20), 1<<20)
	for sc.Scan() {
		out = append(out, append([]byte(nil), sc.Bytes()...))
	}
	require.NoError(t, sc.Err())
	return out
}

func TestLineReaderAppServer(t *testing.T) {
	r := codex.NewLineReader()
	var turns []stem.Turn
	var perLine []int
	for _, l := range fileLines(t, "testdata/appserver/thread.jsonl") {
		got := r.Feed(l, time.Time{})
		perLine = append(perLine, len(got))
		turns = append(turns, got...)
	}
	// response, thread/started, turn/started, item/started, user item,
	// reasoning delta, reasoning item, item/started, output delta,
	// command item (call + result), 2 deltas, agent item, turn/completed.
	assert.Equal(t, []int{0, 0, 0, 0, 1, 0, 1, 0, 0, 2, 0, 0, 1, 0}, perLine)

	require.Len(t, turns, 5)
	assert.Equal(t, stem.RoleUser, turns[0].Role)
	assert.Equal(t, "item-1", turns[0].ID)
	assert.Equal(t, time.UnixMilli(1795000001010).UTC(), turns[0].CreatedAt)
	assert.Equal(t, stem.PartTypeThinking, turns[1].Content[0].Type)
	assert.Equal(t, stem.PartTypeToolCall, turns[2].Content[0].Type)
	assert.Equal(t, "item-3", turns[2].Content[0].CallID)
	assert.JSONEq(t, `{"command":"ls","cwd":"/work/demo"}`, string(turns[2].Content[0].Input))
	assert.Equal(t, stem.RoleTool, turns[3].Role)
	assert.Equal(t, "item-3", turns[3].Content[0].CallID)
	assert.Equal(t, "Two files: a.txt, b.txt.", turns[4].Content[0].Text)

	env, err := r.Envelope()
	require.NoError(t, err)
	assert.Equal(t, appThreadID, env.ID)
	assert.Equal(t, "0.169.0", env.Source.Version)
	assert.Equal(t, "/work/demo", env.Metadata[codex.MetaCwd])
	require.Len(t, env.Turns, 5)
	validateEnvelope(t, env)

	skips := r.Account().Skipped()
	assert.Equal(t, 4, skips[codex.SkipDelta])
	assert.Equal(t, 5, skips[codex.SkipNotification])
}

func TestLineReaderAppServerUnknownItemAndOrphan(t *testing.T) {
	r := codex.NewLineReader(codex.WithSessionID(appThreadID))
	got := r.Feed([]byte(`{"jsonrpc":"2.0","method":"item/completed","params":{"threadId":"x","turnId":"t","completedAtMs":1795000000000,"item":{"type":"sleep","id":"i","durationMs":5}}}`), time.Time{})
	assert.Empty(t, got)
	assert.Equal(t, 1, r.Account().Skipped()[codex.SkipUnknownItem])
	env, err := r.Envelope()
	require.NoError(t, err)
	assert.Empty(t, env.Turns)
}

func TestLineReaderNoIdentity(t *testing.T) {
	r := codex.NewLineReader()
	r.Feed([]byte(`{"jsonrpc":"2.0","method":"item/completed","params":{"completedAtMs":1795000000000,"item":{"type":"agentMessage","id":"a","text":"hi"}}}`), time.Time{})
	_, err := r.Envelope()
	require.ErrorIs(t, err, codex.ErrNoSession)
}

func TestLineReaderAppServerTimestampFallback(t *testing.T) {
	at := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
	r := codex.NewLineReader(codex.WithSessionID(appThreadID))
	got := r.Feed([]byte(`{"jsonrpc":"2.0","method":"item/completed","params":{"threadId":"x","turnId":"t","item":{"type":"agentMessage","id":"a","text":"hi"}}}`), at)
	require.Len(t, got, 1)
	assert.Equal(t, at, got[0].CreatedAt)
}

// Rollout lines through the line reader equal Parse output.
func TestLineReaderRolloutMatchesParseFile(t *testing.T) {
	want, _, err := codex.ParseFile(modernPath)
	require.NoError(t, err)

	r := codex.NewLineReader(codex.WithNativePath(want.Metadata[codex.MetaNativePath].(string)))
	for _, l := range fileLines(t, modernPath) {
		r.Feed(l, time.Time{})
	}
	got, err := r.Envelope()
	require.NoError(t, err)
	assert.Equal(t, want, got)
}
