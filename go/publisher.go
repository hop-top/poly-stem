package stem

import "context"

// Canonical crtx v0.1 event topics. Names follow the 4-part shape
// crtx.<category>.<object>.<action> defined in
// hop-top/spec-crtx specs/v0.1/events.md §2.
//
// Producers emit these. The crtx.* namespace is reserved by the spec;
// stem-private lifecycle hooks (turn start, child done, etc.) live
// under the x.io.jadb.stem.* extension namespace per §5.
const (
	// Turn-category — one Turn appended to an Envelope.

	TopicTurnUserReceived     = "crtx.turn.user.received"
	TopicTurnAssistantEmitted = "crtx.turn.assistant.emitted"
	TopicTurnToolCalled       = "crtx.turn.tool.called"
	TopicTurnToolCompleted    = "crtx.turn.tool.completed"
	TopicTurnThinkingEmitted  = "crtx.turn.thinking.emitted"
	TopicTurnSystemInjected   = "crtx.turn.system.injected"

	// Session-category — Envelope lifecycle.

	TopicSessionEnvelopeCreated  = "crtx.session.envelope.created"
	TopicSessionEnvelopeForked   = "crtx.session.envelope.forked"
	TopicSessionEnvelopeNested   = "crtx.session.envelope.nested"
	TopicSessionEnvelopeReturned = "crtx.session.envelope.returned"

	// Stem-private extension topics (events.md §5). These describe
	// stem-internal lifecycle moments that have no crtx canonical
	// counterpart; downstream tooling MAY subscribe.

	TopicStemTurnStarted   = "x.io.jadb.stem.turn.started"
	TopicStemTurnFinished  = "x.io.jadb.stem.turn.finished"
	TopicStemChildDone     = "x.io.jadb.stem.child.done"
	TopicStemSessionClosed = "x.io.jadb.stem.session.closed"
)

// Publisher emits lifecycle events. A nil Publisher is safe — callers
// must guard with a nil check before calling Publish.
type Publisher interface {
	Publish(ctx context.Context, topic string, payload any) error
}
