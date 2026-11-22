package git

import (
	"reflect"
	"strings"
	"testing"
)

const (
	fileNew    = "new.txt"
	fileKeep   = "keep.txt"
	fileStaged = "staged.txt"
	commitMsg  = "chore: prepare release 1.0.0"
)

// commitRepo is a committed keep.txt, a.txt and .gitignore, with the identity set in the repository (go-git reads
// it from the configuration, not from GIT_COMMITTER_*).
func commitRepo(t *testing.T) *testRepo {
	t.Helper()
	r := branchRepo(t)
	r.git("config", "user.name", "Test User")
	r.git("config", "user.email", "user@example.com")
	r.write(fileKeep, "keep\n")
	r.git("add", fileKeep)
	r.git("commit", "--quiet", "-m", "add keep")
	return r
}

// eachCommitter runs fn for the CLI backend and for go-git, the latter with an empty PATH.
func eachCommitter(t *testing.T, fn func(t *testing.T, r *testRepo, c Committer)) {
	t.Helper()
	for _, backend := range []string{backendCLI, backendGoGit} {
		t.Run(backend, func(t *testing.T) { //nolint:paralleltest // sets environment variables
			r := commitRepo(t)
			var g Git
			if backend == backendCLI {
				g = openGit(t, r.dir)
			} else {
				g = openGoGit(t, r.dir)
				t.Setenv("PATH", t.TempDir())
			}
			c, ok := AsCommitter(g)
			if !ok {
				t.Fatalf("%T does not implement Committer", g)
			}
			fn(t, r, c)
		})
	}
}

func TestChanges(t *testing.T) { //nolint:paralleltest // sets environment variables
	tests := map[string]struct {
		setup func(r *testRepo)
		want  []Change
	}{
		"clean": {setup: func(*testRepo) {}},
		"modified untracked and deleted": {
			setup: func(r *testRepo) {
				r.write(fileA, "changed\n")
				r.write(fileNew, "new\n")
				r.removeAll(fileKeep)
			},
			want: []Change{{Path: fileA}, {Path: fileKeep, Deleted: true}, {Path: fileNew}},
		},
		"ignored files are left out": {
			setup: func(r *testRepo) { r.write("ignored.txt", "x\n") },
		},
		"files in a new directory are listed one by one": {
			setup: func(r *testRepo) { r.mkdirWrite("dir/sub/x.txt", "x\n") },
			want:  []Change{{Path: "dir/sub/x.txt"}},
		},
		"staged deletion": {
			setup: func(r *testRepo) { r.git("rm", "--quiet", fileKeep) },
			want:  []Change{{Path: fileKeep, Deleted: true}},
		},
		"file with spaces and unicode": {
			setup: func(r *testRepo) { r.write("a b é.txt", "x\n") },
			want:  []Change{{Path: "a b é.txt"}},
		},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) { //nolint:paralleltest // sets environment variables
			eachCommitter(t, func(t *testing.T, r *testRepo, c Committer) {
				tc.setup(r)
				got, err := c.Changes(t.Context())
				if err != nil {
					t.Fatal(err)
				}
				if len(got) == 0 {
					got = nil
				}
				if !reflect.DeepEqual(got, tc.want) {
					t.Fatalf("Changes = %v, want %v", got, tc.want)
				}
			})
		})
	}
}

func TestChangesRenameCLI(t *testing.T) { //nolint:paralleltest // sets environment variables
	r := commitRepo(t)
	r.git("mv", fileKeep, "moved.txt")
	c, _ := AsCommitter(openGit(t, r.dir))
	got, err := c.Changes(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	want := []Change{{Path: fileKeep, Deleted: true}, {Path: "moved.txt"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Changes = %v, want %v", got, want)
	}
}

func TestParsePorcelain(t *testing.T) {
	t.Parallel()
	tests := map[string]struct {
		in   string
		want []Change
	}{
		"copy keeps the source": {in: "C  b.txt\x00a.txt\x00", want: []Change{{Path: "b.txt"}}},
		"added then deleted":    {in: "AD x.txt\x00"},
		"unmerged":              {in: "UU x.txt\x00", want: []Change{{Path: "x.txt"}}},
		"empty":                 {in: ""},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			got := parsePorcelain(tc.in)
			if len(got) == 0 {
				got = nil
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("parsePorcelain(%q) = %v, want %v", tc.in, got, tc.want)
			}
		})
	}
}

