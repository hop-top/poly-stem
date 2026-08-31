package commands_test

import (
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	idNervFork   = "0198bb00-2222-7abc-8def-aabbccddeeff"
	idStemMain   = "0198aa00-1111-7abc-8def-123456789abc"
	idStemSubdir = "0198dd00-4444-7abc-8def-000000000002"
	idStemDeep   = "0198ee00-5555-7abc-8def-000000000003"
	idDriftVer   = "0198cc00-3333-7abc-8def-000000000001"
	idDup        = "9a1f0e2c-3b4d-4a5e-8f60-712233445566"
	idCodexLive  = "01900000-0000-7000-8000-00000000000a"
	idCodexOld   = "01900000-0000-7000-8000-00000000000b"
	idCCAsync    = "88d7e6f0-1122-4333-8444-99aabbccddee"
	idCopilot    = "0199aa00-6666-7abc-8def-0000000000c1"
)

func TestListJSONContract(t *testing.T) {
	fixtureEnv(t)
	stdout, _, exit := runCLI(t, "sessions", "list", "--format", "json", "--limit", "0")
	require.Zero(t, exit)

	doc := assertContract(t, "list.yaml", stdout)
	assert.Equal(t, false, doc["truncated"])

	ids := sessionIDs(t, doc)
	require.NotEmpty(t, ids)

	// Newest first: updated_at descending across every source.
	raw := doc["sessions"].([]any)
	updated := make([]string, 0, len(raw))
	for _, s := range raw {
		updated = append(updated, s.(map[string]any)["updated_at"].(string))
	}
	assert.True(t, sort.SliceIsSorted(updated, func(i, j int) bool {
		return updated[i] > updated[j]
	}), "sessions must be ordered by updated_at descending: %v", updated)

	// Every source class is represented: crtx-native producers plus
	// all three native-store adapters.
	kinds := map[string]bool{}
	for _, s := range raw {
		kinds[s.(map[string]any)["source"].(map[string]any)["kind"].(string)] = true
	}
	for _, k := range []string{"stem", "nerv", "codex", "claude-code", "copilot"} {
		assert.True(t, kinds[k], "missing source kind %s", k)
	}

	// Dispatch-nested sessions are excluded by default.
	for _, s := range raw {
		assert.Nil(t, s.(map[string]any)["dispatched_from"],
			"session %v must not be dispatch-nested in default list", s.(map[string]any)["id"])
	}

	// Fork children stay in the default list.
	fork := findSession(doc, idNervFork)
	require.NotNil(t, fork, "fork child must be listed by default")
	assert.Equal(t, idStemMain, fork["parent_id"])
	assert.Equal(t, float64(1), fork["fork_point"])
	assert.Equal(t, "main", fork["git_branch"])
	assert.Equal(t, "/home/dev/acme", fork["cwd"])

	// Streaming files: the last non-empty line is authoritative.
	main := findSession(doc, idStemMain)
	require.NotNil(t, main)
	assert.Equal(t, float64(2), main["turn_count"])
	assert.Equal(t, "2026-08-05T18:02:00Z", main["updated_at"])
	assert.Equal(t, "compare retrieval settings", main["title"])

	// One-level subdirectories are scanned; deeper nesting is not.
	assert.NotNil(t, findSession(doc, idStemSubdir))
	assert.Nil(t, findSession(doc, idStemDeep))

	// Non-0.1 envelopes are skipped.
	assert.Nil(t, findSession(doc, idDriftVer))
}

func TestListDedupCrtxWins(t *testing.T) {
	fixtureEnv(t)
	stdout, _, exit := runCLI(t, "sessions", "list", "--format", "json", "--limit", "0")
	require.Zero(t, exit)
	doc := assertContract(t, "list.yaml", stdout)

	var hits []map[string]any
	for _, s := range doc["sessions"].([]any) {
		m := s.(map[string]any)
		if m["id"] == idDup {
			hits = append(hits, m)
		}
	}
	require.Len(t, hits, 1, "duplicate id must collapse to one session")
	assert.Equal(t, "stem", hits[0]["source"].(map[string]any)["kind"],
		"crtx-native envelope must win over the adapter-normalized one")
	store := hits[0]["store"].(map[string]any)
	assert.Equal(t, "crtx", store["adapter"])
	assert.Contains(t, store["path"].(string), idDup+".jsonl")
}

func TestListProvenance(t *testing.T) {
	fixtureEnv(t)
	stdout, _, exit := runCLI(t, "sessions", "list", "--format", "json", "--limit", "0")
	require.Zero(t, exit)
	doc := assertContract(t, "list.yaml", stdout)

	codex := findSession(doc, idCodexLive)
	require.NotNil(t, codex)
	store := codex["store"].(map[string]any)
	assert.Equal(t, "codex", store["adapter"])
	assert.Contains(t, store["path"].(string), "rollout-2026-08-01T10-00-00")

	copilot := findSession(doc, idCopilot)
	require.NotNil(t, copilot, "copilot store must be scanned")
	assert.Equal(t, "copilot", copilot["source"].(map[string]any)["kind"])
	assert.Equal(t, "profile the thumbnail worker", copilot["title"])
	assert.Equal(t, "/home/dev/tooling", copilot["cwd"])
	store = copilot["store"].(map[string]any)
	assert.Equal(t, "copilot", store["adapter"])
	assert.Contains(t, store["path"].(string), filepath.Join(idCopilot, "events.jsonl"))
}

