package events

import (
	"testing"
	"time"
)

func receive(t *testing.T, ch chan Event) Event {
	t.Helper()
	select {
	case e := <-ch:
		return e
	case <-time.After(time.Second):
		t.Fatal("no event")
	}
	return Event{}
}

func TestPublishCallsRelay(t *testing.T) {
	ev := New()
	sub, _ := ev.Subscribe("a")

	var relayed []Event
	ev.SetRelay(func(e Event) { relayed = append(relayed, e) })

	if err := ev.Publish(Event{Type: TypeError, Message: "local"}); err != nil {
		t.Fatal(err)
	}
	if e := receive(t, sub); e.Message != "local" {
		t.Fatalf("subscriber got %q", e.Message)
	}
	if len(relayed) != 1 || relayed[0].Message != "local" {
		t.Fatalf("relay got %v, want the published event", relayed)
	}

	// An event from another instance goes to the subscribers, not back to the relay.
	if err := ev.Deliver(Event{Type: TypeError, Message: "remote"}); err != nil {
		t.Fatal(err)
	}
	if e := receive(t, sub); e.Message != "remote" {
		t.Fatalf("subscriber got %q", e.Message)
	}
	if len(relayed) != 1 {
		t.Fatalf("Deliver relayed the event: %v", relayed)
	}
}

func TestPublishWithoutRelay(t *testing.T) {
	ev := New()
	sub, _ := ev.Subscribe("a")

	if err := ev.Publish(Event{Type: TypeError, Message: "x"}); err != nil {
		t.Fatal(err)
	}
	if e := receive(t, sub); e.Message != "x" {
		t.Fatalf("subscriber got %q", e.Message)
	}
}
