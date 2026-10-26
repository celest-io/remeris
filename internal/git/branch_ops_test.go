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
	branchMain    = "main"
	branchOther   = "other"
	fileA         = "a.txt"
	codeBadBranch = "remeris::git::bad_branch_name"
	onMain        = "main\n"
	onOther       = "other\n"
	backendCLI    = "cli"
	backendGoGit  = "gogit"
)

// hostileBranchNames are names that git would take for an option or refuse. None of them may reach the git command.
var hostileBranchNames = []string{
	"", "-", "--force", "-D", "--end-of-options", "--orphan=x", "HEAD", "@", "a b", "a..b", "x.lock", "a/.b", "a/",
	"/a", "a//b", "a.", "@{-1}", "a~1", "a^", "a:b", "a?b", "a*b", "a[b", "a\\b", "a\nb", "a\x00b", "a\x7fb",
}

// branchRepo is a repository on main with a committed a.txt and a .gitignore for ignored.txt, and a branch "other"
// one commit ahead with another a.txt.
func branchRepo(t *testing.T) *testRepo {
	t.Helper()
	isolateConfig(t)
	r := newTestRepo(t, true)
	r.write(fileA, "main\n")
	r.write(".gitignore", "ignored.txt\n")
	r.git("add", ".")
	r.commit("one")
	r.git("switch", "--quiet", "-c", branchOther)
	r.write(fileA, "other\n")
	r.git("commit", "--quiet", "-am", "two")
	r.git("switch", "--quiet", branchMain)
	return r
}

// eachBackend runs fn on a fresh branchRepo for the CLI and the go-git backend (with an empty PATH).
func eachBackend(t *testing.T, fn func(t *testing.T, r *testRepo, g Git)) {
	t.Helper()
	for _, backend := range []string{backendCLI, backendGoGit} {
		t.Run(backend, func(t *testing.T) { //nolint:paralleltest // sets environment variables
			r := branchRepo(t)
			var g Git
			if backend == backendCLI {
				g = openGit(t, r.dir)
			} else {
				var err error
				if g, err = NewGoGit(r.dir); err != nil {
					t.Fatal(err)
				}
				t.Setenv("PATH", t.TempDir())
			}
			fn(t, r, g)
		})
	}
}

func head(r *testRepo) string { return r.git("symbolic-ref", "--short", "-q", "HEAD") }

func TestLocalBranches(t *testing.T) { //nolint:paralleltest // sets environment variables
	eachBackend(t, func(t *testing.T, r *testRepo, g Git) {
		r.git("branch", "feature/x")
		got, err := g.LocalBranches(context.Background())
		if want := []string{"feature/x", branchMain, branchOther}; err != nil || !slices.Equal(got, want) {
			t.Fatalf("LocalBranches = %v, %v, want %v", got, err, want)
		}
	})
}

func TestBranchExists(t *testing.T) { //nolint:paralleltest // sets environment variables
	eachBackend(t, func(t *testing.T, r *testRepo, g Git) {
		r.git("branch", "feature/x")
		r.git("tag", "sometag")
		tests := map[string]bool{
			branchMain: true, branchOther: true, "feature/x": true, "feature": false, "nope": false, "sometag": false,
		}
		for name, want := range tests {
			if got, err := g.BranchExists(context.Background(), name); err != nil || got != want {
				t.Errorf("BranchExists(%q) = %t, %v, want %t", name, got, err, want)
			}
		}
	})
}

