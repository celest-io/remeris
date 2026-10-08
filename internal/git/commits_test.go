package git

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

const (
	authorTest    = "Test Author"
	authorUnicode = "Zoë Müller 日本"
	msgOne        = "one\n"
	msgTwo        = "two\n"
)

// commitFiles writes files (an empty content removes the file), stages everything and commits with the
// message kept verbatim.
func (r *testRepo) commitFiles(msg, author string, files map[string]string) {
	r.t.Helper()
	for name, content := range files {
		full := filepath.Join(r.dir, name)
		if content == "" {
			if err := os.Remove(full); err != nil {
				r.t.Fatal(err)
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(full), 0o700); err != nil {
			r.t.Fatal(err)
		}
		r.write(name, content)
	}
	r.git("add", "-A")
	r.git("commit", "--quiet", "--allow-empty", "--cleanup=verbatim", "--author="+author+" <a@example.com>", "-m", msg)
}

// commitWant is the expected message and author of a commit.
type commitWant struct{ msg, author string }

func runCommitsAfterTagContract(t *testing.T, open func(t *testing.T, dir string) Git) {
	t.Helper()
	ptr := func(s string) *string { return &s }
	plain := func(r *testRepo, msg string) { r.commitFiles(msg, authorTest, nil) }
	tests := map[string]struct {
		setup func(r *testRepo)
		tag   *string
		want  []commitWant
	}{
		"lightweight tag": {
			setup: func(r *testRepo) { plain(r, msgOne); r.lightweight(tagV1); plain(r, msgTwo); plain(r, "three\n") },
			tag:   ptr(tagV1),
			want:  []commitWant{{msgTwo, authorTest}, {"three\n", authorTest}},
		},
		"annotated tag": {
			setup: func(r *testRepo) { plain(r, msgOne); r.annotated(tagV1); plain(r, msgTwo) },
			tag:   ptr(tagV1),
			want:  []commitWant{{msgTwo, authorTest}},
		},
		"tag on HEAD": {
			setup: func(r *testRepo) { plain(r, msgOne); plain(r, msgTwo); r.annotated(tagV1) },
			tag:   ptr(tagV1),
			want:  []commitWant{},
		},
		"missing tag gives all commits": {
			setup: func(r *testRepo) { plain(r, msgOne); r.lightweight(tagV1); plain(r, msgTwo) },
			tag:   ptr("nope"),
			want:  []commitWant{{msgOne, authorTest}, {msgTwo, authorTest}},
		},
		"nil tag gives all commits": {
			setup: func(r *testRepo) { plain(r, msgOne); r.lightweight(tagV1); plain(r, msgTwo) },
			want:  []commitWant{{msgOne, authorTest}, {msgTwo, authorTest}},
		},
		"merge brings commits from both sides": {
			setup: mergeHistory,
			tag:   ptr(tagV1),
			want:  []commitWant{{"side\n", authorTest}, {msgTwo, authorTest}, {"merge\n", authorTest}},
		},
		"commits behind the tag on another branch are excluded": {
			setup: func(r *testRepo) {
				plain(r, "base\n")
				r.git("checkout", "--quiet", "-b", "side")
				plain(r, "side\n")
				r.git("checkout", "--quiet", "main")
				plain(r, msgTwo)
				r.git("merge", "--quiet", "--no-ff", "-m", "merge", "side")
				r.lightweight(tagV1)
				plain(r, "after\n")
			},
			tag:  ptr(tagV1),
			want: []commitWant{{"after\n", authorTest}},
		},
		"multi-line messages and trailing newlines": {
			setup: func(r *testRepo) {
				plain(r, "feat: title\n\nbody line 1\n\n\nbody line 2\n\nBREAKING CHANGE: x\n\n")
				plain(r, "no newline")
			},
			want: []commitWant{
				{"feat: title\n\nbody line 1\n\n\nbody line 2\n\nBREAKING CHANGE: x\n\n", authorTest},
				{"no newline\n", authorTest},
			},
		},
		"non-ASCII author and message": {
			setup: func(r *testRepo) { r.commitFiles("fix: café ☕\n", authorUnicode, nil) },
			want:  []commitWant{{"fix: café ☕\n", authorUnicode}},
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			r := newTestRepo(t, true)
			tt.setup(r)
			got, err := open(t, r.dir).CommitMessagesAfterTag(context.Background(), tt.tag)
			if err != nil {
				t.Fatal(err)
			}
			want := make([]commitWant, len(got))
			for i, c := range got {
				want[i] = commitWant{c.Message, c.Info.AuthorName}
			}
			if !slices.Equal(want, tt.want) && (len(got) != 0 || len(tt.want) != 0) {
				t.Fatalf("commits = %q, want %q", want, tt.want)
			}
			all := r.git("rev-list", "--all")
			for _, c := range got {
				if len(c.Info.Hash) != shortHashLen || !strings.Contains(all, c.Info.Hash) {
					t.Fatalf("hash %q is not the first 7 characters of a commit id", c.Info.Hash)
				}
			}
		})
	}
}

func TestCommitsAfterTagErrors(t *testing.T) {
	t.Parallel()
	for name, open := range map[string]func(*testing.T, string) Git{"cli": openGit, "gogit": openGoGit} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			r := newTestRepo(t, true) // no commits: HEAD is unborn
			if _, err := open(t, r.dir).CommitMessagesAfterTag(context.Background(), nil); err == nil {
				t.Fatal("expected an error for a repository without commits")
			}
		})
	}
}

