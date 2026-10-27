package conventional_test

import (
	"context"
	"log/slog"
	"slices"
	"sync"
	"testing"

	"github.com/celest-io/remeris/pkg/versioning/changes/conventional"
	"github.com/celest-io/remeris/pkg/versioning/trace"
)

type recorder struct {
	mu   *sync.Mutex
	msgs *[]string
}

func (recorder) Enabled(context.Context, slog.Level) bool { return true }
func (r recorder) Handle(_ context.Context, rec slog.Record) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	*r.msgs = append(*r.msgs, rec.Message)
	return nil
}
func (r recorder) WithAttrs([]slog.Attr) slog.Handler { return r }
func (r recorder) WithGroup(string) slog.Handler      { return r }

func TestChangesTracesScopes(t *testing.T) {
	t.Parallel()
	scoped := []conventional.CommitMessage{{Message: "feat(ui): button"}, {Message: "fix: plain"}}
	tests := map[string]struct {
		commits []conventional.CommitMessage
		scopes  []string
		want    []string
	}{
		"nil scopes print nothing": {commits: scoped},
		"empty scopes print brackets": {
			commits: scoped,
			scopes:  []string{},
			want:    []string{"Only checking commits with scopes: []"},
		},
		"scopes are listed with quotes": {
			commits: scoped, scopes: []string{"ui", `a"b`},
			want: []string{`Only checking commits with scopes: ["ui", "a\"b"]`},
		},
		"ignored commits still print the scopes": {
			scopes: []string{"ui"}, want: []string{`Only checking commits with scopes: ["ui"]`},
		},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			var msgs []string
			tr := trace.New(slog.New(recorder{&sync.Mutex{}, &msgs}))
			conventional.Changes(tc.commits, tc.scopes, nil, tr)
			if !slices.Equal(msgs, tc.want) {
				t.Errorf("debug lines = %q, want %q", msgs, tc.want)
			}
		})
	}
}
