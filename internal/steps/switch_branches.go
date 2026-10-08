package steps

import (
	"context"

	"github.com/celest-io/remeris/internal/workflow"
)

// SwitchBranches switches to the branch of the selected issue (milestone M11).
type SwitchBranches struct{}

func (*SwitchBranches) Type() string { return "SwitchBranches" }

// Run fails until the step is implemented, in a dry run too, so --validate reports it.
func (s *SwitchBranches) Run(_ context.Context, rt workflow.RunType[workflow.State]) (workflow.RunType[workflow.State], error) {
	return rt, notImplemented(s, "M11")
}
