package events

import (
	"bytes"
	"context"
	"io"
	"log"
	"strings"
	"sync"
	"testing"
	"time"
)

// syncBuf is a bytes.Buffer safe to read while Stream writes to it.
type syncBuf struct {
	b bytes.Buffer
	sync.Mutex
}

func (s *syncBuf) Write(p []byte) (int, error) {
	s.Lock()
	defer s.Unlock()
	return s.b.Write(p)
}

func (s *syncBuf) String() string {
	s.Lock()
	defer s.Unlock()
	return s.b.String()
}

var nolog = log.New(io.Discard, "", 0)

func TestStreamDefault(t *testing.T) {
	var (
		w           = &syncBuf{}
		sub         = make(chan Event, 1)
		ctx, cancel = context.WithCancel(context.Background())
		done        = make(chan error)
	)
	go func() { done <- Stream(ctx, w, func() {}, sub, StreamOpt{}, nolog) }()

	sub <- Event{Type: TypeError, Message: "boom"}
	time.Sleep(50 * time.Millisecond)
	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}

	// The output is what listmonk always sent: no retry line before the first event, no pings.
	want := "retry: 3000\ndata: {\"id\":\"\",\"type\":\"error\",\"message\":\"boom\",\"data\":null}\n\n"
	if got := w.String(); got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestStreamMaxAgeAndPing(t *testing.T) {
	var (
		w       = &syncBuf{}
		flushes int
		start   = time.Now()
	)

	// The stream ends by itself after MaxAge, with no event and a live context.
	err := Stream(context.Background(), w, func() { flushes++ }, make(chan Event),
		StreamOpt{MaxAge: 350 * time.Millisecond, Ping: 100 * time.Millisecond}, nolog)
	if err != nil {
		t.Fatal(err)
	}
	if took := time.Since(start); took < 350*time.Millisecond || took > 2*time.Second {
		t.Fatalf("the stream ended after %v, want 350ms", took)
	}

	got := w.String()
	if !strings.HasPrefix(got, "retry: 3000\n\n") {
		t.Fatalf("the stream does not start with the retry line: %q", got)
	}
	if n := strings.Count(got, ": ping\n\n"); n < 2 {
		t.Fatalf("got %d pings, want 3: %q", n, got)
	}
	if flushes < 3 {
		t.Fatalf("got %d flushes", flushes)
	}
}
