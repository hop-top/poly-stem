// Package codex normalizes Codex CLI rollout session files into crtx
// v0.1 Envelopes. It is a read-only adapter over the native store at
// ~/.codex/sessions/YYYY/MM/DD/rollout-*.jsonl: it never writes to,
// moves, or locks native files.
//
// # Record mapping
//
// A rollout file is JSONL; every line is a record with a top-level
// timestamp, a type, and a payload. Records map as follows:
//
//   - session_meta → envelope identity. The first session_meta wins:
//     id (falling back to session_id, then the rollout filename UUID)
//     becomes the Envelope id verbatim; cli_version becomes
//     source.version; cwd, git branch/commit, and instructions land
//     in metadata (see Meta* keys). Later session_meta lines are
//     copied parent history (fork files) or duplicates and are
//     skipped with accounting.
//   - response_item message → one Turn with the native role
//     (user/assistant/system/developer/tool); input_text/output_text
//     content becomes text parts, input_image becomes an image part.
//   - response_item reasoning → one assistant Turn; summary_text
//     entries and reasoning_text content entries become thinking
//     parts. Reasoning items that carry only encrypted content are
//     skipped.
//   - response_item function_call / custom_tool_call → one assistant
//     Turn with a tool_call part. function_call arguments that parse
//     as JSON ride verbatim; anything else (including custom tool
//     input) rides as a JSON string.
//   - response_item function_call_output / custom_tool_call_output →
//     one tool Turn with a tool_result part, linked by call_id. An
//     output whose call_id was never opened is skipped so the
//     envelope always satisfies crtx tool linkage.
//   - turn_context, event_msg, world_state, compacted → no Turn.
//     event_msg duplicates response_item content; compacted replaces
//     history the file still carries in full, so dropping it is
//     lossless for recall. All are counted in the Report.
//
// # Resume and fork linkage
//
// Codex encodes continuation three ways across vintages; all three
// map to the crtx fork relationship (parent_id + fork_point), in
// this precedence order:
//
//  1. session_meta.forked_from_id — explicit fork/resume.
//  2. session_meta.parent_thread_id — multi-agent parent thread.
//  3. session_meta.session_id, when it differs from id — sub-thread
//     of a root session (subagent vintages).
//
// fork_point is always 0: Codex materializes the full inherited
// history into the child rollout file, so the child envelope carries
// the complete transcript and inherits nothing by reference
// (parent.turns[0:0] + child.turns reconstructs exactly the child
// file's content). dispatched_from is never emitted — the native
// store records no spawning tool call_id, and crtx requires one.
//
// # Drift tolerance
//
// Rollout schemas drift across Codex versions. Parsing never fails
// on content: malformed lines, unknown record types, unknown
// response_item types, unknown content parts, and unknown roles are
// skipped and counted per reason in the Report. Only the absence of
// any session identity (no usable session_meta and no rollout
// filename) is an error.
package codex

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
// adapters package); the instructions key is codex-scoped.
const (
	MetaCwd          = adapters.MetaCWD
	MetaGitBranch    = adapters.MetaGitBranch
	MetaGitCommit    = adapters.MetaGitCommit
	MetaNativePath   = adapters.MetaNativePath
	MetaInstructions = "top.hop.stem.codex.instructions"
)

// SourceKind is the crtx source.kind for Codex-produced envelopes.
const SourceKind = "codex"

// ErrNoSession is returned when input yields no session identity:
// no usable session_meta record and no rollout filename to recover
// the id from.
var ErrNoSession = errors.New("codex: no session identity in input")

// Skip reasons accumulated in Report.Skips.
const (
	SkipMalformedLine    = "malformed_line"
	SkipUnknownRecord    = "unknown_record"
	SkipEventRecord      = "event_msg"
	SkipTurnContext      = "turn_context"
	SkipWorldState       = "world_state"
	SkipCompacted        = "compacted"
	SkipDuplicateMeta    = "session_meta_duplicate"
	SkipUnknownItem      = "unknown_item"
	SkipUnknownRole      = "unknown_role"
	SkipEmptyMessage     = "empty_message"
	SkipUnknownPart      = "unknown_content_part"
	SkipEmptyReasoning   = "empty_reasoning"
	SkipInvalidToolCall  = "invalid_tool_call"
	SkipOrphanToolResult = "orphan_tool_result"
	SkipWebSearchCall    = "web_search_call"
)

// Report accounts for what a parse consumed, emitted, and dropped.
// Skips is keyed by the Skip* reason constants.
type Report struct {
	Lines int            // non-blank physical lines read
	Turns int            // turns emitted on the envelope
	Skips map[string]int // dropped records/parts per reason
}

func (r *Report) skip(reason string) {
	if r.Skips == nil {
		r.Skips = make(map[string]int)
	}
	r.Skips[reason]++
}

// DefaultRoot returns the native Codex session store root:
// $CODEX_HOME/sessions when CODEX_HOME is set, otherwise
// ~/.codex/sessions.
func DefaultRoot() (string, error) {
	if home := os.Getenv("CODEX_HOME"); home != "" {
		return filepath.Join(home, "sessions"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".codex", "sessions"), nil
}

// Discover walks root and returns every rollout session file
// (rollout-*.jsonl), sorted by path. Directory layout beneath root
// is not assumed; any depth is accepted. A missing root is an error;
// use LoadAll for the missing-store-is-empty convenience.
func Discover(root string) ([]string, error) {
	var files []string
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			return nil
		}
		name := d.Name()
		if strings.HasPrefix(name, "rollout-") && strings.HasSuffix(name, ".jsonl") {
			files = append(files, path)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(files)
	return files, nil
}

// Entry is one discovered rollout file with its parse outcome. A
// per-file failure is carried on Err; it never aborts the batch.
type Entry struct {
	Path    string
	Session *stem.Session
	Report  *Report
	Err     error
}

// LoadAll discovers and parses every rollout file under root. A
// missing root returns an empty slice and no error (machines without
// a Codex store are normal); any other discovery failure is
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
