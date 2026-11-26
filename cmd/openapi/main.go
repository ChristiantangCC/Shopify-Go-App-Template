// Command openapi prints the JSON API's OpenAPI 3.1 spec as YAML. It
// registers every operation (billing and the server-to-server routes
// included, whatever the environment enables) without starting anything:
//
//	go run ./cmd/openapi > api/openapi.yaml
//
// `bun run codegen:api` runs it and then regenerates the frontend's typed
// client from the result.
package main

import (
	"fmt"
	"io"
	"log/slog"
	"os"

	"github.com/go-chi/chi/v5"

	"github.com/ChristiantangCC/Shopify-Go-App-Template/internal/apikey"
	"github.com/ChristiantangCC/Shopify-Go-App-Template/internal/handlers"
)

func main() {
	if err := run(os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(out io.Writer) error {
	api := handlers.API{
		BillingEnabled: true,
		APIKeys:        apikey.NewChecker(nil),
		Logger:         slog.New(slog.DiscardHandler),
	}.Mount(chi.NewRouter())
	spec, err := api.OpenAPI().YAML()
	if err != nil {
		return err
	}
	_, err = out.Write(spec)
	return err
}
