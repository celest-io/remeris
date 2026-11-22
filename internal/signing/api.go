package signing

import (
	"context"

	"github.com/celest-io/remeris/internal/diag"
	"github.com/celest-io/remeris/internal/git"
)

// apiSigner makes the commit through a forge API, which signs it.
type apiSigner struct {
	git       git.Git
	committer git.Committer
	api       APICommitter
}

func (s *apiSigner) Name() string { return "the " + s.api.Name() + " API" }

func (s *apiSigner) Signed() bool { return true }

func (s *apiSigner) Commit(context.Context, Request) (string, error) {
	return "", errNotYet("signing through the " + s.api.Name() + " API")
}

// errNotYet is the error of a way to sign that is chosen but not built in this version.
func errNotYet(what string) error {
	return diag.Newf("%s is not available yet", what).
		WithCode("remeris::commit::not_available").
		WithHelp("This build can detect the signing setup but cannot make the commit. Use sign = false or sign with git.")
}
