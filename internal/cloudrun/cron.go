package cloudrun

import (
	"context"
	"errors"
	"log"
	"net/http"
	"strings"

	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/labstack/echo/v4"
)

const (
	googleIssuer = "https://accounts.google.com"
	googleCerts  = "https://www.googleapis.com/oauth2/v3/certs"
)

// CronJob runs one pass of a job that a timer runs inside the process when
// the app is not on Cloud Run. ran is false when the job is turned off in the
// settings, and the call did nothing.
type CronJob func() (ran bool, err error)

type cronResp struct {
	Job string `json:"job"`
	Ran bool   `json:"ran"`
}

// Cron returns the handler for POST /cron/:name, which Cloud Scheduler calls:
// it runs one pass of the named job. A 2xx answer tells Scheduler the job is
// done; a 500 lets it retry.
func Cron(jobs map[string]CronJob, lo *log.Logger) echo.HandlerFunc {
	return func(c echo.Context) error {
		name := c.Param("name")
		job, ok := jobs[name]
		if !ok {
			return echo.NewHTTPError(http.StatusNotFound, "unknown cron job")
		}

		ran, err := job()
		if err != nil {
			lo.Printf("error running cron job %s: %v", name, err)
			return echo.NewHTTPError(http.StatusInternalServerError, "cron job failed")
		}

		return c.JSON(http.StatusOK, map[string]any{"data": cronResp{Job: name, Ran: ran}})
	}
}

// OIDCCheck admits only requests that carry a Google-signed ID token for the
// given audience, issued to the given service account: Cloud Scheduler's
// requests made with --oidc-service-account-email and --oidc-token-audience.
// A public Cloud Run service cannot make one path private, so the app checks
// the token itself.
type OIDCCheck struct {
	verifier       *oidc.IDTokenVerifier
	serviceAccount string
}

// NewGoogleOIDC returns a check against Google's signing keys, which are
// fetched (and cached) on the first request.
func NewGoogleOIDC(audience, serviceAccount string) (*OIDCCheck, error) {
	if audience == "" || serviceAccount == "" {
		return nil, errors.New("the audience and the service account are both required")
	}

	keys := oidc.NewRemoteKeySet(context.Background(), googleCerts)
	return newOIDCCheck(oidc.NewVerifier(googleIssuer, keys, &oidc.Config{ClientID: audience}), serviceAccount), nil
}

func newOIDCCheck(v *oidc.IDTokenVerifier, serviceAccount string) *OIDCCheck {
	return &OIDCCheck{verifier: v, serviceAccount: strings.ToLower(serviceAccount)}
}

// Middleware answers 401 without a valid token and 403 for a valid token of
// another account.
func (o *OIDCCheck) Middleware(next echo.HandlerFunc) echo.HandlerFunc {
	return func(c echo.Context) error {
		raw, ok := strings.CutPrefix(c.Request().Header.Get(echo.HeaderAuthorization), "Bearer ")
		if !ok || raw == "" {
			return echo.NewHTTPError(http.StatusUnauthorized, "missing bearer token")
		}

		// Checks the signature, the issuer, the audience and the expiry.
		tok, err := o.verifier.Verify(c.Request().Context(), raw)
		if err != nil {
			return echo.NewHTTPError(http.StatusUnauthorized, "invalid token")
		}

		var claims struct {
			Email    string `json:"email"`
			Verified bool   `json:"email_verified"`
		}
		if err := tok.Claims(&claims); err != nil {
			return echo.NewHTTPError(http.StatusUnauthorized, "invalid token")
		}
		if !claims.Verified || strings.ToLower(claims.Email) != o.serviceAccount {
			return echo.NewHTTPError(http.StatusForbidden, "this account may not run cron jobs")
		}

		return next(c)
	}
}
