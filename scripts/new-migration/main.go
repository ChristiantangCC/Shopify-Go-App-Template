// Command new-migration creates the next numbered SQL migration in
// internal/db/migrations, with goose's Up and Down sections:
//
//	bun run db:migration add_shop_plan
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

const dir = "internal/db/migrations"

var validName = regexp.MustCompile(`^[a-z0-9_]+$`)

const template = `-- +goose Up
-- Write the change here. Keep internal/store/models.go in step: the drift
-- test (go test ./internal/db) fails when they disagree.


-- +goose Down

`

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) != 1 || !validName.MatchString(args[0]) {
		return fmt.Errorf("usage: bun run db:migration <name_in_snake_case>")
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	next := 1
	for _, entry := range entries {
		number, _, ok := strings.Cut(entry.Name(), "_")
		if !ok {
			continue
		}
		if n, err := strconv.Atoi(number); err == nil && n >= next {
			next = n + 1
		}
	}
	path := filepath.Join(dir, fmt.Sprintf("%05d_%s.sql", next, args[0]))
	if err := os.WriteFile(path, []byte(template), 0o644); err != nil {
		return err
	}
	fmt.Println("created", path)
	return nil
}
