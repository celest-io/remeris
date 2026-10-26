package steps

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/celest-io/remeris/internal/diag"
	"github.com/celest-io/remeris/internal/git"
	"github.com/celest-io/remeris/internal/workflow"
)

const (
	rebaseTarget   = "release"
	rebaseWouldLog = "Would rebase current branch onto " + rebaseTarget
	rebasedLog     = "Rebased current branch onto " + rebaseTarget
	rebasedSwitch  = "Switched to branch " + rebaseTarget + ", don't forget to push!"
	notFFText      = "Cannot fast-forward branch release to main: release has commits that are not in main"
)

// rebaseGit records the Rebase calls. The embedded nil interface makes any other call panic.
type rebaseGit struct {
	git.Git
	err   error
	calls []string
}

func (g *rebaseGit) Rebase(_ context.Context, to string) error {
	g.calls = append(g.calls, to)
	return g.err
}

func TestRebaseBranchDryRun(t *testing.T) {
	logs := captureLogs(t)
	// ensureGit would open "." and fail or succeed by accident: a dry run must not call it at all.
	rt, err := (&RebaseBranch{To: rebaseTarget}).Run(context.Background(), workflow.DryRun(workflow.State{}))
	if err != nil {
		t.Fatal(err)
	}
	if rt.State.Git != nil {
		t.Error("a dry run opened Git")
	}
	if !strings.Contains(logs.String(), rebaseWouldLog) {
		t.Errorf("log = %q, want %q", logs.String(), rebaseWouldLog)
	}
}

func TestRebaseBranchReal(t *testing.T) {
	cases := map[string]struct {
		err     error
		wantErr string
		wantLog []string
	}{
		"success logs both lines": {wantLog: []string{rebasedLog, rebasedSwitch}},
		"git refusal is returned as is": {
			err:     diag.New(notFFText).WithCode("remeris::git::not_fast_forward"),
			wantErr: notFFText,
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			logs := captureLogs(t)
			g := &rebaseGit{err: tc.err}
			_, err := (&RebaseBranch{To: rebaseTarget}).Run(context.Background(), workflow.Real(workflow.State{Git: g}))
			if tc.wantErr == "" && err != nil || tc.wantErr != "" && (err == nil || err.Error() != tc.wantErr) {
				t.Fatalf("err = %v, want %q", err, tc.wantErr)
			}
			var d diag.Diagnostic
			if tc.err != nil && !errors.As(err, &d) {
				t.Errorf("err = %#v, want a diagnostic", err)
			}
			if len(g.calls) != 1 || g.calls[0] != rebaseTarget {
				t.Errorf("calls = %v", g.calls)
			}
			for _, want := range tc.wantLog {
				if !strings.Contains(logs.String(), want) {
					t.Errorf("log = %q, want %q", logs.String(), want)
				}
			}
			if tc.wantLog == nil && strings.Contains(logs.String(), "Rebased") {
				t.Errorf("a failed rebase logged success: %q", logs.String())
			}
		})
	}
}