func runFirstCommitsContract(t *testing.T, open func(t *testing.T, dir string) Git) {
	t.Helper()
	const fileA, fileB, fileC, nested = "a.md", "b.md", "c.md", ".changeset/dir/x.md"
	tests := map[string]struct {
		setup func(r *testRepo)
		paths []string
		want  map[string]string // path -> message of the expected commit
	}{
		"added in the first commit": {
			setup: func(r *testRepo) { r.commitFiles(msgOne, authorTest, map[string]string{fileA: "1"}) },
			paths: []string{fileA},
			want:  map[string]string{fileA: msgOne},
		},
		"added later": {
			setup: func(r *testRepo) {
				r.commitFiles(msgOne, authorTest, map[string]string{fileB: "1"})
				r.commitFiles(msgTwo, authorUnicode, map[string]string{fileA: "1"})
			},
			paths: []string{fileA},
			want:  map[string]string{fileA: msgTwo},
		},
		"modified later keeps the first commit": {
			setup: func(r *testRepo) {
				r.commitFiles(msgOne, authorTest, map[string]string{fileA: "1"})
				r.commitFiles(msgTwo, authorUnicode, map[string]string{fileA: "2"})
			},
			paths: []string{fileA},
			want:  map[string]string{fileA: msgOne},
		},
		"deleted and re-added keeps the first commit": {
			setup: func(r *testRepo) {
				r.commitFiles(msgOne, authorTest, map[string]string{fileA: "1"})
				r.commitFiles(msgTwo, authorTest, map[string]string{fileA: ""})
				r.commitFiles("three\n", authorTest, map[string]string{fileA: "1"})
			},
			paths: []string{fileA},
			want:  map[string]string{fileA: msgOne},
		},
		"path never present": {
			setup: func(r *testRepo) { r.commitFiles(msgOne, authorTest, map[string]string{fileA: "1"}) },
			paths: []string{"missing.md", "a.*"},
			want:  map[string]string{},
		},
		"several paths at once": {
			setup: func(r *testRepo) {
				r.commitFiles(msgOne, authorTest, map[string]string{fileA: "1"})
				r.commitFiles(msgTwo, authorUnicode, map[string]string{fileB: "1"})
			},
			paths: []string{fileB, fileA, "missing.md"},
			want:  map[string]string{fileA: msgOne, fileB: msgTwo},
		},
		"file in a subdirectory": {
			setup: func(r *testRepo) {
				r.commitFiles(msgOne, authorTest, map[string]string{fileA: "1"})
				r.commitFiles(msgTwo, authorTest, map[string]string{nested: "1"})
			},
			paths: []string{nested, ".changeset/dir"},
			want:  map[string]string{nested: msgTwo},
		},
		"added on a merged branch": {
			setup: func(r *testRepo) {
				r.commitFiles("base\n", authorTest, map[string]string{fileB: "1"})
				r.git("checkout", "--quiet", "-b", "side")
				r.commitFiles("side\n", authorUnicode, map[string]string{fileA: "1"})
				r.git("checkout", "--quiet", "main")
				r.commitFiles(msgTwo, authorTest, map[string]string{fileC: "1"})
				r.git("merge", "--quiet", "--no-ff", "-m", "merge", "side")
			},
			paths: []string{fileA, fileC},
			want:  map[string]string{fileA: "side\n", fileC: msgTwo},
		},
		"empty path list": {
			setup: func(r *testRepo) { r.commitFiles(msgOne, authorTest, map[string]string{fileA: "1"}) },
			want:  map[string]string{},
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			r := newTestRepo(t, true)
			tt.setup(r)
			got := open(t, r.dir).FirstCommitsForFiles(context.Background(), tt.paths)
			if len(got) != len(tt.want) {
				t.Fatalf("got %d results %v, want %d", len(got), got, len(tt.want))
			}
			for path, msg := range tt.want {
				info := got[path]
				if info == nil {
					t.Fatalf("no commit for %s", path)
				}
				hash := strings.TrimSpace(r.git("log", "-1", "--format=%H", "--grep=^"+strings.TrimSpace(msg)+"$", "HEAD"))
				if hash == "" || !strings.HasPrefix(hash, info.Hash) || len(info.Hash) != shortHashLen {
					t.Fatalf("%s: hash %q, want prefix of %q (commit %q)", path, info.Hash, hash, msg)
				}
			}
		})
	}
}

func TestFirstCommitsSwallowsErrors(t *testing.T) {
	t.Parallel()
	for name, open := range map[string]func(*testing.T, string) Git{"cli": openGit, "gogit": openGoGit} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			empty := newTestRepo(t, true)
			if got := open(t, empty.dir).FirstCommitsForFiles(context.Background(), []string{"a"}); len(got) != 0 {
				t.Fatalf("unborn HEAD: got %v", got)
			}
			notRepo := newTestRepo(t, false)
			if got := open(t, notRepo.dir).FirstCommitsForFiles(context.Background(), []string{"a"}); len(got) != 0 {
				t.Fatalf("not a repository: got %v", got)
			}
		})
	}
}

func openGoGit(t *testing.T, dir string) Git {
	t.Helper()
	g, err := NewGoGit(dir)
	if err != nil {
		t.Fatal(err)
	}
	return g
}

func TestCLICommits(t *testing.T) {
	t.Parallel()
	runCommitsAfterTagContract(t, openGit)
	runFirstCommitsContract(t, openGit)
}

func TestGoGitCommits(t *testing.T) {
	t.Parallel()
	runCommitsAfterTagContract(t, openGoGit)
	runFirstCommitsContract(t, openGoGit)
}
