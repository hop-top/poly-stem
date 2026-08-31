package copilot

import (
	"fmt"
	"iter"

	"hop.top/stem/adapters"
)

// Result is one normalized session file on the shared adapter seam:
// the envelope plus its parse accounting. Copilot encodes no
// cross-file lineage (resume appends in place, sub-agents run
// inline), so DispatchRefs is always nil; see the package doc.
type Result = adapters.Result

// Adapter is the shared-seam view over the Copilot session store.
// The zero value is ready to use.
type Adapter struct{}

var _ adapters.Adapter = Adapter{}

// Kind implements adapters.Adapter.
func (Adapter) Kind() string { return SourceKind }

// DefaultRoots implements adapters.Adapter. Honors COPILOT_HOME;
// empty when no root can be resolved.
func (Adapter) DefaultRoots() []string {
	root, err := DefaultRoot()
	if err != nil {
		return nil
	}
	return []string{root}
}

// Scan implements adapters.Adapter by discovering and parsing every
// session file under root. Per-file failures surface as (nil, err)
// pairs and the scan continues; a missing root yields nothing.
func (Adapter) Scan(root string) iter.Seq2[*adapters.Result, error] {
	return func(yield func(*adapters.Result, error) bool) {
		entries, err := LoadAll(root)
		if err != nil {
			yield(nil, err)
			return
		}
		for _, e := range entries {
			if e.Err != nil {
				if !yield(nil, fmt.Errorf("%s: %w", e.Path, e.Err)) {
					return
				}
				continue
			}
			if e.Session == nil {
				continue
			}
			if !yield(&adapters.Result{Envelope: e.Session, Account: e.Report}, nil) {
				return
			}
		}
	}
}

// Skipped implements adapters.Accounting: a copy of the per-reason
// drop counts accumulated during the parse.
func (r *Report) Skipped() map[string]int {
	out := make(map[string]int, len(r.Skips))
	for reason, n := range r.Skips {
		out[reason] = n
	}
	return out
}
