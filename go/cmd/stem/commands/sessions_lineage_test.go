package commands_test

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Lineage fixture universe (testdata/lineage): a three-level
// cross-source fork chain with a dispatch nest and a sibling branch,
// plus an unresolved parent, a two-member parent cycle, and a
// standalone session. Kept separate from the shared fixture universe
// so list/search expectations stay untouched.
//
//	root (stem)
//	└─ fork @t2   mid (nerv)
//	   ├─ fork @t1   leaf (stem)          ◀ chain under test
//	   │  └─ dispatch (tc_0031)   nest (stem)
//	   └─ fork @t2   sib (stem)
const (
	idLinRoot    = "0198f100-aaaa-7abc-8def-00000000000a"
	idLinMid     = "0198f200-bbbb-7abc-8def-00000000000b"
	idLinLeaf    = "0198f300-cccc-7abc-8def-00000000000c"
	idLinNest    = "0198f400-dddd-7abc-8def-00000000000d"
	idLinSib     = "0198f500-eeee-7abc-8def-00000000000e"
	idLinOrphan  = "0198f600-ffff-7abc-8def-00000000000f"
	idLinMissing = "0198dead-beef-7abc-8def-000000000000"
	idLinCycA    = "0198f700-1111-7abc-8def-000000000010"
	idLinCycB    = "0198f800-2222-7abc-8def-000000000011"
	idLinCycNest = "0198f900-3333-7abc-8def-000000000012"
	idLinSolo    = "0198fa00-4444-7abc-8def-000000000013"
)

// Adapter-universe lineage ids (shared fixtures under go/adapters).
const (
	idCodexFork       = "01900000-0000-7000-8000-00000000000c"
	idCodexForkParent = "01900000-0000-7000-8000-0000000000aa"
	idCCSidechain     = "a1b2c3d4e5f6a7b8c"
)

// lineageEnv points the crtx roots at the lineage fixture universe
// and every native-store adapter at a nonexistent directory.
func lineageEnv(t *testing.T) {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("testdata", "lineage"))
	require.NoError(t, err)
	none := filepath.Join(t.TempDir(), "none")
	t.Setenv("STEM_CRTX_DIRS",
		filepath.Join(root, "stem", "sessions")+":"+filepath.Join(root, "nerv", "sessions"))
	t.Setenv("STEM_CLAUDECODE_DIRS", none)
	t.Setenv("STEM_CODEX_DIRS", none)
	t.Setenv("STEM_COPILOT_DIRS", none)
}

// lineageNodes decodes and shape-checks the node array of a lineage
// document.
func lineageNodes(t *testing.T, doc map[string]any) []map[string]any {
	t.Helper()
	raw, ok := doc["nodes"].([]any)
	require.True(t, ok, "nodes array missing")
	nodes := make([]map[string]any, 0, len(raw))
	for _, n := range raw {
		m, ok := n.(map[string]any)
		require.True(t, ok, "node is not an object")
		nodes = append(nodes, m)
	}
	return nodes
}

func nodeIDs(nodes []map[string]any) []string {
	ids := make([]string, 0, len(nodes))
	for _, n := range nodes {
		ids = append(ids, n["id"].(string))
	}
	return ids
}

func TestLineageChainJSON(t *testing.T) {
	lineageEnv(t)
	stdout, _, exit := runCLI(t, "sessions", "lineage", idLinLeaf, "--format", "json")
	require.Zero(t, exit)
	doc := assertContract(t, "lineage.yaml", stdout)

	assert.Equal(t, idLinLeaf, doc["target"])
	nodes := lineageNodes(t, doc)
	require.Equal(t, []string{idLinRoot, idLinMid, idLinLeaf, idLinNest}, nodeIDs(nodes),
		"root first, then breadth-first down to the dispatch nest")
	assert.NotContains(t, nodeIDs(nodes), idLinSib,
		"siblings of the target are out of scope")

	root := nodes[0]
	assert.Nil(t, root["parent"])
	assert.Nil(t, root["relation"])
	assert.Nil(t, root["fork_point"])
	assert.Nil(t, root["call_id"])
	assert.Equal(t, true, root["resolved"])
	require.NotNil(t, root["summary"])
	assert.Equal(t, "stem", root["summary"].(map[string]any)["source"].(map[string]any)["kind"])

	mid := nodes[1]
	assert.Equal(t, idLinRoot, mid["parent"])
	assert.Equal(t, "fork", mid["relation"])
	assert.EqualValues(t, 2, mid["fork_point"])
	assert.Nil(t, mid["call_id"])
	assert.Equal(t, "nerv", mid["summary"].(map[string]any)["source"].(map[string]any)["kind"],
		"ancestors may live in a different source than descendants")

	leaf := nodes[2]
	assert.Equal(t, idLinMid, leaf["parent"])
	assert.Equal(t, "fork", leaf["relation"])
	assert.EqualValues(t, 1, leaf["fork_point"])

	nest := nodes[3]
	assert.Equal(t, idLinLeaf, nest["parent"])
	assert.Equal(t, "dispatch", nest["relation"])
	assert.Nil(t, nest["fork_point"])
	assert.Equal(t, "tc_0031", nest["call_id"])
}

