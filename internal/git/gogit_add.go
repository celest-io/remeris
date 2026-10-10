package git

import (
	"context"
	"errors"
	"path"
	"slices"
	"strings"

	gogit "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing/format/index"
)

// AddFiles stages paths (spec F9.4) like the CLI backend: modified and untracked files are added, deleted tracked
// files are removed from the index, and ignored untracked files and paths that match nothing are skipped.
//
// go-git's status already leaves out the ignored untracked files, so the staged entries are the status entries
// found under the paths. Worktree.Add is called per file; the deleted ones are removed from the index first.
func (g *GoGit) AddFiles(ctx context.Context, paths []string) error {
	if len(paths) == 0 {
		return nil
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	repo, err := g.open()
	if err != nil {
		return err
	}
	wt, err := repo.Worktree()
	if err != nil {
		return openRepoError(err)
	}
	status, err := wt.Status()
	if err != nil {
		return unknownGitError(err)
	}
	roots := make([]string, len(paths))
	for i, p := range paths {
		roots[i] = path.Clean(p)
	}
	names := make([]string, 0, len(status))
	for name := range status {
		names = append(names, name)
	}
	slices.Sort(names) // a stable order for the index writes
	var deleted, changed []string
	for _, name := range names {
		if status[name].Worktree == gogit.Unmodified || !underAny(name, roots) {
			continue
		}
		if status[name].Worktree == gogit.Deleted {
			deleted = append(deleted, name)
		} else {
			changed = append(changed, name)
		}
	}
	// Deleted entries go first, straight out of the index: when a path swaps between file and directory
	// (b.txt gone, b.txt/inner.txt new), the stale entry must not be in the index when its replacement is added,
	// and Worktree.Remove would try to delete the parents of a file that is already gone.
	if err := removeFromIndex(repo, deleted); err != nil {
		return err
	}
	for _, name := range changed {
		if err := ctx.Err(); err != nil {
			return err
		}
		if _, err := wt.Add(name); err != nil {
			return unknownGitError(err)
		}
	}
	return nil
}

// removeFromIndex drops the entries for names in one index write. Names that are not in the index are ignored.
func removeFromIndex(repo *gogit.Repository, names []string) error {
	if len(names) == 0 {
		return nil
	}
	idx, err := repo.Storer.Index()
	if err != nil {
		return unknownGitError(err)
	}
	for _, name := range names {
		if _, err := idx.Remove(name); err != nil && !errors.Is(err, index.ErrEntryNotFound) {
			return unknownGitError(err)
		}
	}
	if err := repo.Storer.SetIndex(idx); err != nil {
		return unknownGitError(err)
	}
	return nil
}

// underAny reports whether name is one of the roots or below one of them. The root "." is the whole tree.
func underAny(name string, roots []string) bool {
	for _, r := range roots {
		if r == "." || name == r || strings.HasPrefix(name, r+"/") {
			return true
		}
	}
	return false
}
