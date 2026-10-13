package cloudrun

import (
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/go-jose/go-jose/v4"
	"github.com/labstack/echo/v4"
)

const (
	testAudience = "https://listmonk-xyz.europe-west1.run.app"
	testAccount  = "scheduler@project.iam.gserviceaccount.com"
)

func signer(t *testing.T) (*rsa.PrivateKey, jose.Signer) {
	t.Helper()

	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	s, err := jose.NewSigner(jose.SigningKey{Algorithm: jose.RS256, Key: key}, (&jose.SignerOptions{}).WithType("JWT"))
	if err != nil {
		t.Fatal(err)
	}
	return key, s
}

func token(t *testing.T, s jose.Signer, claims map[string]any) string {
	t.Helper()

	b, _ := json.Marshal(claims)
	jws, err := s.Sign(b)
	if err != nil {
		t.Fatal(err)
	}
	out, err := jws.CompactSerialize()
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// claims are those of a Cloud Scheduler token, with the given changes.
func claims(change map[string]any) map[string]any {
	c := map[string]any{
		"iss":            googleIssuer,
		"aud":            testAudience,
		"email":          testAccount,
		"email_verified": true,
		"iat":            time.Now().Unix(),
		"exp":            time.Now().Add(time.Hour).Unix(),
	}
	for k, v := range change {
		c[k] = v
	}
	return c
}

func TestCronEndpoint(t *testing.T) {
	key, sign := signer(t)
	_, other := signer(t)

	keys := &oidc.StaticKeySet{PublicKeys: []crypto.PublicKey{&key.PublicKey}}
	check := newOIDCCheck(oidc.NewVerifier(googleIssuer, keys, &oidc.Config{ClientID: testAudience}), testAccount)

	runs := map[string]int{}
	jobs := map[string]CronJob{
		"db-vacuum":        func() (bool, error) { runs["db-vacuum"]++; return true, nil },
		"slow-query-cache": func() (bool, error) { return false, nil },
		"broken":           func() (bool, error) { return true, errors.New("boom") },
	}

	e := echo.New()
	e.POST("/cron/:name", Cron(jobs, log.New(io.Discard, "", 0)), check.Middleware)

	good := token(t, sign, claims(nil))
	for _, c := range []struct {
		name, method, path, token string
		want                      int
		body                      string
	}{
		{"a Scheduler token runs the job", http.MethodPost, "/cron/db-vacuum", good, 200, `{"data":{"job":"db-vacuum","ran":true}}`},
		{"a job turned off in the settings", http.MethodPost, "/cron/slow-query-cache", good, 200, `{"data":{"job":"slow-query-cache","ran":false}}`},
		{"a failing job lets Scheduler retry", http.MethodPost, "/cron/broken", good, 500, ""},
		{"an unknown job", http.MethodPost, "/cron/nope", good, 404, ""},
		{"no token", http.MethodPost, "/cron/db-vacuum", "", 401, ""},
		{"not a JWT", http.MethodPost, "/cron/db-vacuum", "abc", 401, ""},
		{"signed by another key", http.MethodPost, "/cron/db-vacuum", token(t, other, claims(nil)), 401, ""},
		{"for another audience", http.MethodPost, "/cron/db-vacuum", token(t, sign, claims(map[string]any{"aud": "https://other.run.app"})), 401, ""},
		{"from another issuer", http.MethodPost, "/cron/db-vacuum", token(t, sign, claims(map[string]any{"iss": "https://evil.example"})), 401, ""},
		{"expired", http.MethodPost, "/cron/db-vacuum", token(t, sign, claims(map[string]any{"exp": time.Now().Add(-time.Hour).Unix()})), 401, ""},
		{"another service account", http.MethodPost, "/cron/db-vacuum", token(t, sign, claims(map[string]any{"email": "someone@project.iam.gserviceaccount.com"})), 403, ""},
		{"an unverified email", http.MethodPost, "/cron/db-vacuum", token(t, sign, claims(map[string]any{"email_verified": false})), 403, ""},
		{"GET is not a cron call", http.MethodGet, "/cron/db-vacuum", good, 405, ""},
	} {
		t.Run(c.name, func(t *testing.T) {
			before := runs["db-vacuum"]

			req := httptest.NewRequest(c.method, c.path, nil)
			if c.token != "" {
				req.Header.Set(echo.HeaderAuthorization, "Bearer "+c.token)
			}
			rec := httptest.NewRecorder()
			e.ServeHTTP(rec, req)

			if rec.Code != c.want {
				t.Fatalf("got %d, want %d: %s", rec.Code, c.want, rec.Body.String())
			}
			if c.body != "" {
				var got, want any
				_ = json.Unmarshal(rec.Body.Bytes(), &got)
				_ = json.Unmarshal([]byte(c.body), &want)
				if !reflect.DeepEqual(got, want) {
					t.Fatalf("got %s, want %s", rec.Body.String(), c.body)
				}
			}

			ran := runs["db-vacuum"] - before
			if c.want == 200 && c.path == "/cron/db-vacuum" {
				if ran != 1 {
					t.Fatalf("the job ran %d times, want 1", ran)
				}
			} else if ran != 0 {
				t.Fatalf("the job ran without a valid call")
			}
		})
	}
}

func TestNewGoogleOIDCNeedsConfig(t *testing.T) {
	if _, err := NewGoogleOIDC("", testAccount); err == nil {
		t.Fatal("no error without an audience")
	}
	if _, err := NewGoogleOIDC(testAudience, ""); err == nil {
		t.Fatal("no error without a service account")
	}
	if _, err := NewGoogleOIDC(testAudience, testAccount); err != nil {
		t.Fatal(err)
	}
}
