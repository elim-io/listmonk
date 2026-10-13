// Package events implements a simple event broadcasting mechanism
// for usage in broadcasting error messages, postbacks etc. various
// channels.
package events

import (
	"bytes"
	"fmt"
	"io"
	"sync"
)

const (
	TypeError = "error"
)

// Event represents a single event in the system.
type Event struct {
	ID       string   `json:"id"`
	Type     string   `json:"type"`
	Message  string   `json:"message"`
	Data     any      `json:"data"`
	Channels []string `json:"-"`
}

type Events struct {
	subs map[string]chan Event

	// relay, when set, carries the events published here to other instances.
	relay func(Event)
	sync.RWMutex
}

// New returns a new instance of Events.
func New() *Events {
	return &Events{
		subs: make(map[string]chan Event),
	}
}

// Subscribe returns a channel to which the given event `types` are streamed.
// id is the unique identifier for the caller. A caller can only register
// for subscription once.
func (ev *Events) Subscribe(id string) (chan Event, error) {
	ev.Lock()
	defer ev.Unlock()

	if ch, ok := ev.subs[id]; ok {
		return ch, nil
	}

	ch := make(chan Event, 100)
	ev.subs[id] = ch

	return ch, nil
}

// Unsubscribe unsubscribes a subscriber (obviously).
func (ev *Events) Unsubscribe(id string) {
	ev.Lock()
	defer ev.Unlock()
	delete(ev.subs, id)
}

// Publish publishes an event to all subscribers, and to the relay if one is set.
func (ev *Events) Publish(e Event) error {
	err := ev.Deliver(e)

	ev.RLock()
	relay := ev.relay
	ev.RUnlock()
	if relay != nil {
		relay(e)
	}

	return err
}

// Deliver publishes an event to this instance's subscribers only. It is
// used for events that a relay received from another instance.
func (ev *Events) Deliver(e Event) error {
	ev.Lock()
	defer ev.Unlock()

	for _, ch := range ev.subs {
		select {
		case ch <- e:
		default:
			return fmt.Errorf("event queue full for type: %s", e.Type)
		}
	}

	return nil
}

// SetRelay sets a function that is called with every event published on this
// instance, to carry it to the other instances of the app (eg: on Cloud Run).
// It must not block.
func (ev *Events) SetRelay(fn func(Event)) {
	ev.Lock()
	ev.relay = fn
	ev.Unlock()
}

// This implements an io.Writer specifically for receiving error messages
// mirrored (io.MultiWriter) from error log writing.
type wri struct {
	ev *Events
}

func (w *wri) Write(b []byte) (n int, err error) {
	// Only broadcast error messages.
	if !bytes.Contains(b, []byte("error")) {
		return 0, nil
	}

	w.ev.Publish(Event{
		Type:    TypeError,
		Message: string(b),
	})

	return len(b), nil
}

func (ev *Events) ErrWriter() io.Writer {
	return &wri{ev: ev}
}
