package cloudrun

import (
	"io"
	"log"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/knadh/listmonk/internal/events"
)

// instance is one listmonk instance's event stream, relayed through Postgres.
func instance(t *testing.T) (*events.Events, chan events.Event) {
	t.Helper()

	db := testDB(t)
	ev := events.New()
	r, err := NewEventRelay(os.Getenv("LISTMONK_TEST_DB_DSN"), db, ev, log.New(io.Discard, "", 0))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { r.Close() })
	ev.SetRelay(r.Send)

	sub, _ := ev.Subscribe("browser")
	return ev, sub
}

func next(sub chan events.Event, wait time.Duration) (events.Event, bool) {
	select {
	case e := <-sub:
		return e, true
	case <-time.After(wait):
		return events.Event{}, false
	}
}

func TestEventRelayAcrossInstances(t *testing.T) {
	a, subA := instance(t)
	_, subB := instance(t)

	long := strings.Repeat("é", 6000)
	for _, msg := range []string{"error sending campaign", long} {
		if err := a.Publish(events.Event{Type: events.TypeError, Message: msg}); err != nil {
			t.Fatal(err)
		}

		// The browser on instance B sees the event published on A.
		e, ok := next(subB, 5*time.Second)
		if !ok {
			t.Fatalf("instance B got no event for %.20q", msg)
		}
		if msg == long {
			if len(e.Message) > maxMessage || !strings.HasPrefix(long, e.Message) || e.Message == "" {
				t.Fatalf("a long message is relayed cut to %d bytes on a character boundary, got %d bytes", maxMessage, len(e.Message))
			}
		} else if e.Message != msg || e.Type != events.TypeError {
			t.Fatalf("instance B got %+v", e)
		}

		// The browser on A sees it once: its own notification is not delivered back.
		if _, ok := next(subA, time.Second); !ok {
			t.Fatal("instance A did not get its own event")
		}
		if e, ok := next(subA, 500*time.Millisecond); ok {
			t.Fatalf("instance A got its own event twice: %.40q", e.Message)
		}
	}
}

func TestTruncate(t *testing.T) {
	for _, c := range []struct {
		in   string
		n    int
		want string
	}{
		{"short", 10, "short"},
		{"abcdef", 3, "abc"},
		{"ééé", 3, "é"},
		{"ab\n\ncd", 4, "ab"},
	} {
		if got := truncate(c.in, c.n); got != c.want {
			t.Errorf("truncate(%q, %d) = %q, want %q", c.in, c.n, got, c.want)
		}
	}
}
