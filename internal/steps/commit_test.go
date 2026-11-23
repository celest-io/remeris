package steps

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"strings"
	"testing"

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

func commitState(t *testing.T, options signing.Options) workflow.State {
	t.Helper()
	return workflow.State{Signing: signing.Config{Options: options}}
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

func TestCommitDryRun(t *testing.T) {
	commitDir(t)
	writeTestFile(t, versionFile, "1\n")
	logs := captureLogs(t)
	before := runGit(t, "rev-parse", "HEAD")

	if err := runCommit(t, &Commit{Message: releaseMsg}, commitState(t, signing.Options{}), true); err != nil {
		t.Fatal(err)
	}
	got := logs.String()
	for _, want := range []string{"Would create a commit using git on main: " + releaseMsg, "Would commit these files:", "  VERSION"} {
		if !strings.Contains(got, want) {
			t.Errorf("log lacks %q:\n%s", want, got)
		}
	}
	if runGit(t, "rev-parse", "HEAD") != before || !strings.Contains(runGit(t, "status", "--porcelain"), versionFile) {
		t.Fatal("a dry run changed the repository")
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
