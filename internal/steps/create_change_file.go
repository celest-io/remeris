package steps

import (
	"context"

	"github.com/celest-io/remeris/internal/workflow"
)

// CreateChangeFile writes a new change file (milestone M4).
type CreateChangeFile struct{}

func (*CreateChangeFile) Type() string { return "CreateChangeFile" }

// Run fails until the step is implemented, in a dry run too, so --validate reports it.
func (s *CreateChangeFile) Run(
	_ context.Context,
	rt workflow.RunType[workflow.State],
) (workflow.RunType[workflow.State], error) {
	return rt, notImplemented(s, "M4")
}
