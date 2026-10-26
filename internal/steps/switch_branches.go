package steps

import (
	"context"
	"errors"
	"log/slog"

	"github.com/celest-io/remeris/internal/diag"
	"github.com/celest-io/remeris/internal/prompt"
	"github.com/celest-io/remeris/internal/workflow"
)

// baseBranchPrompt is Knope's question when a new branch is created.
const baseBranchPrompt = "Which branch do you want to base off of?"

// errNoLocalBranches says why the base branch prompt had nothing to offer.
var errNoLocalBranches = errors.New("there is no local branch to base the new branch on")

// SwitchBranches switches to the branch named after the selected issue, creating it when it does not exist
// (spec F19.7).
type SwitchBranches struct{}

func (*SwitchBranches) Type() string { return "SwitchBranches" }

// Run needs an issue from a Select*Issue step, in a dry run too. A dry run then only logs and touches no Git.
// A real run switches to the issue's branch when it exists. Otherwise it asks which local branch to base the
// new one on, creates it and switches to it. The branch name comes from an issue title, so Git's branch methods
// refuse a hostile one with "Bad branch name" before any command runs. The clean check comes before the
// branch is created (a deviation from Knope, which creates first), so a failed switch leaves nothing behind.
func (*SwitchBranches) Run(
	ctx context.Context,
	rt workflow.RunType[workflow.State],
) (workflow.RunType[workflow.State], error) {
	issue := rt.State.Issue
	if issue == nil {
		return rt, diag.New("No issue selected").
			WithCode("remeris::git::no_issue_selected").
			WithHelp("Switching branches requires selecting an issue first with SelectGitHubIssue or SelectJiraIssue")
	}
	name := issue.BranchName()
	if rt.DryRun {
		slog.InfoContext(ctx, "Would switch to or create a branch named "+name)
		return rt, nil
	}
	g, err := ensureGit(ctx, &rt.State)
	if err != nil {
		return rt, err
	}
	exists, err := g.BranchExists(ctx, name)
	if err != nil {
		return rt, err
	}
	if exists {
		slog.InfoContext(ctx, "Found existing branch named "+name+", switching to it.")
		return rt, g.SwitchTo(ctx, name)
	}
	slog.InfoContext(ctx, "Creating a new branch called "+name)
	dirty, err := g.HasUncommittedChanges(ctx)
	if err != nil {
		return rt, err
	}
	if dirty {
		return rt, uncommittedChanges()
	}
	branches, err := g.LocalBranches(ctx)
	if err != nil {
		return rt, err
	}
	base, err := chooseBaseBranch(ctx, rt.State.Prompter, branches)
	if err != nil {
		return rt, err
	}
	if err := g.CreateBranch(ctx, name, base); err != nil {
		return rt, err
	}
	return rt, g.SwitchTo(ctx, name)
}

func chooseBaseBranch(ctx context.Context, p prompt.Prompter, branches []string) (string, error) {
	if len(branches) == 0 {
		return "", prompt.Error(errNoLocalBranches)
	}
	if p == nil {
		return "", prompt.Error(errNoPrompter)
	}
	index, err := p.Select(ctx, baseBranchPrompt, branches)
	if err != nil {
		return "", err
	}
	return branches[index], nil
}

// uncommittedChanges is the error of git.SwitchTo, raised early so that nothing is asked or created first.
func uncommittedChanges() error {
	return diag.New("Uncommitted changes").
		WithCode("remeris::git::uncommitted_changes").
		WithHelp("You need to commit your changes before running this step.")
}