func TestHasUncommittedChanges(t *testing.T) { //nolint:paralleltest // sets environment variables
	tests := map[string]struct {
		change func(r *testRepo)
		want   bool
	}{
		"clean":   {change: func(*testRepo) {}},
		"ignored": {change: func(r *testRepo) { r.write("ignored.txt", "x") }},
		"ignored by info/exclude": {change: func(r *testRepo) {
			r.write(".git/info/exclude", "excluded.txt\n")
			r.write("excluded.txt", "x")
		}},
		"ignored by core.excludesFile": {change: func(r *testRepo) {
			r.write("global-ignore", "global.txt\n")
			r.git("config", "core.excludesFile", filepath.Join(r.dir, "global-ignore"))
			r.write("global.txt", "x")
			r.write("global-ignore", "global.txt\n") // the file itself must be ignored too
			r.write(".git/info/exclude", "global-ignore\n")
		}},
		"modified":  {change: func(r *testRepo) { r.write(fileA, "changed\n") }, want: true},
		"untracked": {change: func(r *testRepo) { r.write("new.txt", "x") }, want: true},
		"untracked in a directory": {change: func(r *testRepo) {
			if err := os.MkdirAll(filepath.Join(r.dir, "d"), 0o750); err != nil {
				t.Fatal(err)
			}
			r.write("d/new.txt", "x")
		}, want: true},
		"staged":  {change: func(r *testRepo) { r.write(fileA, "c\n"); r.git("add", fileA) }, want: true},
		"deleted": {change: func(r *testRepo) { r.git("rm", "--quiet", fileA) }, want: true},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) { //nolint:paralleltest // sets environment variables
			eachBackend(t, func(t *testing.T, r *testRepo, g Git) {
				tt.change(r)
				if got, err := g.HasUncommittedChanges(context.Background()); err != nil || got != tt.want {
					t.Fatalf("HasUncommittedChanges = %t, %v, want %t", got, err, tt.want)
				}
			})
		})
	}
}

func TestCreateBranch(t *testing.T) { //nolint:paralleltest // sets environment variables
	ctx := context.Background()
	eachBackend(t, func(t *testing.T, r *testRepo, g Git) {
		if err := g.CreateBranch(ctx, "issue/1-fix", branchOther); err != nil {
			t.Fatal(err)
		}
		if got, want := r.git("rev-parse", "issue/1-fix"), r.git("rev-parse", branchOther); got != want {
			t.Fatalf("new branch at %s, want %s", got, want)
		}
		if got := head(r); got != onMain {
			t.Fatalf("CreateBranch moved HEAD to %q", got)
		}
		if got := r.git("for-each-ref", "--format=%(upstream)", "refs/heads/issue/1-fix"); got != "\n" {
			t.Fatalf("branch is tracking: %q", got)
		}
		wantDiagCode(t, g.CreateBranch(ctx, "issue/1-fix", branchMain), "remeris::git::command") // exists
		wantDiagCode(t, g.CreateBranch(ctx, "fresh", "missing"), "remeris::git::command")
		wantDiagCode(t, g.CreateBranch(ctx, "fresh", "-D"), codeBadBranch)
		if ok, _ := g.BranchExists(ctx, "fresh"); ok {
			t.Fatal("a failed CreateBranch created the branch")
		}
	})
}

func TestBranchNamesAreNeverOptions(t *testing.T) { //nolint:paralleltest // sets environment variables
	ctx := context.Background()
	eachBackend(t, func(t *testing.T, r *testRepo, g Git) {
		for _, name := range hostileBranchNames {
			if _, err := g.BranchExists(ctx, name); err == nil {
				t.Errorf("BranchExists(%q) succeeded", name)
			}
			wantDiagCode(t, g.CreateBranch(ctx, name, branchMain), codeBadBranch)
			wantDiagCode(t, g.SwitchTo(ctx, name), codeBadBranch)
		}
		if got, err := g.LocalBranches(ctx); err != nil || !slices.Equal(got, []string{branchMain, branchOther}) {
			t.Fatalf("branches = %v, %v", got, err)
		}
		// The names above are rejected by checkBranchName, so --end-of-options is only a second guard. A name that
		// looks like a shell command is a valid ref name: it is created literally and never run.
		name := "$(touch${IFS}pwned);`touch${IFS}pwned`"
		if err := g.CreateBranch(ctx, name, branchMain); err != nil {
			t.Fatal(err)
		}
		if ok, err := g.BranchExists(ctx, name); err != nil || !ok {
			t.Fatalf("BranchExists(%q) = %t, %v", name, ok, err)
		}
		if got, err := g.LocalBranches(ctx); err != nil || !slices.Contains(got, name) {
			t.Fatalf("branches = %v, %v, want %q in them", got, err, name)
		}
		cwd, _ := os.Getwd()
		for _, dir := range []string{r.dir, cwd} {
			if _, err := os.Stat(filepath.Join(dir, "pwned")); err == nil {
				t.Fatalf("a branch name was run by a shell (pwned in %s)", dir)
			}
		}
	})
}

