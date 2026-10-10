package git

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/celest-io/remeris/internal/diag"
)

const (
	addA     = "a.txt"
	addB     = "b.txt"
	addDirA  = "dir/a.txt"
	addDeepD = "sub/deep/d.txt"
)

// runAddFilesContract stages paths in a repository prepared by setup and compares `git diff --cached --name-status`.
func runAddFilesContract(t *testing.T, open func(t *testing.T, dir string) Git) {
	t.Helper()
	tests := map[string]struct {
		setup func(r *testRepo)
		// openSub is the subdirectory the backend is opened in, if any.
		openSub string
		paths   []string
		want    string // the staged changes, as git diff --cached --name-status prints them
	}{
		"modified tracked file": {
			setup: func(r *testRepo) {
				r.commitFiles("one", authorTest, map[string]string{addA: "a", addB: "b"})
				r.write(addA, "changed")
				r.write(addB, "changed")
			},
			paths: []string{addA},
			want:  "M\ta.txt\n",
		},
		"new untracked file": {
			setup: func(r *testRepo) {
				r.commitFiles("one", authorTest, map[string]string{addA: "a"})
				r.write("new.txt", "new")
				r.write("other.txt", "other")
			},
			paths: []string{"new.txt"},
			want:  "A\tnew.txt\n",
		},
		"deleted tracked file": {
			setup: func(r *testRepo) {
				r.commitFiles("one", authorTest, map[string]string{addA: "a", addB: "b"})
				if err := os.Remove(filepath.Join(r.dir, addB)); err != nil {
					t.Fatal(err)
				}
				if err := os.Remove(filepath.Join(r.dir, addA)); err != nil {
					t.Fatal(err)
				}
			},
			paths: []string{addB},
			want:  "D\tb.txt\n",
		},
		"modified, new and deleted files together in the given order": {
			setup: func(r *testRepo) {
				r.commitFiles("one", authorTest, map[string]string{addA: "a", addB: "b"})
				r.write(addA, "changed")
				r.write("new.txt", "new")
				if err := os.Remove(filepath.Join(r.dir, addB)); err != nil {
					t.Fatal(err)
				}
			},
			paths: []string{"new.txt", addB, addA},
			want:  "M\ta.txt\nD\tb.txt\nA\tnew.txt\n",
		},
		"empty list stages nothing": {
			setup: func(r *testRepo) {
				r.commitFiles("one", authorTest, map[string]string{addA: "a"})
				r.write(addA, "changed")
			},
			paths: []string{},
			want:  "",
		},
		"ignored untracked file is skipped silently": {
			setup: func(r *testRepo) {
				r.commitFiles("one", authorTest, map[string]string{".gitignore": "*.log\nbuild/\n"})
				r.write("debug.log", "log")
				if err := os.MkdirAll(filepath.Join(r.dir, "build"), 0o700); err != nil {
					t.Fatal(err)
				}
				r.write("build/out.txt", "out")
				r.write("ok.txt", "ok")
			},
			paths: []string{"debug.log", "build/out.txt", "ok.txt"},
			want:  "A\tok.txt\n",
		},
		"tracked file that is now ignored is still staged": {
			setup: func(r *testRepo) {
				r.commitFiles("one", authorTest, map[string]string{"a.log": "a"})
				r.commitFiles("two", authorTest, map[string]string{".gitignore": "*.log\n"})
				r.write("a.log", "changed")
			},
			paths: []string{"a.log"},
			want:  "M\ta.log\n",
		},
		"file in a subdirectory": {
			setup: func(r *testRepo) {
				r.commitFiles("one", authorTest, map[string]string{"dir/sub/a.txt": "a"})
				r.write("dir/sub/a.txt", "changed")
				r.write("dir/sub/new.txt", "new")
				r.write("dir/untouched.txt", "u")
			},
			paths: []string{"dir/sub/a.txt", "dir/sub/new.txt"},
			want:  "M\tdir/sub/a.txt\nA\tdir/sub/new.txt\n",
		},
		"directory stages the files under it": {
			setup: func(r *testRepo) {
				r.commitFiles("one", authorTest, map[string]string{addDirA: "a", "dir/b.txt": "b", "x.txt": "x"})
				r.write(addDirA, "changed")
				r.write("dir/new.txt", "new")
				r.write("x.txt", "changed")
				if err := os.Remove(filepath.Join(r.dir, "dir/b.txt")); err != nil {
					t.Fatal(err)
				}
			},
			paths: []string{"dir"},
			want:  "M\tdir/a.txt\nD\tdir/b.txt\nA\tdir/new.txt\n",
		},
		"path with spaces and non-ASCII": {
			setup: func(r *testRepo) {
				r.commitFiles("one", authorTest, map[string]string{"my file.txt": "a"})
				r.write("my file.txt", "changed")
				r.write("zoë 日本.txt", "new")
			},
			paths: []string{"my file.txt", "zoë 日本.txt"},
			want:  "M\tmy file.txt\nA\tzoë 日本.txt\n",
		},
		"path that neither exists nor is tracked is skipped silently": {
			setup: func(r *testRepo) {
				r.commitFiles("one", authorTest, map[string]string{addA: "a"})
				r.write(addA, "changed")
			},
			paths: []string{"missing.txt", addA, "nodir/missing.txt"},
			want:  "M\ta.txt\n",
		},
		"glob characters are literal": {
			setup: func(r *testRepo) {
				r.commitFiles("one", authorTest, map[string]string{addA: "a", "star*.txt": "s"})
				r.write(addA, "changed")
				r.write("star*.txt", "changed")
			},
			paths: []string{"star*.txt"},
			want:  "M\tstar*.txt\n",
		},
		"unchanged file and a repeated path": {
			setup: func(r *testRepo) {
				r.commitFiles("one", authorTest, map[string]string{addA: "a", addB: "b"})
				r.write(addB, "changed")
			},
			paths: []string{addA, addB, addB},
			want:  "M\tb.txt\n",
		},
		"paths are relative to the working tree root when opened in a subdirectory": {
			setup: func(r *testRepo) {
				r.commitFiles("one", authorTest, map[string]string{addDirA: "a", "top.txt": "t"})
				r.write(addDirA, "changed")
				r.write("top.txt", "changed")
			},
			openSub: "dir",
			paths:   []string{"top.txt", addDirA},
			want:    "M\tdir/a.txt\nM\ttop.txt\n",
		},
		"file replaced by a directory": {
			setup: func(r *testRepo) {
				r.commitFiles("one", authorTest, map[string]string{addB: "b", addA: "a"})
				r.removeAll(addB)
				r.mkdirWrite("b.txt/inner.txt", "inner")
			},
			paths: []string{addB, "b.txt/inner.txt"},
			want:  "D\tb.txt\nA\tb.txt/inner.txt\n",
		},
		"file replaced by a directory, staged by the file path, which covers the directory now at it": {
			setup: func(r *testRepo) {
				r.commitFiles("one", authorTest, map[string]string{addB: "b", addA: "a"})
				r.removeAll(addB)
				r.mkdirWrite("b.txt/inner.txt", "inner")
			},
			paths: []string{addB},
			want:  "D\tb.txt\nA\tb.txt/inner.txt\n",
		},
		"directory replaced by a file, staged by the parent": {
			setup: func(r *testRepo) {
				r.commitFiles("one", authorTest, map[string]string{addDeepD: "d", "sub/keep.txt": "k"})
				r.removeAll("sub/deep")
				r.write("sub/deep", "now a file")
			},
			paths: []string{"sub"},
			want:  "A\tsub/deep\nD\tsub/deep/d.txt\n",
		},
		"directory replaced by a file, staged by the old file path": {
			setup: func(r *testRepo) {
				r.commitFiles("one", authorTest, map[string]string{addDeepD: "d", "sub/keep.txt": "k"})
				r.removeAll("sub/deep")
				r.write("sub/deep", "now a file")
			},
			paths: []string{addDeepD},
			want:  "D\tsub/deep/d.txt\n",
		},
		"repository without commits": {
			setup: func(r *testRepo) { r.write(addA, "a") },
			paths: []string{addA},
			want:  "A\ta.txt\n",
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			r := newTestRepo(t, true)
			tt.setup(r)
			dir := filepath.Join(r.openDir(), tt.openSub)
			if err := open(t, dir).AddFiles(context.Background(), tt.paths); err != nil {
				t.Fatalf("AddFiles: %v", err)
			}
			r.git("write-tree") // the index must stay valid: no file and directory entries with the same name
			got := r.git("-c", "core.quotepath=false", "diff", "--cached", "--name-status", "--no-renames")
			if got != tt.want {
				t.Fatalf("staged = %q, want %q", got, tt.want)
			}
		})
	}
}

