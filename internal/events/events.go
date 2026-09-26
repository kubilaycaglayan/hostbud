// Package events is hostbud's typed in-process pub/sub bus. Every state
// change is published here; the events WebSocket (and, in v2, the
// orchestrator) subscribe to it.
package events

import "sync"

// Type names an event kind. The strings are part of the /ws/events protocol.
type Type string

const (
	// MachineStatus carries a MachineStatus payload.
	MachineStatus Type = "machine.status"
	// SessionsChanged carries a SessionsChanged payload.
	SessionsChanged Type = "sessions.changed"
	// ProjectsChanged carries a projects.Changed payload.
	ProjectsChanged Type = "projects.changed"
)

// Event is one published event. Payload's type is determined by Type.
type Event struct {
	Type    Type   `json:"type"`
	Machine string `json:"machine"`
	Payload any    `json:"payload"`
}

// Bus fans events out to subscribers. Publish never blocks: a subscriber
// whose buffer is full is dropped (its channel is closed), and is expected
// to resubscribe and resync from a snapshot.
type Bus struct {
	mu   sync.Mutex
	subs map[*sub]struct{}
}

type sub struct {
	ch     chan Event
	closed bool
}

// NewBus returns an empty bus.
func NewBus() *Bus { return &Bus{subs: map[*sub]struct{}{}} }

// Subscribe returns a channel of events and a cancel func (idempotent).
// buffer is how many events may queue before the subscriber is dropped.
func (b *Bus) Subscribe(buffer int) (<-chan Event, func()) {
	s := &sub{ch: make(chan Event, buffer)}
	b.mu.Lock()
	b.subs[s] = struct{}{}
	b.mu.Unlock()
	return s.ch, func() { b.drop(s) }
}

func (b *Bus) drop(s *sub) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if s.closed {
		return
	}
	s.closed = true
	delete(b.subs, s)
	close(s.ch)
}

// Publish delivers e to every subscriber without blocking.
func (b *Bus) Publish(e Event) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for s := range b.subs {
		select {
		case s.ch <- e:
		default:
			s.closed = true
			delete(b.subs, s)
			close(s.ch)
		}
	}
}

// Subscribers returns the number of live subscribers.
func (b *Bus) Subscribers() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return len(b.subs)
}