func TestSwitchTo(t *testing.T) { //nolint:paralleltest // sets environment variables
	ctx := context.Background()
	t.Run("clean tree", func(t *testing.T) {
		eachBackend(t, func(t *testing.T, r *testRepo, g Git) {
			r.write("ignored.txt", "kept")
			if err := g.SwitchTo(ctx, branchOther); err != nil {
				t.Fatal(err)
			}
			if got := head(r); got != onOther {
				t.Fatalf("HEAD = %q", got)
			}
			if b, _ := os.ReadFile(filepath.Join(r.dir, fileA)); string(b) != onOther {
				t.Fatalf("a.txt = %q", b)
			}
			if b, _ := os.ReadFile(filepath.Join(r.dir, "ignored.txt")); string(b) != "kept" {
				t.Fatalf("ignored.txt = %q", b)
			}
			if dirty, err := g.HasUncommittedChanges(ctx); err != nil || dirty {
				t.Fatalf("dirty after switch: %t, %v", dirty, err)
			}
		})
	})
	t.Run("dirty tree", func(t *testing.T) {
		for name, change := range map[string]func(r *testRepo){
			"modified":  func(r *testRepo) { r.write(fileA, "changed\n") },
			"untracked": func(r *testRepo) { r.write("new.txt", "x") },
		} {
			t.Run(name, func(t *testing.T) {
				eachBackend(t, func(t *testing.T, r *testRepo, g Git) {
					change(r)
					err := g.SwitchTo(ctx, branchOther)
					wantDiagCode(t, err, "remeris::git::uncommitted_changes")
					if got := head(r); got != onMain {
						t.Fatalf("HEAD moved to %q", got)
					}
				})
			})
		}
	})
	t.Run("missing branch", func(t *testing.T) {
		eachBackend(t, func(t *testing.T, r *testRepo, g Git) {
			wantDiagCode(t, g.SwitchTo(ctx, "nope"), "remeris::git::command")
			r.git("tag", "sometag")
			wantDiagCode(t, g.SwitchTo(ctx, "sometag"), "remeris::git::command")
			if got := head(r); got != onMain {
				t.Fatalf("HEAD moved to %q", got)
			}
		})
	})
	t.Run("detached HEAD", func(t *testing.T) {
		eachBackend(t, func(t *testing.T, r *testRepo, g Git) {
			r.git("checkout", "--quiet", "--detach")
			if err := g.SwitchTo(ctx, branchOther); err != nil {
				t.Fatal(err)
			}
			if got, err := g.CurrentBranch(ctx); err != nil || got != branchOther {
				t.Fatalf("CurrentBranch = %q, %v", got, err)
			}
		})
	})
	t.Run("branch checked out in another worktree", func(t *testing.T) {
		eachBackend(t, func(t *testing.T, r *testRepo, g Git) {
			other := filepath.Join(t.TempDir(), "wt")
			r.git("worktree", "add", "--quiet", other, branchOther)
			err := g.SwitchTo(ctx, branchOther)
			wantDiagCode(t, err, "remeris::git::command")
			if got := head(r); got != onMain {
				t.Fatalf("HEAD moved to %q", got)
			}
			// From the linked worktree, main (checked out in the main one) is refused too, and its own branch is not.
			linked, err := NewGoGit(other)
			if err != nil {
				t.Fatal(err)
			}
			wantDiagCode(t, linked.SwitchTo(ctx, branchMain), "remeris::git::command")
			if err := linked.SwitchTo(ctx, branchOther); err != nil {
				t.Fatalf("switching to the current branch: %v", err)
			}
		})
	})
	t.Run("branch name that is also a file", func(t *testing.T) {
		eachBackend(t, func(t *testing.T, r *testRepo, g Git) {
			r.git("branch", "a.txt")
			if err := g.SwitchTo(ctx, fileA); err != nil {
				t.Fatal(err)
			}
			if got := head(r); got != "a.txt\n" {
				t.Fatalf("HEAD = %q", got)
			}
		})
	})
}

