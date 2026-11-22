package git

import (
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

const (
	keyGPGSign  = "commit.gpgsign"
	fakeGPGBody = "#!/bin/sh\ncat >/dev/null\necho '[GNUPG:] SIG_CREATED D 1 8 00 1 KEY' >&2\n" +
		"printf -- '-----BEGIN PGP SIGNATURE-----\\n\\nZmFrZQ==\\n-----END PGP SIGNATURE-----\\n'\n"
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

func TestSigningConfig(t *testing.T) { //nolint:paralleltest // sets environment variables
	tests := map[string]struct {
		config [][]string
		want   SigningConfig
		set    bool
	}{
		"nothing": {},
		"gpgsign": {
			config: [][]string{{keyGPGSign, "true"}},
			want:   SigningConfig{GPGSign: true}, set: true,
		},
		"key only": {
			config: [][]string{{"user.signingkey", "ABCD1234"}},
			want:   SigningConfig{Key: "ABCD1234"}, set: true,
		},
		"gpgsign false": {config: [][]string{{keyGPGSign, "false"}}},
		formatSSH: {
			config: [][]string{
				{"gpg.format", formatSSH}, {"user.signingkey", "/k.pub"},
				{"gpg.ssh.program", "/bin/ssh-keygen"}, {keyGPGSign, "yes"},
			},
			want: SigningConfig{GPGSign: true, Key: "/k.pub", Format: formatSSH, Program: "/bin/ssh-keygen"}, set: true,
		},
		"openpgp program": {
			config: [][]string{{"gpg.program", "/bin/gpg2"}},
			want:   SigningConfig{Program: "/bin/gpg2"},
		},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) { //nolint:paralleltest // sets environment variables
			eachCommitter(t, func(t *testing.T, r *testRepo, c Committer) {
				for _, kv := range tc.config {
					r.git("config", kv[0], kv[1])
				}
				got, err := c.SigningConfig(t.Context())
				if err != nil {
					t.Fatal(err)
				}
				if got != tc.want {
					t.Fatalf("SigningConfig = %+v, want %+v", got, tc.want)
				}
				if got.Configured() != tc.set {
					t.Fatalf("Configured = %v, want %v", got.Configured(), tc.set)
				}
			})
		})
	}
}

func TestCommitNoSign(t *testing.T) { //nolint:paralleltest // sets environment variables
	r := commitRepo(t)
	program := filepath.Join(t.TempDir(), "gpg")
	if err := os.WriteFile(program, []byte(fakeGPGBody), 0o700); err != nil { //nolint:gosec // an executable test script
		t.Fatal(err)
	}
	waitExecutable(t, program)
	r.git("config", "gpg.program", program)
	r.git("config", "user.signingkey", "KEY")
	r.git("config", keyGPGSign, "true")
	c, _ := AsCommitter(openGit(t, r.dir))

	// The user's own configuration signs every commit, and NoSign turns that off for this one.
	r.write(fileA, "changed\n")
	if _, err := c.Commit(t.Context(), commitMsg, CommitOptions{NoSign: true, Paths: []string{fileA}}); err != nil {
		t.Fatal(err)
	}
	if got := r.git("cat-file", "commit", "HEAD"); strings.Contains(got, "gpgsig") {
		t.Fatalf("NoSign made a signed commit:\n%s", got)
	}
	r.write(fileA, "changed again\n")
	if _, err := c.Commit(t.Context(), commitMsg, CommitOptions{Paths: []string{fileA}}); err != nil {
		t.Fatal(err)
	}
	if got := r.git("cat-file", "commit", "HEAD"); !strings.Contains(got, "gpgsig ") {
		t.Fatalf("a plain commit ignored commit.gpgsign:\n%s", got)
	}
}

func TestCommitSigned(t *testing.T) { //nolint:paralleltest // sets environment variables
	t.Run("cli signs through the configured program", func(t *testing.T) { //nolint:paralleltest // sets environment variables
		r := commitRepo(t)
		program := filepath.Join(t.TempDir(), "gpg")
		if err := os.WriteFile(program, []byte(fakeGPGBody), 0o700); err != nil { //nolint:gosec // an executable test script
			t.Fatal(err)
		}
		waitExecutable(t, program)
		r.git("config", "gpg.program", program)
		r.git("config", "user.signingkey", "KEY")
		r.write(fileA, "changed\n")
		c, _ := AsCommitter(openGit(t, r.dir))
		if _, err := c.Commit(t.Context(), commitMsg, CommitOptions{Sign: true, Paths: []string{fileA}}); err != nil {
			t.Fatal(err)
		}
		if got := r.git("cat-file", "commit", "HEAD"); !strings.Contains(got, "gpgsig ") {
			t.Fatalf("the commit is not signed:\n%s", got)
		}
	})

	t.Run(
		"cli fails and does not fall back to an unsigned commit",
		func(t *testing.T) { //nolint:paralleltest // sets environment variables
			r := commitRepo(t)
			r.git("config", "gpg.program", "/nonexistent/gpg")
			r.git("config", "user.signingkey", "KEY")
			r.write(fileA, "changed\n")
			before := r.git("rev-parse", "HEAD")
			c, _ := AsCommitter(openGit(t, r.dir))
			_, err := c.Commit(t.Context(), commitMsg, CommitOptions{Sign: true, Paths: []string{fileA}})
			if err == nil || !strings.Contains(err.Error(), "Could not commit") {
				t.Fatalf("error = %v", err)
			}
			if after := r.git("rev-parse", "HEAD"); after != before {
				t.Fatal("a commit was made")
			}
		},
	)

	t.Run(
		"a plain commit follows the user's own configuration",
		func(t *testing.T) { //nolint:paralleltest // sets environment variables
			r := commitRepo(t)
			r.write(fileA, "changed\n")
			c, _ := AsCommitter(openGit(t, r.dir))
			if _, err := c.Commit(t.Context(), commitMsg, CommitOptions{Paths: []string{fileA}}); err != nil {
				t.Fatal(err)
			}
			if got := r.git("cat-file", "commit", "HEAD"); strings.Contains(got, "gpgsig") {
				t.Fatal("the commit is signed")
			}
		},
	)

	t.Run("go-git cannot sign", func(t *testing.T) { //nolint:paralleltest // sets environment variables
		r := commitRepo(t)
		r.write(fileA, "changed\n")
		c, _ := AsCommitter(openGoGit(t, r.dir))
		_, err := c.Commit(t.Context(), commitMsg, CommitOptions{Sign: true, Paths: []string{fileA}})
		if err == nil || !strings.Contains(err.Error(), "cannot sign") {
			t.Fatalf("error = %v", err)
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

// waitExecutable runs the freshly written script until the system lets it run: a fork in another test can hold the
// file open for writing for a moment, which makes executing it fail with "text file busy".
func waitExecutable(t *testing.T, program string) {
	t.Helper()
	for range 100 {
		err := exec.CommandContext(t.Context(), program).Run()
		if err == nil || !strings.Contains(err.Error(), "text file busy") {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("the test script stays busy")
}
