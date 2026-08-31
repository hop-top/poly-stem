package claudecode

import (
	"errors"
	"fmt"
	"io/fs"
	"iter"
	"os"
	"path/filepath"
	"sort"
	"strings"

	stem "hop.top/stem"
	"hop.top/stem/adapters"
)

// SourceKind is the crtx source.kind for envelopes normalized from
// the Claude Code store.
const SourceKind = "claude-code"

// unknownVersion fills source.version when no record carries the
// producing CLI's version; the field is required by the schema.
const unknownVersion = "unknown"

// Adapter metadata keys (sessions spec §1.3), shared across adapters
// via the parent adapters package.
const (
	MetaCWD        = adapters.MetaCWD
	MetaGitBranch  = adapters.MetaGitBranch
	MetaNativePath = adapters.MetaNativePath
)

// agentFilePrefix marks sidechain transcript files.
const agentFilePrefix = "agent-"

// DispatchRef locates the tool call that spawned a sidechain: the
// parent envelope and the open tool_call's id within it. Alias of the
// shared seam type so indexes flow to adapters.Result without copying.
type DispatchRef = adapters.DispatchRef

// DispatchIndex maps sidechain agent ids to their spawning tool
// call, harvested from parent sessions' toolUseResult records and
// task-notification payloads.
type DispatchIndex map[string]DispatchRef

// Account reports what a file parse skipped or degraded. Everything
// here is informational: skips are never errors (sessions spec §1).
type Account struct {
	// Path is the native file the counters describe.
	Path string
	// MalformedLines counts lines that failed to parse as JSON.
	MalformedLines int
	// SkippedRecords counts non-turn records by native record type;
	// isMeta user records count under "user:meta".
	SkippedRecords map[string]int
	// SkippedTurnRecords counts user/assistant records that could not
	// become turns (missing uuid, unparsable timestamp, no mappable
	// content).
	SkippedTurnRecords int
	// DroppedParts counts content blocks of unknown type.
	DroppedParts int
	// OrphanToolResults counts tool_result blocks dropped because no
	// open tool_call precedes them in the file.
	OrphanToolResults int
	// DispatchUnresolved reports a sidechain whose spawning tool call
	// could not be resolved; the envelope is emitted without
	// dispatched_from.
	DispatchUnresolved bool
}

// Result is one normalized session file: the envelope (nil when the
// file yields no turns), the parse accounting, and any dispatch refs
// this file contributes for sidechain resolution.
type Result struct {
	Envelope     *stem.Session
	Account      Account
	DispatchRefs DispatchIndex
}

// DefaultRoot returns the live Claude Code store root,
// ~/.claude/projects.
func DefaultRoot() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("claudecode: resolve home: %w", err)
	}
	return filepath.Join(home, ".claude", "projects"), nil
}

// Scan enumerates every session under root, one Result per
// transcript file. Main sessions parse first (all projects) so their
// dispatch refs resolve sidechain linkage; files yielding no turns
// are omitted. Per-file failures surface as (nil, err) pairs and the
// scan continues; a missing root yields nothing.
func Scan(root string) iter.Seq2[*Result, error] {
	return func(yield func(*Result, error) bool) {
		if _, err := os.Stat(root); errors.Is(err, fs.ErrNotExist) {
			return
		}
		mains, sidechains := discover(root)

		idx := DispatchIndex{}
		for _, path := range mains {
			res, err := ParseFile(path, nil)
			if err != nil {
				if !yield(nil, err) {
					return
				}
				continue
			}
			for id, ref := range res.DispatchRefs {
				idx[id] = ref
			}
			if res.Envelope == nil {
				continue
			}
			if !yield(res, nil) {
				return
			}
		}
		for _, path := range sidechains {
			res, err := ParseFile(path, idx)
			if err != nil {
				if !yield(nil, err) {
					return
				}
				continue
			}
			if res.Envelope == nil {
				continue
			}
			if !yield(res, nil) {
				return
			}
		}
	}
}

// discover globs the two transcript layouts under root and splits
// main sessions from sidechains, each sorted for determinism.
func discover(root string) (mains, sidechains []string) {
	flat, _ := filepath.Glob(filepath.Join(root, "*", "*.jsonl"))
	nested, _ := filepath.Glob(filepath.Join(root, "*", "*", "subagents", "*.jsonl"))
	for _, p := range flat {
		if strings.HasPrefix(filepath.Base(p), agentFilePrefix) {
			sidechains = append(sidechains, p)
		} else {
			mains = append(mains, p)
		}
	}
	sidechains = append(sidechains, nested...)
	sort.Strings(mains)
	sort.Strings(sidechains)
	return mains, sidechains
}
