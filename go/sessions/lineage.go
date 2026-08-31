package sessions

import (
	"sort"

	stem "hop.top/stem"
)

// Relation values naming how a lineage node attaches to its parent
// (spec §3.5).
const (
	RelationFork     = "fork"
	RelationDispatch = "dispatch"
)

// LineageNode is one member of a continuation chain (spec §3.5):
// which node it attaches to and how, plus the shared summary when the
// envelope was found in a scanned store. Unresolved placeholder
// parents carry Resolved=false with a nil Summary. Attachment fields
// are nil on the chain root.
type LineageNode struct {
	ID        string   `json:"id"`
	Parent    *string  `json:"parent"`
	Relation  *string  `json:"relation"`
	ForkPoint *int     `json:"fork_point"`
	CallID    *string  `json:"call_id"`
	Resolved  bool     `json:"resolved"`
	Summary   *Summary `json:"summary"`
}

// lineageEdge is an envelope's parent reference: the referenced id
// and the crtx relationship it rides on.
type lineageEdge struct {
	parentID  string
	relation  string
	forkPoint *int
	callID    *string
}

// parentEdge extracts an envelope's parent reference. crtx declares
// parent_id/fork_point and dispatched_from mutually exclusive; when
// an invalid envelope carries both, the dispatch edge wins — it is
// the more specific linkage (it names the spawning call).
func parentEdge(env *stem.Session) (lineageEdge, bool) {
	if df := env.DispatchedFrom; df != nil {
		callID := df.CallID
		return lineageEdge{
			parentID: df.EnvelopeID,
			relation: RelationDispatch,
			callID:   &callID,
		}, true
	}
	if env.ParentID != "" {
		return lineageEdge{
			parentID:  env.ParentID,
			relation:  RelationFork,
			forkPoint: env.ForkPoint,
		}, true
	}
	return lineageEdge{}, false
}

// Lineage resolves the continuation chain around target (spec §2.5)
// over the scanned session set: ancestors transitively to the root
// via fork (parent_id/fork_point) and dispatch (dispatched_from)
// references, then the transitive descendants of the target itself —
// children of ancestors (the target's siblings) stay out of scope.
// Nodes come back root first, then breadth-first (spec §3.5), with
// siblings ordered oldest first (id ascending on ties).
//
// A parent reference that resolves to no scanned envelope truncates
// the ancestor walk at an unresolved placeholder root. No id is ever
// visited twice, so resolution terminates on cyclic input; where a
// cycle closes, the last envelope reached renders as the chain root.
func Lineage(recs []*Record, target *Record) []LineageNode {
	byID := make(map[string]*Record, len(recs))
	children := make(map[string][]*Record)
	for _, r := range recs {
		byID[r.Envelope.ID] = r
		if e, ok := parentEdge(r.Envelope); ok {
			children[e.parentID] = append(children[e.parentID], r)
		}
	}
	for _, kids := range children {
		sort.Slice(kids, func(i, j int) bool {
			a, b := kids[i].Envelope, kids[j].Envelope
			if !a.CreatedAt.Equal(b.CreatedAt) {
				return a.CreatedAt.Before(b.CreatedAt)
			}
			return a.ID < b.ID
		})
	}

	// Ancestors: walk parent references to the root. spine holds
	// target..root; placeholder records a parent id no scanned
	// envelope carries.
	spine := []*Record{target}
	seen := map[string]bool{target.Envelope.ID: true}
	placeholder := ""
	for cur := target; ; {
		e, ok := parentEdge(cur.Envelope)
		if !ok || seen[e.parentID] {
			break
		}
		p, found := byID[e.parentID]
		if !found {
			placeholder = e.parentID
			break
		}
		seen[e.parentID] = true
		spine = append(spine, p)
		cur = p
	}

	nodes := make([]LineageNode, 0, len(spine)+1)
	if placeholder != "" {
		nodes = append(nodes, LineageNode{ID: placeholder})
	}
	// Root first: a spine member attaches upward when the node above
	// it is in the chain — always true below the root, true for the
	// root itself only when the placeholder stands in for its parent.
	attached := placeholder != ""
	for i := len(spine) - 1; i >= 0; i-- {
		r := spine[i]
		n := resolvedNode(r)
		if attached {
			e, _ := parentEdge(r.Envelope)
			n.Parent = &e.parentID
			n.Relation = &e.relation
			n.ForkPoint = e.forkPoint
			n.CallID = e.callID
		}
		attached = true
		nodes = append(nodes, n)
	}

	// Descendants: breadth-first transitive closure of the target's
	// children, skipping anything already in the chain.
	queue := []*Record{target}
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		for _, kid := range children[cur.Envelope.ID] {
			if seen[kid.Envelope.ID] {
				continue
			}
			seen[kid.Envelope.ID] = true
			e, _ := parentEdge(kid.Envelope)
			n := resolvedNode(kid)
			n.Parent = &e.parentID
			n.Relation = &e.relation
			n.ForkPoint = e.forkPoint
			n.CallID = e.callID
			nodes = append(nodes, n)
			queue = append(queue, kid)
		}
	}
	return nodes
}

// resolvedNode builds the node for a scanned record, attachment
// fields unset.
func resolvedNode(r *Record) LineageNode {
	s := Summarize(r)
	return LineageNode{ID: r.Envelope.ID, Resolved: true, Summary: &s}
}
