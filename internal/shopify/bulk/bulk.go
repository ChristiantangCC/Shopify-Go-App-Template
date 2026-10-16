// Package bulk runs Admin API bulk operations: the escape hatch when a
// query would page through thousands of records. Start one, let Shopify
// grind through it server-side, and handle the bulk_operations/finish
// webhook (see the registry), which downloads the JSONL result.
package bulk

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/ChristiantangCC/Shopify-Go-App-Template/internal/shopify/admin"
	"github.com/ChristiantangCC/Shopify-Go-App-Template/internal/shopify/graphql"
)

// ProductsQuery is the demo bulk document: every product's id and title.
// Bulk queries use plain connections without pagination arguments; Shopify
// walks them for you.
const ProductsQuery = `{
  products {
    edges {
      node {
        id
        title
      }
    }
  }
}`

// Start begins a bulk query for the shop and returns the operation id.
// Shopify allows one running bulk query per app and shop; starting a
// second one fails with a userError until the first finishes.
func Start(ctx context.Context, adminClient *admin.Client, shop, query string) (string, error) {
	res, err := graphql.BulkOperationRunQuery(ctx, adminClient.ForShop(shop), query, false)
	if err != nil {
		return "", fmt.Errorf("bulk: %w", err)
	}
	if errs := res.BulkOperationRunQuery.UserErrors; len(errs) > 0 {
		return "", fmt.Errorf("bulk: %s", errs[0].Message)
	}
	if res.BulkOperationRunQuery.BulkOperation == nil {
		return "", errors.New("bulk: no operation returned")
	}
	return res.BulkOperationRunQuery.BulkOperation.Id, nil
}

// Operation is one bulk operation's state.
type Operation struct {
	ID          string
	Status      string
	ErrorCode   string
	ObjectCount string
	// URL holds the JSONL result; empty while running and for zero results.
	URL string
}

// Get reads the operation, typically from the finish-webhook handler to
// learn the result URL.
func Get(ctx context.Context, adminClient *admin.Client, shop, id string) (Operation, error) {
	res, err := graphql.BulkOperationByID(ctx, adminClient.ForShop(shop), id)
	if err != nil {
		return Operation{}, fmt.Errorf("bulk: %w", err)
	}
	if res.Node == nil {
		return Operation{}, errors.New("bulk: operation not found")
	}
	node, ok := (*res.Node).(*graphql.BulkOperationNodeBulkOperation)
	if !ok || node == nil {
		return Operation{}, errors.New("bulk: operation not found")
	}
	op := Operation{ID: node.Id, Status: string(node.Status), ObjectCount: node.ObjectCount}
	if node.ErrorCode != nil {
		op.ErrorCode = string(*node.ErrorCode)
	}
	if node.Url != nil {
		op.URL = *node.Url
	}
	return op, nil
}

// Download streams the JSONL result and calls handle once per line. The
// URL is a signed, time-limited link Shopify hosts; no token is needed.
func Download(ctx context.Context, url string, handle func(line []byte) error) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("bulk: %w", err)
	}
	client := &http.Client{Timeout: 5 * time.Minute}
	res, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("bulk: download: %w", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("bulk: download answered %d", res.StatusCode)
	}

	scanner := bufio.NewScanner(res.Body)
	// A single JSONL line can be large (a product with many fields).
	scanner.Buffer(make([]byte, 0, 64<<10), 16<<20)
	for scanner.Scan() {
		if err := handle(scanner.Bytes()); err != nil {
			return err
		}
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("bulk: %w", err)
	}
	return nil
}
