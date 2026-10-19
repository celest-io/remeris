package git

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/celest-io/remeris/internal/diag"
)

// tagV1 is a tag name used by many cases.
const (
	tagV1   = "v1"
	tagV2   = "v2"
	tagV3   = "v3"
	tagSide = "side-tag"
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
	// target, when set, is the directory the backend opens instead of dir (a linked worktree or a clone).
	target string
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
	return r.gitIn("", args...)
}

// gitIn runs git with stdin.
func (r *testRepo) gitIn(stdin string, args ...string) string {
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
	cmd.Stdin = strings.NewReader(stdin)
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

// openDir is the directory the backend opens: target when a case set one, else the repository itself.
func (r *testRepo) openDir() string {
	if r.target != "" {
		return r.target
	}
	return r.dir
}

// shallowClone clones the repository with the given depth and makes the clone the target.
func (r *testRepo) shallowClone(depth int) {
	r.t.Helper()
	r.target = filepath.Join(r.t.TempDir(), "clone")
	r.git("clone", "--quiet", "--depth", strconv.Itoa(depth), "file://"+r.dir, r.target)
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
	r.annotated(tagSide)
	r.git("checkout", "--quiet", "main")
	r.commit("two")
	r.lightweight(tagV2)
	r.git("merge", "--quiet", "--no-ff", "-m", "merge", "side")
	r.annotated(tagV3)
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
			want:  []string{tagV3, tagV2, tagSide, tagV1},
		},
		"equal commit timestamps on a merge": {
			setup: func(r *testRepo) { r.sameDate = true; mergeHistory(r) },
			want:  []string{tagV3, tagV2, tagSide, tagV1},
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
		"linked worktree": {
			setup: func(r *testRepo) {
				mergeHistory(r)
				r.target = filepath.Join(r.t.TempDir(), "linked")
				r.git("worktree", "add", "--quiet", "--detach", r.target, "main~1")
			},
			want: []string{tagV2, tagV1},
		},
		"shallow clone of depth 1": {
			setup: func(r *testRepo) { mergeHistory(r); r.shallowClone(1) },
			want:  []string{tagV3},
		},
		"shallow clone of depth 2": {
			setup: func(r *testRepo) { mergeHistory(r); r.shallowClone(2) },
			want:  []string{tagV3, tagV2, tagSide},
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			r := newTestRepo(t, name != "not a repository")
			tt.setup(r)
			got, err := open(t, r.openDir()).AllTagsOnBranch(context.Background())
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

// runCancelled checks that an already-cancelled context gives an error satisfying errors.Is(err, context.Canceled),
// not a diagnostic about the repository.
func runCancelled(t *testing.T, open func(t *testing.T, dir string) Git) {
	t.Helper()
	r := newTestRepo(t, true)
	r.commit("one")
	r.lightweight(tagV1)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	got, err := open(t, r.dir).AllTagsOnBranch(ctx)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v (tags %v), want context.Canceled", err, got)
	}
	var d diag.Diagnostic
	if errors.As(err, &d) {
		t.Fatalf("error = %v, want the plain context error, not a diagnostic", err)
	}
}

func TestCLICancelledContext(t *testing.T) {
	t.Parallel()
	runCancelled(t, openGit)
}

func TestGoGitCancelledContext(t *testing.T) {
	t.Parallel()
	runCancelled(t, func(t *testing.T, dir string) Git {
		t.Helper()
		g, err := NewGoGit(dir)
		if err != nil {
			t.Fatal(err)
		}
		return g
	})
}

const (
	cfgName  = "Config Name"
	cfgEmail = "config@example.com"
)

var sha40 = regexp.MustCompile(`^[0-9a-f]{40}$`)

// isolateConfig points every Git configuration scope at an empty place and sets committer variables that must
// be ignored. It sets environment variables, so the tests that call it cannot run in parallel.
func isolateConfig(t *testing.T) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, "xdg"))
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	t.Setenv("GIT_COMMITTER_NAME", "Env Committer")
	t.Setenv("GIT_COMMITTER_EMAIL", "env@example.com")
}

// tagRepo returns a repository with one commit and, when identity is true, user.name and user.email set.
func tagRepo(t *testing.T, identity bool) *testRepo {
	t.Helper()
	isolateConfig(t)
	r := newTestRepo(t, true)
	r.commit("one")
	if identity {
		r.git("config", "user.name", cfgName)
		r.git("config", "user.email", cfgEmail)
	}
	return r
}

func wantDiagCode(t *testing.T, err error, code string) {
	t.Helper()
	var d diag.Diagnostic
	if err == nil || !errors.As(err, &d) || d.Code() != code {
		t.Fatalf("error = %v, want a diagnostic with code %s", err, code)
	}
}

