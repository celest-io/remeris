package steps

import (
	"context"

	"github.com/celest-io/remeris/internal/git"
	"github.com/celest-io/remeris/internal/workflow"
)

// ensureGit returns the state's Git access. Like Knope, a step that needs Git opens the repository itself when
// the state has none (a workflow without packages, or opening failed when the project was loaded), and keeps the
// result in the state for the steps after it.
func ensureGit(ctx context.Context, state *workflow.State) (git.Git, error) {
	if state.Git != nil {
		return state.Git, nil
	}
	g, err := git.Open(ctx, ".")
	if err != nil {
		return nil, err
	}
	state.Git = g
	return g, nil
}
