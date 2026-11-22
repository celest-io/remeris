package signing

import (
	"context"

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
