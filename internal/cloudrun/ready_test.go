package cloudrun

import (
	"database/sql"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/labstack/echo/v4"
	_ "github.com/lib/pq"
)

func probe(t *testing.T, db *sql.DB) *httptest.ResponseRecorder {
	t.Helper()

	e := echo.New()
	e.GET("/ready", Ready(db, time.Second))

	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/ready", nil))
	return rec
}

func TestReadyWithoutDatabase(t *testing.T) {
	// Nothing listens on port 1.
	db, err := sql.Open("postgres", "host=127.0.0.1 port=1 user=listmonk dbname=listmonk sslmode=disable connect_timeout=1")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	if rec := probe(t, db); rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("got %d, want 503: %s", rec.Code, rec.Body.String())
	}
}

func TestReadyWithHungDatabase(t *testing.T) {
	// A server that accepts connections and never answers.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			defer conn.Close()
		}
	}()

	port := ln.Addr().(*net.TCPAddr).Port
	db, err := sql.Open("postgres", fmt.Sprintf("host=127.0.0.1 port=%d user=listmonk dbname=listmonk sslmode=disable", port))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	start := time.Now()
	rec := probe(t, db)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("got %d, want 503: %s", rec.Code, rec.Body.String())
	}
	if took := time.Since(start); took > 2*time.Second {
		t.Fatalf("the probe took %v, want it to answer at its 1s timeout", took)
	}
}

func TestReadyWithDatabase(t *testing.T) {
	db := testDB(t)

	if rec := probe(t, db); rec.Code != http.StatusOK {
		t.Fatalf("got %d, want 200: %s", rec.Code, rec.Body.String())
	}
}

// testDB connects to the Postgres in LISTMONK_TEST_DB_DSN, or skips the test.
func testDB(t *testing.T) *sql.DB {
	t.Helper()

	dsn := os.Getenv("LISTMONK_TEST_DB_DSN")
	if dsn == "" {
		t.Skip("LISTMONK_TEST_DB_DSN is not set")
	}

	db, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })

	return db
}
