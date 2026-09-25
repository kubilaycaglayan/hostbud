package events

import "testing"

func TestPublishFanOut(t *testing.T) {
	b := NewBus()
	a, cancelA := b.Subscribe(4)
	c, cancelC := b.Subscribe(4)
	defer cancelA()
	defer cancelC()

	b.Publish(Event{Type: SessionsChanged, Machine: "host"})
	for _, ch := range []<-chan Event{a, c} {
		if e := <-ch; e.Type != SessionsChanged || e.Machine != "host" {
			t.Fatalf("got %+v", e)
		}
	}
}

func TestCancelClosesAndIsIdempotent(t *testing.T) {
	b := NewBus()
	ch, cancel := b.Subscribe(1)
	cancel()
	cancel()
	if _, ok := <-ch; ok {
		t.Fatal("channel not closed")
	}
	if b.Subscribers() != 0 {
		t.Fatal("subscriber not removed")
	}
	b.Publish(Event{Type: MachineStatus}) // no panic on closed subscriber
}

func TestSlowSubscriberIsDroppedNotBlocking(t *testing.T) {
	b := NewBus()
	slow, cancelSlow := b.Subscribe(1)
	fast, cancelFast := b.Subscribe(8)
	defer cancelFast()

	for range 3 {
		b.Publish(Event{Type: SessionsChanged}) // must not block
	}
	if e, ok := <-slow; !ok || e.Type != SessionsChanged {
		t.Fatal("slow subscriber lost its buffered event")
	}
	if _, ok := <-slow; ok {
		t.Fatal("slow subscriber not dropped")
	}
	cancelSlow() // safe after drop
	if len(fast) != 3 || b.Subscribers() != 1 {
		t.Fatalf("fast got %d events, %d subscribers", len(fast), b.Subscribers())
	}
}
