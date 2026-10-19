package git

import (
	"context"
	"slices"
	"strings"

	"github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing"
)

// CurrentBranch returns the short name of the branch HEAD points to, with the same results and errors as the CLI backend.
func (g *GoGit) CurrentBranch(ctx context.Context) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	repo, err := g.open()
	if err != nil {
		return "", err
	}
	head, err := repo.Reference(plumbing.HEAD, false)
	if err != nil {
		return "", unknownGitError(err)
	}
	if head.Type() != plumbing.SymbolicReference || !head.Target().IsBranch() {
		return "", notOnBranchError()
	}
	target := head.Target()
	if _, err := repo.Storer.Reference(target); err != nil {
		return "", unknownGitError(refNotFoundError(target.String()))
	}
	return strings.TrimPrefix(target.String(), branchRefPrefix), nil
}

// FirstRemoteURL returns the fetch URL of the first remote by name, like the CLI backend: insteadOf is applied, and with
// several url values the first is used.
func (g *GoGit) FirstRemoteURL(ctx context.Context) (string, bool) {
	if ctx.Err() != nil {
		return "", false
	}
	repo, err := g.open()
	if err != nil {
		return "", false
	}
	cfg, err := repo.ConfigScoped(config.SystemScope)
	if err != nil || len(cfg.Remotes) == 0 {
		return "", false
	}
	names := make([]string, 0, len(cfg.Remotes))
	for name := range cfg.Remotes {
		names = append(names, name)
	}
	slices.Sort(names)
	urls := cfg.Remotes[names[0]].URLs // the insteadOf rules are already applied
	if len(urls) == 0 || urls[0] == "" {
		return "", false
	}
	return urls[0], true // the first value, like git remote get-url
}
