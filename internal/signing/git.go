package signing

import (
	"context"
	"slices"

	"github.com/celest-io/remeris/internal/git"
)

// gitSigner commits with the git command: plain, or signed with the user's own key.
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
	paths := make([]string, len(req.Changes))
	for i, ch := range req.Changes {
		paths[i] = ch.Path
	}
	return s.git.Commit(ctx, req.Message, git.CommitOptions{Sign: s.sign, NoSign: s.noSign, Paths: slices.Clip(paths)})
}
