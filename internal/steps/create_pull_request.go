package steps

import (
	"context"

	"github.com/celest-io/remeris/internal/variables"
	"github.com/celest-io/remeris/internal/workflow"
)

// CreatePullRequest opens a pull request on the forge (milestone M7).
type CreatePullRequest struct {
	Base  string             `toml:"base"`
	Title variables.Template `toml:"title"`
	Body  variables.Template `toml:"body"`
}

func (*CreatePullRequest) Type() string { return "CreatePullRequest" }

// Run fails until the step is implemented, in a dry run too, so --validate reports it.
func (s *CreatePullRequest) Run(
	_ context.Context,
	rt workflow.RunType[workflow.State],
) (workflow.RunType[workflow.State], error) {
	return rt, notImplemented(s, "M7")
}
