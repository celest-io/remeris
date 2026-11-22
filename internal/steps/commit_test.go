package steps

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/celest-io/remeris/internal/diag"
	"github.com/celest-io/remeris/internal/signing"
	"github.com/celest-io/remeris/internal/workflow"
)

const (
	changelogFile = "CHANGELOG.md"
	versionFile   = "VERSION"
	gitInit       = "init"
	lockFile      = "mise.lock"
	releaseMsg    = "chore: prepare release 1.2.3"
	fakeGPG       = "#!/bin/sh\ncat >/dev/null\necho '[GNUPG:] SIG_CREATED D 1 8 00 1 KEY' >&2\n" +
		"printf -- '-----BEGIN PGP SIGNATURE-----\\n\\nZmFrZQ==\\n-----END PGP SIGNATURE-----\\n'\n"
)

// commitDir makes a repository in a temporary directory with one commit, and moves into it. The machine's Git
// configuration is not read.
func commitDir(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}
	dir := t.TempDir()
	t.Chdir(dir)
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	for _, args := range [][]string{
		{gitInit, "-q", "--initial-branch=main"},
		{"config", "user.name", "T"},
		{"config", "user.email", "t@example.com"},
	} {
		runGit(t, args...)
	}
	writeTestFile(t, changelogFile, "# Changelog\n")
	writeTestFile(t, lockFile, "lock\n")
	runGit(t, "add", ".")
	runGit(t, "commit", "-q", "-m", "init")
	return dir
}

func runGit(t *testing.T, args ...string) string {
	t.Helper()
	out, err := exec.CommandContext(t.Context(), "git", args...).CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return string(out)
}

func writeTestFile(t *testing.T, name, content string) {
	t.Helper()
	if err := os.WriteFile(name, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func commitState(t *testing.T, options signing.Options, api ...signing.APICommitter) workflow.State {
	t.Helper()
	return workflow.State{Signing: signing.Config{Options: options, API: api}}
}

func runCommit(t *testing.T, s *Commit, state workflow.State, dry bool) error {
	t.Helper()
	rt := workflow.Real(state)
	if dry {
		rt = workflow.DryRun(state)
	}
	_, err := s.Run(context.Background(), rt)
	return err
}

func ptrBool(b bool) *bool { return &b }

// configureSigning makes git sign with a fake gpg program.
func configureSigning(t *testing.T, dir string) {
	t.Helper()
	program := filepath.Join(t.TempDir(), "gpg")
	if err := os.WriteFile(program, []byte(fakeGPG), 0o700); err != nil { //nolint:gosec // an executable test script
		t.Fatal(err)
	}
	waitExecutable(t, program)
	runGit(t, "config", "gpg.program", program)
	runGit(t, "config", "user.signingkey", "KEY")
	_ = dir
}

func TestCommitPlain(t *testing.T) {
	commitDir(t)
	writeTestFile(t, changelogFile, "# Changelog\n\n## 1.2.3\n")
	writeTestFile(t, versionFile, "1.2.3\n")
	logs := captureLogs(t)

	if err := runCommit(t, &Commit{Message: releaseMsg}, commitState(t, signing.Options{}), false); err != nil {
		t.Fatal(err)
	}
	if got := strings.TrimSpace(runGit(t, "log", "-1", "--format=%s")); got != releaseMsg {
		t.Fatalf("subject = %q", got)
	}
	if got := runGit(t, "status", "--porcelain"); got != "" {
		t.Fatalf("status = %q", got)
	}
	if got := runGit(t, "cat-file", "commit", "HEAD"); strings.Contains(got, "gpgsig") {
		t.Fatal("a commit without sign is signed")
	}
	if !strings.Contains(logs.String(), "Created commit ") {
		t.Fatalf("log = %q", logs)
	}
}

func TestCommitSubstitutesVariables(t *testing.T) {
	commitDir(t)
	writeTestFile(t, versionFile, "1\n")
	step := &Commit{Message: "release $x", Variables: nil}
	step.Variables = nil
	// With the default variables nothing needs the packages here: $x is not a variable.
	if err := runCommit(t, step, commitState(t, signing.Options{}), false); err != nil {
		t.Fatal(err)
	}
	if got := strings.TrimSpace(runGit(t, "log", "-1", "--format=%s")); got != "release $x" {
		t.Fatalf("subject = %q", got)
	}
}

func TestCommitIncludeExclude(t *testing.T) {
	tests := map[string]struct {
		step    Commit
		options signing.Options
		want    []string
	}{
		"exclude from the step": {
			step: Commit{Exclude: []string{"mise*.lock"}}, want: []string{changelogFile, versionFile},
		},
		"include from the step": {step: Commit{Include: []string{versionFile}}, want: []string{versionFile}},
		"the step beats the command line": {
			step:    Commit{Exclude: []string{versionFile}},
			options: signing.Options{Exclude: []string{lockFile}},
			want:    []string{changelogFile, lockFile},
		},
		"the command line when the step has none": {
			options: signing.Options{Exclude: []string{"mise*.lock", versionFile}}, want: []string{changelogFile},
		},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			commitDir(t)
			writeTestFile(t, changelogFile, "changed\n")
			writeTestFile(t, lockFile, "changed\n")
			writeTestFile(t, versionFile, "1\n")
			step := tc.step
			step.Message = releaseMsg
			if err := runCommit(t, &step, commitState(t, tc.options), false); err != nil {
				t.Fatal(err)
			}
			got := strings.Fields(runGit(t, "show", "--name-only", "--format=", "HEAD"))
			if strings.Join(sortedCopy(got), ",") != strings.Join(sortedCopy(tc.want), ",") {
				t.Fatalf("committed %v, want %v", got, tc.want)
			}
		})
	}
}

