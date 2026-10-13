// Package cloudrun has the pieces listmonk needs to run on Google Cloud Run
// (docs/docs/content/cloud-run.md). Each of them is off unless it is turned on
// in the [cloud_run] config.
package cloudrun

import (
	"context"
	"database/sql"
	"net/http"
	"time"

	"github.com/labstack/echo/v4"
)

// Ready returns a handler for Cloud Run's startup probe. It answers 200 only
// when the database answers a ping within the timeout, and 503 otherwise, so
// that a new instance gets traffic only once it can reach Postgres. Unlike
// GET /health, it should not be used as a liveness probe: when the database
// is down, every instance would fail it and be restarted at once.
func Ready(db *sql.DB, timeout time.Duration) echo.HandlerFunc {
	return func(c echo.Context) error {
		ctx, cancel := context.WithTimeout(c.Request().Context(), timeout)
		defer cancel()

		// The driver can block past the context's deadline on a database that
		// accepts connections and never answers, so the probe does not wait for it.
		errc := make(chan error, 1)
		go func() { errc <- db.PingContext(ctx) }()

		select {
		case err := <-errc:
			if err == nil {
				return c.JSON(http.StatusOK, map[string]bool{"data": true})
			}
		case <-ctx.Done():
		}

		return c.JSON(http.StatusServiceUnavailable, map[string]string{"message": "database unavailable"})
	}
}
