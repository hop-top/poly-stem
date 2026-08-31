package stem

import (
	"context"
	"testing"
)

// TestTopicConstants pins the wire-string for each exported topic
// constant so a stray rename doesn't silently change what subscribers
// see. Names follow crtx v0.1 events.md §2 four-part shape for
// canonical topics; stem-private topics use the x.io.jadb.stem.*
// extension namespace per §5.
func TestTopicConstants(t *testing.T) {
	expected := map[string]string{
		// Canonical crtx topics
		"crtx.turn.user.received":        TopicTurnUserReceived,
		"crtx.turn.assistant.emitted":    TopicTurnAssistantEmitted,
		"crtx.turn.tool.called":          TopicTurnToolCalled,
		"crtx.turn.tool.completed":       TopicTurnToolCompleted,
		"crtx.turn.thinking.emitted":     TopicTurnThinkingEmitted,
		"crtx.turn.system.injected":      TopicTurnSystemInjected,
		"crtx.session.envelope.created":  TopicSessionEnvelopeCreated,
		"crtx.session.envelope.forked":   TopicSessionEnvelopeForked,
		"crtx.session.envelope.nested":   TopicSessionEnvelopeNested,
		"crtx.session.envelope.returned": TopicSessionEnvelopeReturned,

		// Stem-private extension topics
		"x.io.jadb.stem.turn.started":   TopicStemTurnStarted,
		"x.io.jadb.stem.turn.finished":  TopicStemTurnFinished,
		"x.io.jadb.stem.child.done":     TopicStemChildDone,
		"x.io.jadb.stem.session.closed": TopicStemSessionClosed,
	}

	for want, got := range expected {
		if got != want {
			t.Errorf("topic %q: expected %q, got %q", want, want, got)
		}
	}
}

func TestNilPublisherSafe(t *testing.T) {
	rt := &Runtime{
		session:   newSession("s-1"),
		publisher: nil,
	}
	rt.emitEvent(context.TODO(), TopicStemTurnStarted, nil)
}
