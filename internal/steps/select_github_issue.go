package steps

import (
	"context"
	"log/slog"
	"strconv"
	"strings"

	"github.com/celest-io/remeris/internal/diag"
	"github.com/celest-io/remeris/internal/prompt"
	"github.com/celest-io/remeris/internal/workflow"
)

// selectIssuePrompt is the question of every Select*Issue step (Knope's wording).
const selectIssuePrompt = "Select an Issue"

// SelectGitHubIssue asks the user to pick one of the open issues of the configured GitHub repository (spec F19.1)
// and keeps it in State.Issue for SwitchBranches and the IssueBranch variable. Labels, when given, narrow the list.
type SelectGitHubIssue struct {
	Labels []string `toml:"labels"`
}

func (*SelectGitHubIssue) Type() string { return "SelectGitHubIssue" }

// Run needs the `[github]` table in a dry run too. A dry run then logs what would happen, selects a fake issue so
// that later steps can run, and builds no client, reads no credential and asks nothing.
func (s *SelectGitHubIssue) Run(
	ctx context.Context,
	rt workflow.RunType[workflow.State],
) (workflow.RunType[workflow.State], error) {
	if rt.State.GitHub == nil {
		return rt, diag.New("GitHub is not configured").
			WithCode("remeris::issues::github::not_configured").
			WithHelp("GitHub must be configured in order to use the SelectGitHubIssue step")
	}
	if rt.DryRun {
		if s.Labels != nil {
			slog.InfoContext(ctx, "Would query configured GitHub instance for issues with labels "+strings.Join(s.Labels, ", "))
		} else {
			slog.InfoContext(ctx, "Would query configured GitHub instance for issues with any labels")
		}
		slog.InfoContext(ctx, "Would prompt user to select an issue and move workflow to IssueSelected state.")
		rt.State.Issue = &workflow.Issue{Key: "123", Summary: fakeIssueSummary}
		return rt, nil
	}

	client, err := rt.State.GitHub.RequireAuthentication(ctx)
	if err != nil {
		return rt, err
	}
	listed, err := client.ListOpenIssues(ctx, s.Labels)
	if err != nil {
		return rt, err
	}
	issues := make([]workflow.Issue, len(listed))
	for i, issue := range listed {
		issues[i] = workflow.Issue{Key: strconv.Itoa(issue.Number), Summary: issue.Title}
	}
	issue, err := selectIssue(ctx, rt.State.Prompter, issues)
	if err != nil {
		return rt, err
	}
	slog.InfoContext(ctx, "Selected item : "+issue.String())
	rt.State.Issue = &issue
	return rt, nil
}

// fakeIssueSummary is the title of the fake issue that the dry run of the Select*Issue steps selects.
const fakeIssueSummary = "Test issue"

// selectIssue asks the user to choose one of issues. No issue to choose from, or no way to ask, is the prompt
// failure Knope reports.
func selectIssue(ctx context.Context, p prompt.Prompter, issues []workflow.Issue) (workflow.Issue, error) {
	if len(issues) == 0 {
		return workflow.Issue{}, prompt.Error(errNoOptions)
	}
	if p == nil {
		return workflow.Issue{}, prompt.Error(errNoPrompter)
	}
	options := make([]string, len(issues))
	for i, issue := range issues {
		options[i] = issue.String()
	}
	index, err := p.Select(ctx, selectIssuePrompt, options)
	if err != nil {
		return workflow.Issue{}, err
	}
	return issues[index], nil
}
