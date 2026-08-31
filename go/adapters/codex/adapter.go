package codex

import (
	"fmt"
	"iter"

	"hop.top/stem/adapters"
)

// Result is one normalized rollout file on the shared adapter seam:
// the envelope plus its parse accounting. Codex encodes continuation
// as forks only, so DispatchRefs is always nil (dispatched_from is
// never emitted; see the package doc).
type Result = adapters.Result

// Adapter is the shared-seam view over the Codex rollout store. The
// zero value is ready to use.
type Adapter struct{}

var _ adapters.Adapter = Adapter{}

// Kind implements adapters.Adapter.
func (Adapter) Kind() string { return SourceKind }

// DefaultRoots implements adapters.Adapter. Honors CODEX_HOME; empty
// when no root can be resolved.
func (Adapter) DefaultRoots() []string {
	root, err := DefaultRoot()
	if err != nil {
		return nil
	}
	return []string{root}
}

// Scan implements adapters.Adapter by discovering and parsing every
// rollout file under root. Per-file failures surface as (nil, err)
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
