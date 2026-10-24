package steps

import (
	"context"
	"log/slog"
	"strconv"
	"strings"

	"github.com/celest-io/remeris/internal/diag"
	"github.com/celest-io/remeris/internal/workflow"
)

// SelectGiteaIssue asks the user to pick one of the open issues of the configured Gitea repository (spec F19.2)
// and keeps it in State.Issue for SwitchBranches and the IssueBranch variable. Labels, when given, narrow the list.
type SelectGiteaIssue struct {
	Labels []string `toml:"labels"`
}

func (*SelectGiteaIssue) Type() string { return "SelectGiteaIssue" }

// Run needs the `[gitea]` table in a dry run too. A dry run then logs what would happen, selects a fake issue so
// that later steps can run, and builds no client, reads no credential and asks nothing. The texts are Knope's,
// which differ from those of SelectGitHubIssue.
func (s *SelectGiteaIssue) Run(
	ctx context.Context,
	rt workflow.RunType[workflow.State],
) (workflow.RunType[workflow.State], error) {
	if rt.State.Gitea == nil {
		return rt, diag.New("Gitea is not configured").
			WithCode("remeris::issues::gitea::not_configured").
			WithHelp("Gitea must be configured in order to use the SelectGiteaIssue step")
	}
	if rt.DryRun {
		if s.Labels != nil {
			slog.InfoContext(ctx, "Would query configured Gitea instance for issues with labels "+strings.Join(s.Labels, ", "))
		} else {
			slog.InfoContext(ctx, "Would query configured Gitea instance for issues with any labels")
		}
		slog.InfoContext(ctx, "Would prompt user to select an issue")
		rt.State.Issue = &workflow.Issue{Key: "123", Summary: "Test issue"}
		return rt, nil
	}

	client, err := rt.State.Gitea.RequireAuthentication(ctx)
	if err != nil {
		return rt, err
	}
	listed, err := client.ListIssues(ctx, s.Labels)
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
	slog.InfoContext(ctx, "Selected item: "+issue.String())
	rt.State.Issue = &issue
	return rt, nil
}
