package admin

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

// fakeTokens counts refreshes and rotates from tok-1 to tok-2.
type fakeTokens struct {
	refreshes atomic.Int32
}

func (f *fakeTokens) AccessToken(ctx context.Context, shop string) (string, error) {
	return "tok-1", nil
}

func (f *fakeTokens) ForceRefresh(ctx context.Context, shop, failed string) (string, error) {
	f.refreshes.Add(1)
	return "tok-2", nil
}

// testClient serves the given status codes in order, then {"data":{}}.
func testClient(t *testing.T, statuses ...int) (*Client, *fakeTokens, *atomic.Int32) {
	t.Helper()
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := int(calls.Add(1))
		if n <= len(statuses) && statuses[n-1] != http.StatusOK {
			w.WriteHeader(statuses[n-1])
			return
		}
		w.Write([]byte(`{"data":{}}`))
	}))
	t.Cleanup(server.Close)
	tokens := &fakeTokens{}
	client := New("2026-07", tokens)
	client.endpoint = func(shop string) string { return server.URL }
	client.backoff = func(n int) time.Duration { return time.Millisecond }
	return client, tokens, &calls
}

func TestDoRetries(t *testing.T) {
	tests := []struct {
		name      string
		document  string
		statuses  []int
		wantErr   bool
		wantCalls int32
	}{
		{"query retried after a 503", `query { shop { id } }`, []int{503}, false, 2},
		{"query gives up after 3 retries", `query { shop { id } }`, []int{503, 503, 503, 503}, true, 4},
		{"mutation not retried after a 503", `mutation { productCreate { product { id } } }`, []int{503}, true, 1},
		{"mutation retried after a 429", `mutation { productCreate { product { id } } }`, []int{429}, false, 2},
		{"idempotent mutation retried", `mutation @idempotent(key: "k1") { x }`, []int{503}, false, 2},
		{"commented mutation is still a mutation", "# create\nmutation { x }", []int{503}, true, 1},
		{"a 400 is not retried", `query { shop { id } }`, []int{400}, true, 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client, _, calls := testClient(t, tt.statuses...)
			err := client.Do(context.Background(), "example.myshopify.com", tt.document, nil, nil)
			if (err != nil) != tt.wantErr || calls.Load() != tt.wantCalls {
				t.Fatalf("err %v calls %d, want err %v calls %d", err, calls.Load(), tt.wantErr, tt.wantCalls)
			}
		})
	}
}

func TestDoRefreshesOnceAfterA401(t *testing.T) {
	var sawTokens []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token := r.Header.Get("X-Shopify-Access-Token")
		sawTokens = append(sawTokens, token)
		if token == "tok-1" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.Write([]byte(`{"data":{}}`))
	}))
	t.Cleanup(server.Close)
	tokens := &fakeTokens{}
	client := New("2026-07", tokens)
	client.endpoint = func(shop string) string { return server.URL }

	if err := client.Do(context.Background(), "example.myshopify.com", `query { shop { id } }`, nil, nil); err != nil {
		t.Fatalf("Do: %v", err)
	}
	if tokens.refreshes.Load() != 1 || len(sawTokens) != 2 || sawTokens[1] != "tok-2" {
		t.Fatalf("refreshes %d tokens %v", tokens.refreshes.Load(), sawTokens)
	}
}

func TestDoGivesUpWhenTheRefreshedTokenIsRejectedToo(t *testing.T) {
	client, tokens, calls := testClient(t, 401, 401)
	err := client.Do(context.Background(), "example.myshopify.com", `query { x }`, nil, nil)
	if !errors.Is(err, errUnauthorized) {
		t.Fatalf("err = %v, want unauthorized", err)
	}
	if tokens.refreshes.Load() != 1 || calls.Load() != 2 {
		t.Fatalf("refreshes %d calls %d", tokens.refreshes.Load(), calls.Load())
	}
}

func TestDoRetriesThrottledWithTheComputedWait(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) == 1 {
			w.Write([]byte(`{"errors":[{"message":"Throttled","extensions":{"code":"THROTTLED"}}],"extensions":{"cost":{"requestedQueryCost":10,"throttleStatus":{"currentlyAvailable":5,"restoreRate":50}}}}`))
			return
		}
		w.Write([]byte(`{"data":{}}`))
	}))
	t.Cleanup(server.Close)
	client := New("2026-07", &fakeTokens{})
	client.endpoint = func(shop string) string { return server.URL }

	if err := client.Do(context.Background(), "example.myshopify.com", `query { x }`, nil, nil); err != nil {
		t.Fatalf("Do: %v", err)
	}
	if calls.Load() != 2 {
		t.Fatalf("calls %d", calls.Load())
	}
}

func TestThrottleWaitIsCapped(t *testing.T) {
	res := &graphqlResponse{}
	res.Extensions.Cost.RequestedQueryCost = 1000
	res.Extensions.Cost.ThrottleStatus.CurrentlyAvailable = 0
	res.Extensions.Cost.ThrottleStatus.RestoreRate = 1
	if wait := throttleWait(res); wait != maxThrottleWait {
		t.Fatalf("wait %v, want the %v cap", wait, maxThrottleWait)
	}
}

func TestRetryAfterIsHonoured(t *testing.T) {
	recorder := httptest.NewRecorder()
	recorder.Header().Set("Retry-After", "2")
	if got := retryAfter(recorder.Result()); got != 2*time.Second {
		t.Fatalf("retryAfter %v", got)
	}
	empty := httptest.NewRecorder()
	if got := retryAfter(empty.Result()); got != 0 {
		t.Fatalf("retryAfter without header %v", got)
	}
}