// ahead puts main one commit ahead of other (other is an ancestor of main).
func ahead(r *testRepo) {
	r.git("branch", "-f", branchOther, branchMain)
	r.write("b.txt", "b\n")
	r.git("add", ".")
	r.commit("three")
}

func TestRebase(t *testing.T) { //nolint:paralleltest // sets environment variables
	ctx := context.Background()
	t.Run("ancestor", func(t *testing.T) {
		eachBackend(t, func(t *testing.T, r *testRepo, g Git) {
			ahead(r)
			r.write("ignored.txt", "kept")
			if err := g.Rebase(ctx, branchOther); err != nil {
				t.Fatal(err)
			}
			if got := head(r); got != onOther {
				t.Fatalf("HEAD = %q", got)
			}
			if got, want := r.git("rev-parse", branchOther), r.git("rev-parse", branchMain); got != want {
				t.Fatalf("other at %s, want %s", got, want)
			}
			if _, err := os.Stat(filepath.Join(r.dir, "b.txt")); err != nil {
				t.Fatalf("working tree changed: %v", err)
			}
			if b, _ := os.ReadFile(filepath.Join(r.dir, "ignored.txt")); string(b) != "kept" {
				t.Fatalf("ignored.txt = %q", b)
			}
			if dirty, err := g.HasUncommittedChanges(ctx); err != nil || dirty {
				t.Fatalf("dirty after rebase: %t, %v", dirty, err)
			}
		})
	})
	t.Run("equal", func(t *testing.T) {
		eachBackend(t, func(t *testing.T, r *testRepo, g Git) {
			r.git("branch", "same")
			if err := g.Rebase(ctx, "same"); err != nil {
				t.Fatal(err)
			}
			if got := head(r); got != "same\n" {
				t.Fatalf("HEAD = %q", got)
			}
		})
	})
	t.Run("current branch", func(t *testing.T) {
		eachBackend(t, func(t *testing.T, r *testRepo, g Git) {
			if err := g.Rebase(ctx, branchMain); err != nil {
				t.Fatal(err)
			}
			if got := head(r); got != onMain {
				t.Fatalf("HEAD = %q", got)
			}
		})
	})
	t.Run("not a fast-forward", func(t *testing.T) {
		for name, extra := range map[string]bool{"to is ahead": false, "both have commits": true} {
			t.Run(name, func(t *testing.T) {
				eachBackend(t, func(t *testing.T, r *testRepo, g Git) {
					if extra {
						r.write("m.txt", "m\n")
						r.git("add", ".")
						r.commit("main only")
					}
					before := r.git("rev-parse", branchOther)
					err := g.Rebase(ctx, branchOther)
					wantDiagCode(t, err, "remeris::git::not_fast_forward")
					wantMessage(t, err, "Cannot fast-forward branch other to main: other has commits that are not in main")
					if got := head(r); got != onMain || r.git("rev-parse", branchOther) != before {
						t.Fatalf("a failed Rebase changed the repository (HEAD %q)", got)
					}
				})
			})
		}
	})
	t.Run("missing branch", func(t *testing.T) {
		eachBackend(t, func(t *testing.T, r *testRepo, g Git) {
			r.git("tag", "sometag")
			wantDiagCode(t, g.Rebase(ctx, "nope"), "remeris::git::command")
			wantMessage(t, g.Rebase(ctx, "nope"), "cannot locate local branch 'nope'")
			wantMessage(t, g.Rebase(ctx, "sometag"), "cannot locate local branch 'sometag'")
		})
	})
	t.Run("dirty tree", func(t *testing.T) {
		eachBackend(t, func(t *testing.T, r *testRepo, g Git) {
			ahead(r)
			r.write("new.txt", "x")
			before := r.git("rev-parse", branchOther)
			wantDiagCode(t, g.Rebase(ctx, branchOther), "remeris::git::uncommitted_changes")
			wantDiagCode(t, g.Rebase(ctx, branchMain), "remeris::git::uncommitted_changes") // even the current branch
			if got := head(r); got != onMain || r.git("rev-parse", branchOther) != before {
				t.Fatalf("a failed Rebase changed the repository (HEAD %q)", got)
			}
		})
	})
	t.Run("detached HEAD", func(t *testing.T) {
		eachBackend(t, func(t *testing.T, r *testRepo, g Git) {
			ahead(r)
			r.git("checkout", "--quiet", "--detach")
			before := r.git("rev-parse", branchOther)
			wantDiagCode(t, g.Rebase(ctx, branchOther), "remeris::git::not_a_branch")
			if r.git("rev-parse", branchOther) != before {
				t.Fatal("a failed Rebase moved the branch")
			}
		})
	})
}

