package steps

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/celest-io/remeris/internal/diag"
	"github.com/celest-io/remeris/internal/git"
	"github.com/celest-io/remeris/internal/prompt"
	"github.com/celest-io/remeris/internal/workflow"
)

const (
	issueBranch   = "7-fix-the-bug"
	baseQuestion  = "Which branch do you want to base off of?"
	existsCall    = "exists " + issueBranch
	dirtyCall     = "dirty?"
	listCall      = "list"
	noInputText   = "Failed to get user input"
	badBranchText = "Bad branch name"
	trunkBranch   = "main"
)

// branchesGit records the calls of SwitchBranches. The embedded nil interface makes any other call panic.
type branchesGit struct {
	git.Git
	branches  []string
	existing  map[string]bool
	dirty     bool
	existsErr error
	createErr error
	calls     []string
}

func (g *branchesGit) BranchExists(_ context.Context, name string) (bool, error) {
	g.calls = append(g.calls, "exists "+name)
	return g.existing[name], g.existsErr
}

func (g *branchesGit) HasUncommittedChanges(context.Context) (bool, error) {
	g.calls = append(g.calls, dirtyCall)
	return g.dirty, nil
}

func (g *branchesGit) LocalBranches(context.Context) ([]string, error) {
	g.calls = append(g.calls, listCall)
	return g.branches, nil
}

func (g *branchesGit) CreateBranch(_ context.Context, name, from string) error {
	g.calls = append(g.calls, "create "+name+" from "+from)
	return g.createErr
}

func (g *branchesGit) SwitchTo(_ context.Context, name string) error {
	g.calls = append(g.calls, "switch "+name)
	return nil
}

func switchState(g git.Git, p prompt.Prompter) workflow.State {
	return workflow.State{Issue: &workflow.Issue{Key: "7", Summary: "Fix The Bug"}, Git: g, Prompter: p}
}

func TestSwitchBranchesNoIssue(t *testing.T) {
	for _, dry := range []bool{true, false} {
		rt := workflow.RunType[workflow.State]{DryRun: dry}
		_, err := (&SwitchBranches{}).Run(context.Background(), rt)
		var d diag.Diagnostic
		if !errors.As(err, &d) || d.Error() != "No issue selected" ||
			d.Code() != "remeris::git::no_issue_selected" ||
			d.Help() != "Switching branches requires selecting an issue first with SelectGitHubIssue or SelectJiraIssue" {
			t.Errorf("dry=%v: err = %#v", dry, err)
		}
	}
}

func TestSwitchBranchesDryRun(t *testing.T) {
	logs := captureLogs(t)
	p := prompt.NewScripted()
	// A nil Git panics on any call: a dry run must not touch it.
	if _, err := (&SwitchBranches{}).Run(context.Background(), workflow.DryRun(switchState(nil, p))); err != nil {
		t.Fatal(err)
	}
	if want := "Would switch to or create a branch named " + issueBranch; !strings.Contains(logs.String(), want) {
		t.Errorf("log = %q, want %q", logs.String(), want)
	}
	if len(p.Asked()) != 0 {
		t.Errorf("asked %v", p.Asked())
	}
}

func TestSwitchBranchesReal(t *testing.T) {
	cases := map[string]struct {
		git       *branchesGit
		answers   []prompt.Answer
		wantCalls []string
		wantLog   string
		wantErr   string
		wantAsked []string
	}{
		"existing branch switches without asking": {
			git:       &branchesGit{existing: map[string]bool{issueBranch: true}},
			wantCalls: []string{existsCall, "switch " + issueBranch},
			wantLog:   "Found existing branch named " + issueBranch + ", switching to it.",
		},
		"new branch asks for a base": {
			git:     &branchesGit{branches: []string{trunkBranch, "release"}},
			answers: []prompt.Answer{{Message: baseQuestion, Index: 1}},
			wantCalls: []string{
				existsCall,
				dirtyCall,
				listCall,
				"create " + issueBranch + " from release",
				"switch " + issueBranch,
			},
			wantLog:   "Creating a new branch called " + issueBranch,
			wantAsked: []string{baseQuestion},
		},
		"dirty tree stops before the prompt and the creation": {
			git:       &branchesGit{branches: []string{trunkBranch}, dirty: true},
			wantCalls: []string{existsCall, dirtyCall},
			wantErr:   "Uncommitted changes",
		},
		"no answer is a prompt failure": {
			git:       &branchesGit{branches: []string{trunkBranch}},
			wantCalls: []string{existsCall, dirtyCall, listCall},
			wantErr:   noInputText,
			wantAsked: []string{baseQuestion},
		},
		"no local branch is a prompt failure": {
			git:       &branchesGit{},
			wantCalls: []string{existsCall, dirtyCall, listCall},
			wantErr:   noInputText,
		},
		"bad branch name stops everything": {
			git:       &branchesGit{existsErr: diag.New(badBranchText)},
			wantCalls: []string{existsCall},
			wantErr:   badBranchText,
		},
		"create failure stops before the switch": {
			git:       &branchesGit{branches: []string{trunkBranch}, createErr: diag.New("create failed")},
			answers:   []prompt.Answer{{}},
			wantCalls: []string{existsCall, dirtyCall, listCall, "create " + issueBranch + " from main"},
			wantErr:   "create failed",
			wantAsked: []string{baseQuestion},
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			logs := captureLogs(t)
			p := prompt.NewScripted(tc.answers...)
			_, err := (&SwitchBranches{}).Run(context.Background(), workflow.Real(switchState(tc.git, p)))
			if tc.wantErr == "" && err != nil || tc.wantErr != "" && (err == nil || err.Error() != tc.wantErr) {
				t.Fatalf("err = %v, want %q", err, tc.wantErr)
			}
			if got, want := strings.Join(tc.git.calls, "|"), strings.Join(tc.wantCalls, "|"); got != want {
				t.Errorf("calls = %s, want %s", got, want)
			}
			if tc.wantLog != "" && !strings.Contains(logs.String(), tc.wantLog) {
				t.Errorf("log = %q, want %q", logs.String(), tc.wantLog)
			}
			var asked []string
			for _, a := range p.Asked() {
				asked = append(asked, a.Message)
			}
			if strings.Join(asked, "|") != strings.Join(tc.wantAsked, "|") {
				t.Errorf("asked = %v, want %v", asked, tc.wantAsked)
			}
		})
	}
}
