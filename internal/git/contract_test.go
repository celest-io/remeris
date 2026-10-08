package git

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/celest-io/remeris/internal/diag"
)

// tagV1 is a tag name used by many cases.
const (
	tagV1 = "v1"
	tagV2 = "v2"
)

// gitBin is the real git binary, resolved once by absolute path so that repository setup does not depend on PATH
// (the go-git backend runs this suite with an empty PATH).
var gitBin, _ = exec.LookPath("git")

// testRepo builds a repository with the real git binary and deterministic commit dates.
type testRepo struct {
	t     *testing.T
	dir   string
	clock int
	// sameDate makes every command use the same date, to get commits with equal timestamps.
	sameDate bool
}

func newTestRepo(t *testing.T, init bool) *testRepo {
	t.Helper()
	if gitBin == "" {
		if os.Getenv("CI") != "" {
			t.Fatal("git is not installed, cannot build test repositories")
		}
		t.Skip("git is not installed, cannot build test repositories")
	}
	r := &testRepo{t: t, dir: t.TempDir()}
	if init {
		r.git("init", "--quiet", "--initial-branch=main")
	}
	return r
}

// cleanEnv is the environment without any GIT_* variable and without XDG_CONFIG_HOME, so that the caller's
// settings cannot leak into the test repositories.
func cleanEnv() []string {
	var env []string
	for _, kv := range os.Environ() {
		name, _, _ := strings.Cut(kv, "=")
		if !strings.HasPrefix(name, "GIT_") && name != "XDG_CONFIG_HOME" {
			env = append(env, kv)
		}
	}
	return env
}

func (r *testRepo) git(args ...string) string {
	r.t.Helper()
	if !r.sameDate {
		r.clock++
	}
	date := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC).Add(time.Duration(r.clock) * time.Minute).Format(time.RFC3339)
	full := append([]string{"-C", r.dir}, args...)
	cmd := exec.CommandContext(context.Background(), gitBin, full...) //nolint:gosec // test helper
	cmd.Env = append(cleanEnv(),
		"HOME="+r.dir, "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null", "LC_ALL=C",
		"GIT_CEILING_DIRECTORIES="+filepath.Dir(r.dir),
		"GIT_AUTHOR_NAME=Test Author", "GIT_AUTHOR_EMAIL=author@example.com", "GIT_AUTHOR_DATE="+date,
		"GIT_COMMITTER_NAME=Test Committer", "GIT_COMMITTER_EMAIL=committer@example.com", "GIT_COMMITTER_DATE="+date,
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		r.t.Fatalf("git %v: %v: %s", args, err, out)
	}
	return string(out)
}

func (r *testRepo) commit(msg string) {
	r.t.Helper()
	r.git("commit", "--quiet", "--allow-empty", "-m", msg)
}

func (r *testRepo) write(name, content string) {
	r.t.Helper()
	if err := os.WriteFile(filepath.Join(r.dir, name), []byte(content), 0o600); err != nil {
		r.t.Fatal(err)
	}
}

func (r *testRepo) lightweight(name string) { r.t.Helper(); r.git("tag", name) }
func (r *testRepo) annotated(name string) {
	r.t.Helper()
	r.git("tag", "-a", "-m", "release "+name, name)
}

// mergeHistory builds: one (v1) <- side (side-tag) and one <- two (v2), merged into main (v3).
func mergeHistory(r *testRepo) {
	r.commit("one")
	r.lightweight(tagV1)
	r.git("checkout", "--quiet", "-b", "side")
	r.commit("side")
	r.annotated("side-tag")
	r.git("checkout", "--quiet", "main")
	r.commit("two")
	r.lightweight(tagV2)
	r.git("merge", "--quiet", "--no-ff", "-m", "merge", "side")
	r.annotated("v3")
}