func TestRebaseRefs(t *testing.T) { //nolint:paralleltest // sets environment variables
	ctx := context.Background()
	t.Run("unborn HEAD", func(t *testing.T) {
		for _, backend := range []string{backendCLI, backendGoGit} {
			t.Run(backend, func(t *testing.T) { //nolint:paralleltest // sets environment variables
				isolateConfig(t)
				r := newTestRepo(t, true)
				var g Git
				if backend == backendCLI {
					g = openGit(t, r.dir)
				} else {
					var err error
					if g, err = NewGoGit(r.dir); err != nil {
						t.Fatal(err)
					}
				}
				wantDiagCode(t, g.Rebase(ctx, branchOther), "remeris::git::command")
			})
		}
	})
	t.Run("packed refs", func(t *testing.T) {
		eachBackend(t, func(t *testing.T, r *testRepo, g Git) {
			ahead(r)
			r.git("pack-refs", "--all", "--prune")
			if err := g.Rebase(ctx, branchOther); err != nil {
				t.Fatal(err)
			}
			if got, want := r.git("rev-parse", branchOther), r.git("rev-parse", branchMain); got != want {
				t.Fatalf("other at %s, want %s", got, want)
			}
		})
	})
	t.Run("symbolic ref alias", func(t *testing.T) {
		eachBackend(t, func(t *testing.T, r *testRepo, g Git) {
			ahead(r)
			r.git("symbolic-ref", "refs/heads/alias", "refs/heads/other")
			before := r.git("rev-parse", branchOther)
			wantDiagCode(t, g.Rebase(ctx, "alias"), "remeris::git::branch_alias")
			if got := head(r); got != onMain || r.git("rev-parse", branchOther) != before {
				t.Fatalf("a failed Rebase changed the repository (HEAD %q)", got)
			}
		})
	})
	t.Run("reflog", func(t *testing.T) {
		eachBackend(t, func(t *testing.T, r *testRepo, g Git) {
			ahead(r)
			if err := g.Rebase(ctx, branchOther); err != nil {
				t.Fatal(err)
			}
			want := "remeris: RebaseBranch fast-forward other to main\n"
			if got := r.git("log", "-g", "-1", "--format=%gs", branchOther); got != want {
				t.Fatalf("branch reflog = %q, want %q", got, want)
			}
			want = "checkout: moving from main to other\n"
			if got := r.git("log", "-g", "-1", "--format=%gs", "HEAD"); got != want {
				t.Fatalf("HEAD reflog = %q, want %q", got, want)
			}
		})
	})
	t.Run("hostile names", func(t *testing.T) {
		eachBackend(t, func(t *testing.T, r *testRepo, g Git) {
			for _, name := range hostileBranchNames {
				wantDiagCode(t, g.Rebase(ctx, name), codeBadBranch)
			}
		})
	})
	t.Run("branch checked out in another worktree", func(t *testing.T) {
		eachBackend(t, func(t *testing.T, r *testRepo, g Git) {
			ahead(r)
			r.git("worktree", "add", "--quiet", filepath.Join(t.TempDir(), "wt"), branchOther)
			before := r.git("rev-parse", branchOther)
			err := g.Rebase(ctx, branchOther)
			wantDiagCode(t, err, "remeris::git::command")
			wantMessage(t, err, "is already used by worktree")
			if got := head(r); got != onMain || r.git("rev-parse", branchOther) != before {
				t.Fatalf("a failed Rebase changed the repository (HEAD %q)", got)
			}
		})
	})
}

