package queue

import (
	"context"
	"slices"
	"strings"
	"testing"
	"time"

	"hostbud/internal/agents"
	"hostbud/internal/events"
	"hostbud/internal/notify"
	"hostbud/internal/store"
)

type switchNotifications struct{ on bool }

func (s *switchNotifications) Enabled(context.Context) bool { return s.on }

// notices drains queue.changed events: each action, and the notification
// key it carried ("" for none).
func notices(ch <-chan events.Event) (actions []string, keys map[string]string, payloads []notify.Payload) {
	keys = map[string]string{}
	for {
		select {
		case ev := <-ch:
			if ev.Type != events.QueueChanged {
				continue
			}
			c := ev.Payload.(Changed)
			actions = append(actions, c.Action)
			if c.Notification != nil {
				keys[c.Action] = c.Notification.Key
				payloads = append(payloads, *c.Notification)
			}
		default:
			return
		}
	}
}

// V2-M3 T1: done, needs attention and queue finished carry a notification
// while an account is on; no other transition does, and with every account
// off none does (V2-M2's events unchanged).
func TestDispatcherNotifiesTheThreeEvents(t *testing.T) {
	e := newDispEnv(t, "first goal with /secret/path", "second")
	on := &switchNotifications{on: true}
	e.d.SetNotifications(on)
	ch, _ := e.bus.Subscribe(1000)
	e.startQueue()
	r1 := e.run(1)
	e.hook(r1, EventSessionStart, "s1")
	e.claude.set("s1", agents.Achieved)
	e.hook(r1, EventTurnEnd, "s1")
	r2 := e.run(2)
	e.hook(r2, EventSessionStart, "s2")
	e.claude.set("s2", agents.Failed)
	e.hook(r2, EventTurnEnd, "s2")
	actions, keys, payloads := notices(ch)
	if keys["item_done"] != "run:"+r1.ID+":done" || keys["needs_attention"] != "run:"+r2.ID+":attention" || len(keys) != 2 {
		t.Fatalf("notification keys %v (actions %v)", keys, actions)
	}
	for _, p := range payloads {
		if p.Project != "app" || strings.Contains(p.Title+p.Body+p.URL, "secret") || strings.Contains(p.Title+p.Body, "sess") {
			t.Fatalf("payload %+v", p)
		}
	}
	if payloads[1].Outcome != store.RunFailed || payloads[1].Position != 2 || payloads[1].URL != "/queues/"+e.queue.ID+"?item="+e.items()[1].ID {
		t.Fatalf("attention payload %+v", payloads[1])
	}

	// Skip the failed item and resume: the queue finishes, keyed by its last run.
	if _, err := e.svc.Override(e.ctx(), e.items()[1].ID, ActionSkip); err != nil {
		t.Fatal(err)
	}
	if _, err := e.svc.Resume(e.ctx(), e.queue.ID); err != nil {
		t.Fatal(err)
	}
	e.d.Sync()
	actions, keys, _ = notices(ch)
	if e.queueStatus() != store.QueueFinished || keys["finished"] != "queue:"+e.queue.ID+":finished:"+r2.ID || len(keys) != 1 {
		t.Fatalf("finish: queue %s, keys %v, actions %v", e.queueStatus(), keys, actions)
	}
	if !slices.Contains(actions, "item_skip") {
		t.Fatalf("owner action not published: %v", actions)
	}
}

func TestDispatcherNotifiesNothingWhileEveryAccountIsOff(t *testing.T) {
	for _, n := range []Notifications{nil, &switchNotifications{on: false}} {
		e := newDispEnv(t, "one")
		e.d.SetNotifications(n)
		ch, _ := e.bus.Subscribe(1000)
		e.startQueue()
		e.hook(e.run(1), EventSessionStart, "s")
		e.claude.set("s", agents.Achieved)
		e.hook(e.run(1), EventTurnEnd, "s")
		actions, keys, _ := notices(ch)
		if len(keys) != 0 || !slices.Contains(actions, "item_done") || !slices.Contains(actions, "finished") {
			t.Fatalf("switch off: keys %v, actions %v", keys, actions)
		}
	}
}

// A stale run notifies once (attention); its late achieved notifies done.
func TestDispatcherNotifiesStaleThenLateDone(t *testing.T) {
	e := newDispEnv(t, "one", "two")
	e.d.SetNotifications(&switchNotifications{on: true})
	ch, _ := e.bus.Subscribe(1000)
	e.startQueue()
	r := e.run(1)
	e.hook(r, EventSessionStart, "s")
	e.advance(staleAfter + time.Second)
	_, keys, payloads := notices(ch)
	if keys["needs_attention"] != "run:"+r.ID+":attention" || payloads[0].Outcome != store.RunStale {
		t.Fatalf("stale: %v %+v", keys, payloads)
	}
	e.claude.set("s", agents.Achieved)
	e.hook(r, EventTurnEnd, "s")
	_, keys, _ = notices(ch)
	if keys["item_done"] != "run:"+r.ID+":done" || len(keys) != 1 {
		t.Fatalf("late achieved: %v", keys)
	}
}
