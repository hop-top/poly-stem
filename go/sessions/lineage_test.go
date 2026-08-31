package sessions

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	stem "hop.top/stem"
)

// forkOf marks r as a fork of parentID at fp.
func forkOf(r *Record, parentID string, fp int) *Record {
	r.Envelope.ParentID = parentID
	r.Envelope.ForkPoint = &fp
	return r
}

// dispatchOf marks r as a dispatch nest spawned by callID in parentID.
func dispatchOf(r *Record, parentID, callID string) *Record {
	r.Envelope.DispatchedFrom = &stem.DispatchedFrom{EnvelopeID: parentID, CallID: callID}
	return r
}

func lineageIDs(nodes []LineageNode) []string {
	ids := make([]string, 0, len(nodes))
	for _, n := range nodes {
		ids = append(ids, n.ID)
	}
	return ids
}

func TestLineageDispatchWinsOverForkOnInvalidEnvelope(t *testing.T) {
	// crtx declares parent_id/fork_point and dispatched_from mutually
	// exclusive; an invalid envelope carrying both must degrade
	// deterministically to the dispatch edge.
	forkParent := rec("aaaa0000-0000-7000-8000-000000000001", "", "2026-08-01T08:00:00Z", "2026-08-01T08:00:00Z")
	dispParent := rec("bbbb0000-0000-7000-8000-000000000002", "", "2026-08-01T09:00:00Z", "2026-08-01T09:00:00Z")
	child := dispatchOf(
		forkOf(rec("cccc0000-0000-7000-8000-000000000003", "", "2026-08-01T10:00:00Z", "2026-08-01T10:00:00Z"),
			forkParent.Envelope.ID, 1),
		dispParent.Envelope.ID, "tc_0001")

	nodes := Lineage([]*Record{forkParent, dispParent, child}, child)
	require.Equal(t, []string{dispParent.Envelope.ID, child.Envelope.ID}, lineageIDs(nodes))
	require.NotNil(t, nodes[1].Relation)
	assert.Equal(t, RelationDispatch, *nodes[1].Relation)
	require.NotNil(t, nodes[1].CallID)
	assert.Equal(t, "tc_0001", *nodes[1].CallID)
}

func TestLineageSelfParentTerminates(t *testing.T) {
	self := forkOf(rec("dddd0000-0000-7000-8000-000000000004", "", "2026-08-01T08:00:00Z", "2026-08-01T08:00:00Z"),
		"dddd0000-0000-7000-8000-000000000004", 1)

	nodes := Lineage([]*Record{self}, self)
	require.Equal(t, []string{self.Envelope.ID}, lineageIDs(nodes),
		"a self-referencing parent yields a single-node chain")
	assert.Nil(t, nodes[0].Parent)
	assert.Nil(t, nodes[0].Relation)
	assert.True(t, nodes[0].Resolved)
}

func TestLineageSiblingOrderDeterministic(t *testing.T) {
	root := rec("eeee0000-0000-7000-8000-000000000005", "", "2026-08-01T08:00:00Z", "2026-08-01T08:00:00Z")
	// Same created_at: id ascending breaks the tie.
	kidB := forkOf(rec("ffff0000-0000-7000-8000-00000000000b", "", "2026-08-02T08:00:00Z", "2026-08-02T08:00:00Z"), root.Envelope.ID, 1)
	kidA := forkOf(rec("ffff0000-0000-7000-8000-00000000000a", "", "2026-08-02T08:00:00Z", "2026-08-02T08:00:00Z"), root.Envelope.ID, 1)
	// Older created_at sorts first regardless of id.
	kidC := forkOf(rec("0000ffff-0000-7000-8000-00000000000c", "", "2026-08-01T09:00:00Z", "2026-08-01T09:00:00Z"), root.Envelope.ID, 1)

	nodes := Lineage([]*Record{root, kidB, kidA, kidC}, root)
	assert.Equal(t, []string{
		root.Envelope.ID,
		kidC.Envelope.ID,
		kidA.Envelope.ID,
		kidB.Envelope.ID,
	}, lineageIDs(nodes), "children ordered oldest first, id ascending on ties")
}
