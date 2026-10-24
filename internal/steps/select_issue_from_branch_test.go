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
	bigNumber   = "18446744073709551616" // usize::MAX + 1
	maxUsize    = "18446744073709551615"
	abc123      = "ABC-123"
	someSummary = "some-summary"
)

// branchGit serves a fixed branch name. The embedded nil interface makes any other call panic.
type branchGit struct {
	git.Git
	branch string
	err    error
	calls  int
}

func (g *branchGit) CurrentBranch(context.Context) (string, error) {
	g.calls++
	return g.branch, g.err
}

func TestIssueFromBranchName(t *testing.T) {
	cases := map[string]struct {
		branch string
		want   workflow.Issue
		bad    bool
	}{
		"jira style":           {branch: "ABC-123-some-summary", want: workflow.Issue{Key: abc123, Summary: someSummary}},
		"github style":         {branch: "123-some-summary", want: workflow.Issue{Key: "123", Summary: someSummary}},
		"no number":            {branch: "some-summary", bad: true},
		"github without text":  {branch: "123", want: workflow.Issue{Key: "123"}},
		"jira without text":    {branch: "ABC-123", want: workflow.Issue{Key: abc123}},
		"leading zeros":        {branch: "007-x", want: workflow.Issue{Key: "7", Summary: "x"}},
		"jira leading zeros":   {branch: "ABC-007-x", want: workflow.Issue{Key: "ABC-7", Summary: "x"}},
		"plus sign":            {branch: "+5-x", want: workflow.Issue{Key: "5", Summary: "x"}},
		"jira plus sign":       {branch: "ABC-+5-x", want: workflow.Issue{Key: "ABC-5", Summary: "x"}},
		"empty project key":    {branch: "-5-x", want: workflow.Issue{Key: "-5", Summary: "x"}}, // as Knope
		"minus in number":      {branch: "ABC--5-x", bad: true},
		"lone plus":            {branch: "+-x", bad: true},
		"double plus":          {branch: "++5-x", bad: true},
		"too big":              {branch: bigNumber + "-x", bad: true},
		"max usize":            {branch: maxUsize + "-x", want: workflow.Issue{Key: maxUsize, Summary: "x"}},
		"jira too big":         {branch: "ABC-" + bigNumber, bad: true},
		"empty":                {branch: "", bad: true},
		"single dash":          {branch: "-", bad: true},
		"project only":         {branch: "ABC-", bad: true},
		"space in number":      {branch: "1 2-x", bad: true},
		"underscore in number": {branch: "1_000-x", bad: true},
		"non ascii digit":      {branch: "١٢٣-x", bad: true},
		"summary keeps dashes": {branch: "12-a-b--c", want: workflow.Issue{Key: "12", Summary: "a-b--c"}},
		"number then project":  {branch: "12-ABC-34", want: workflow.Issue{Key: "12", Summary: "ABC-34"}},
		"slashes are kept":     {branch: "ABC-1-fix/it", want: workflow.Issue{Key: "ABC-1", Summary: "fix/it"}},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			got, err := issueFromBranchName(tc.branch)
			if tc.bad {
				assertBadBranch(t, err)
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if got != tc.want {
				t.Errorf("issue = %+v, want %+v", got, tc.want)
			}
		})
	}
}

func assertBadBranch(t *testing.T, err error) {
	t.Helper()
	var d diag.Diagnostic
	if !errors.As(err, &d) {
		t.Fatalf("err = %v, want a diagnostic", err)
	}
	if d.Error() != badBranchText || d.Help() != "The branch name was not formatted correctly." ||
		d.Code() != "remeris::git::bad_branch_name" {
		t.Errorf("diagnostic = %q / %q / %q", d.Error(), d.Help(), d.Code())
	}
}

func TestSelectIssueFromBranchDryRun(t *testing.T) {
	logs := captureLogs(t)
	g := &branchGit{branch: "unused"}
	state := workflow.State{Git: g}

	rt, err := (&SelectIssueFromBranch{}).Run(context.Background(), workflow.DryRun(state))
	if err != nil {
		t.Fatal(err)
	}

	want := workflow.Issue{Key: "123", Summary: "Fake Issue"}
	if rt.State.Issue == nil || *rt.State.Issue != want {
		t.Errorf("issue = %v, want %v", rt.State.Issue, want)
	}
	if g.calls != 0 {
		t.Error("a dry run must not read Git")
	}
	if got := logs.String(); !strings.Contains(got,
		`msg="Would attempt to parse current branch name to select current issue"`) {
		t.Errorf("log = %q", got)
	}
}

func TestSelectIssueFromBranchDryRunWithoutGit(t *testing.T) {
	t.Chdir(t.TempDir())
	t.Setenv("PATH", "")
	rt, err := (&SelectIssueFromBranch{}).Run(context.Background(), workflow.DryRun(workflow.State{}))
	if err != nil {
		t.Fatal(err)
	}
	if rt.State.Git != nil {
		t.Error("a dry run must not open the repository")
	}
}

func TestSelectIssueFromBranchReal(t *testing.T) {
	cases := map[string]struct {
		branch  string
		gitErr  error
		want    workflow.Issue
		wantLog string
		wantErr string
	}{
		"bad name": {branch: "feature-x", wantErr: badBranchText},
		"issue": {
			branch:  "42-fix-it",
			want:    workflow.Issue{Key: "42", Summary: "fix-it"},
			wantLog: `msg="Auto-selecting issue 42 from ref 42-fix-it"`,
		},
		"jira": {
			branch:  "ABC-9-x",
			want:    workflow.Issue{Key: "ABC-9", Summary: "x"},
			wantLog: `msg="Auto-selecting issue ABC-9 from ref ABC-9-x"`,
		},
		"git error": {gitErr: errors.New("detached"), wantErr: "detached"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			logs := captureLogs(t)
			state := workflow.State{Git: &branchGit{branch: tc.branch, err: tc.gitErr}}

			rt, err := (&SelectIssueFromBranch{}).Run(context.Background(), workflow.Real(state))

			if tc.wantErr != "" {
				if err == nil || err.Error() != tc.wantErr {
					t.Fatalf("err = %v, want %q", err, tc.wantErr)
				}
				if rt.State.Issue != nil {
					t.Errorf("issue = %v after a failure", rt.State.Issue)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if rt.State.Issue == nil || *rt.State.Issue != tc.want {
				t.Errorf("issue = %v, want %v", rt.State.Issue, tc.want)
			}
			if !strings.Contains(logs.String(), tc.wantLog) {
				t.Errorf("log = %q, want %q", logs.String(), tc.wantLog)
			}
		})
	}
}

func TestParseUsize(t *testing.T) {
	cases := map[string]struct {
		in   string
		want string
		ok   bool
	}{
		"zero":       {"0", "0", true},
		"padded":     {"0042", "42", true},
		"plus":       {"+7", "7", true},
		"empty":      {"", "", false},
		"plus only":  {"+", "", false},
		"minus":      {"-1", "", false},
		"minus zero": {"-0", "", false},
		"letters":    {"1a", "", false},
		"overflow":   {bigNumber, "", false},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			got, ok := parseUsize(tc.in)
			if got != tc.want || ok != tc.ok {
				t.Errorf("parseUsize(%q) = %q, %v", tc.in, got, ok)
			}
		})
	}
}