// wantMessage fails unless the error text contains want.
func wantMessage(t *testing.T, err error, want string) {
	t.Helper()
	if err == nil || !strings.Contains(err.Error(), want) {
		t.Fatalf("error = %v, want it to contain %q", err, want)
	}
}

// TestRebaseRace puts a git wrapper on PATH that adds a commit to the branch just before update-ref runs. Rebase must
// fail and the commit must not be lost.
func TestRebaseRace(t *testing.T) { //nolint:paralleltest // sets environment variables
	r := branchRepo(t)
	r.git("branch", "-f", branchOther, branchMain)
	r.write("b.txt", "b\n")
	r.git("add", ".")
	r.commit("three")
	bin := t.TempDir()
	script := `#!/bin/sh
real="` + gitBin + `"
for a in "$@"; do
  if [ "$a" = update-ref ] && [ ! -e "$0.done" ]; then
    : > "$0.done"
    dir="$REMERIS_RACE_DIR"
    tree=$("$real" -C "$dir" rev-parse refs/heads/other^{tree})
    c=$("$real" -C "$dir" -c user.name=t -c user.email=t@example.com commit-tree -p refs/heads/other -m raced "$tree")
    "$real" -C "$dir" update-ref refs/heads/other "$c"
  fi
done
exec "$real" "$@"
`
	wrapper := filepath.Join(bin, "git")
	if err := os.WriteFile(wrapper, []byte(script), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(wrapper, 0o700); err != nil { //nolint:gosec // the wrapper is an executable test script
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("REMERIS_RACE_DIR", r.dir)
	g, err := NewCLI(r.dir)
	if err != nil {
		t.Fatal(err)
	}
	raced := r.git("rev-parse", "refs/heads/other") // before the wrapper adds its commit
	err = g.Rebase(context.Background(), branchOther)
	wantDiagCode(t, err, "remeris::git::command")
	now := r.git("rev-parse", "refs/heads/other")
	if now == raced {
		t.Fatal("the wrapper did not run")
	}
	if got := r.git("log", "-1", "--format=%s", "other"); got != "raced\n" {
		t.Fatalf("the raced commit was lost: other is at %q", got)
	}
	if got := head(r); got != onMain {
		t.Fatalf("HEAD moved to %q", got)
	}
}

func TestIncompleteCheckoutError(t *testing.T) {
	t.Parallel()
	err := incompleteCheckoutError(os.ErrPermission)
	wantDiagCode(t, err, "remeris::git::incomplete_checkout")
	if err.Error() != "Could not complete checkout" {
		t.Fatalf("error = %q", err.Error())
	}
}
