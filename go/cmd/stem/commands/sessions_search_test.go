package commands_test

import (
	"sort"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const idRagSidechain = "ab12cd3" // dispatch-nested claude-code sub-agent

// matchIDs extracts the session id of every match, in order.
func matchIDs(t *testing.T, doc map[string]any) []string {
	t.Helper()
	raw, ok := doc["matches"].([]any)
	require.True(t, ok, "matches array missing")
	ids := make([]string, 0, len(raw))
	for _, m := range raw {
		ids = append(ids, m.(map[string]any)["session"].(map[string]any)["id"].(string))
	}
	return ids
}

func findMatch(doc map[string]any, id string) map[string]any {
	raw, _ := doc["matches"].([]any)
	for _, m := range raw {
		mm := m.(map[string]any)
		if mm["session"].(map[string]any)["id"] == id {
			return mm
		}
	}
	return nil
}

func TestSearchJSONContract(t *testing.T) {
	fixtureEnv(t)
	stdout, _, exit := runCLI(t, "sessions", "search", "retrieval", "--format", "json", "--limit", "0")
	require.Zero(t, exit)

	doc := assertContract(t, "search.yaml", stdout)
	assert.Equal(t, "retrieval", doc["query"])
	assert.Equal(t, false, doc["truncated"])

	ids := matchIDs(t, doc)
	assert.Contains(t, ids, idStemMain)
	assert.Contains(t, ids, idNervFork)
	assert.Contains(t, ids, idRagSidechain,
		"dispatch-nested sub-agent transcripts are searched by default")

	// Sessions ordered newest first.
	raw := doc["matches"].([]any)
	updated := make([]string, 0, len(raw))
	for _, m := range raw {
		updated = append(updated, m.(map[string]any)["session"].(map[string]any)["updated_at"].(string))
	}
	assert.True(t, sort.SliceIsSorted(updated, func(i, j int) bool {
		return updated[i] > updated[j]
	}), "matches must be ordered by session updated_at descending: %v", updated)

	// Content hits carry turn coordinates; snippets are collapsed and
	// capped at 200 characters.
	for _, m := range raw {
		hits := m.(map[string]any)["hits"].([]any)
		require.NotEmpty(t, hits)
		for _, h := range hits {
			hit := h.(map[string]any)
			if hit["scope"] == "content" {
				assert.NotNil(t, hit["turn_index"])
				assert.NotNil(t, hit["turn_id"])
				assert.NotNil(t, hit["role"])
			}
			snippet := hit["snippet"].(string)
			assert.LessOrEqual(t, utf8.RuneCountInString(snippet), 200)
			assert.NotContains(t, snippet, "\n", "snippets are whitespace-collapsed")
			assert.Contains(t, strings.ToLower(snippet), "retrieval",
				"snippet must show the matched region")
		}
	}

	// Hits within a session follow turn order.
	main := findMatch(doc, idStemMain)
	require.NotNil(t, main)
	var lastIdx float64 = -1
	for _, h := range main["hits"].([]any) {
		hit := h.(map[string]any)
		if hit["scope"] != "content" {
			continue
		}
		idx := hit["turn_index"].(float64)
		assert.Greater(t, idx, lastIdx, "content hits in turn order")
		lastIdx = idx
	}
}

func TestSearchScopeMetadata(t *testing.T) {
	fixtureEnv(t)

	t.Run("metadata value match", func(t *testing.T) {
		stdout, _, exit := runCLI(t, "sessions", "search", "keep-verbatim",
			"--format", "json", "--scope", "metadata")
		require.Zero(t, exit)
		doc := assertContract(t, "search.yaml", stdout)
		ids := matchIDs(t, doc)
		require.Contains(t, ids, idDup)

		m := findMatch(doc, idDup)
		for _, h := range m["hits"].([]any) {
			hit := h.(map[string]any)
			assert.Equal(t, "metadata", hit["scope"])
			assert.Nil(t, hit["turn_index"], "metadata hits carry null turn coordinates")
			assert.Nil(t, hit["turn_id"])
			assert.Nil(t, hit["role"])
		}
	})

	t.Run("derived title is metadata", func(t *testing.T) {
		stdout, _, exit := runCLI(t, "sessions", "search", "compare retrieval settings",
			"--format", "json", "--scope", "metadata")
		require.Zero(t, exit)
		doc := assertContract(t, "search.yaml", stdout)
		assert.Contains(t, matchIDs(t, doc), idStemMain)
	})

	t.Run("content scope excludes metadata", func(t *testing.T) {
		stdout, _, exit := runCLI(t, "sessions", "search", "keep-verbatim",
			"--format", "json", "--scope", "content")
		require.Zero(t, exit, "zero matches is success")
		doc := assertContract(t, "search.yaml", stdout)
		assert.Empty(t, doc["matches"])
	})

	t.Run("invalid scope is usage error", func(t *testing.T) {
		_, stderr, exit := runCLI(t, "sessions", "search", "x-any", "--format", "json",
			"--scope", "everything")
		assert.Equal(t, 2, exit)
		doc := assertContract(t, "error.yaml", stderr)
		assert.Equal(t, "usage", doc["error"].(map[string]any)["code"])
	})
}

func TestSearchToolScope(t *testing.T) {
	fixtureEnv(t)

	t.Run("excluded by default", func(t *testing.T) {
		stdout, _, exit := runCLI(t, "sessions", "search", "Begin Patch synthetic",
			"--format", "json")
		require.Zero(t, exit)
		doc := assertContract(t, "search.yaml", stdout)
		assert.Empty(t, doc["matches"], "tool_call input stays out of content scope by default")
	})

	t.Run("tool_call input with --tools", func(t *testing.T) {
		stdout, _, exit := runCLI(t, "sessions", "search", "Begin Patch synthetic",
			"--format", "json", "--tools")
		require.Zero(t, exit)
		doc := assertContract(t, "search.yaml", stdout)
		assert.Contains(t, matchIDs(t, doc), idCodexLive)
	})

	t.Run("tool_result output with --tools", func(t *testing.T) {
		stdout, _, exit := runCLI(t, "sessions", "search", "Process exited with code 0",
			"--format", "json", "--tools")
		require.Zero(t, exit)
		doc := assertContract(t, "search.yaml", stdout)
		assert.Contains(t, matchIDs(t, doc), idCodexLive)
	})
}

func TestSearchRoleFilter(t *testing.T) {
	fixtureEnv(t)
	// "retrieval" appears in idStemMain's assistant text; the nerv
	// fork only mentions it in a user turn.
	stdout, _, exit := runCLI(t, "sessions", "search", "retrieval",
		"--format", "json", "--scope", "content", "--role", "assistant", "--limit", "0")
	require.Zero(t, exit)
	doc := assertContract(t, "search.yaml", stdout)
	ids := matchIDs(t, doc)
	assert.Contains(t, ids, idStemMain)
	assert.NotContains(t, ids, idNervFork)

	for _, m := range doc["matches"].([]any) {
		for _, h := range m.(map[string]any)["hits"].([]any) {
			assert.Equal(t, "assistant", h.(map[string]any)["role"])
		}
	}
}

func TestSearchMatchingModes(t *testing.T) {
	fixtureEnv(t)

	t.Run("case-insensitive by default", func(t *testing.T) {
		stdout, _, exit := runCLI(t, "sessions", "search", "RETRIEVAL", "--format", "json", "--limit", "0")
		require.Zero(t, exit)
		doc := assertContract(t, "search.yaml", stdout)
		assert.Contains(t, matchIDs(t, doc), idStemMain)
	})

	t.Run("case-sensitive", func(t *testing.T) {
		stdout, _, exit := runCLI(t, "sessions", "search", "RETRIEVAL",
			"--format", "json", "--case-sensitive", "--limit", "0")
		require.Zero(t, exit)
		doc := assertContract(t, "search.yaml", stdout)
		assert.Empty(t, doc["matches"])
	})

	t.Run("fixed-string default treats regex metachars literally", func(t *testing.T) {
		stdout, _, exit := runCLI(t, "sessions", "search", "bm25|rerank", "--format", "json")
		require.Zero(t, exit)
		doc := assertContract(t, "search.yaml", stdout)
		assert.Empty(t, doc["matches"], "no fixture contains the literal string")
	})

	t.Run("regex alternation", func(t *testing.T) {
		stdout, _, exit := runCLI(t, "sessions", "search", "bm25|rerank",
			"--format", "json", "--regex", "--limit", "0")
		require.Zero(t, exit)
		doc := assertContract(t, "search.yaml", stdout)
		ids := matchIDs(t, doc)
		assert.Contains(t, ids, idStemMain)
		assert.Contains(t, ids, idNervFork)
	})

	t.Run("invalid regex is usage error", func(t *testing.T) {
		stdout, stderr, exit := runCLI(t, "sessions", "search", "(", "--format", "json", "--regex")
		assert.Equal(t, 2, exit)
		assert.Empty(t, strings.TrimSpace(stdout))
		doc := assertContract(t, "error.yaml", stderr)
		assert.Equal(t, "usage", doc["error"].(map[string]any)["code"])
	})
}

func TestSearchSharedFilters(t *testing.T) {
	fixtureEnv(t)
	stdout, _, exit := runCLI(t, "sessions", "search", "retrieval",
		"--format", "json", "--limit", "0", "--since", "2026-08-01T00:00:00Z")
	require.Zero(t, exit)
	doc := assertContract(t, "search.yaml", stdout)
	ids := matchIDs(t, doc)
	assert.Contains(t, ids, idStemMain)
	assert.Contains(t, ids, idNervFork)
	assert.NotContains(t, ids, idRagSidechain, "february session outside the window")
}

func TestSearchLimitTruncates(t *testing.T) {
	fixtureEnv(t)
	stdout, stderr, exit := runCLI(t, "sessions", "search", "retrieval",
		"--format", "json", "--limit", "1")
	require.Zero(t, exit)
	doc := assertContract(t, "search.yaml", stdout)
	assert.Equal(t, true, doc["truncated"])
	ids := matchIDs(t, doc)
	require.Len(t, ids, 1)
	assert.Equal(t, idNervFork, ids[0], "newest matching session first")
	assert.Contains(t, stderr, "truncated")
}

func TestSearchMissingQueryIsUsageError(t *testing.T) {
	fixtureEnv(t)
	stdout, stderr, exit := runCLI(t, "sessions", "search", "--format", "json")
	assert.Equal(t, 2, exit)
	assert.Empty(t, strings.TrimSpace(stdout))
	doc := assertContract(t, "error.yaml", stderr)
	assert.Equal(t, "usage", doc["error"].(map[string]any)["code"])
}

func TestSearchText(t *testing.T) {
	fixtureEnv(t)
	stdout, _, exit := runCLI(t, "sessions", "search", "retrieval", "--limit", "0")
	require.Zero(t, exit)

	assert.Contains(t, stdout, "0198aa00", "session header shows the id prefix")
	assert.NotContains(t, stdout, idStemMain, "full ids stay out of text output")
	assert.Contains(t, stdout, "stem")
	assert.Contains(t, stdout, "[t", "hit lines carry turn coordinates")
	assert.Contains(t, stdout, "user")
	assert.Contains(t, strings.ToLower(stdout), "retrieval")
}
