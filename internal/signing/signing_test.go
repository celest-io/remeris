package signing_test

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"github.com/celest-io/remeris/internal/git"
	"github.com/celest-io/remeris/internal/signing"
)

const (
	fileChange = "CHANGELOG.md"
	fileCS     = ".changeset/x.md"
	fileVer    = "VERSION"
	fileDocA   = "docs/a.md"
	fileDocB   = "docs/sub/b.md"
	headSHA    = "0123456789abcdef0123456789abcdef01234567"
)

// fakeGit is a Git and Committer that records what it was asked.
type fakeGit struct {
	git.Git // only the methods below are used

	commits  []git.CommitOptions
	messages []string
	sha      string
}

func (f *fakeGit) Changes(context.Context) ([]git.Change, error) { return nil, nil }

func (f *fakeGit) Commit(_ context.Context, msg string, o git.CommitOptions) (string, error) {
	f.commits = append(f.commits, o)
	f.messages = append(f.messages, msg)
	return f.sha, nil
}

func TestSelectIsAPlainGitCommit(t *testing.T) {
	t.Parallel()
	g := &fakeGit{sha: headSHA}
	s, err := signing.Select(t.Context(), signing.Settings{}, signing.Env{Git: g})
	if err != nil {
		t.Fatal(err)
	}
	if s.Name() != "git" || s.Signed() {
		t.Fatalf("signer = %q signed %v", s.Name(), s.Signed())
	}
	sha, err := s.Commit(
		t.Context(),
		signing.Request{Message: "msg", Changes: []git.Change{{Path: "b"}, {Path: "a", Deleted: true}}},
	)
	if err != nil || sha != headSHA {
		t.Fatalf("sha %q, error %v", sha, err)
	}
	want := []git.CommitOptions{{Paths: []string{"b", "a"}}}
	if !reflect.DeepEqual(g.commits, want) || g.messages[0] != "msg" {
		t.Fatalf("commits %+v, want %+v", g.commits, want)
	}
}

func TestSelectNeedsACommitter(t *testing.T) {
	t.Parallel()
	_, err := signing.Select(t.Context(), signing.Settings{}, signing.Env{Git: struct{ git.Git }{}})
	if err == nil || !strings.Contains(err.Error(), "Git repository") {
		t.Fatalf("error = %v", err)
	}
}

func TestOptionsOver(t *testing.T) {
	t.Parallel()
	tests := map[string]struct {
		high, low signing.Options
		want      signing.Settings
	}{
		"defaults": {},
		"low is used when high is unset": {
			low:  signing.Options{Include: []string{"a"}, Exclude: []string{"b"}},
			want: signing.Settings{Include: []string{"a"}, Exclude: []string{"b"}},
		},
		"high wins": {
			high: signing.Options{Include: []string{"x"}},
			low:  signing.Options{Include: []string{"a"}, Exclude: []string{"b"}},
			want: signing.Settings{Include: []string{"x"}, Exclude: []string{"b"}},
		},
		"an empty list replaces the lower one": {
			high: signing.Options{Exclude: []string{}},
			low:  signing.Options{Exclude: []string{"b"}},
			want: signing.Settings{Exclude: []string{}},
		},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if got := tc.high.Over(tc.low).Resolve(); !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("resolved %+v, want %+v", got, tc.want)
			}
		})
	}
}

func TestFilter(t *testing.T) {
	t.Parallel()
	changes := []git.Change{
		{Path: fileChange}, {Path: fileVer}, {Path: "mise.lock"}, {Path: "mise.claude.lock"},
		{Path: fileDocA}, {Path: fileDocB}, {Path: fileCS, Deleted: true},
	}
	paths := func(cs []git.Change) []string {
		out := make([]string, len(cs))
		for i, c := range cs {
			out[i] = c.Path
		}
		return out
	}
	tests := map[string]struct {
		include, exclude []string
		want             []string
		wantErr          bool
	}{
		"everything": {
			want: []string{
				fileCS,
				fileChange,
				fileVer,
				fileDocA,
				fileDocB,
				"mise.claude.lock",
				"mise.lock",
			},
		},
		"include narrows": {
			include: []string{fileChange, fileVer}, want: []string{fileChange, fileVer},
		},
		"exclude removes": {
			exclude: []string{"mise*.lock", "docs/**"},
			want:    []string{fileCS, fileChange, "VERSION"},
		},
		"exclude wins over include": {
			include: []string{"docs/**"}, exclude: []string{"docs/sub"}, want: []string{fileDocA},
		},
		"a directory selects what is under it": {include: []string{"docs"}, want: []string{fileDocA, fileDocB}},
		"a pattern that matches nothing":       {include: []string{"nothing"}, want: nil},
		"deleted files can be selected":        {include: []string{".changeset/*.md"}, want: []string{fileCS}},
		"a bad pattern":                        {include: []string{"a["}, wantErr: true},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			got, err := signing.Filter(changes, tc.include, tc.exclude)
			if (err != nil) != tc.wantErr {
				t.Fatalf("error = %v, wantErr %v", err, tc.wantErr)
			}
			if tc.wantErr {
				return
			}
			if p := paths(got); !reflect.DeepEqual(p, tc.want) && (len(p) != 0 || len(tc.want) != 0) {
				t.Fatalf("Filter = %v, want %v", p, tc.want)
			}
		})
	}
}