func runTagContract(t *testing.T, open func(t *testing.T, dir string) Git) {
	t.Helper()
	ctx := context.Background()

	t.Run("annotated tag on HEAD with an empty message and the configured tagger", func(t *testing.T) {
		r := tagRepo(t, true)
		if err := open(t, r.dir).CreateTag(ctx, "v1.0.0", TagOptions{}); err != nil {
			t.Fatal(err)
		}
		if got := strings.TrimSpace(r.git("cat-file", "-t", "v1.0.0")); got != "tag" {
			t.Fatalf("object type = %q, want an annotated tag", got)
		}
		head := strings.TrimSpace(r.git("rev-parse", "HEAD"))
		if got := strings.TrimSpace(r.git("rev-parse", "v1.0.0^{commit}")); got != head {
			t.Fatalf("tag points to %s, want HEAD %s", got, head)
		}
		raw := r.git("cat-file", "tag", "v1.0.0")
		_, message, _ := strings.Cut(raw, "\n\n")
		if message != "" {
			t.Fatalf("message = %q, want empty", message)
		}
		if !regexp.MustCompile(`(?m)^tagger Config Name <config@example\.com> \d+ [+-]\d{4}$`).MatchString(raw) {
			t.Fatalf("tagger is not the configured identity:\n%s", raw)
		}
		if out := r.git("fsck", "--strict"); strings.Contains(out, "error") {
			t.Fatalf("fsck: %s", out)
		}
	})

	t.Run("an existing tag is an error and stays unchanged", func(t *testing.T) {
		r := tagRepo(t, true)
		r.annotated("v1.0.0")
		before := strings.TrimSpace(r.git("rev-parse", "v1.0.0"))
		r.commit("two")
		err := open(t, r.dir).CreateTag(ctx, "v1.0.0", TagOptions{})
		wantDiagCode(t, err, "remeris::git::command")
		if !strings.Contains(err.Error(), "Unknown Git error: ") || !strings.Contains(err.Error(), "already exists") {
			t.Fatalf("error = %v", err)
		}
		if after := strings.TrimSpace(r.git("rev-parse", "v1.0.0")); after != before {
			t.Fatalf("tag moved from %s to %s", before, after)
		}
	})

	t.Run("a missing identity is an error", func(t *testing.T) {
		for key, other := range map[string]string{"user.name": "user.email", "user.email": "user.name"} {
			r := tagRepo(t, false)
			r.git("config", other, "x")
			err := open(t, r.dir).CreateTag(ctx, "v1.0.0", TagOptions{})
			wantDiagCode(t, err, "remeris::git::no_committer")
			want := "Could not determine Git committer to commit changes: config value '" + key + "' was not found"
			if err.Error() != want {
				t.Fatalf("error = %q, want %q", err.Error(), want)
			}
			if out := r.git("tag", "--list"); out != "" {
				t.Fatalf("tags = %q, want none", out)
			}
		}
	})

	t.Run("the new tag is on the branch", func(t *testing.T) {
		r := tagRepo(t, true)
		g := open(t, r.dir)
		if err := g.CreateTag(ctx, "v1.0.0", TagOptions{}); err != nil {
			t.Fatal(err)
		}
		got, err := g.AllTagsOnBranch(ctx)
		if err != nil || !slices.Equal(got, []string{"v1.0.0"}) {
			t.Fatalf("tags = %v, err = %v", got, err)
		}
	})

	t.Run("no commits", func(t *testing.T) {
		isolateConfig(t)
		r := newTestRepo(t, true)
		r.git("config", "user.name", cfgName)
		r.git("config", "user.email", cfgEmail)
		g := open(t, r.dir)
		_, err := g.HeadCommitSHA(ctx)
		wantDiagCode(t, err, "remeris::git::command")
		wantDiagCode(t, g.CreateTag(ctx, "v1.0.0", TagOptions{}), "remeris::git::command")
	})

	t.Run("HeadCommitSHA", func(t *testing.T) {
		r := tagRepo(t, true)
		got, err := open(t, r.dir).HeadCommitSHA(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if want := strings.TrimSpace(r.git("rev-parse", "HEAD")); got != want || !sha40.MatchString(got) {
			t.Fatalf("sha = %q, want %q", got, want)
		}
	})
}

func TestCLICreateTag(t *testing.T) { runTagContract(t, openGit) } //nolint:paralleltest // sets environment variables

func TestGoGitCreateTag(t *testing.T) { //nolint:paralleltest // sets environment variables
	runTagContract(t, func(t *testing.T, dir string) Git {
		t.Helper()
		g, err := NewGoGit(dir)
		if err != nil {
			t.Fatal(err)
		}
		return g
	})
}