func runAddFilesErrors(t *testing.T, open func(t *testing.T, dir string) Git) {
	t.Helper()
	t.Run("not a repository", func(t *testing.T) {
		t.Parallel()
		r := newTestRepo(t, false)
		err := open(t, r.dir).AddFiles(context.Background(), []string{addA})
		var d diag.Diagnostic
		if !errors.As(err, &d) || d.Code() != "remeris::git::open_repo" {
			t.Fatalf("error = %v, want remeris::git::open_repo", err)
		}
	})
	t.Run("empty list does not open the repository", func(t *testing.T) {
		t.Parallel()
		r := newTestRepo(t, false)
		if err := open(t, r.dir).AddFiles(context.Background(), nil); err != nil {
			t.Fatalf("error = %v, want nil", err)
		}
	})
	t.Run("cancelled context", func(t *testing.T) {
		t.Parallel()
		r := newTestRepo(t, true)
		r.write(addA, "a")
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		err := open(t, r.dir).AddFiles(ctx, []string{addA})
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("error = %v, want context.Canceled", err)
		}
		if got := r.git("diff", "--cached", "--name-only"); strings.TrimSpace(got) != "" {
			t.Fatalf("staged %q after a cancelled call", got)
		}
	})
}

func TestCLIAddFiles(t *testing.T) {
	t.Parallel()
	runAddFilesContract(t, openGit)
	runAddFilesErrors(t, openGit)
}

func TestGoGitAddFiles(t *testing.T) {
	t.Parallel()
	runAddFilesContract(t, openGoGit)
	runAddFilesErrors(t, openGoGit)
}

// removeAll deletes a file or a directory tree of the working tree.
func (r *testRepo) removeAll(name string) {
	r.t.Helper()
	if err := os.RemoveAll(filepath.Join(r.dir, name)); err != nil {
		r.t.Fatal(err)
	}
}

// mkdirWrite writes a file, creating its parent directories.
func (r *testRepo) mkdirWrite(name, content string) {
	r.t.Helper()
	if err := os.MkdirAll(filepath.Dir(filepath.Join(r.dir, name)), 0o700); err != nil {
		r.t.Fatal(err)
	}
	r.write(name, content)
}
