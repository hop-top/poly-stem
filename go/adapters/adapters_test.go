package adapters_test

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"hop.top/stem/adapters"
	"hop.top/stem/adapters/claudecode"
	"hop.top/stem/adapters/codex"
	"hop.top/stem/adapters/copilot"
)

// collect drains a seam scan, failing the test on per-file errors.
func collect(t *testing.T, a adapters.Adapter, root string) []*adapters.Result {
	t.Helper()
	var out []*adapters.Result
	for res, err := range a.Scan(root) {
		require.NoError(t, err)
		require.NotNil(t, res.Envelope)
		out = append(out, res)
	}
	return out
}

func TestSeamClaudeCode(t *testing.T) {
	var a adapters.Adapter = claudecode.Adapter{}
	assert.Equal(t, "claude-code", a.Kind())

	results := collect(t, a, filepath.Join("claudecode", "testdata", "projects"))
	require.NotEmpty(t, results)

	var dispatchNested int
	for _, res := range results {
		require.NotNil(t, res.Account, "accounting must ride every result")
		assert.Equal(t, "claude-code", res.Envelope.Source.Kind)
		assert.Contains(t, res.Envelope.Metadata, adapters.MetaNativePath)
		if res.Envelope.DispatchedFrom != nil {
			dispatchNested++
		}
	}
	assert.NotZero(t, dispatchNested,
		"sidechain fixtures must surface dispatched_from through the seam")
}

func TestSeamCodex(t *testing.T) {
	var a adapters.Adapter = codex.Adapter{}
	assert.Equal(t, "codex", a.Kind())

	results := collect(t, a, filepath.Join("codex", "testdata", "store"))
	require.NotEmpty(t, results)

	var forked int
	for _, res := range results {
		require.NotNil(t, res.Account)
		assert.Equal(t, "codex", res.Envelope.Source.Kind)
		assert.Nil(t, res.DispatchRefs, "codex never contributes dispatch refs")
		if res.Envelope.ParentID != "" {
			forked++
		}
	}
	assert.NotZero(t, forked,
		"fork fixtures must surface parent_id through the seam")
}

func TestSeamCopilot(t *testing.T) {
	var a adapters.Adapter = copilot.Adapter{}
	assert.Equal(t, "copilot", a.Kind())

	results := collect(t, a, filepath.Join("copilot", "testdata", "store"))
	require.NotEmpty(t, results)

	for _, res := range results {
		require.NotNil(t, res.Account, "accounting must ride every result")
		assert.Equal(t, "copilot", res.Envelope.Source.Kind)
		assert.Contains(t, res.Envelope.Metadata, adapters.MetaNativePath)
		assert.Nil(t, res.DispatchRefs, "copilot never contributes dispatch refs")
		assert.Nil(t, res.Envelope.DispatchedFrom,
			"copilot encodes no cross-file lineage; sub-agents run inline")
		assert.Empty(t, res.Envelope.ParentID,
			"copilot resume continues in place; no fork exists to represent")
	}
}

func TestSeamMissingRootYieldsNothing(t *testing.T) {
	for _, a := range []adapters.Adapter{claudecode.Adapter{}, codex.Adapter{}, copilot.Adapter{}} {
		for res, err := range a.Scan(filepath.Join(t.TempDir(), "nope")) {
			t.Fatalf("%s: expected no yields, got (%v, %v)", a.Kind(), res, err)
		}
	}
}

func TestSeamAccountingSkips(t *testing.T) {
	// The claudecode drift fixture drops records for several reasons;
	// the seam view must expose them per reason. The store layout is
	// root/<project-dir>/*.jsonl, so the scan root is testdata and the
	// drift directory is the project dir.
	var a adapters.Adapter = claudecode.Adapter{}
	results := collect(t, a, filepath.Join("claudecode", "testdata"))
	require.Len(t, results, 1)
	skips := results[0].Account.Skipped()
	assert.NotEmpty(t, skips, "drift fixture must account its drops")

	// Mutating the returned map must not corrupt the accounting.
	for k := range skips {
		skips[k] = -1
	}
	again := results[0].Account.Skipped()
	for reason, n := range again {
		assert.Positive(t, n, "reason %s", reason)
	}
}

func TestSeamDefaultRoots(t *testing.T) {
	t.Setenv("CODEX_HOME", t.TempDir())
	t.Setenv("COPILOT_HOME", t.TempDir())
	for _, a := range []adapters.Adapter{claudecode.Adapter{}, codex.Adapter{}, copilot.Adapter{}} {
		roots := a.DefaultRoots()
		require.NotEmpty(t, roots, "%s: default roots on a host with a home dir", a.Kind())
		for _, r := range roots {
			assert.True(t, filepath.IsAbs(r), "%s: root %q must be absolute", a.Kind(), r)
		}
	}
}
