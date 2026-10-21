package steps

import (
	"context"
	"log/slog"

	"github.com/celest-io/remeris/internal/diag"
	"github.com/celest-io/remeris/internal/git"
	"github.com/celest-io/remeris/internal/variables"
	"github.com/celest-io/remeris/internal/workflow"
)

// CreatePullRequest creates a pull request from the current branch to Base, or updates the one that exists
// (spec F18). Title and Body are templates. GitHub is done (M7); Gitea arrives in milestone M8 and the step fails
// with "not implemented" until then, in a dry run too. With both configured, GitHub runs first, as in Knope.
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
	if rt.State.GitHub != nil {
		if err := s.onGitHub(ctx, &rt, title, body); err != nil {
			return rt, err
		}
	}
	if rt.State.Gitea != nil {
		return rt, notImplemented(s, "M8")
	}
	return rt, nil
}

func (s *CreatePullRequest) onGitHub(
	ctx context.Context, rt *workflow.RunType[workflow.State], title, body string,
) error {
	branch, err := currentBranch(ctx, &rt.State)
	if err != nil {
		return err
	}
	if rt.DryRun {
		slog.InfoContext(ctx, "Would create or update a pull request from "+branch+" to "+s.Base+":")
		slog.InfoContext(ctx, "\tTitle: "+title)
		slog.InfoContext(ctx, "\tBody: "+body)
		return nil
	}
	client, err := rt.State.GitHub.RequireAuthentication(ctx)
	if err != nil {
		return err
	}
	return client.CreateOrUpdatePullRequest(ctx, title, body, branch, s.Base)
}

// currentBranch is the short name of the branch HEAD points to (Quirk Q3). Like Knope, it opens the repository
// itself when the state has none.
func currentBranch(ctx context.Context, state *workflow.State) (string, error) {
	g := state.Git
	if g == nil {
		var err error
		if g, err = git.Open(ctx, "."); err != nil {
			return "", err
		}
		state.Git = g
	}
	return g.CurrentBranch(ctx)
}
