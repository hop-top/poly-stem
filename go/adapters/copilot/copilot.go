package copilot

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	stem "hop.top/stem"
	"hop.top/stem/adapters"
)

// Envelope metadata keys set by this adapter. The first four follow
// the shared stem adapter metadata contract (hoisted to the parent
// adapters package); the model and repository keys are
// copilot-scoped.
const (
	MetaCwd        = adapters.MetaCWD
	MetaGitBranch  = adapters.MetaGitBranch
	MetaGitCommit  = adapters.MetaGitCommit
	MetaNativePath = adapters.MetaNativePath
	// MetaModel is the model selected at session start
	// (session.start data.selectedModel).
	MetaModel = "top.hop.stem.copilot.model"
	// MetaRepository is the repository identifier derived from the
	// git remote at session start (session.start
	// data.context.repository, e.g. "owner/name").
	MetaRepository = "top.hop.stem.copilot.repository"
)

// SourceKind is the crtx source.kind for Copilot-produced envelopes.
const SourceKind = "copilot"

// ErrNoSession is returned when input yields no session identity:
// no session.start event and no store path to recover the id from.
var ErrNoSession = errors.New("copilot: no session identity in input")

// Skip reasons accumulated in Report.Skips. Events that carry no
// conversation content are counted under EventSkipPrefix + the
// native event type (e.g. "event:session.resume"); the constants
// below cover structural drops within content-bearing events.
const (
	SkipMalformedLine     = "malformed_line"
	SkipDuplicateStart    = "session_start_duplicate"
	SkipEmptyMessage      = "empty_message"
	SkipEmptyReasoning    = "empty_reasoning"
	SkipOpaqueReasoning   = "opaque_reasoning"
	SkipAttachment        = "attachment"
	SkipInvalidToolCall   = "invalid_tool_call"
	SkipDuplicateToolCall = "tool_call_duplicate"
	SkipOrphanToolResult  = "orphan_tool_result"
)

// EventSkipPrefix prefixes the per-type skip reason for events that
// map to no Turn, known and unknown alike.
const EventSkipPrefix = "event:"

// Report accounts for what a parse consumed, emitted, and dropped.
// Skips is keyed by the Skip* reason constants and by
// EventSkipPrefix + native event type.
type Report struct {
	Lines int            // non-blank physical lines read
	Turns int            // turns emitted on the envelope
	Skips map[string]int // dropped events/parts per reason
}

func (r *Report) skip(reason string) {
	if r.Skips == nil {
		r.Skips = make(map[string]int)
	}
	r.Skips[reason]++
}

// DefaultRoot returns the native Copilot session store root:
// $COPILOT_HOME/session-state when COPILOT_HOME is set, otherwise
// ~/.copilot/session-state.
func DefaultRoot() (string, error) {
	if home := os.Getenv("COPILOT_HOME"); home != "" {
		return filepath.Join(home, "session-state"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".copilot", "session-state"), nil
}

// Discover reads root's immediate entries and returns every session
// event log, sorted by path: <id>/events.jsonl for the directory
// layout, <id>.jsonl for the older flat layout. A flat file shadowed
// by a directory form of the same id is omitted (the directory is
// the store of record). A missing root is an error; use LoadAll for
// the missing-store-is-empty convenience.
func Discover(root string) ([]string, error) {
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, err
	}
	dirIDs := make(map[string]struct{})
	var files []string
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		path := filepath.Join(root, e.Name(), "events.jsonl")
		if info, err := os.Stat(path); err == nil && info.Mode().IsRegular() {
			dirIDs[e.Name()] = struct{}{}
			files = append(files, path)
		}
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".jsonl") {
			continue
		}
		id := strings.TrimSuffix(e.Name(), ".jsonl")
		if _, shadowed := dirIDs[id]; shadowed {
			continue
		}
		files = append(files, filepath.Join(root, e.Name()))
	}
	sort.Strings(files)
	return files, nil
}

// Entry is one discovered session file with its parse outcome. A
// per-file failure is carried on Err; it never aborts the batch.
type Entry struct {
	Path    string
	Session *stem.Session
	Report  *Report
	Err     error
}

// LoadAll discovers and parses every session file under root. A
// missing root returns an empty slice and no error (machines without
// a Copilot store are normal); any other discovery failure is
// returned. Per-file parse failures are recorded on the Entry.
func LoadAll(root string) ([]Entry, error) {
	files, err := Discover(root)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	entries := make([]Entry, 0, len(files))
	for _, path := range files {
		s, rep, perr := ParseFile(path)
		entries = append(entries, Entry{Path: path, Session: s, Report: rep, Err: perr})
	}
	return entries, nil
}
