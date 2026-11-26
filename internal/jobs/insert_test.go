package jobs_test

import (
	"context"
	"testing"

	"github.com/riverqueue/river"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"

	"github.com/ChristiantangCC/Shopify-Go-App-Template/internal/jobs"
	"github.com/ChristiantangCC/Shopify-Go-App-Template/internal/testdb"
)

// Opening the app repeatedly must queue one webhook sync per shop and
// host, while a new dev tunnel (another host) still gets its own.
func TestOncePerWindowDedupesAppOpenSyncs(t *testing.T) {
	database := testdb.Open(t)
	ctx := context.Background()
	client, err := river.NewClient(riverpgxv5.New(database.Pool), &river.Config{})
	if err != nil {
		t.Fatal(err)
	}
	insert := func(args jobs.SyncWebhooksArgs) bool {
		t.Helper()
		res, err := client.Insert(ctx, args, jobs.OncePerWindow())
		if err != nil {
			t.Fatal(err)
		}
		return !res.UniqueSkippedAsDuplicate
	}
	const shop = "example-shop.myshopify.com"

	if !insert(jobs.SyncWebhooksArgs{Shop: shop, Host: "https://a.example.com"}) {
		t.Fatal("first open must queue a sync")
	}
	if insert(jobs.SyncWebhooksArgs{Shop: shop, Host: "https://a.example.com"}) {
		t.Fatal("second open within the window must not queue another")
	}
	if !insert(jobs.SyncWebhooksArgs{Shop: shop, Host: "https://b.example.com"}) {
		t.Fatal("a new host must queue its own sync")
	}
	if !insert(jobs.SyncWebhooksArgs{Shop: "other-shop.myshopify.com", Host: "https://a.example.com"}) {
		t.Fatal("another shop must queue its own sync")
	}

	// Without OncePerWindow (a settings save) the sync always queues.
	res, err := client.Insert(ctx, jobs.SyncWebhooksArgs{Shop: shop, Host: "https://a.example.com"}, nil)
	if err != nil || res.UniqueSkippedAsDuplicate {
		t.Fatalf("settings-save sync: skipped %v err %v", res != nil && res.UniqueSkippedAsDuplicate, err)
	}
	if res.Job.MaxAttempts != 5 {
		t.Fatalf("sync MaxAttempts = %d, want 5", res.Job.MaxAttempts)
	}
}
