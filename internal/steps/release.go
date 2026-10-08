package steps

import (
	"context"

	"github.com/celest-io/remeris/internal/workflow"
)

// Release tags and publishes the prepared release (milestone M6).
type Release struct{}

func (*Release) Type() string { return "Release" }

// Run fails until the step is implemented, in a dry run too, so --validate reports it.
func (s *Release) Run(_ context.Context, rt workflow.RunType[workflow.State]) (workflow.RunType[workflow.State], error) {
	return rt, notImplemented(s, "M6")
}
