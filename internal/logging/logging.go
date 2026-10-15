// Package logging builds the app's slog logger. Messages follow the
// convention "[Component] Capitalised message" with key/value attributes,
// and errors travel as the "error" attribute.
package logging

import (
	"log/slog"
	"os"
	"strings"
)

// Options configures New.
type Options struct {
	// Level is debug, info, warn or error; anything else means info.
	Level string
	// Format is "text" for human-readable development output; anything
	// else means JSON.
	Format string
}

// New returns a logger writing to stdout.
func New(opts Options) *slog.Logger {
	handlerOpts := &slog.HandlerOptions{Level: parseLevel(opts.Level)}
	if strings.EqualFold(opts.Format, "text") {
		return slog.New(slog.NewTextHandler(os.Stdout, handlerOpts))
	}
	return slog.New(slog.NewJSONHandler(os.Stdout, handlerOpts))
}

func parseLevel(level string) slog.Level {
	switch strings.ToLower(level) {
	case "debug":
		return slog.LevelDebug
	case "warn":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}
