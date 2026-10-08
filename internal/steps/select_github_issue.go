package steps

import (
	"context"

	"github.com/celest-io/remeris/internal/workflow"
)

// SelectGitHubIssue selects a GitHub issue (milestone M11).
type SelectGitHubIssue struct {
	Labels []string `toml:"labels"`
}

func (*SelectGitHubIssue) Type() string { return "SelectGitHubIssue" }

// Run fails until the step is implemented, in a dry run too, so --validate reports it.
func (s *SelectGitHubIssue) Run(
	_ context.Context,
	rt workflow.RunType[workflow.State],
) (workflow.RunType[workflow.State], error) {
	return rt, notImplemented(s, "M11")
}
