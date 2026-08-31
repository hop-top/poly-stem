package stem

import (
	"context"
	"sync"
)

// recordedEvent is one Publish call observed by recordingPublisher.
type recordedEvent struct {
	topic   string
	payload any
}

// recordingPublisher is a Publisher that records every Publish call
// (topic + payload) for later inspection. Shared across multi_test.go
// and payload_conformance_test.go to assert on emitted events.
type recordingPublisher struct {
	mu     sync.Mutex
	events []recordedEvent
}

// Publish appends the call to events under a lock; safe for parallel
// emit paths.
func (p *recordingPublisher) Publish(
	_ context.Context, topic string, payload any,
) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.events = append(p.events, recordedEvent{topic, payload})
	return nil
}

// find returns every recorded event for topic in arrival order.
func (p *recordingPublisher) find(topic string) []recordedEvent {
	p.mu.Lock()
	defer p.mu.Unlock()
	var out []recordedEvent
	for _, e := range p.events {
		if e.topic == topic {
			out = append(out, e)
		}
	}
	return out
}