func TestLineageDescendantsBFS(t *testing.T) {
	lineageEnv(t)
	stdout, _, exit := runCLI(t, "sessions", "lineage", idLinRoot, "--format", "json")
	require.Zero(t, exit)
	doc := assertContract(t, "lineage.yaml", stdout)

	assert.Equal(t, idLinRoot, doc["target"])
	nodes := lineageNodes(t, doc)
	require.Equal(t, []string{idLinRoot, idLinMid, idLinLeaf, idLinSib, idLinNest}, nodeIDs(nodes),
		"transitive descendants of the target in breadth-first order, siblings oldest first")
	assert.Equal(t, idLinMid, nodes[2]["parent"])
	assert.Equal(t, idLinMid, nodes[3]["parent"])
	assert.Equal(t, idLinLeaf, nodes[4]["parent"])
	assert.Equal(t, "dispatch", nodes[4]["relation"])
}

func TestLineageUnresolvedParent(t *testing.T) {
	lineageEnv(t)
	stdout, _, exit := runCLI(t, "sessions", "lineage", idLinOrphan, "--format", "json")
	require.Zero(t, exit)
	doc := assertContract(t, "lineage.yaml", stdout)

	nodes := lineageNodes(t, doc)
	require.Equal(t, []string{idLinMissing, idLinOrphan}, nodeIDs(nodes))

	placeholder := nodes[0]
	assert.Equal(t, false, placeholder["resolved"])
	assert.Nil(t, placeholder["summary"])
	assert.Nil(t, placeholder["parent"])
	assert.Nil(t, placeholder["relation"])

	orphan := nodes[1]
	assert.Equal(t, idLinMissing, orphan["parent"])
	assert.Equal(t, "fork", orphan["relation"])
	assert.EqualValues(t, 5, orphan["fork_point"])
	assert.Equal(t, true, orphan["resolved"])
}

func TestLineageCycleTerminates(t *testing.T) {
	lineageEnv(t)
	stdout, _, exit := runCLI(t, "sessions", "lineage", idLinCycA, "--format", "json")
	require.Zero(t, exit, "cycle in parent references must terminate, not hang or fail")
	doc := assertContract(t, "lineage.yaml", stdout)

	nodes := lineageNodes(t, doc)
	ids := nodeIDs(nodes)
	require.Equal(t, []string{idLinCycB, idLinCycA, idLinCycNest}, ids,
		"walk truncates where the cycle closes; every id appears exactly once")

	assert.Nil(t, nodes[0]["parent"], "cycle-truncated root renders as the chain root")
	assert.Equal(t, idLinCycB, nodes[1]["parent"])
	assert.Equal(t, idLinCycA, nodes[2]["parent"])
	assert.Equal(t, "tc_0099", nodes[2]["call_id"])
}

func TestLineageSingleNode(t *testing.T) {
	lineageEnv(t)
	stdout, _, exit := runCLI(t, "sessions", "lineage", idLinSolo, "--format", "json")
	require.Zero(t, exit)
	doc := assertContract(t, "lineage.yaml", stdout)

	nodes := lineageNodes(t, doc)
	require.Len(t, nodes, 1, "a session with no lineage is its own one-node chain")
	assert.Equal(t, idLinSolo, nodes[0]["id"])
	assert.Equal(t, idLinSolo, doc["target"])
	assert.Nil(t, nodes[0]["parent"])
	assert.Nil(t, nodes[0]["relation"])
	assert.Equal(t, true, nodes[0]["resolved"])
}

func TestLineageCrossCLIFork(t *testing.T) {
	fixtureEnv(t)
	stdout, _, exit := runCLI(t, "sessions", "lineage", idNervFork, "--format", "json")
	require.Zero(t, exit)
	doc := assertContract(t, "lineage.yaml", stdout)

	nodes := lineageNodes(t, doc)
	require.Equal(t, []string{idStemMain, idNervFork}, nodeIDs(nodes))
	assert.Equal(t, "stem", nodes[0]["summary"].(map[string]any)["source"].(map[string]any)["kind"])
	assert.Equal(t, "nerv", nodes[1]["summary"].(map[string]any)["source"].(map[string]any)["kind"])
	assert.Equal(t, "fork", nodes[1]["relation"])
	assert.EqualValues(t, 1, nodes[1]["fork_point"])
}

