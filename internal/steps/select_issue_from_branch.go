package steps

import (
	"context"

	"github.com/celest-io/remeris/internal/workflow"
)

// SelectIssueFromBranch selects the issue named by the current branch (milestone M11).
type SelectIssueFromBranch struct{}

func (*SelectIssueFromBranch) Type() string { return "SelectIssueFromBranch" }

// Run fails until the step is implemented, in a dry run too, so --validate reports it.
func (s *SelectIssueFromBranch) Run(
	_ context.Context,
	rt workflow.RunType[workflow.State],
) (workflow.RunType[workflow.State], error) {
	return rt, notImplemented(s, "M11")
}
