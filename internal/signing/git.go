package signing

import (
	"context"
	"slices"

	"github.com/celest-io/remeris/internal/diag"
	"github.com/celest-io/remeris/internal/git"
)

// gitSigner commits with the git command: plain, signed with the user's own key, or with signing turned off.
type gitSigner struct {
	git    git.Committer
	sign   bool
	noSign bool
	cfg    git.SigningConfig
}

func (s *gitSigner) Name() string {
	if s.sign {
		return "git and your own signing key"
	}
	return "git"
}

func (s *gitSigner) Signed() bool { return s.sign }

func (s *gitSigner) Commit(ctx context.Context, req Request) (string, error) {
	if s.sign {
		return "", errNotYet("signing with your own key")
	}
	paths := make([]string, len(req.Changes))
	for i, ch := range req.Changes {
		paths[i] = ch.Path
	}
	return s.git.Commit(ctx, req.Message, git.CommitOptions{NoSign: s.noSign, Paths: slices.Clip(paths)})
}

// errNotYet is the error of a way to sign that is chosen but not built in this version.
func errNotYet(what string) error {
	return diag.Newf("%s is not available yet", what).
		WithCode("remeris::commit::not_available").
		WithHelp("This build can detect the signing setup but cannot make the commit. Use sign = false for now.")
}
