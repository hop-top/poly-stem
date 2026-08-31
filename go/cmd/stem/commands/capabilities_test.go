package commands_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// capabilitiesJSON runs `capabilities --format json` and decodes the
// single stdout document.
func capabilitiesJSON(t *testing.T) map[string]any {
	t.Helper()
	stdout, stderr, exit := runCLI(t, "capabilities", "--format", "json")
	require.Equal(t, 0, exit)
	require.Empty(t, stderr)
	var doc map[string]any
	require.NoError(t, json.Unmarshal([]byte(stdout), &doc),
		"stdout is not one JSON document: %q", stdout)
	return doc
}

func leafByPath(t *testing.T, doc map[string]any, path string) map[string]any {
	t.Helper()
	raw, ok := doc["leaves"].([]any)
	require.True(t, ok, "leaves array missing")
	for _, l := range raw {
		m := l.(map[string]any)
		if m["path"] == path {
			return m
		}
	}
	return nil
}

func TestCapabilitiesJSONDocument(t *testing.T) {
	doc := capabilitiesJSON(t)

	require.Equal(t, "stem", doc["tool"])
	require.Equal(t, "test", doc["version"])
	require.Equal(t, "1.0", doc["schema_version"])

	for _, path := range []string{
		"stem sessions list",
		"stem sessions show",
		"stem sessions search",
		"stem sessions lineage",
		"stem capabilities",
	} {
		require.NotNil(t, leafByPath(t, doc, path), "leaf %q missing", path)
	}
}

// The whole sessions surface is read-only: every projected leaf must
// declare side_effect=read, idempotent=yes, and (derived from that
// contract) no dry-run requirement.
func TestCapabilitiesLeavesDeclareReadOnlyContract(t *testing.T) {
	doc := capabilitiesJSON(t)
	leaves, ok := doc["leaves"].([]any)
	require.True(t, ok, "leaves array missing")
	require.NotEmpty(t, leaves)

	for _, l := range leaves {
		m := l.(map[string]any)
		require.Equal(t, "read", m["side_effect"], "leaf %v", m["path"])
		require.Equal(t, "yes", m["idempotent"], "leaf %v", m["path"])
		require.Equal(t, false, m["dry_run_supported"], "leaf %v", m["path"])
	}
}

// Per-leaf flags must include each leaf's own --format declaration
// (spec §2: every sessions leaf owns its format contract).
func TestCapabilitiesListsPerLeafFormatFlag(t *testing.T) {
	doc := capabilitiesJSON(t)
	leaf := leafByPath(t, doc, "stem sessions list")
	require.NotNil(t, leaf)

	names := []string{}
	for _, f := range leaf["flags"].([]any) {
		names = append(names, f.(map[string]any)["name"].(string))
	}
	require.Contains(t, names, "format")
	require.Contains(t, names, "limit")
}

func TestCapabilitiesTextTable(t *testing.T) {
	stdout, stderr, exit := runCLI(t, "capabilities")
	require.Equal(t, 0, exit)
	require.Empty(t, stderr)
	require.True(t, strings.HasPrefix(stdout, "PATH"), "missing header: %q", stdout)
	require.Contains(t, stdout, "stem sessions list")
	require.Contains(t, stdout, "read")
}

func TestCapabilitiesRejectsUnknownFormat(t *testing.T) {
	stdout, stderr, exit := runCLI(t, "capabilities", "--format", "xml")
	require.Equal(t, 2, exit)
	require.Empty(t, stdout)
	require.Contains(t, stderr, "invalid --format")
}
