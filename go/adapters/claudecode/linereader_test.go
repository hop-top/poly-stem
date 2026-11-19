package claudecode

import (
	"bufio"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	stem "hop.top/stem"
	"hop.top/stem/adapters"
)

const streamSessionID = "c0ffee00-1111-4222-8333-444455556666"

var _ adapters.LineReader = (*LineReader)(nil)

func readLines(t *testing.T, path string) [][]byte {
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

func TestLineReaderStreamJSON(t *testing.T) {
	at := time.Date(2026, 11, 1, 9, 0, 0, 0, time.UTC)
	r := NewLineReader()
	var perLine [][]stem.Turn
	for i, l := range readLines(t, filepath.Join("testdata", "stream", "live", "session.jsonl")) {
		perLine = append(perLine, r.Feed(l, at.Add(time.Duration(i)*time.Second)))
	}

	counts := make([]int, len(perLine))
	for i, ts := range perLine {
		counts[i] = len(ts)
	}
	// Control, lifecycle, status, init, stream events, results and the
	// compaction boundary yield nothing. Line 1 is the host's stdin
	// copy of the first user message (no timestamp: stamped from at);
	// line 6 is its isReplay echo and must not duplicate it. Line 25
	// is the synthetic compaction summary.
	assert.Equal(t, []int{
		0, 1, 0, 0, 0, 0, 0, 0, 0, 1,
		0, 0, 1, 1, 0, 0, 0, 0, 0, 0,
		1, 1, 0, 0, 0, 0, 0,
	}, counts)

	assert.Equal(t, at.Add(1*time.Second), perLine[1][0].CreatedAt)
	assert.Equal(t, time.Date(2026, 11, 1, 10, 0, 2, 200_000_000, time.UTC), perLine[9][0].CreatedAt)
	assert.Equal(t, stem.RoleTool, perLine[12][0].Role)
	assert.Equal(t, stem.RoleUser, perLine[20][0].Role)

	env, err := r.Envelope()
	require.NoError(t, err)
	require.NotNil(t, env)
	assert.Equal(t, streamSessionID, env.ID)
	assert.Equal(t, SourceKind, env.Source.Kind)
	assert.Equal(t, "2.1.328", env.Source.Version)
	assert.Equal(t, "/work/demo", env.Metadata[MetaCWD])
	require.Len(t, env.Turns, 6)
	require.NoError(t, stem.Validate(env))

	skipped := r.Account().Skipped()
	assert.Equal(t, 2, skipped["record:stream_event"])
	assert.Equal(t, 2, skipped["record:result"])
	assert.Equal(t, 5, skipped["record:system"])
	assert.Equal(t, 7, skipped["record:command_lifecycle"])
	assert.Equal(t, 2, skipped["record:control_response"])
	assert.Equal(t, 1, skipped["record:control_request"])
	assert.Equal(t, 1, skipped["record:user:duplicate"])
	assert.Equal(t, 1, skipped["record:user:synthetic"])
}

// The echo alone (no host stdin copy) is the user turn, with its own
// timestamp.
func TestLineReaderReplayEchoAlone(t *testing.T) {
	r := NewLineReader()
	got := r.Feed([]byte(`{"type":"user","message":{"role":"user","content":"hi"},"session_id":"s1","uuid":"u1","timestamp":"2026-11-01T10:00:01.100Z","isReplay":true}`), time.Time{})
	require.Len(t, got, 1)
	assert.Equal(t, time.Date(2026, 11, 1, 10, 0, 1, 100_000_000, time.UTC), got[0].CreatedAt)
}

func TestLineReaderNoTurnsNoEnvelope(t *testing.T) {
	r := NewLineReader()
	r.Feed([]byte(`{"type":"system","subtype":"init","session_id":"s1"}`), time.Time{})
	env, err := r.Envelope()
	require.NoError(t, err)
	assert.Nil(t, env)
}

func TestLineReaderMissingTimestampNoFallbackSkips(t *testing.T) {
	r := NewLineReader()
	ts := r.Feed([]byte(`{"type":"user","uuid":"u1","message":{"role":"user","content":"hi"}}`), time.Time{})
	assert.Empty(t, ts)
	assert.Equal(t, 1, r.Account().Skipped()["turn_record"])
}

func TestLineReaderWithSessionID(t *testing.T) {
	r := NewLineReader(WithSessionID("seeded"))
	r.Feed([]byte(`{"type":"user","uuid":"u1","message":{"role":"user","content":"hi"}}`), time.Unix(1700000000, 0))
	env, err := r.Envelope()
	require.NoError(t, err)
	assert.Equal(t, "seeded", env.ID)
}

// Feeding a transcript file line by line is the same code path as
// ParseFile; the envelopes must be identical.
func TestLineReaderMatchesParseFile(t *testing.T) {
	path := acmeMainPath(t)
	want, err := ParseFile(path, nil)
	require.NoError(t, err)

	r := NewLineReader(WithNativePath(path))
	for _, l := range readLines(t, path) {
		r.Feed(l, time.Time{})
	}
	got, err := r.Envelope()
	require.NoError(t, err)
	abs, _ := filepath.Abs(path)
	want.Envelope.Metadata[MetaNativePath] = abs
	assert.Equal(t, want.Envelope, got)
}
