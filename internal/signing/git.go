package signing

import (
	"context"

	"github.com/celest-io/remeris/internal/git"
)

// gitSigner commits with the git command, the way a plain "git commit" does.
type gitSigner struct {
	git git.Committer
}

func (*gitSigner) Name() string { return "git" }

func (*gitSigner) Signed() bool { return false }

func (s *gitSigner) Commit(ctx context.Context, req Request) (string, error) {
	paths := make([]string, len(req.Changes))
	for i, ch := range req.Changes {
		paths[i] = ch.Path
	}
	return s.git.Commit(ctx, req.Message, git.CommitOptions{Paths: paths})
}
