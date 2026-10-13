package cloudrun

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"log"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/knadh/listmonk/internal/events"
	"github.com/lib/pq"
)

const (
	// eventsChannel is the Postgres NOTIFY channel the instances share.
	eventsChannel = "listmonk_events"

	// maxMessage keeps a notification under Postgres' 8000 byte payload limit.
	maxMessage = 4000
)

// relayMsg is the NOTIFY payload: an event and the instance that published it.
type relayMsg struct {
	Instance string       `json:"instance"`
	Event    events.Event `json:"event"`
}

// EventRelay carries the events published on one instance (the admin's error
// toasts) to the event streams of every other instance through Postgres
// LISTEN/NOTIFY. On Cloud Run, a browser's stream can be on any instance.
type EventRelay struct {
	instance string
	db       *sql.DB
	ev       *events.Events
	listener *pq.Listener
	queue    chan events.Event
	ctx      context.Context
	cancel   context.CancelFunc
}

// NewEventRelay starts listening on the shared channel with its own connection
// (dsn) and delivers the other instances' events to ev. Call ev.SetRelay(r.Send)
// to send this instance's events. Notifications are sent through db.
func NewEventRelay(dsn string, db *sql.DB, ev *events.Events, lo *log.Logger) (*EventRelay, error) {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return nil, err
	}

	// The callback only reports the connection's state. A failed send is
	// never logged: the log's errors are themselves relayed.
	l := pq.NewListener(dsn, time.Second, time.Minute, func(t pq.ListenerEventType, err error) {
		switch t {
		case pq.ListenerEventDisconnected:
			lo.Printf("events relay: lost the Postgres connection, reconnecting: %v", err)
		case pq.ListenerEventReconnected:
			lo.Printf("events relay: reconnected to Postgres")
		}
	})
	if err := l.Listen(eventsChannel); err != nil {
		l.Close()
		return nil, err
	}

	ctx, cancel := context.WithCancel(context.Background())
	r := &EventRelay{
		instance: hex.EncodeToString(b),
		db:       db,
		ev:       ev,
		listener: l,
		queue:    make(chan events.Event, 100),
		ctx:      ctx,
		cancel:   cancel,
	}

	go r.receive()
	go r.send()

	return r, nil
}

// Send queues an event for the other instances. It never blocks: when the
// queue is full, the event is dropped.
func (r *EventRelay) Send(e events.Event) {
	select {
	case r.queue <- e:
	default:
	}
}

// Close stops the relay.
func (r *EventRelay) Close() error {
	r.cancel()
	return r.listener.Close()
}

func (r *EventRelay) send() {
	for {
		select {
		case <-r.ctx.Done():
			return
		case e := <-r.queue:
			e.Message = truncate(e.Message, maxMessage)
			b, err := json.Marshal(relayMsg{Instance: r.instance, Event: e})
			if err != nil {
				continue
			}

			ctx, cancel := context.WithTimeout(r.ctx, 5*time.Second)
			_, _ = r.db.ExecContext(ctx, "SELECT pg_notify($1, $2)", eventsChannel, string(b))
			cancel()
		}
	}
}

func (r *EventRelay) receive() {
	for {
		select {
		case <-r.ctx.Done():
			return
		case n, ok := <-r.listener.Notify:
			if !ok {
				return
			}

			// nil after a reconnect: notifications sent meanwhile are lost,
			// which is acceptable for toasts.
			if n == nil {
				continue
			}

			var m relayMsg
			if err := json.Unmarshal([]byte(n.Extra), &m); err != nil || m.Instance == r.instance {
				continue
			}
			_ = r.ev.Deliver(m.Event)
		}
	}
}

// truncate cuts s to at most n bytes without splitting a UTF-8 character.
func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	s = s[:n]
	for len(s) > 0 && !utf8.ValidString(s) {
		s = s[:len(s)-1]
	}
	return strings.TrimRight(s, "\n")
}
