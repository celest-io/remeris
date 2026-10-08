package git

import (
	"context"
	"os"
	"os/exec"
	"slices"
	"testing"
	"time"
)

// gitBin is the real git binary, resolved once by absolute path so that repository setup does not depend on PATH
// (the go-git backend runs this suite with an empty PATH).
var gitBin, _ = exec.LookPath("git")

// testRepo builds a repository with the real git binary and deterministic commit dates.
type testRepo struct {
	t     *testing.T
	dir   string
	clock int
}

func newTestRepo(t *testing.T, init bool) *testRepo {
	t.Helper()
	if gitBin == "" {
		t.Skip("git is not installed, cannot build test repositories")
	}
	r := &testRepo{t: t, dir: t.TempDir()}
	if init {
		r.git("init", "--quiet", "--initial-branch=main")
	}
	return r
}

func (r *testRepo) git(args ...string) string {
	r.t.Helper()
	r.clock++
	date := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC).Add(time.Duration(r.clock) * time.Minute).Format(time.RFC3339)
	full := append([]string{"-C", r.dir}, args...)
	cmd := exec.CommandContext(context.Background(), gitBin, full...) //nolint:gosec // test helper
	cmd.Env = append(os.Environ(),
		"HOME="+r.dir, "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null", "LC_ALL=C",
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

func (r *testRepo) lightweight(name string) { r.t.Helper(); r.git("tag", name) }
func (r *testRepo) annotated(name string) {
	r.t.Helper()
	r.git("tag", "-a", "-m", "release "+name, name)
}

func runContract(t *testing.T, open func(t *testing.T, dir string) Git) {
	t.Helper()
	tests := map[string]struct {
		setup   func(r *testRepo)
		want    []string
		wantErr bool
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
			setup: func(r *testRepo) {
				r.commit("one")
				r.lightweight("v1")
				r.git("checkout", "--quiet", "-b", "side")
				r.commit("side")
				r.annotated("side-tag")
				r.git("checkout", "--quiet", "main")
				r.commit("two")
				r.lightweight("v2")
				r.git("merge", "--quiet", "--no-ff", "-m", "merge", "side")
				r.annotated("v3")
			},
			want: []string{"v3", "v2", "side-tag", "v1"},
		},
		"tags that are not commits": {
			setup: func(r *testRepo) {
				r.commit("one")
				r.lightweight("v1")
				r.git("tag", "tree-tag", "HEAD^{tree}")
				r.git("tag", "-a", "-m", "tag of a tag", "outer", "v1")
				r.commit("two")
			},
			want: []string{"outer", "v1"},
		},
		"empty repository": {
			setup:   func(*testRepo) {},
			wantErr: true,
		},
		"not a repository": {
			setup:   func(*testRepo) {},
			wantErr: true,
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			r := newTestRepo(t, name != "not a repository")
			tt.setup(r)
			got, err := open(t, r.dir).AllTagsOnBranch(context.Background())
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected an error, got tags %v", got)
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

func TestCLIContract(t *testing.T) {
	t.Parallel()
	runContract(t, func(t *testing.T, dir string) Git {
		t.Helper()
		g, err := Open(context.Background(), dir)
		if err != nil {
			t.Fatal(err)
		}
		return g
	})
}
