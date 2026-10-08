package steps

import (
	"context"

	"github.com/celest-io/remeris/internal/workflow"
)

// RebaseBranch rebases onto another branch (milestone M11).
type RebaseBranch struct {
	To string `toml:"to"`
}

func (*RebaseBranch) Type() string { return "RebaseBranch" }

// Run fails until the step is implemented, in a dry run too, so --validate reports it.
func (s *RebaseBranch) Run(_ context.Context, rt workflow.RunType[workflow.State]) (workflow.RunType[workflow.State], error) {
	return rt, notImplemented(s, "M11")
}
