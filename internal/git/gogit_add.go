package git

import (
	"context"
	"path"
	"slices"
	"strings"

	gogit "github.com/go-git/go-git/v5"
)

// AddFiles stages paths (spec F9.4) like the CLI backend: modified and untracked files are added, deleted tracked
// files are removed from the index, and ignored untracked files and paths that match nothing are skipped.
//
// go-git's status already leaves out the ignored untracked files, so the staged entries are the status entries
// found under the paths. Worktree.Add is called per file and Worktree.Remove for the deleted ones (the file is
// already gone, so only the index changes).
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
	for _, name := range names {
		if err := ctx.Err(); err != nil {
			return err
		}
		if status[name].Worktree == gogit.Unmodified || !underAny(name, roots) {
			continue
		}
		if status[name].Worktree == gogit.Deleted {
			_, err = wt.Remove(name)
		} else {
			_, err = wt.Add(name)
		}
		if err != nil {
			return unknownGitError(err)
		}
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
