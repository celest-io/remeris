package steps

import (
	"context"

	"github.com/celest-io/remeris/internal/workflow"
)

// TransitionJiraIssue moves the selected Jira issue to another status (milestone M11).
type TransitionJiraIssue struct {
	Status string `toml:"status"`
}

func (*TransitionJiraIssue) Type() string { return "TransitionJiraIssue" }

// Run fails until the step is implemented, in a dry run too, so --validate reports it.
func (s *TransitionJiraIssue) Run(
	_ context.Context,
	rt workflow.RunType[workflow.State],
) (workflow.RunType[workflow.State], error) {
	return rt, notImplemented(s, "M11")
}
