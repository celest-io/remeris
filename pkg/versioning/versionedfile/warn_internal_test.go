package versionedfile

import (
	"bytes"
	"log/slog"
	"testing"
)

// TestWarn replaces the default logger, so it must not run in parallel.
func TestWarn(t *testing.T) {
	var buf bytes.Buffer
	old := slog.Default()
	slog.SetDefault(
		slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{ReplaceAttr: func(_ []string, a slog.Attr) slog.Attr {
			if a.Key == slog.TimeKey || a.Key == slog.LevelKey {
				return slog.Attr{}
			}
			return a
		}})),
	)
	t.Cleanup(func() { slog.SetDefault(old) })

	warn("Cargo.lock is out of date")
	if got, want := buf.String(), "msg=\"Cargo.lock is out of date\"\n"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}
