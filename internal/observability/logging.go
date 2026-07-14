package observability

import (
	"io"
	"log/slog"
)

// NewLogger returns a JSON slog logger that redacts sensitive keys, arbitrary
// values, and every registered secret literal before bytes reach the writer.
func NewLogger(output io.Writer, service string, secrets ...string) *slog.Logger {
	base := slog.NewJSONHandler(output, &slog.HandlerOptions{Level: slog.LevelInfo})
	return slog.New(newRedactingHandler(base, secrets)).With("service", service)
}