func TestListNested(t *testing.T) {
	fixtureEnv(t)
	stdout, _, exit := runCLI(t, "sessions", "list", "--format", "json", "--limit", "0", "--nested")
	require.Zero(t, exit)
	doc := assertContract(t, "list.yaml", stdout)

	var nested int
	for _, s := range doc["sessions"].([]any) {
		if s.(map[string]any)["dispatched_from"] != nil {
			nested++
		}
	}
	assert.NotZero(t, nested, "--nested must include dispatch-nest children")
}

func TestListLimitTruncates(t *testing.T) {
	fixtureEnv(t)
	stdout, stderr, exit := runCLI(t, "sessions", "list", "--format", "json", "--limit", "3")
	require.Zero(t, exit)
	doc := assertContract(t, "list.yaml", stdout)

	assert.Equal(t, true, doc["truncated"])
	ids := sessionIDs(t, doc)
	require.Len(t, ids, 3)
	assert.Equal(t, []string{idNervFork, idDup, idStemMain}, ids,
		"top three sessions by updated_at")
	assert.Contains(t, stderr, "truncated", "truncation notice goes to stderr")
}

func TestListFilters(t *testing.T) {
	fixtureEnv(t)

	t.Run("since", func(t *testing.T) {
		stdout, _, exit := runCLI(t, "sessions", "list", "--format", "json", "--limit", "0",
			"--since", "2026-08-01T00:00:00Z")
		require.Zero(t, exit)
		doc := assertContract(t, "list.yaml", stdout)
		ids := sessionIDs(t, doc)
		assert.Contains(t, ids, idStemMain)
		assert.Contains(t, ids, idCodexLive)
		assert.NotContains(t, ids, idCodexOld, "2025 session outside the window")
		assert.NotContains(t, ids, idCCAsync, "april session outside the window")
	})

	t.Run("until", func(t *testing.T) {
		stdout, _, exit := runCLI(t, "sessions", "list", "--format", "json", "--limit", "0",
			"--until", "2026-01-01T00:00:00Z")
		require.Zero(t, exit)
		doc := assertContract(t, "list.yaml", stdout)
		ids := sessionIDs(t, doc)
		assert.Contains(t, ids, idCodexOld)
		assert.NotContains(t, ids, idStemMain)
	})

	t.Run("cwd subtree", func(t *testing.T) {
		stdout, _, exit := runCLI(t, "sessions", "list", "--format", "json", "--limit", "0",
			"--cwd", "/home/dev/acme")
		require.Zero(t, exit)
		doc := assertContract(t, "list.yaml", stdout)
		ids := sessionIDs(t, doc)
		assert.Contains(t, ids, idNervFork, "exact cwd match")
		assert.Contains(t, ids, idStemMain, "subtree cwd match")
		assert.Contains(t, ids, idCodexLive, "codex session in /home/dev/acme")
		assert.NotContains(t, ids, idDup,
			"/home/dev/dup is outside the subtree")
		for _, id := range ids {
			s := findSession(doc, id)
			cwd, _ := s["cwd"].(string)
			assert.True(t, cwd == "/home/dev/acme" || strings.HasPrefix(cwd, "/home/dev/acme/"),
				"id %s cwd %q escapes the subtree (segment boundary)", id, cwd)
		}
	})

	t.Run("source", func(t *testing.T) {
		stdout, _, exit := runCLI(t, "sessions", "list", "--format", "json", "--limit", "0",
			"--source", "codex")
		require.Zero(t, exit)
		doc := assertContract(t, "list.yaml", stdout)
		for _, s := range doc["sessions"].([]any) {
			assert.Equal(t, "codex", s.(map[string]any)["source"].(map[string]any)["kind"])
		}
		assert.NotEmpty(t, doc["sessions"])
	})

	t.Run("empty result is success", func(t *testing.T) {
		stdout, _, exit := runCLI(t, "sessions", "list", "--format", "json",
			"--source", "no-such-tool")
		require.Zero(t, exit)
		doc := assertContract(t, "list.yaml", stdout)
		assert.Empty(t, doc["sessions"])
	})
}

func TestListText(t *testing.T) {
	fixtureEnv(t)
	stdout, stderr, exit := runCLI(t, "sessions", "list", "--limit", "0")
	require.Zero(t, exit)

	assert.Contains(t, stdout, "0198bb00", "ids render as 8-char prefixes")
	assert.NotContains(t, stdout, idNervFork, "full ids stay out of text output")
	assert.Contains(t, stdout, "compare retrieval settings", "derived title")
	assert.Contains(t, stdout, "claude-code")
	assert.Contains(t, stderr, "crtx_version",
		"version-skip warning for the 0.9 envelope goes to stderr")
}

func TestListBadSinceIsUsageError(t *testing.T) {
	fixtureEnv(t)
	stdout, stderr, exit := runCLI(t, "sessions", "list", "--format", "json", "--since", "not-a-time")
	assert.Equal(t, 2, exit)
	assert.Empty(t, strings.TrimSpace(stdout), "stdout stays empty on failure")
	doc := assertContract(t, "error.yaml", stderr)
	assert.Equal(t, "usage", doc["error"].(map[string]any)["code"])
}

func TestListBadFormatIsUsageError(t *testing.T) {
	fixtureEnv(t)
	_, _, exit := runCLI(t, "sessions", "list", "--format", "yaml")
	assert.Equal(t, 2, exit)
}
