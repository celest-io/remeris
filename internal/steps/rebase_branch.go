package steps

import (
	"context"
	"log/slog"

	"github.com/celest-io/remeris/internal/workflow"
)

// RebaseBranch moves a local branch to the current commit and switches to it (spec F19.8). Despite its name it is
// fast-forward only: Knope never replays commits, and remeris refuses when the branch has commits of its own.
type RebaseBranch struct {
	To string `toml:"to"`
}

func (*RebaseBranch) Type() string { return "RebaseBranch" }

// Run logs what it would do in a dry run, checks nothing and opens no Git. A real run asks Git to move the branch
// and switch to it; Git refuses a dirty tree, a missing branch and a branch that is not behind HEAD.
func (s *RebaseBranch) Run(
	ctx context.Context,
	rt workflow.RunType[workflow.State],
) (workflow.RunType[workflow.State], error) {
	if rt.DryRun {
		slog.InfoContext(ctx, "Would rebase current branch onto "+s.To)
		return rt, nil
	}
	g, err := ensureGit(ctx, &rt.State)
	if err != nil {
		return rt, err
	}
	if err := g.Rebase(ctx, s.To); err != nil {
		return rt, err
	}
	slog.InfoContext(ctx, "Rebased current branch onto "+s.To)
	slog.InfoContext(ctx, "Switched to branch "+s.To+", don't forget to push!")
	return rt, nil
}