func TestLineageDispatchNestAcrossStores(t *testing.T) {
	fixtureEnv(t)
	stdout, _, exit := runCLI(t, "sessions", "lineage", idCCSidechain, "--format", "json")
	require.Zero(t, exit)
	doc := assertContract(t, "lineage.yaml", stdout)

	nodes := lineageNodes(t, doc)
	require.Equal(t, []string{idDup, idCCSidechain}, nodeIDs(nodes),
		"dispatch ancestor resolves across stores")

	parent := nodes[0]
	assert.Equal(t, "crtx", parent["summary"].(map[string]any)["store"].(map[string]any)["adapter"],
		"parent resolves to the deduped crtx-native envelope")

	child := nodes[1]
	assert.Equal(t, "dispatch", child["relation"])
	assert.Equal(t, "toolu_01BBB", child["call_id"])
	assert.Nil(t, child["fork_point"])
	sum := child["summary"].(map[string]any)
	assert.Equal(t, "claude-code", sum["source"].(map[string]any)["kind"])
	require.NotNil(t, sum["dispatched_from"])
	assert.Equal(t, idDup, sum["dispatched_from"].(map[string]any)["envelope_id"])
}

func TestLineageUnresolvedParentCodex(t *testing.T) {
	fixtureEnv(t)
	stdout, _, exit := runCLI(t, "sessions", "lineage", idCodexFork, "--format", "json")
	require.Zero(t, exit)
	doc := assertContract(t, "lineage.yaml", stdout)

	nodes := lineageNodes(t, doc)
	require.Equal(t, []string{idCodexForkParent, idCodexFork}, nodeIDs(nodes))
	assert.Equal(t, false, nodes[0]["resolved"])
	assert.Nil(t, nodes[0]["summary"])
	assert.Equal(t, "fork", nodes[1]["relation"])
	assert.EqualValues(t, 0, nodes[1]["fork_point"],
		"codex materializes inherited history, fork point 0")
}

func TestLineageNotFound(t *testing.T) {
	lineageEnv(t)
	stdout, stderr, exit := runCLI(t, "sessions", "lineage", "ffffffff", "--format", "json")
	assert.Equal(t, 3, exit)
	assert.Empty(t, strings.TrimSpace(stdout))
	doc := assertContract(t, "error.yaml", stderr)
	assert.Equal(t, "not_found", doc["error"].(map[string]any)["code"])
}

func TestLineageAmbiguousPrefix(t *testing.T) {
	fixtureEnv(t)
	stdout, stderr, exit := runCLI(t, "sessions", "lineage", "01900000", "--format", "json")
	assert.Equal(t, 4, exit)
	assert.Empty(t, strings.TrimSpace(stdout))
	doc := assertContract(t, "error.yaml", stderr)

	errObj := doc["error"].(map[string]any)
	assert.Equal(t, "ambiguous_id", errObj["code"])
	details, ok := errObj["details"].(map[string]any)
	require.True(t, ok, "ambiguous error must carry details")
	candidates, ok := details["candidates"].([]any)
	require.True(t, ok, "details.candidates must be structured")
	assert.GreaterOrEqual(t, len(candidates), 2)
	assert.LessOrEqual(t, len(candidates), 10)
}

func TestLineageShortPrefixIsUsageError(t *testing.T) {
	lineageEnv(t)
	_, stderr, exit := runCLI(t, "sessions", "lineage", "019", "--format", "json")
	assert.Equal(t, 2, exit)
	doc := assertContract(t, "error.yaml", stderr)
	assert.Equal(t, "usage", doc["error"].(map[string]any)["code"])
}

func TestLineageMissingArgIsUsageError(t *testing.T) {
	lineageEnv(t)
	_, stderr, exit := runCLI(t, "sessions", "lineage", "--format", "json")
	assert.Equal(t, 2, exit)
	doc := assertContract(t, "error.yaml", stderr)
	assert.Equal(t, "usage", doc["error"].(map[string]any)["code"])
}

func TestLineageText(t *testing.T) {
	lineageEnv(t)
	stdout, _, exit := runCLI(t, "sessions", "lineage", idLinLeaf)
	require.Zero(t, exit)

	lines := strings.Split(strings.TrimRight(stdout, "\n"), "\n")
	require.NotEmpty(t, lines)
	assert.True(t, strings.HasPrefix(lines[0], idLinRoot[:8]),
		"tree renders root first: %q", lines[0])

	assert.Contains(t, stdout, "└─ fork @t2")
	assert.Contains(t, stdout, "fork @t1")
	assert.Contains(t, stdout, "dispatch (call tc_0031)")
	assert.NotContains(t, stdout, idLinSib[:8], "sibling stays out of the tree")
	assert.NotContains(t, stdout, idLinLeaf, "text renders id prefixes, not full ids")

	var targetLine string
	for _, l := range lines {
		if strings.Contains(l, "◀ target") {
			targetLine = l
		}
	}
	require.NotEmpty(t, targetLine, "queried session is marked")
	assert.Contains(t, targetLine, idLinLeaf[:8])
}

func TestLineageTextUnresolved(t *testing.T) {
	lineageEnv(t)
	stdout, _, exit := runCLI(t, "sessions", "lineage", idLinOrphan)
	require.Zero(t, exit)
	assert.Contains(t, stdout, idLinMissing[:8])
	assert.Contains(t, stdout, "unresolved")
}
