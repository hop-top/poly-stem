package projection_test

import (
	"os"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	stem "hop.top/stem"
	"hop.top/stem/projection"
)

func records(t *testing.T, path string) []projection.Record {
	t.Helper()
	b, err := os.ReadFile(path)
	require.NoError(t, err)
	base := time.Date(2026, 11, 1, 10, 0, 0, 0, time.UTC)
	var out []projection.Record
	for i, l := range strings.Split(strings.TrimSpace(string(b)), "\n") {
		out = append(out, projection.Record{
			Seq:  uint64(i + 1),
			At:   base.Add(time.Duration(i) * time.Second),
			Line: []byte(l),
		})
	}
	return out
}

const claudeFixture = "../adapters/claudecode/testdata/stream/session.jsonl"
const codexFixture = "../adapters/codex/testdata/appserver/thread.jsonl"

func TestProjectClaude(t *testing.T) {
	p, err := projection.New("claude-code")
	require.NoError(t, err)
	for _, rec := range records(t, claudeFixture) {
		_, err := p.Apply(rec)
		require.NoError(t, err)
	}
	assert.Equal(t, uint64(11), p.Position())
	env, err := p.Envelope()
	require.NoError(t, err)
	require.Len(t, env.Turns, 5)
	require.NoError(t, stem.Validate(env))
}

func TestProjectCodex(t *testing.T) {
	p, err := projection.New("codex")
	require.NoError(t, err)
	for _, rec := range records(t, codexFixture) {
		_, err := p.Apply(rec)
		require.NoError(t, err)
	}
	env, err := p.Envelope()
	require.NoError(t, err)
	require.Len(t, env.Turns, 5)
	require.NoError(t, stem.Validate(env))
}

func TestSinceCursor(t *testing.T) {
	p, err := projection.New("claude-code")
	require.NoError(t, err)
	recs := records(t, claudeFixture)
	for _, rec := range recs {
		_, err := p.Apply(rec)
		require.NoError(t, err)
	}
	all := p.Since(0)
	require.Len(t, all, 5)
	// Turns produced by seq 3 (user), 7, 8, 9, 10.
	assert.Len(t, p.Since(3), 4)
	assert.Len(t, p.Since(9), 1)
	assert.Empty(t, p.Since(p.Position()))
}

func TestApplyRejectsOutOfOrder(t *testing.T) {
	p, err := projection.New("claude-code")
	require.NoError(t, err)
	recs := records(t, claudeFixture)
	_, err = p.Apply(recs[2])
	require.NoError(t, err)
	_, err = p.Apply(recs[2])
	require.ErrorIs(t, err, projection.ErrOutOfOrder)
	_, err = p.Apply(recs[1])
	require.ErrorIs(t, err, projection.ErrOutOfOrder)
	assert.Equal(t, uint64(3), p.Position())
}

func TestRecordSessionIDSeedsIdentity(t *testing.T) {
	p, err := projection.New("claude-code")
	require.NoError(t, err)
	_, err = p.Apply(projection.Record{
		SessionID: "journal-sess", Seq: 1, At: time.Unix(1700000000, 0),
		Line: []byte(`{"type":"user","uuid":"u1","message":{"role":"user","content":"hi"}}`),
	})
	require.NoError(t, err)
	env, err := p.Envelope()
	require.NoError(t, err)
	assert.Equal(t, "journal-sess", env.ID)
}

func TestNewUnknownKind(t *testing.T) {
	_, err := projection.New("gemini")
	require.ErrorIs(t, err, projection.ErrUnknownKind)
}

func TestProjectHelper(t *testing.T) {
	p, err := projection.Project("codex", records(t, codexFixture))
	require.NoError(t, err)
	assert.Equal(t, uint64(14), p.Position())
}
