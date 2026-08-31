// Package copilot normalizes GitHub Copilot CLI session event logs
// into crtx v0.1 envelopes, read-only, per the stem sessions spec
// (docs/specs/sessions-cli.md §1).
//
// # Store layout
//
// The Copilot CLI persists sessions as append-only event logs under
// its state directory ($COPILOT_HOME, default ~/.copilot), across two
// layout vintages:
//
//	session-state/<session-id>/events.jsonl   current (directory per session)
//	session-state/<session-id>.jsonl          older (flat file per session)
//
// Both vintages carry the same JSON Lines event format. When one id
// exists in both layouts, the directory form is the store of record
// and the flat file is skipped, matching the CLI's own session
// listing. Discovery reads one directory level; the sibling
// session-store.db is a query index over the same sessions, not the
// transcript of record, and is never opened. The retired
// history-session-state store predates the event log; the CLI itself
// imports it (emitting session.import_legacy) so this adapter reads
// only session-state.
//
// # Event to envelope mapping
//
// Every line is one event: {id, timestamp, parentId, type, data}.
// The parentId chain orders events within one file and never
// references another session; it is ignored. Events map as follows:
//
//   - session.start → envelope identity. data.sessionId becomes the
//     Envelope id verbatim (falling back to the session's store path
//     — directory name or flat-file stem — when absent);
//     data.copilotVersion becomes source.version; startTime becomes
//     created_at; data.context lands in metadata (see Meta* keys),
//     as do selectedModel and context.repository. Later
//     session.start lines are drift and are skipped with accounting.
//   - user.message → one user Turn with a text part from
//     data.content (the displayed message; transformedContent is a
//     prompt-caching augmentation and is not the user's words).
//     Attachments are counted, not mapped.
//   - assistant.message → one assistant Turn: readable
//     reasoningText becomes a thinking part, content a text part,
//     and each toolRequests entry a tool_call part (toolCallId →
//     call_id, name, arguments verbatim). Opaque/encrypted reasoning
//     with no readable text is counted, never decoded.
//   - assistant.reasoning → one assistant Turn with a thinking part.
//   - tool.user_requested → one user Turn with a tool_call part:
//     the user invoked the tool directly (shell mode), so the call
//     is authored by the user, not the assistant.
//   - tool.execution_start → normally a duplicate of the call opened
//     by assistant.message or tool.user_requested and skipped with
//     accounting. When the call was never opened (sub-agent tool
//     calls have no assistant.message carrier in the parent log), it
//     opens the call as an assistant Turn so the result stays linked.
//   - tool.execution_complete → one tool Turn with a tool_result
//     part linked by toolCallId; result.content (the LLM-visible
//     text) rides as a JSON string, success:false sets is_error. An
//     output whose call was never opened is skipped so the envelope
//     always satisfies crtx tool linkage.
//   - everything else (session.*, subagent.*, assistant deltas,
//     permission.*, ...) carries no conversation content → no Turn,
//     counted per type under an "event:"-prefixed reason.
//
// # Continuation semantics: no forks, no dispatch nests
//
// The store encodes no cross-file lineage, so parent_id/fork_point
// and dispatched_from are never emitted and DispatchRefs is always
// nil (spec §1.4 maps only what the native store encodes):
//
//   - Resume appends a session.resume event to the same events.jsonl
//     under the same session id — a continuation in place, not a new
//     session, so no fork exists to represent.
//   - Sub-agents run inline: subagent.started names the spawning
//     toolCallId and the sub-agent's subsequent events are recorded
//     in the parent's own log (tagged parentToolCallId), never in a
//     separate session file — so there is no child envelope to hang
//     dispatched_from on.
//   - session.handoff records a remote/local transfer but carries no
//     source session id, leaving nothing to link.
//
// The resume-time context on session.resume (cwd/git may have moved)
// is not folded into metadata: the Meta* keys are defined as
// session-start values.
//
// # Drift tolerance
//
// Event schemas drift across Copilot CLI versions. Parsing is
// streaming (line-at-a-time) and never fails on content: malformed
// lines, unknown event types, empty messages, invalid tool requests,
// and orphan tool results are skipped and counted per reason in the
// Report. Only the absence of any session identity (no session.start
// and no store path) is an error. Turn ids reuse the native event id
// verbatim; an event that drifted away its id gets a positional
// t_%06d fallback.
package copilot
