package steps

import (
	"bytes"
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/celest-io/remeris/internal/diag"
	"github.com/celest-io/remeris/internal/variables"
	"github.com/celest-io/remeris/internal/workflow"
)

const (
	milestone11 = "M11"
)

func skipOnWindows(t *testing.T) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("uses a POSIX shell")
	}
}

func captureLogs(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	old := slog.Default()
	slog.SetDefault(
		slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{ReplaceAttr: func(_ []string, a slog.Attr) slog.Attr {
			if a.Key == slog.TimeKey || a.Key == slog.LevelKey {
				return slog.Attr{}
			}
			return a
		}})),
	)
	t.Cleanup(func() { slog.SetDefault(old) })
	return &buf
}

func TestCommandDryRunDoesNotRun(t *testing.T) {
	skipOnWindows(t)
	t.Chdir(t.TempDir())
	logs := captureLogs(t)
	step := &Command{Command: "touch created", Variables: []variables.Entry{}}

	if _, err := step.Run(context.Background(), workflow.DryRun(workflow.State{})); err != nil {
		t.Fatal(err)
	}

	if _, err := os.Stat("created"); !os.IsNotExist(err) {
		t.Error("a dry run must not run the command")
	}
	if got := logs.String(); !strings.Contains(got, "msg=\"Would run touch created\"") {
		t.Errorf("log = %q", got)
	}
}

func TestCommandDryRunSubstitutesVariablesFirst(t *testing.T) {
	logs := captureLogs(t)
	step := &Command{Command: "echo $version"} // default variables apply, and Version cannot resolve in M0

	_, err := step.Run(context.Background(), workflow.DryRun(workflow.State{}))

	if err == nil || err.Error() != "No packages to operate on" {
		t.Errorf("err = %v", err)
	}
	if strings.Contains(logs.String(), "Would run") {
		t.Errorf("nothing should be logged when substitution fails: %q", logs)
	}
}

func TestCommandRun(t *testing.T) {
	skipOnWindows(t)
	cases := map[string]struct {
		step    Command
		wantMsg string // empty means success
		wantHel string
	}{
		"success":         {step: Command{Command: "true"}},
		"quoted argument": {step: Command{Command: `sh -c "exit 0"`}},
		"shell success":   {step: Command{Command: "exit 0", Shell: true}},
		"non-zero": {
			step:    Command{Command: "false"},
			wantMsg: "Command returned non-zero exit code",
			wantHel: "Try running it manually",
		},
		"shell non-zero":  {step: Command{Command: "exit 3", Shell: true}, wantMsg: "Command returned non-zero exit code"},
		"missing program": {step: Command{Command: "remeris-no-such-program"}, wantMsg: "I/O error: "},
		"empty command":   {step: Command{Command: "  "}, wantMsg: "I/O error: the command is empty"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := tc.step.Run(context.Background(), workflow.Real(workflow.State{}))
			if tc.wantMsg == "" {
				if err != nil {
					t.Fatal(err)
				}
				return
			}
			if err == nil || !strings.HasPrefix(err.Error(), tc.wantMsg) {
				t.Fatalf("err = %v, want prefix %q", err, tc.wantMsg)
			}
			if d, ok := err.(diag.Diagnostic); !ok || !strings.Contains(d.Help(), tc.wantHel) {
				t.Errorf("help of %v should contain %q", err, tc.wantHel)
			}
		})
	}
}

func TestCommandRunSideEffectsAndShellExpansion(t *testing.T) {
	skipOnWindows(t)
	t.Chdir(t.TempDir())
	t.Setenv("REMERIS_TEST_VALUE", "a value")

	shell := &Command{Command: `printf %s "$REMERIS_TEST_VALUE" > shell.out`, Shell: true}
	direct := &Command{Command: `sh -c 'printf %s "$REMERIS_TEST_VALUE" > direct.out'`}
	literal := &Command{Command: `touch $REMERIS_TEST_VALUE`} // direct mode must not expand the variable

	for _, step := range []*Command{shell, direct, literal} {
		if _, err := step.Run(context.Background(), workflow.Real(workflow.State{})); err != nil {
			t.Fatal(err)
		}
	}

	for _, name := range []string{"shell.out", "direct.out"} {
		if got, _ := os.ReadFile(name); string(got) != "a value" {
			t.Errorf("%s = %q", name, got)
		}
	}
	if _, err := os.Stat(filepath.Join(".", "$REMERIS_TEST_VALUE")); err != nil {
		t.Errorf("the file named by the literal $VAR should exist: %v", err)
	}
}

func TestUnimplementedSteps(t *testing.T) {
	cases := map[string]struct {
		step      workflow.Step
		milestone string
	}{
		"CreateChangeFile":      {&CreateChangeFile{}, "M4"},
		"PrepareRelease":        {&PrepareRelease{}, "M5"},
		"Release":               {&Release{}, "M6"},
		"CreatePullRequest":     {&CreatePullRequest{}, "M7"},
		"SelectJiraIssue":       {&SelectJiraIssue{}, milestone11},
		"TransitionJiraIssue":   {&TransitionJiraIssue{}, milestone11},
		"SelectGitHubIssue":     {&SelectGitHubIssue{}, milestone11},
		"SelectGiteaIssue":      {&SelectGiteaIssue{}, milestone11},
		"SelectIssueFromBranch": {&SelectIssueFromBranch{}, milestone11},
		"SwitchBranches":        {&SwitchBranches{}, milestone11},
		"RebaseBranch":          {&RebaseBranch{}, milestone11},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			if tc.step.Type() != name {
				t.Errorf("Type() = %q", tc.step.Type())
			}
			want := "Step " + name + " is not implemented yet (milestone " + tc.milestone + ")"
			for _, rt := range []workflow.RunType[workflow.State]{workflow.Real(workflow.State{}), workflow.DryRun(workflow.State{})} {
				_, err := tc.step.Run(context.Background(), rt)
				if err == nil || err.Error() != want {
					t.Errorf("dry-run=%v: err = %v, want %q", rt.DryRun, err, want)
				}
			}
		})
	}
}

func TestCommandType(t *testing.T) {
	if got := (&Command{}).Type(); got != "Command" {
		t.Errorf("Type() = %q", got)
	}
}
