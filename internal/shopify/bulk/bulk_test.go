package bulk

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestDownloadStreamsEveryLine(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"id":"gid://shopify/Product/1","title":"A"}` + "\n" + `{"id":"gid://shopify/Product/2","title":"B"}` + "\n"))
	}))
	t.Cleanup(server.Close)

	var lines []string
	err := Download(context.Background(), server.URL, func(line []byte) error {
		lines = append(lines, string(line))
		return nil
	})
	if err != nil || len(lines) != 2 {
		t.Fatalf("lines %d err %v", len(lines), err)
	}
	if lines[1] != `{"id":"gid://shopify/Product/2","title":"B"}` {
		t.Fatalf("line 2 = %q", lines[1])
	}
}

func TestDownloadStopsOnHandlerError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("{}\n{}\n{}\n"))
	}))
	t.Cleanup(server.Close)

	calls := 0
	boom := errors.New("boom")
	err := Download(context.Background(), server.URL, func(line []byte) error {
		calls++
		return boom
	})
	if !errors.Is(err, boom) || calls != 1 {
		t.Fatalf("calls %d err %v", calls, err)
	}
}

func TestDownloadRejectsNon200(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	t.Cleanup(server.Close)

	if err := Download(context.Background(), server.URL, func([]byte) error { return nil }); err == nil {
		t.Fatal("expected an error")
	}
}