func TestCommit(t *testing.T) { //nolint:paralleltest // sets environment variables
	t.Run("commits only the selected paths", func(t *testing.T) { //nolint:paralleltest // sets environment variables
		eachCommitter(t, func(t *testing.T, r *testRepo, c Committer) {
			r.write(fileA, "changed\n")
			r.write(fileNew, "new\n")
			r.removeAll(fileKeep)
			sha, err := c.Commit(t.Context(), commitMsg, CommitOptions{Paths: []string{fileA, fileNew, fileKeep}})
			if err != nil {
				t.Fatal(err)
			}
			if got := strings.TrimSpace(r.git("rev-parse", "HEAD")); got != sha {
				t.Fatalf("returned %s, HEAD is %s", sha, got)
			}
			if got := strings.TrimSpace(r.git("log", "-1", "--format=%s")); got != commitMsg {
				t.Fatalf("subject = %q", got)
			}
			files := strings.Fields(r.git("show", "--name-status", "--format=", "HEAD"))
			if want := []string{"M", fileA, "D", fileKeep, "A", fileNew}; !reflect.DeepEqual(sorted(files), sorted(want)) {
				t.Fatalf("committed %v, want %v", files, want)
			}
		})
	})

	t.Run("leaves other changes alone", func(t *testing.T) { //nolint:paralleltest // sets environment variables
		eachCommitter(t, func(t *testing.T, r *testRepo, c Committer) {
			r.write(fileA, "changed\n")
			r.write(fileNew, "new\n")
			if _, err := c.Commit(t.Context(), commitMsg, CommitOptions{Paths: []string{fileA}}); err != nil {
				t.Fatal(err)
			}
			if got := strings.TrimSpace(r.git("status", "--porcelain")); got != "?? "+fileNew {
				t.Fatalf("status = %q, want only the untracked %s", got, fileNew)
			}
		})
	})

	t.Run("nothing to commit", func(t *testing.T) { //nolint:paralleltest // sets environment variables
		eachCommitter(t, func(t *testing.T, r *testRepo, c Committer) {
			for _, paths := range [][]string{nil, {fileA}} {
				_, err := c.Commit(t.Context(), commitMsg, CommitOptions{Paths: paths})
				if err == nil || !strings.Contains(err.Error(), "Nothing to commit") {
					t.Fatalf("paths %v: error = %v", paths, err)
				}
			}
		})
	})

	t.Run("hostile names are never options or patterns", func(t *testing.T) { //nolint:paralleltest // env
		eachCommitter(t, func(t *testing.T, r *testRepo, c Committer) {
			r.write("-n", "x\n")
			r.write("*.txt", "x\n")
			if _, err := c.Commit(t.Context(), commitMsg, CommitOptions{Paths: []string{"-n", "*.txt"}}); err != nil {
				t.Fatal(err)
			}
			files := strings.Fields(r.git("show", "--name-only", "--format=", "HEAD"))
			if !reflect.DeepEqual(sorted(files), []string{"*.txt", "-n"}) {
				t.Fatalf("committed %v", files)
			}
		})
	})
}

func TestCommitStagedElsewhere(t *testing.T) { //nolint:paralleltest // sets environment variables
	r := commitRepo(t)
	r.write(fileA, "changed\n")
	r.write(fileStaged, "staged\n")
	r.git("add", fileStaged)

	t.Run("cli leaves it staged", func(t *testing.T) { //nolint:paralleltest // sets environment variables
		c, _ := AsCommitter(openGit(t, r.dir))
		if _, err := c.Commit(t.Context(), commitMsg, CommitOptions{Paths: []string{fileA}}); err != nil {
			t.Fatal(err)
		}
		if got := strings.TrimSpace(r.git("status", "--porcelain")); got != "A  "+fileStaged {
			t.Fatalf("status = %q", got)
		}
	})

	t.Run("go-git refuses", func(t *testing.T) { //nolint:paralleltest // sets environment variables
		r := commitRepo(t)
		r.write(fileA, "changed\n")
		r.write(fileStaged, "staged\n")
		r.git("add", fileStaged)
		c, _ := AsCommitter(openGoGit(t, r.dir))
		_, err := c.Commit(t.Context(), commitMsg, CommitOptions{Paths: []string{fileA}})
		if err == nil || !strings.Contains(err.Error(), fileStaged) {
			t.Fatalf("error = %v, want one that names %s", err, fileStaged)
		}
	})
}

func sorted(s []string) []string {
	out := append([]string(nil), s...)
	for i := range out {
		for j := i + 1; j < len(out); j++ {
			if out[j] < out[i] {
				out[i], out[j] = out[j], out[i]
			}
		}
	}
	return out
}
