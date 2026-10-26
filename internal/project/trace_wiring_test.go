package project

import (
	"bytes"
	"context"
	"io/fs"
	"log/slog"
	"slices"
	"strings"
	"testing"

	"github.com/celest-io/remeris/internal/config"
	"github.com/celest-io/remeris/internal/git"
	"github.com/celest-io/remeris/pkg/versioning"
)

// TestStateTracesThroughTheCore proves that loading hands the core a tracer on the default logger: without it,
// --verbose loses the lines Knope prints while it loads a package. The default logger is global, so this test does
// not run in parallel.
func TestStateTracesThroughTheCore(t *testing.T) {
	var buf bytes.Buffer
	old := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{
		Level: slog.LevelDebug,
		ReplaceAttr: func(_ []string, a slog.Attr) slog.Attr {
			if a.Key == slog.TimeKey || a.Key == slog.LevelKey {
				return slog.Attr{}
			}
			return a
		},
	})))
	t.Cleanup(func() { slog.SetDefault(old) })

	var calls int
	l := Loader{
		ReadFile: func(path string) (string, error) {
			if path == versionTxt {
				return versionBody, nil
			}
			return "", fs.ErrNotExist
		},
		OpenGit: func(context.Context) (git.Git, error) { return fakeGit{calls: &calls}, nil },
	}
	pkgs := pkgCfg(versioning.CustomName("foo"), regexCfg(t, versionTxt))
	if _, err := l.State(context.Background(), []config.PackageConfig{pkgs}, true); err != nil {
		t.Fatal(err)
	}

	var got []string
	for _, line := range strings.Split(strings.TrimSpace(buf.String()), "\n") {
		got = append(got, strings.Trim(strings.TrimPrefix(line, "msg="), `"`))
	}
	want := []string{
		"Loading package foo",
		"VERSION.txt has version 1.2.3",
		"Looking for Git tags matching package name.",
		"No tags found starting with foo/v",
	}
	if !slices.Equal(got, want) {
		t.Errorf("debug lines = %q\nwant         %q", got, want)
	}
}
