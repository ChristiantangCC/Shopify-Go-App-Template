// Command fetch-schema downloads the Admin API schema as SDL from
// shopify.dev's public introspection proxy (no token needed) and writes it
// where genqlient expects it. bun run codegen runs it before generating.
package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/suessflorian/gqlfetch"
)

// Shopify wraps some deprecation reasons over several lines, which is
// invalid inside a single-quoted GraphQL string; fold them onto one line.
var (
	deprecatedReason = regexp.MustCompile(`(?s)@deprecated\(reason: "(.*?)"\)`)
	lineBreaks       = regexp.MustCompile(`\s*\n\s*`)
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	version := os.Getenv("SHOPIFY_API_VERSION")
	if version == "" {
		version = "2026-07"
	}
	endpoint := "https://shopify.dev/admin-graphql-direct-proxy/" + version

	fmt.Printf("Fetching Admin API schema %s ...\n", version)
	schema, err := gqlfetch.BuildClientSchema(context.Background(), endpoint, false)
	if err != nil {
		return fmt.Errorf("fetch-schema: %w", err)
	}

	schema = deprecatedReason.ReplaceAllStringFunc(schema, func(match string) string {
		return lineBreaks.ReplaceAllString(match, " ")
	})

	// Shopify's roots are QueryRoot and Mutation, not the spec defaults,
	// so the SDL needs an explicit schema block.
	if !strings.Contains(schema, "\nschema {") {
		schema = "schema {\n  query: QueryRoot\n  mutation: Mutation\n}\n\n" + schema
	}

	target := filepath.Join("internal", "shopify", "graphql", "schema.graphql")
	if err := os.WriteFile(target, []byte(schema), 0o644); err != nil {
		return fmt.Errorf("fetch-schema: %w", err)
	}
	fmt.Printf("Wrote %s (%d KB)\n", target, len(schema)/1024)
	return nil
}
