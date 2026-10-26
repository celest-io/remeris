package steps

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/celest-io/remeris/internal/diag"
	"github.com/celest-io/remeris/internal/prompt"
	"github.com/celest-io/remeris/internal/workflow"
)

// TestSwitchBranchesHostileTitle runs against a real repository: a title that git refuses in a ref name ends in the
// badBranchText diagnostic, before anything is asked or created, and never reaches git as an option.
func TestSwitchBranchesHostileTitle(t *testing.T) {
	skipOnWindows(t)
	titles := map[string]string{
		"double dot":      "evil..title",
		"revision":        "evil~1",
		"colon":           "a:b",
		"lock suffix":     "evil.lock",
		"trailing slash":  "evil/",
		"spaces and dots": "--upload-pack=touch pwned..",
		"glob":            "x[*?]",
	}
	for name, title := range titles {
		t.Run(name, func(t *testing.T) {
			t.Chdir(t.TempDir())
			initArgs := [][]string{
				{"init", "--initial-branch=main"},
				{"-c", "user.email=a@b.c", "-c", "user.name=n", "commit", "--allow-empty", "-m", "init"},
			}
			for _, args := range initArgs {
				if out, err := exec.Command("git", args...).CombinedOutput(); err != nil {
					t.Fatalf("git %v: %v\n%s", args, err, out)
				}
			}
			p := prompt.NewScripted()
			state := workflow.State{Issue: &workflow.Issue{Key: "9", Summary: title}, Prompter: p}
			_, err := (&SwitchBranches{}).Run(context.Background(), workflow.Real(state))
			var d diag.Diagnostic
			if !errors.As(err, &d) || d.Error() != badBranchText || d.Code() != "remeris::git::bad_branch_name" {
				t.Fatalf("err = %#v", err)
			}
			if len(p.Asked()) != 0 {
				t.Errorf("asked %v", p.Asked())
			}
			out, err := exec.Command("git", "branch", "--format=%(refname:short)").Output()
			if err != nil || strings.TrimSpace(string(out)) != trunkBranch {
				t.Errorf("branches = %q, %v", out, err)
			}
			if _, err := os.Stat("pwned"); err == nil {
				t.Error("an option was executed")
			}
		})
	}
}
