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

// FirstRemoteURL returns the fetch URL of the first remote, like the CLI backend: only remotes with a url or pushurl
// key count, sorted by name; insteadOf is applied, and with several url values the first is used.
func (g *GoGit) FirstRemoteURL(ctx context.Context) (string, bool) {
	if ctx.Err() != nil {
		return "", false
	}
	repo, err := g.open()
	if err != nil {
		return "", false
	}
	cfg, err := repo.ConfigScoped(config.SystemScope)
	if err != nil {
		return "", false
	}
	var names []string
	for _, sub := range cfg.Raw.Section("remote").Subsections {
		if sub.HasOption("url") || sub.HasOption("pushurl") {
			names = append(names, sub.Name)
		}
	}
	if len(names) == 0 {
		return "", false
	}
	slices.Sort(names)
	if !cfg.Raw.Section("remote").Subsection(names[0]).HasOption("url") {
		return "", false
	}
	remote, ok := cfg.Remotes[names[0]]
	if !ok || len(remote.URLs) == 0 || remote.URLs[0] == "" {
		return "", false
	}
	return remote.URLs[0], true // the insteadOf rules are already applied
}