func runContract(t *testing.T, open func(t *testing.T, dir string) Git) {
	t.Helper()
	tests := map[string]struct {
		setup    func(r *testRepo)
		want     []string
		wantCode string // the diag code of the expected error, if any
	}{
		"no tags": {
			setup: func(r *testRepo) { r.commit("one"); r.commit("two") },
			want:  []string{},
		},
		"lightweight and annotated tags": {
			setup: func(r *testRepo) {
				r.commit("one")
				r.lightweight("v1.0.0")
				r.commit("two")
				r.annotated("v1.1.0")
				r.commit("three")
				r.lightweight("v1.2.0")
			},
			want: []string{"v1.2.0", "v1.1.0", "v1.0.0"},
		},
		"several tags on one commit": {
			setup: func(r *testRepo) {
				r.commit("one")
				r.lightweight("b")
				r.annotated("c")
				r.lightweight("a")
				r.commit("two")
				r.lightweight("z")
			},
			want: []string{"z", "a", "b", "c"},
		},
		"tags on an unmerged branch are excluded": {
			setup: func(r *testRepo) {
				r.commit("one")
				r.lightweight("v0.9.0")
				r.git("checkout", "--quiet", "-b", "side")
				r.commit("side")
				r.lightweight("v2.0.0")
				r.git("checkout", "--quiet", "main")
				r.commit("two")
				r.annotated("v1.1.0")
			},
			want: []string{"v1.1.0", "v0.9.0"},
		},
		"tag on HEAD": {
			setup: func(r *testRepo) { r.commit("one"); r.commit("two"); r.annotated("v9.0.0") },
			want:  []string{"v9.0.0"},
		},
		"merge commit history": {
			setup: mergeHistory,
			want:  []string{"v3", tagV2, "side-tag", tagV1},
		},
		"equal commit timestamps on a merge": {
			setup: func(r *testRepo) { r.sameDate = true; mergeHistory(r) },
			want:  []string{"v3", tagV2, "side-tag", tagV1},
		},
		"packed and loose tags on one commit": {
			setup: func(r *testRepo) {
				r.commit("one")
				r.lightweight("b")
				r.annotated("d")
				r.commit("two")
				r.lightweight("z")
				r.git("pack-refs", "--all")
				r.git("checkout", "--quiet", "main~1")
				r.lightweight("c")
				r.annotated("a")
				r.git("checkout", "--quiet", "main")
			},
			want: []string{"z", "a", "b", "c", "d"},
		},
		"a file named HEAD": {
			setup: func(r *testRepo) {
				r.commit("one")
				r.lightweight(tagV1)
				r.write("HEAD", "not a revision\n")
				r.git("add", "HEAD")
				r.commit("add HEAD")
				r.lightweight(tagV2)
			},
			want: []string{tagV2, tagV1},
		},
		"replaced objects are ignored": {
			setup: func(r *testRepo) {
				r.commit("a")
				r.lightweight("ta")
				r.commit("b")
				r.lightweight("tb")
				r.commit("c")
				r.lightweight("tc")
				r.git("replace", "HEAD~1", "HEAD~2")
			},
			want: []string{"tc", "tb", "ta"},
		},
		"tags that are not commits": {
			setup: func(r *testRepo) {
				r.commit("one")
				r.lightweight(tagV1)
				r.git("tag", "tree-tag", "HEAD^{tree}")
				r.git("tag", "-a", "-m", "tag of a tag", "outer", tagV1)
				r.commit("two")
			},
			want: []string{"outer", tagV1},
		},
		"empty repository": {
			setup:    func(*testRepo) {},
			wantCode: "remeris::git::command",
		},
		"not a repository": {
			setup:    func(*testRepo) {},
			wantCode: "remeris::git::open_repo",
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			r := newTestRepo(t, name != "not a repository")
			tt.setup(r)
			got, err := open(t, r.dir).AllTagsOnBranch(context.Background())
			if tt.wantCode != "" {
				if err == nil {
					t.Fatalf("expected an error, got tags %v", got)
				}
				var d diag.Diagnostic
				if !errors.As(err, &d) || d.Code() != tt.wantCode {
					t.Fatalf("error = %v, want a diagnostic with code %s", err, tt.wantCode)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !slices.Equal(got, tt.want) {
				t.Fatalf("tags = %v, want %v", got, tt.want)
			}
		})
	}
}

func openGit(t *testing.T, dir string) Git {
	t.Helper()
	g, err := Open(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	return g
}

func TestCLIContract(t *testing.T) {
	t.Parallel()
	runContract(t, openGit)
}

func TestGoGitContract(t *testing.T) {
	t.Parallel()
	runContract(t, func(t *testing.T, dir string) Git {
		t.Helper()
		g, err := NewGoGit(dir)
		if err != nil {
			t.Fatal(err)
		}
		return g
	})
}

// TestOpenFallsBackToGoGit cannot run in parallel: it sets PATH.
func TestOpenFallsBackToGoGit(t *testing.T) {
	r := newTestRepo(t, true) // repository setup uses the absolute gitBin, so an empty PATH is fine
	r.commit("one")
	r.lightweight(tagV1)
	if _, ok := openGit(t, r.dir).(*CLI); !ok {
		t.Fatal("Open should return the CLI backend when git is on PATH")
	}
	t.Setenv("PATH", t.TempDir())
	g := openGit(t, r.dir)
	if _, ok := g.(*GoGit); !ok {
		t.Fatalf("Open returned %T, want *GoGit", g)
	}
	got, err := g.AllTagsOnBranch(context.Background())
	if err != nil || !slices.Equal(got, []string{tagV1}) {
		t.Fatalf("tags = %v, err = %v", got, err)
	}
}

// snapshot lists the files under dir with their sizes and modification times.
func snapshot(t *testing.T, dir string) []string {
	t.Helper()
	var out []string
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		out = append(out, fmt.Sprintf("%s %s %s %d", path, info.ModTime(), info.Mode(), info.Size()))
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// TestCLIIgnoresRepositoryEnvironment checks that GIT_DIR and friends do not redirect git to another repository.
// It cannot run in parallel: it sets environment variables.
func TestCLIIgnoresRepositoryEnvironment(t *testing.T) {
	repo := newTestRepo(t, true)
	repo.commit("one")
	repo.lightweight("real")
	decoy := newTestRepo(t, true)
	decoy.commit("decoy")
	decoy.lightweight("decoy")
	before := snapshot(t, decoy.dir)

	t.Setenv("GIT_DIR", filepath.Join(decoy.dir, ".git"))
	t.Setenv("GIT_WORK_TREE", decoy.dir)
	t.Setenv("GIT_INDEX_FILE", filepath.Join(decoy.dir, ".git", "index"))
	got, err := openGit(t, repo.dir).AllTagsOnBranch(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"real"}; !slices.Equal(got, want) {
		t.Fatalf("tags = %v, want %v", got, want)
	}
	if after := snapshot(t, decoy.dir); !slices.Equal(before, after) {
		t.Fatalf("the decoy repository changed:\nbefore %v\nafter  %v", before, after)
	}
}

func TestGitEnv(t *testing.T) {
	t.Parallel()
	got := gitEnv([]string{"GIT_DIR=/x", "GIT_CONFIG_GLOBAL=/y", "HOME=/h", "GIT_WORK_TREE=/w", "GIT_INDEX_FILE=/i"})
	want := []string{"GIT_CONFIG_GLOBAL=/y", "HOME=/h", "GIT_TERMINAL_PROMPT=0", "LC_ALL=C"}
	if !slices.Equal(got, want) {
		t.Fatalf("gitEnv = %v, want %v", got, want)
	}
}
