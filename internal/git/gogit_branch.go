package git

import (
	"context"
	"errors"
	"slices"
	"strings"

	gogit "github.com/go-git/go-git/v5"
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

// LocalBranches returns the short names of the local branches, sorted by name, like the CLI backend.
func (g *GoGit) LocalBranches(ctx context.Context) ([]string, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	repo, err := g.open()
	if err != nil {
		return nil, err
	}
	iter, err := repo.References()
	if err != nil {
		return nil, unknownGitError(err)
	}
	branches := []string{}
	err = iter.ForEach(func(ref *plumbing.Reference) error {
		if ref.Name().IsBranch() {
			branches = append(branches, ref.Name().Short())
		}
		return nil
	})
	if err != nil {
		return nil, unknownGitError(err)
	}
	slices.Sort(branches)
	return branches, nil
}

// BranchExists reports whether the local branch name exists. An invalid name is an error.
func (g *GoGit) BranchExists(ctx context.Context, name string) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	if err := checkBranchName(name); err != nil {
		return false, err
	}
	repo, err := g.open()
	if err != nil {
		return false, err
	}
	_, err = repo.Storer.Reference(plumbing.NewBranchReferenceName(name))
	if errors.Is(err, plumbing.ErrReferenceNotFound) {
		return false, nil
	}
	if err != nil {
		return false, unknownGitError(err)
	}
	return true, nil
}

// CreateBranch creates the local branch name at the tip of the local branch from, without switching to it.
func (g *GoGit) CreateBranch(ctx context.Context, name, from string) error {
	if err := checkCreateBranch(ctx, g, name, from); err != nil {
		return err
	}
	repo, err := g.open()
	if err != nil {
		return err
	}
	base, err := repo.Reference(plumbing.NewBranchReferenceName(from), true)
	if err != nil {
		return unknownGitError(err)
	}
	ref := plumbing.NewHashReference(plumbing.NewBranchReferenceName(name), base.Hash())
	// Look again just before the write: go-git has no ref lock, so a branch created in between by another process is
	// still possible (see testdata/deviations.md), but the window is small.
	if _, err := repo.Storer.Reference(ref.Name()); err == nil {
		return branchExistsError(name)
	}
	if err := repo.Storer.CheckAndSetReference(ref, nil); err != nil {
		return unknownGitError(err)
	}
	return nil
}

// HasUncommittedChanges reports whether the working tree or the index differ from HEAD. Untracked files count and
// ignored files do not.
func (g *GoGit) HasUncommittedChanges(ctx context.Context) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	repo, err := g.open()
	if err != nil {
		return false, err
	}
	wt, err := repo.Worktree()
	if err != nil {
		return false, openRepoError(err)
	}
	// Files ignored by info/exclude and the global ignore file do not count, like git. Known limits of go-git's status
	// (core.fileMode=false, skip-worktree, submodules) are listed in testdata/deviations.md.
	wt.Excludes = append(wt.Excludes, excludePatterns(repo)...)
	status, err := wt.Status()
	if err != nil {
		return false, unknownGitError(err)
	}
	return !status.IsClean(), nil
}

// SwitchTo checks out the local branch name, like the CLI backend. A failure after HEAD moved is the "Could not
// complete checkout" error; a failure before that is an unknown Git error.
func (g *GoGit) SwitchTo(ctx context.Context, name string) error {
	if err := checkSwitch(ctx, g, name); err != nil {
		return err
	}
	repo, err := g.open()
	if err != nil {
		return err
	}
	if err := checkedOutElsewhere(repo, name); err != nil {
		return err
	}
	wt, err := repo.Worktree()
	if err != nil {
		return openRepoError(err)
	}
	before, _ := repo.Reference(plumbing.HEAD, false)
	err = wt.Checkout(&gogit.CheckoutOptions{Branch: plumbing.NewBranchReferenceName(name)})
	if err == nil {
		return nil
	}
	after, aerr := repo.Reference(plumbing.HEAD, false)
	if aerr == nil && (before == nil || after.Target() != before.Target()) {
		return incompleteCheckoutError(err)
	}
	return unknownGitError(err)
}
