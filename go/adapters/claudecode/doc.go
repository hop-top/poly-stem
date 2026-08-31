// Package claudecode normalizes Claude Code's native session store
// into crtx v0.1 envelopes, read-only, per the stem sessions spec
// (docs/specs/sessions-cli.md §1).
//
// # Store layout
//
// The live store is ~/.claude/projects/<project-dir>/, one directory
// per working directory (path with separators flattened to dashes).
// Two kinds of transcript files exist, across two layout vintages:
//
//	<project-dir>/<session-uuid>.jsonl                            main session
//	<project-dir>/agent-<agent-id>.jsonl                          sidechain (flat, older)
//	<project-dir>/<session-uuid>/subagents/agent-<agent-id>.jsonl sidechain (nested, newer)
//
// Each file is JSON Lines: one record per line, appended as the
// session runs. Only records of type "user" and "assistant" carry
// conversation turns; the remainder ("system", "summary",
// "file-history-snapshot", "queue-operation", "mode", "ai-title",
// "attachment", ...) are host bookkeeping and are skipped with
// accounting, never errors.
//
// # Record to envelope mapping
//
// One file becomes one Envelope. The Envelope id reuses the native
// identifier verbatim (spec §1.2): the records' sessionId for main
// sessions, the records' agentId for sidechains. Turn ids reuse the
// record uuid; created_at/updated_at come from the first/last mapped
// turn's timestamp. Source is {kind: "claude-code", version:
// <records' version field>}; when no record carries a version the
// placeholder "unknown" keeps source.version schema-valid.
//
// Content blocks map as:
//
//	text                     -> text
//	thinking                 -> thinking (thinking -> text, signature kept)
//	tool_use                 -> tool_call (id -> call_id, name, input)
//	tool_result              -> tool_result (tool_use_id -> call_id,
//	                            content -> output verbatim, is_error)
//	image (base64/url source)-> image (media_type -> mime, data / url)
//
// A user record whose mapped parts are exclusively tool_result
// becomes a tool-role turn (matching crtx convention); every other
// user record maps role user, assistant records role assistant.
// Legacy records whose message.content is a plain string become a
// single text part. Records flagged isMeta (command caveats, local
// command echoes) are skipped and accounted under "user:meta".
//
// # Dispatch-nest linkage (sidechains)
//
// Claude Code encodes the sub-agent linkage across two files: the
// sidechain file's records carry agentId plus the parent's sessionId,
// and the parent session names the sidechain in one of two ways:
//
//   - synchronous dispatch: a user record whose toolUseResult.agentId
//     names the sidechain while its tool_result block's tool_use_id
//     names the spawning tool call
//   - background dispatch: a later <task-notification> payload whose
//     <task-id> element names the sidechain and whose <tool-use-id>
//     element names the spawning tool call — injected as a user
//     record while the session was live, or stranded in a
//     queue-operation record when the session ended first
//
// The adapter resolves dispatched_from.call_id from the parent:
// parsing a main session collects Result.DispatchRefs (agentId ->
// {EnvelopeID, CallID}) from both encodings; Scan feeds the merged
// index to sidechain parsing, which emits
// DispatchedFrom{envelope_id, call_id} and sets the parent's
// tool_result child_envelope_id to the agent id. When the parent
// never recorded the dispatch (parent file deleted, dispatch still
// running, ephemeral agents), the linkage is natively broken: the
// sidechain envelope is still emitted, without dispatched_from, and
// Account.DispatchUnresolved reports it. This answers the sidechain
// call_id open question in the sessions spec (§6).
//
// No fork/resume linkage is emitted: the store encodes none — every
// scanned file's records carry exactly the file's own sessionId and
// no field references a predecessor session — so parent_id/fork_point
// stay unset (spec §1.4 maps only what the native store encodes).
//
// # Archived stores
//
// Claude Code keeps no archive of project transcripts: ~/.claude
// holds only the live projects/ tree (backups/, sessions/, and
// file-history/ hold non-transcript state). The adapter scans the
// live store only; rotated or deleted files are out of scope until
// the native tool grows an archive. This answers the archived-stores
// open question in the sessions spec (§6).
//
// # Drift tolerance
//
// Native shapes drift across CLI versions. Parsing is streaming
// (line-at-a-time) and lossy-tolerant, never fatal past file open:
//
//   - unparsable lines are counted (Account.MalformedLines) and skipped
//   - unknown record types are counted by type (Account.SkippedRecords)
//   - unknown top-level fields and unknown block fields are ignored
//   - unknown content-block types are dropped (Account.DroppedParts)
//   - tool_result blocks with no preceding open tool_call in the same
//     file are dropped (Account.OrphanToolResults) so the envelope
//     honors the crtx tool linkage invariant
//   - turn records missing uuid or a parsable timestamp, or left with
//     zero mappable parts, are skipped (Account.SkippedTurnRecords)
//
// A file that yields zero turns produces no envelope (Result.Envelope
// is nil); Scan omits it.
package claudecode
