package commands_test

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// lastNonEmptyLine returns the authoritative envelope line of a crtx
// session file.
func lastNonEmptyLine(t *testing.T, path string) string {
	t.Helper()
	f, err := os.Open(path)
	require.NoError(t, err)
	defer f.Close()
	var last string
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1024*1024), 1024*1024)
	for sc.Scan() {
		if strings.TrimSpace(sc.Text()) != "" {
			last = sc.Text()
		}
	}
	require.NoError(t, sc.Err())
	require.NotEmpty(t, last)
	return last
}

func TestShowJSONVerbatimCrtxEnvelope(t *testing.T) {
	fixtureEnv(t)
	stdout, _, exit := runCLI(t, "sessions", "show", idDup, "--format", "json")
	require.Zero(t, exit)

	// Dedup must resolve to the crtx-native envelope, and the emitted
	// document must be the stored line byte-for-byte (plus trailing
	// newline) — the fixture's field order differs from Go's marshal
	// order, so any re-serialization would be caught here.
	raw := lastNonEmptyLine(t, filepath.Join("testdata", "crtx", "stem", "sessions", idDup+".jsonl"))
	assert.Equal(t, raw+"\n", stdout, "show --format json must emit the envelope verbatim")

	assertContract(t, "show.yaml", stdout)
}

func TestShowJSONAdapterEnvelope(t *testing.T) {
	fixtureEnv(t)
	stdout, _, exit := runCLI(t, "sessions", "show", idCodexLive, "--format", "json")
	require.Zero(t, exit)
	doc := assertContract(t, "show.yaml", stdout)
	assert.Equal(t, idCodexLive, doc["id"])
	assert.Equal(t, "codex", doc["source"].(map[string]any)["kind"])
	assert.NotEmpty(t, doc["turns"])
}

func TestShowPrefixResolution(t *testing.T) {
	fixtureEnv(t)

	t.Run("unique 8-char prefix", func(t *testing.T) {
		stdout, _, exit := runCLI(t, "sessions", "show", "0198bb00", "--format", "json")
		require.Zero(t, exit)
		doc := assertContract(t, "show.yaml", stdout)
		assert.Equal(t, idNervFork, doc["id"])
	})

	t.Run("case-insensitive prefix", func(t *testing.T) {
		stdout, _, exit := runCLI(t, "sessions", "show", "0198BB00", "--format", "json")
		require.Zero(t, exit)
		doc := assertContract(t, "show.yaml", stdout)
		assert.Equal(t, idNervFork, doc["id"])
	})

	t.Run("full id", func(t *testing.T) {
		stdout, _, exit := runCLI(t, "sessions", "show", idStemMain, "--format", "json")
		require.Zero(t, exit)
		doc := assertContract(t, "show.yaml", stdout)
		assert.Equal(t, idStemMain, doc["id"])
	})
}

func TestShowNotFound(t *testing.T) {
	fixtureEnv(t)
	stdout, stderr, exit := runCLI(t, "sessions", "show", "ffffffff", "--format", "json")
	assert.Equal(t, 3, exit)
	assert.Empty(t, strings.TrimSpace(stdout))
	doc := assertContract(t, "error.yaml", stderr)
	assert.Equal(t, "not_found", doc["error"].(map[string]any)["code"])
}

func TestShowAmbiguousPrefix(t *testing.T) {
	fixtureEnv(t)
	stdout, stderr, exit := runCLI(t, "sessions", "show", "01900000", "--format", "json")
	assert.Equal(t, 4, exit)
	assert.Empty(t, strings.TrimSpace(stdout))
	doc := assertContract(t, "error.yaml", stderr)

	errObj := doc["error"].(map[string]any)
	assert.Equal(t, "ambiguous_id", errObj["code"])
	assert.Contains(t, errObj["message"], "01900000")

	details, ok := errObj["details"].(map[string]any)
	require.True(t, ok, "ambiguous error must carry details")
	candidates, ok := details["candidates"].([]any)
	require.True(t, ok, "details.candidates must be structured")
	assert.GreaterOrEqual(t, len(candidates), 2)
	assert.LessOrEqual(t, len(candidates), 10)

	// Candidates reuse the SessionSummary shape pinned by list.yaml:
	// validate by wrapping them into a list document.
	for _, cand := range candidates {
		m := cand.(map[string]any)
		assert.Contains(t, m, "id")
		assert.Contains(t, m, "source")
		assert.Contains(t, m, "store")
	}
}

func TestShowShortPrefixIsUsageError(t *testing.T) {
	fixtureEnv(t)
	_, stderr, exit := runCLI(t, "sessions", "show", "9a1", "--format", "json")
	assert.Equal(t, 2, exit)
	doc := assertContract(t, "error.yaml", stderr)
	assert.Equal(t, "usage", doc["error"].(map[string]any)["code"])
}

func TestShowMissingArgIsUsageError(t *testing.T) {
	fixtureEnv(t)
	_, stderr, exit := runCLI(t, "sessions", "show", "--format", "json")
	assert.Equal(t, 2, exit)
	doc := assertContract(t, "error.yaml", stderr)
	assert.Equal(t, "usage", doc["error"].(map[string]any)["code"])
}

func TestShowText(t *testing.T) {
	fixtureEnv(t)
	stdout, _, exit := runCLI(t, "sessions", "show", idNervFork)
	require.Zero(t, exit)

	assert.Contains(t, stdout, idNervFork, "header carries the full id")
	assert.Contains(t, stdout, "nerv")
	assert.Contains(t, stdout, "/home/dev/acme")
	assert.Contains(t, stdout, "main", "git branch in header")
	assert.Contains(t, stdout, idStemMain, "fork lineage pointer in header")
	assert.Contains(t, stdout, "user")
	assert.Contains(t, stdout, "rerank wins when latency is off the table")
	assert.NotContains(t, stdout, "weigh rerank latency against quality",
		"thinking parts render collapsed, not inline")
}

func TestShowTextNotFoundExitCode(t *testing.T) {
	fixtureEnv(t)
	stdout, stderr, exit := runCLI(t, "sessions", "show", "ffffffff")
	assert.Equal(t, 3, exit)
	assert.Empty(t, strings.TrimSpace(stdout))
	assert.Contains(t, stderr, "ffffffff")
}