func sortedCopy(s []string) []string {
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

func TestCommitNothingToCommit(t *testing.T) {
	commitDir(t)
	logs := captureLogs(t)
	if err := runCommit(t, &Commit{Message: releaseMsg}, commitState(t, signing.Options{}), false); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(logs.String(), "Nothing to commit") ||
		strings.TrimSpace(runGit(t, "rev-list", "--count", "HEAD")) != "1" {
		t.Fatalf("log = %q", logs)
	}
}

func TestCommitEmptyMessage(t *testing.T) {
	commitDir(t)
	err := runCommit(t, &Commit{Message: "  "}, commitState(t, signing.Options{}), false)
	var d diag.Diagnostic
	if !errors.As(err, &d) || d.Code() != codeCommit {
		t.Fatalf("error = %v", err)
	}
}

func TestCommitSigning(t *testing.T) {
	t.Run("signs with git when it is set up", func(t *testing.T) {
		dir := commitDir(t)
		configureSigning(t, dir)
		writeTestFile(t, versionFile, "1\n")
		state := commitState(t, signing.Options{Sign: ptrBool(true)})
		if err := runCommit(t, &Commit{Message: releaseMsg}, state, false); err != nil {
			t.Fatal(err)
		}
		if got := runGit(t, "cat-file", "commit", "HEAD"); !strings.Contains(got, "gpgsig ") {
			t.Fatalf("the commit is not signed:\n%s", got)
		}
	})

	t.Run("sign = false turns off the signing of the user's own configuration", func(t *testing.T) {
		dir := commitDir(t)
		configureSigning(t, dir)
		runGit(t, "config", "commit.gpgsign", "true")
		writeTestFile(t, versionFile, "1\n")
		// The command line says sign and git signs everything, the step says no: the commit is not signed.
		state := commitState(t, signing.Options{Sign: ptrBool(true)})
		if err := runCommit(t, &Commit{Message: releaseMsg, Sign: ptrBool(false)}, state, false); err != nil {
			t.Fatal(err)
		}
		if got := runGit(t, "cat-file", "commit", "HEAD"); strings.Contains(got, "gpgsig") {
			t.Fatal("the commit is signed")
		}
	})

	t.Run("fails with the guide when there is nothing to sign with", func(t *testing.T) {
		commitDir(t)
		writeTestFile(t, versionFile, "1\n")
		before := runGit(t, "rev-parse", "HEAD")
		err := runCommit(t, &Commit{Message: releaseMsg, Sign: ptrBool(true)}, commitState(t, signing.Options{}), false)
		var d diag.Diagnostic
		if !errors.As(err, &d) || d.Code() != signing.CodeNotConfigured || !strings.Contains(d.Help(), "ssh-keygen") {
			t.Fatalf("error = %v", err)
		}
		if runGit(t, "rev-parse", "HEAD") != before {
			t.Fatal("a commit was made")
		}
	})

	t.Run("a bad signer in the step is refused", func(t *testing.T) {
		commitDir(t)
		if err := runCommit(
			t,
			&Commit{Message: releaseMsg, Signer: "gpg"},
			commitState(t, signing.Options{}),
			false,
		); err == nil {
			t.Fatal("no error")
		}
	})
}

func TestCommitDryRun(t *testing.T) {
	dir := commitDir(t)
	configureSigning(t, dir)
	writeTestFile(t, versionFile, "1\n")
	logs := captureLogs(t)
	before := runGit(t, "rev-parse", "HEAD")

	state := commitState(t, signing.Options{Sign: ptrBool(true)})
	if err := runCommit(t, &Commit{Message: releaseMsg}, state, true); err != nil {
		t.Fatal(err)
	}
	got := logs.String()
	for _, want := range []string{
		"Would create a signed commit using git and your own signing key on main: " + releaseMsg, "Would commit these files:", "  VERSION",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("log lacks %q:\n%s", want, got)
		}
	}
	if runGit(t, "rev-parse", "HEAD") != before || !strings.Contains(runGit(t, "status", "--porcelain"), versionFile) {
		t.Fatal("a dry run changed the repository")
	}
}

