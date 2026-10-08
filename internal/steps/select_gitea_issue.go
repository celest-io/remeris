package steps

import (
	"context"

	"github.com/celest-io/remeris/internal/workflow"
)

// SelectGiteaIssue selects a Gitea issue (milestone M11).
type SelectGiteaIssue struct {
	Labels []string `toml:"labels"`
}

func (*SelectGiteaIssue) Type() string { return "SelectGiteaIssue" }

// Run fails until the step is implemented, in a dry run too, so --validate reports it.
func (s *SelectGiteaIssue) Run(
	_ context.Context,
	rt workflow.RunType[workflow.State],
) (workflow.RunType[workflow.State], error) {
	return rt, notImplemented(s, "M11")
}
