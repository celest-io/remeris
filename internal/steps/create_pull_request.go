package steps

import (
	"context"
	"log/slog"

	"github.com/celest-io/remeris/internal/diag"
	"github.com/celest-io/remeris/internal/forge/gitea"
	"github.com/celest-io/remeris/internal/variables"
	"github.com/celest-io/remeris/internal/workflow"
)

// CreatePullRequest creates a pull request from the current branch to Base, or updates the one that exists
// (spec F18). Title and Body are templates. With both forges configured, GitHub runs first, as in Knope, and a
// failure stops the step: Gitea gets no request after a GitHub error.
type CreatePullRequest struct {
	Base  string             `toml:"base"`
	Title variables.Template `toml:"title"`
	Body  variables.Template `toml:"body"`
}

func (*CreatePullRequest) Type() string { return "CreatePullRequest" }

// NeedsPackages is true when the title or body uses the Version or ChangelogEntry variable.
func (s *CreatePullRequest) NeedsPackages() bool {
	return s.Title.NeedsPackages() || s.Body.NeedsPackages()
}

// Run substitutes the variables, then creates or updates the pull request on each configured forge. A dry run
// reads the current branch (a local Git read) and logs what would happen; it builds no client.
func (s *CreatePullRequest) Run(
	ctx context.Context,
	rt workflow.RunType[workflow.State],
) (workflow.RunType[workflow.State], error) {
	title, err := s.Title.Replace(rt.State)
	if err != nil {
		return rt, err
	}
	body, err := s.Body.Replace(rt.State)
	if err != nil {
		return rt, err
	}
	if rt.State.GitHub == nil && rt.State.Gitea == nil {
		return rt, diag.New("No forge has been configured").
			WithCode("remeris::create_pull_request::forge::not_configured").
			WithHelp("A forge must be configured in order to use the CreatePullRequest step")
	}
	// The branch is read once, before the dry-run check: a detached HEAD fails in a dry run too.
	branch, err := currentBranch(ctx, &rt.State)
	if err != nil {
		return rt, err
	}
	if rt.DryRun {
		// Like Knope, the lines are printed once per configured forge.
		for _, present := range []bool{rt.State.GitHub != nil, rt.State.Gitea != nil} {
			if !present {
				continue
			}
			for _, line := range gitea.DescribePullRequest(title, body, s.Base, branch) {
				slog.InfoContext(ctx, line)
			}
		}
		return rt, nil
	}
	if rt.State.GitHub != nil {
		client, err := rt.State.GitHub.RequireAuthentication(ctx)
		if err != nil {
			return rt, err
		}
		if err := client.CreateOrUpdatePullRequest(ctx, title, body, branch, s.Base); err != nil {
			return rt, err
		}
	}
	if rt.State.Gitea != nil {
		client, err := rt.State.Gitea.RequireAuthentication(ctx)
		if err != nil {
			return rt, err
		}
		if err := client.CreateOrUpdatePullRequest(ctx, title, body, s.Base, branch); err != nil {
			return rt, err
		}
	}
	return rt, nil
}

// currentBranch is the short name of the branch HEAD points to (Quirk Q3).
func currentBranch(ctx context.Context, state *workflow.State) (string, error) {
	g, err := ensureGit(ctx, state)
	if err != nil {
		return "", err
	}
	return g.CurrentBranch(ctx)
}
