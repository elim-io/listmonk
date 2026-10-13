package main

import (
	"fmt"
	"log"
	"time"

	"github.com/knadh/listmonk/internal/events"
	"github.com/labstack/echo/v4"
)

// EventStream serves an endpoint that never closes and pushes a
// live event stream (text/event-stream) such as a error messages.
func (a *App) EventStream(c echo.Context) error {
	hdr := c.Response().Header()
	hdr.Set(echo.HeaderContentType, "text/event-stream")
	hdr.Set(echo.HeaderCacheControl, "no-store")
	hdr.Set(echo.HeaderConnection, "keep-alive")

	// Subscribe to the event stream with a random ID.
	id := fmt.Sprintf("api:%v", time.Now().UnixNano())
	sub, err := a.events.Subscribe(id)
	if err != nil {
		log.Fatalf("error subscribing to events: %v", err)
	}

	// On HTTP connection close (or the stream's end), unsubscribe.
	defer a.events.Unsubscribe(id)

	opt := events.StreamOpt{
		MaxAge: a.cfg.CloudRun.EventsMaxAge,
		Ping:   a.cfg.CloudRun.EventsPing,
	}
	// A write error means the client went away: there is no one to tell.
	_ = events.Stream(c.Request().Context(), c.Response(), c.Response().Flush, sub, opt, a.log)

	return nil
}
