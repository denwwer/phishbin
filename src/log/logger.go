package log

import (
	"context"
	"log/slog"
	"os"
	"sync/atomic"
)

// errorCount is an atomic counter that tracks the number of error-level logs.
var errorCount atomic.Int64

type countingHandler struct {
	next slog.Handler
}

func (h countingHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.next.Enabled(ctx, level)
}

func (h countingHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return h.next.WithAttrs(attrs)
}

func (h countingHandler) WithGroup(name string) slog.Handler {
	return h.next.WithGroup(name)
}

func (h countingHandler) Handle(ctx context.Context, r slog.Record) error {
	if r.Level >= slog.LevelError {
		errorCount.Add(1)
	}
	return h.next.Handle(ctx, r)
}

func init() {
	handler := slog.NewTextHandler(os.Stderr, nil)
	slog.SetDefault(slog.New(countingHandler{next: handler}))
}

// ErrorCount returns the current number of error-level log entries.
func ErrorCount() int64 {
	return errorCount.Load()
}

// ResetErrorCount resets the atomic counter tracking error-level logs to zero.
func ResetErrorCount() {
	errorCount.Store(0)
}
