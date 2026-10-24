package steps

import (
	"context"
	"log/slog"
	"strconv"
	"strings"

	"github.com/celest-io/remeris/internal/diag"
	"github.com/celest-io/remeris/internal/workflow"
)

// SelectIssueFromBranch selects the issue named by the current branch (spec F19.5): "123-some-summary" is the
// GitHub or Gitea issue 123, "ABC-123-some-summary" the Jira issue ABC-123.
type SelectIssueFromBranch struct{}

func (*SelectIssueFromBranch) Type() string { return "SelectIssueFromBranch" }

// Run reads the current branch and sets the state's issue. A dry run reads nothing and selects a fake issue.
func (*SelectIssueFromBranch) Run(
	ctx context.Context,
	rt workflow.RunType[workflow.State],
) (workflow.RunType[workflow.State], error) {
	if rt.DryRun {
		slog.InfoContext(ctx, "Would attempt to parse current branch name to select current issue")
		rt.State.Issue = &workflow.Issue{Key: "123", Summary: "Fake Issue"}
		return rt, nil
	}
	g, err := ensureGit(ctx, &rt.State)
	if err != nil {
		return rt, err
	}
	branch, err := g.CurrentBranch(ctx)
	if err != nil {
		return rt, err
	}
	issue, err := issueFromBranchName(branch)
	if err != nil {
		return rt, err
	}
	slog.InfoContext(ctx, "Auto-selecting issue "+issue.Key+" from ref "+branch)
	rt.State.Issue = &issue
	return rt, nil
}

// issueFromBranchName parses a short branch name (Quirk Q3: Knope parsed the full ref). When the first
// hyphen-separated part is a number it is the issue key; otherwise it is a Jira project key and the second part
// must be a number. Numbers are read as Rust's usize does and printed back, so "007" becomes "7".
func issueFromBranchName(name string) (workflow.Issue, error) {
	parts := strings.Split(name, "-")
	if n, ok := parseUsize(parts[0]); ok {
		return workflow.Issue{Key: n, Summary: strings.Join(parts[1:], "-")}, nil
	}
	if len(parts) > 1 {
		if n, ok := parseUsize(parts[1]); ok {
			return workflow.Issue{Key: parts[0] + "-" + n, Summary: strings.Join(parts[2:], "-")}, nil
		}
	}
	return workflow.Issue{}, diag.New("Bad branch name").
		WithCode("remeris::git::bad_branch_name").
		WithHelp("The branch name was not formatted correctly.")
}

// parseUsize parses s like Rust's usize::from_str on a 64-bit target: decimal digits with one optional leading
// "+", no other sign, no spaces or separators, and a value that fits in 64 bits. It returns the number printed
// back.
func parseUsize(s string) (string, bool) {
	digits := strings.TrimPrefix(s, "+")
	if digits == "" || digits[0] < '0' || digits[0] > '9' {
		return "", false
	}
	n, err := strconv.ParseUint(digits, 10, 64)
	if err != nil {
		return "", false
	}
	return strconv.FormatUint(n, 10), true
}
