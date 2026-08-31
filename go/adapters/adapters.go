// Package adapters defines the shared seam every native-store session
// adapter implements: a read-only Scan over a store root that yields
// crtx v0.1 envelopes plus per-file accounting, and the reverse-DNS
// metadata keys the sessions CLI reads off every envelope
// (docs/specs/sessions-cli.md §1).
//
// Concrete adapters live in subpackages (claudecode, codex, ...); each
// keeps its native-store parsing API and additionally exposes an
// Adapter value satisfying the interface here. Consumers that only
// need "every session this machine knows about" range over
// Adapter.Scan and never touch store-specific types.
package adapters

import (
	"iter"

	stem "hop.top/stem"
)

// Shared envelope metadata keys (sessions spec §1.3). Adapters SHOULD
// set these on every envelope they normalize; absence degrades to
// null in CLI output, never to an error.
const (
	// MetaCWD is the absolute working directory the session ran in.
	MetaCWD = "top.hop.stem.cwd"
	// MetaGitBranch is the git branch at session start, when known.
	MetaGitBranch = "top.hop.stem.git_branch"
	// MetaGitCommit is the git commit hash at session start, when known.
	MetaGitCommit = "top.hop.stem.git_commit"
	// MetaNativePath is the absolute path of the native store file the
	// envelope was normalized from (adapter-normalized only).
	MetaNativePath = "top.hop.stem.native_path"
)

// DispatchRef locates the tool call that spawned a dispatch-nested
// envelope: the parent envelope id and the open tool_call's call id
// within it.
type DispatchRef struct {
	EnvelopeID string
	CallID     string
}

// Accounting is the per-adapter skip/degradation detail attached to a
// Result. Adapters keep their store-specific counter types; the seam
// only requires a uniform per-reason view so consumers can surface
// skip notices without knowing the store.
type Accounting interface {
	// Skipped returns per-reason drop counts for the parse the
	// accounting describes. Reasons are adapter-scoped strings; an
	// empty map means nothing was dropped. The returned map is a
	// copy — callers may mutate it freely.
	Skipped() map[string]int
}

// Result is one normalized session file: the envelope, the parse
// accounting, and any dispatch refs the file contributes for
// sidechain resolution (agent id -> spawning tool call). Envelope is
// never nil on a yielded Result; files that produce no envelope are
// omitted by Scan.
type Result struct {
	Envelope     *stem.Session
	Account      Accounting
	DispatchRefs map[string]DispatchRef
}

// Adapter is the read-only seam over one native session store.
//
// Scan enumerates every session under root, one Result per store
// file. Per-file failures surface as (nil, err) pairs and the scan
// continues; a missing root yields nothing. Adapters never write to,
// move, or lock native files.
type Adapter interface {
	// Kind returns the crtx source.kind this adapter produces
	// (e.g. "claude-code", "codex").
	Kind() string
	// DefaultRoots returns the store roots scanned when the caller
	// supplies none. Empty when no root can be resolved on this host.
	DefaultRoots() []string
	// Scan enumerates sessions under root.
	Scan(root string) iter.Seq2[*Result, error]
}