func TestCommitDryRunStillFailsWithoutSigning(t *testing.T) {
	commitDir(t)
	err := runCommit(t, &Commit{Message: releaseMsg, Sign: ptrBool(true)}, commitState(t, signing.Options{}), true)
	var d diag.Diagnostic
	if !errors.As(err, &d) || d.Code() != signing.CodeNotConfigured {
		t.Fatalf("error = %v, want the guide: the configuration checks fail in a dry run too", err)
	}
}

// apiStub is a configured forge that fails the test when it is asked to commit.
type apiStub struct{ t *testing.T }

func (apiStub) Name() string     { return "GitHub" }
func (apiStub) Configured() bool { return true }
func (a apiStub) CommitFiles(context.Context, signing.APICommit) (string, error) {
	a.t.Helper()
	a.t.Error("the forge must not be used in a dry run")
	return "", nil
}

func TestCommitDryRunUsesNoForge(t *testing.T) {
	commitDir(t)
	writeTestFile(t, versionFile, "1\n")
	logs := captureLogs(t)
	state := commitState(t, signing.Options{Sign: ptrBool(true)}, apiStub{t})
	if err := runCommit(t, &Commit{Message: releaseMsg}, state, true); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(logs.String(), "Would create a signed commit using the GitHub API on main") {
		t.Fatalf("log = %q", logs)
	}
}

func TestCommitOpensGitWhenStateHasNone(t *testing.T) {
	commitDir(t)
	writeTestFile(t, versionFile, "1\n")
	rt, err := (&Commit{Message: releaseMsg}).Run(context.Background(), workflow.Real(workflow.State{}))
	if err != nil {
		t.Fatal(err)
	}
	if rt.State.Git == nil {
		t.Error("the opened repository should be kept in the state")
	}
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

func TestCommitDryRunSignsWhenGitIsSetUpToSign(t *testing.T) {
	dir := commitDir(t)
	configureSigning(t, dir)
	writeTestFile(t, versionFile, "1\n")
	logs := captureLogs(t)

	// No sign option anywhere: the signing setup of Git is enough.
	if err := runCommit(t, &Commit{Message: releaseMsg}, commitState(t, signing.Options{}), true); err != nil {
		t.Fatal(err)
	}
	if want := "Would create a signed commit using git and your own signing key on main: " + releaseMsg; !strings.Contains(
		logs.String(),
		want,
	) {
		t.Fatalf("log lacks %q:\n%s", want, logs)
	}
}

func TestCommitSignsWhenGitIsSetUpToSign(t *testing.T) {
	dir := commitDir(t)
	configureSigning(t, dir)
	writeTestFile(t, versionFile, "1\n")

	// No sign option anywhere: the signing setup of Git is enough.
	if err := runCommit(t, &Commit{Message: releaseMsg}, commitState(t, signing.Options{}), false); err != nil {
		t.Fatal(err)
	}
	if got := runGit(t, "cat-file", "commit", "HEAD"); !strings.Contains(got, "gpgsig ") {
		t.Fatalf("the commit is not signed:\n%s", got)
	}
}
