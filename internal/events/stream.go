package events

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"time"
)

// StreamOpt limits a server-sent event stream. The zero value streams until
// the client goes away and sends nothing between events.
type StreamOpt struct {
	// MaxAge ends the stream after this long, so that it ends before a proxy's
	// request timeout (eg: Cloud Run's) cuts it; the browser then reconnects.
	MaxAge time.Duration

	// Ping writes a comment line this often, so that a stream with no events
	// is seen to be alive and a client that went away is noticed.
	Ping time.Duration
}

// retryMs is the time the browser waits before it reconnects to a stream.
const retryMs = 3000

// Stream writes the events from sub to w as server-sent events (text/event-stream),
// calling flush after each write, until ctx is done or opt.MaxAge has passed.
func Stream(ctx context.Context, w io.Writer, flush func(), sub <-chan Event, opt StreamOpt, lo *log.Logger) error {
	var maxAge, ping <-chan time.Time
	if opt.MaxAge > 0 {
		t := time.NewTimer(opt.MaxAge)
		defer t.Stop()
		maxAge = t.C

		// Tell the browser how soon to reconnect before any event arrives, and
		// send the headers now.
		if _, err := fmt.Fprintf(w, "retry: %d\n\n", retryMs); err != nil {
			return err
		}
		flush()
	}
	if opt.Ping > 0 {
		t := time.NewTicker(opt.Ping)
		defer t.Stop()
		ping = t.C
	}

	for {
		select {
		case e := <-sub:
			b, err := json.Marshal(e)
			if err != nil {
				lo.Printf("error marshalling event: %v", err)
				continue
			}

			if _, err := fmt.Fprintf(w, "retry: %d\ndata: %s\n\n", retryMs, b); err != nil {
				return err
			}
			flush()

		case <-ping:
			if _, err := io.WriteString(w, ": ping\n\n"); err != nil {
				return err
			}
			flush()

		case <-maxAge:
			return nil

		case <-ctx.Done():
			return nil
		}
	}
}
