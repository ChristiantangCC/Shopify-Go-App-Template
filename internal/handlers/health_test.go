package handlers_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ChristiantangCC/Shopify-Go-App-Template/internal/handlers"
	"github.com/ChristiantangCC/Shopify-Go-App-Template/internal/testdb"
)

func probe(t *testing.T, handler http.HandlerFunc) (int, map[string]any) {
	t.Helper()
	rec := httptest.NewRecorder()
	handler(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("body %q: %v", rec.Body.String(), err)
	}
	return rec.Code, body
}

func TestProbes(t *testing.T) {
	database := testdb.Open(t)
	probes := &handlers.Probes{Pool: database.Pool}

	if code, _ := probe(t, probes.Live); code != http.StatusOK {
		t.Fatalf("live: %d", code)
	}
	code, body := probe(t, probes.Ready)
	if code != http.StatusOK || body["database"] != "ok" || body["redis"] != "disabled" {
		t.Fatalf("ready: %d %v", code, body)
	}

	// Shutting down: unready, but still alive.
	probes.Drain()
	if code, body := probe(t, probes.Ready); code != http.StatusServiceUnavailable || body["draining"] != true {
		t.Fatalf("ready while draining: %d %v", code, body)
	}
	if code, _ := probe(t, probes.Live); code != http.StatusOK {
		t.Fatalf("live while draining: %d", code)
	}
}

func TestReadyFailsWithoutTheDatabase(t *testing.T) {
	database := testdb.Open(t)
	probes := &handlers.Probes{Pool: database.Pool}
	database.Pool.Close()

	if code, body := probe(t, probes.Ready); code != http.StatusServiceUnavailable || body["database"] != "unreachable" {
		t.Fatalf("ready without a database: %d %v", code, body)
	}
	if code, _ := probe(t, probes.Live); code != http.StatusOK {
		t.Fatalf("liveness must not depend on the database: %d", code)
	}
}
