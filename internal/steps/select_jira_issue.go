package steps

import (
	"context"

	"github.com/celest-io/remeris/internal/workflow"
)

// SelectJiraIssue selects a Jira issue (milestone M11).
type SelectJiraIssue struct {
	Status string `toml:"status"`
}

func (*SelectJiraIssue) Type() string { return "SelectJiraIssue" }

// Run fails until the step is implemented, in a dry run too, so --validate reports it.
func (s *SelectJiraIssue) Run(
	_ context.Context,
	rt workflow.RunType[workflow.State],
) (workflow.RunType[workflow.State], error) {
	return rt, notImplemented(s, "M11")
}
