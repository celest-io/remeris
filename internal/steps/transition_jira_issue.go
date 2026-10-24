package steps

import (
	"context"
	"log/slog"

	"github.com/celest-io/remeris/internal/diag"
	"github.com/celest-io/remeris/internal/workflow"
)

// TransitionJiraIssue moves the issue that SelectJiraIssue selected to another status (spec F19.4).
type TransitionJiraIssue struct {
	Status string `toml:"status"`
}

func (*TransitionJiraIssue) Type() string { return "TransitionJiraIssue" }

// Run checks the selected issue before the `[jira]` table, as Knope does, in a dry run too. A dry run then only
// logs: it builds no client, reads no credential and sends no request.
func (s *TransitionJiraIssue) Run(
	ctx context.Context,
	rt workflow.RunType[workflow.State],
) (workflow.RunType[workflow.State], error) {
	issue := rt.State.Issue
	if issue == nil {
		return rt, diag.New("No issue selected").
			WithCode("remeris::issues::jira::no_issue_selected").
			WithHelp("You must use the SelectJiraIssue step before TransitionJiraIssue in the same workflow")
	}
	if rt.State.Jira == nil {
		return rt, jiraNotConfigured()
	}
	if rt.DryRun {
		slog.InfoContext(ctx, "Would transition currently selected issue to status "+s.Status)
		return rt, nil
	}

	client, err := rt.State.Jira.RequireAuthentication(ctx)
	if err != nil {
		return rt, err
	}
	if err := client.TransitionIssue(ctx, issue.Key, s.Status); err != nil {
		return rt, err
	}
	slog.InfoContext(ctx, issue.Key+" transitioned to "+s.Status)
	return rt, nil
}
