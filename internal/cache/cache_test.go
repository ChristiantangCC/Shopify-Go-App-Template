package cache_test

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/ChristiantangCC/Shopify-Go-App-Template/internal/cache"
)

type value struct {
	Name string `json:"name"`
}

func TestNilCacheAlwaysLoads(t *testing.T) {
	calls := 0
	load := func(context.Context) (value, error) { calls++; return value{Name: "x"}, nil }
	for range 2 {
		if _, err := cache.GetOrLoad(context.Background(), nil, "k", time.Minute, load); err != nil {
			t.Fatal(err)
		}
	}
	if calls != 2 {
		t.Fatalf("nil cache must call load every time, got %d calls", calls)
	}
}

// TestGetOrLoadAndDelete needs Redis; it skips without TEST_REDIS_URL.
func TestGetOrLoadAndDelete(t *testing.T) {
	url := os.Getenv("TEST_REDIS_URL")
	if url == "" {
		t.Skip("TEST_REDIS_URL not set, skipping Redis integration test")
	}
	ctx := context.Background()
	c, err := cache.Connect(ctx, url, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Close() })
	key := "test:" + t.Name() + ":" + time.Now().Format(time.RFC3339Nano)
	t.Cleanup(func() { c.Delete(context.Background(), key) })

	calls := 0
	load := func(context.Context) (value, error) { calls++; return value{Name: "first"}, nil }
	for range 3 {
		got, err := cache.GetOrLoad(ctx, c, key, time.Minute, load)
		if err != nil || got.Name != "first" {
			t.Fatalf("got %+v err %v", got, err)
		}
	}
	if calls != 1 {
		t.Fatalf("load ran %d times, want 1", calls)
	}

	c.Delete(ctx, key)
	if _, err := cache.GetOrLoad(ctx, c, key, time.Minute, load); err != nil || calls != 2 {
		t.Fatalf("after delete load must run again: calls %d err %v", calls, err)
	}

	boom := errors.New("boom")
	failing := func(context.Context) (value, error) { return value{}, boom }
	if _, err := cache.GetOrLoad(ctx, c, key+":missing", time.Minute, failing); !errors.Is(err, boom) {
		t.Fatalf("load error must surface, got %v", err)
	}
}
