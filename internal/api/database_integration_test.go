//go:build integration

package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"testing/fstest"
	"time"

	"hostbud/internal/store"
)

func TestIntegrationHealthTracksPostgresAvailability(t *testing.T) {
	host := os.Getenv("HOSTBUD_TEST_DB_HOST")
	if host == "" {
		host = "hostbud-test-postgres"
	}
	password := os.Getenv("HOSTBUD_TEST_DB_PASSWORD")
	if password == "" {
		password = "hostbud-test-" + "password" // #nosec G101 -- disposable test database credential.
	}
	conf := store.Config{Host: host, Port: 5432, Name: "hostbud_test", User: "hostbud_test", Password: password, SSLMode: "disable", Schema: "health_it_" + time.Now().UTC().Format("150405000000")}
	open := func() *store.Store {
		db, err := store.Open(t.Context(), conf)
		if err != nil {
			t.Fatalf("open integration database: %v", err)
		}
		return db
	}
	db := open()
	h := New(Config{Dist: fstest.MapFS{}, DBPing: db.Ping})
	status := func() int {
		req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/api/health", nil)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, req)
		return w.Code
	}
	if got := status(); got != http.StatusOK {
		t.Fatalf("healthy status=%d", got)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	if got := status(); got != http.StatusServiceUnavailable {
		t.Fatalf("closed pool status=%d", got)
	}
	db = open()
	defer func() { _ = db.Close() }()
	h = New(Config{Dist: fstest.MapFS{}, DBPing: db.Ping})
	if got := status(); got != http.StatusOK {
		t.Fatalf("recovered status=%d", got)
	}
}
