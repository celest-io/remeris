package signing

import (
	"context"

	"github.com/celest-io/remeris/internal/forge/github"
)

// GitHub is the APICommitter of GitHub: createCommitOnBranch, which GitHub signs.
type GitHub struct {
	// Forge is the GitHub forge to commit with. It is the one of the [github] table, or one with a dedicated App.
	Forge *github.Forge
}

var _ APICommitter = GitHub{}

func (GitHub) Name() string { return "GitHub" }

// Configured is true when the forge has an App or a token variable. It reads no secret and makes no request.
func (g GitHub) Configured() bool { return g.Forge != nil && g.Forge.HasCredentials() }

// CommitFiles builds the authenticated client (which may read the App key and call GitHub) and makes the commit.
func (g GitHub) CommitFiles(ctx context.Context, in APICommit) (string, error) {
	client, err := g.Forge.RequireAuthentication(ctx)
	if err != nil {
		return "", err
	}
	files := make([]github.CommitFile, len(in.Additions))
	for i, f := range in.Additions {
		files[i] = github.CommitFile{Path: f.Path, Content: f.Content}
	}
	return client.CreateCommitOnBranch(ctx, github.CommitInput{
		Branch: in.Branch, ExpectedHead: in.ExpectedHead, Message: in.Message, Additions: files, Deletions: in.Deletions,
	})
}
