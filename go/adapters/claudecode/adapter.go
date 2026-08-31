package claudecode

import (
	"iter"

	"hop.top/stem/adapters"
)

// Adapter is the shared-seam view over the Claude Code store. The
// zero value is ready to use.
type Adapter struct{}

var _ adapters.Adapter = Adapter{}

// Kind implements adapters.Adapter.
func (Adapter) Kind() string { return SourceKind }

// DefaultRoots implements adapters.Adapter. Empty when the home
// directory cannot be resolved.
func (Adapter) DefaultRoots() []string {
	root, err := DefaultRoot()
	if err != nil {
		return nil
	}
	return []string{root}
}

// Scan implements adapters.Adapter by delegating to the package-level
// Scan and lifting each Result onto the shared seam type.
func (Adapter) Scan(root string) iter.Seq2[*adapters.Result, error] {
	return func(yield func(*adapters.Result, error) bool) {
		for res, err := range Scan(root) {
			if err != nil {
				if !yield(nil, err) {
					return
				}
				continue
			}
			shared := &adapters.Result{
				Envelope:     res.Envelope,
				Account:      res.Account,
				DispatchRefs: res.DispatchRefs,
			}
			if !yield(shared, nil) {
				return
			}
		}
	}
}

// Skipped implements adapters.Accounting: a per-reason view of what
// the parse dropped. Native record types are prefixed "record:";
// structural drops use stable reason keys.
func (a Account) Skipped() map[string]int {
	out := make(map[string]int, len(a.SkippedRecords)+5)
	for typ, n := range a.SkippedRecords {
		out["record:"+typ] = n
	}
	if a.MalformedLines > 0 {
		out["malformed_line"] = a.MalformedLines
	}
	if a.SkippedTurnRecords > 0 {
		out["turn_record"] = a.SkippedTurnRecords
	}
	if a.DroppedParts > 0 {
		out["content_part"] = a.DroppedParts
	}
	if a.OrphanToolResults > 0 {
		out["orphan_tool_result"] = a.OrphanToolResults
	}
	if a.DispatchUnresolved {
		out["dispatch_unresolved"] = 1
	}
	return out
}
