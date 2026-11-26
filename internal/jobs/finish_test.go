package jobs

import (
	"errors"
	"testing"

	"github.com/riverqueue/river"

	"github.com/ChristiantangCC/Shopify-Go-App-Template/internal/retry"
)

func TestFinishCancelsOnlyPermanentErrors(t *testing.T) {
	var cancel *river.JobCancelError
	if err := finish(retry.Permanent(errors.New("bad json"))); !errors.As(err, &cancel) {
		t.Fatalf("permanent error must cancel the job, got %v", err)
	}
	transient := errors.New("shopify unreachable")
	if err := finish(transient); err != transient {
		t.Fatalf("transient error must pass through for a retry, got %v", err)
	}
	if finish(nil) != nil {
		t.Fatal("nil must stay nil")
	}
}
