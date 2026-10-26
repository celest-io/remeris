package steps

import (
	"context"
	"log/slog"

	"github.com/celest-io/remeris/internal/diag"
	"github.com/celest-io/remeris/internal/workflow"
)

// jiraNotConfigured is Knope's error for a missing `[jira]` table, shared by both Jira steps (so its help speaks of
// selecting an issue even for TransitionJiraIssue).
func jiraNotConfigured() error {
	return diag.New("Jira is not configured").
		WithCode("remeris::issues::jira::not_configured").
		WithHelp("Jira must be configured in order to select a Jira issue")
}

// SelectJiraIssue asks the user to pick one of the issues of the configured Jira project that are in a status
// (spec F19.3) and keeps it in State.Issue for SwitchBranches, TransitionJiraIssue and the IssueBranch variable.
type SelectJiraIssue struct {
	Status string `toml:"status"`
}

func (*SelectJiraIssue) Type() string { return "SelectJiraIssue" }

// Run needs the `[jira]` table in a dry run too, and checks it before the dry-run branch, as Knope does. A dry run
// then logs what would happen, selects a fake issue so that later steps can run, and builds no client, reads no
// credential, sends no request and asks nothing.
func (s *SelectJiraIssue) Run(
	ctx context.Context,
	rt workflow.RunType[workflow.State],
) (workflow.RunType[workflow.State], error) {
	if rt.State.Jira == nil {
		return rt, jiraNotConfigured()
	}
	if rt.DryRun {
		slog.InfoContext(ctx, "Would query configured Jira instance for issues with status "+s.Status)
		slog.InfoContext(ctx, "Would prompt user to select an issue and move workflow to IssueSelected state.")
		rt.State.Issue = &workflow.Issue{Key: "FAKE-123", Summary: fakeIssueSummary}
		return rt, nil
	}

	client, err := rt.State.Jira.RequireAuthentication(ctx)
	if err != nil {
		return rt, err
	}
	listed, err := client.SearchIssues(ctx, s.Status)
	if err != nil {
		return rt, err
	}
	issues := make([]workflow.Issue, len(listed))
	for i, issue := range listed {
		issues[i] = workflow.Issue{Key: issue.Key, Summary: issue.Summary}
	}
	issue, err := selectIssue(ctx, rt.State.Prompter, issues)
	if err != nil {
		return rt, err
	}
	slog.InfoContext(ctx, "Selected item : "+issue.String())
	rt.State.Issue = &issue
	return rt, nil
}
