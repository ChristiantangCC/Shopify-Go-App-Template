package retry_test

import (
	"errors"
	"fmt"
	"testing"

	"github.com/ChristiantangCC/Shopify-Go-App-Template/internal/retry"
)

func TestPermanent(t *testing.T) {
	base := errors.New("bad payload")
	if retry.Permanent(nil) != nil {
		t.Fatal("Permanent(nil) must stay nil")
	}
	if retry.IsPermanent(base) {
		t.Fatal("a plain error is not permanent")
	}
	wrapped := fmt.Errorf("handler: %w", retry.Permanent(base))
	if !retry.IsPermanent(wrapped) || !errors.Is(wrapped, base) {
		t.Fatalf("wrapped permanent error lost: %v", wrapped)
	}
}
