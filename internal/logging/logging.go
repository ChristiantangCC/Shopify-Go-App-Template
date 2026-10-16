// Package logging builds the app's slog logger. Messages follow the
// convention "[Component] Capitalised message" with key/value attributes,
// and errors travel as the "error" attribute.
package logging

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"
	"sync"
)

// Options configures New.
type Options struct {
	// Level is debug, info, warn or error; anything else means info.
	Level string
	// Format is "text" for human-readable development output; anything
	// else means JSON.
	Format string
}

// New returns a logger writing to stdout. The text format is compact and
// has no timestamp, because in development every line already gets one
// from `shopify app dev`; production JSON keeps full timestamps.
func New(opts Options) *slog.Logger {
	if strings.EqualFold(opts.Format, "text") {
		return slog.New(&compactHandler{out: os.Stdout, level: parseLevel(opts.Level), mu: &sync.Mutex{}})
	}
	return slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: parseLevel(opts.Level)}))
}

// WithMinLevel returns a logger that drops records below level, keeping
// the underlying handler. The River client gets one at WARN, so its
// periodic bookkeeping lines stay out of the log.
func WithMinLevel(logger *slog.Logger, level slog.Level) *slog.Logger {
	return slog.New(&levelFilter{inner: logger.Handler(), level: level})
}

type levelFilter struct {
	inner slog.Handler
	level slog.Level
}

func (f *levelFilter) Enabled(ctx context.Context, level slog.Level) bool {
	return level >= f.level && f.inner.Enabled(ctx, level)
}

func (f *levelFilter) Handle(ctx context.Context, record slog.Record) error {
	return f.inner.Handle(ctx, record)
}

func (f *levelFilter) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &levelFilter{inner: f.inner.WithAttrs(attrs), level: f.level}
}

func (f *levelFilter) WithGroup(name string) slog.Handler {
	return &levelFilter{inner: f.inner.WithGroup(name), level: f.level}
}

// compactHandler prints "INF [Component] Message key=value ...", one line
// per record, made to be read next to the Shopify CLI's own prefixes.
type compactHandler struct {
	out   io.Writer
	level slog.Level
	attrs []slog.Attr
	mu    *sync.Mutex
}

func (h *compactHandler) Enabled(_ context.Context, level slog.Level) bool {
	return level >= h.level
}

func (h *compactHandler) Handle(_ context.Context, record slog.Record) error {
	var line strings.Builder
	line.WriteString(shortLevel(record.Level))
	line.WriteString(" ")
	line.WriteString(record.Message)
	for _, attr := range h.attrs {
		writeAttr(&line, attr)
	}
	record.Attrs(func(attr slog.Attr) bool {
		writeAttr(&line, attr)
		return true
	})
	line.WriteString("\n")

	h.mu.Lock()
	defer h.mu.Unlock()
	_, err := io.WriteString(h.out, line.String())
	return err
}

func (h *compactHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	clone := *h
	clone.attrs = append(append([]slog.Attr{}, h.attrs...), attrs...)
	return &clone
}

// WithGroup flattens groups; this handler is for development reading, not
// structured processing.
func (h *compactHandler) WithGroup(name string) slog.Handler { return h }

func writeAttr(line *strings.Builder, attr slog.Attr) {
	value := attr.Value.String()
	if strings.ContainsAny(value, " \"") {
		value = fmt.Sprintf("%q", value)
	}
	fmt.Fprintf(line, " %s=%s", attr.Key, value)
}

func shortLevel(level slog.Level) string {
	switch {
	case level >= slog.LevelError:
		return "ERR"
	case level >= slog.LevelWarn:
		return "WRN"
	case level >= slog.LevelInfo:
		return "INF"
	default:
		return "DBG"
	}
}

// ShortID trims a chi request id like "hostname/aBcDe-000042" to its
// unique tail, which is all a human needs to match two log lines.
func ShortID(id string) string {
	if idx := strings.LastIndex(id, "/"); idx >= 0 {
		return id[idx+1:]
	}
	return id
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
