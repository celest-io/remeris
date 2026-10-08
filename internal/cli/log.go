package cli

import (
	"context"
	"io"
	"log/slog"
	"sync"
)

// plainHandler prints the bare message of each record on its own line: no time, level, target or
// attributes (spec F3.2). Remeris' own messages are the only ones that go through slog.
type plainHandler struct {
	mu    *sync.Mutex
	w     io.Writer
	level slog.Level
}

func newPlainHandler(w io.Writer, level slog.Level) *plainHandler {
	return &plainHandler{mu: &sync.Mutex{}, w: w, level: level}
}

func (h *plainHandler) Enabled(_ context.Context, level slog.Level) bool { return level >= h.level }

func (h *plainHandler) Handle(_ context.Context, r slog.Record) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	_, err := io.WriteString(h.w, r.Message+"\n")
	return err
}

func (h *plainHandler) WithAttrs([]slog.Attr) slog.Handler { return h }
func (h *plainHandler) WithGroup(string) slog.Handler      { return h }

// setupLogging installs the default logger: INFO, or DEBUG when verbose, to w. While quiet,
// nothing is printed (used by --validate).
func setupLogging(w io.Writer, verbose, quiet bool) {
	switch {
	case quiet:
		slog.SetDefault(slog.New(slog.DiscardHandler))
	case verbose:
		slog.SetDefault(slog.New(newPlainHandler(w, slog.LevelDebug)))
	default:
		slog.SetDefault(slog.New(newPlainHandler(w, slog.LevelInfo)))
	}
}
