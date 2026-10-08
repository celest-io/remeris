package git

import (
	"context"
	"log/slog"
	"slices"

	gogit "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/filemode"
	"github.com/go-git/go-git/v5/plumbing/object"
)

// CommitMessagesAfterTag returns the commits reachable from HEAD but not from the tag, oldest first, with the
// same result as the CLI backend.
func (g *GoGit) CommitMessagesAfterTag(ctx context.Context, tag *string) ([]Commit, error) {
	repo, err := g.open()
	if err != nil {
		return nil, err
	}
	head, err := repo.Head()
	if err != nil {
		return nil, unknownGitError(err)
	}
	exclude := map[plumbing.Hash]bool{}
	if tag == nil {
		slog.DebugContext(ctx, "Using ALL commits")
	} else if tagCommit, ok := resolveTagCommit(repo, *tag); ok {
		slog.DebugContext(ctx, "Using commits since tag "+*tag)
		err = walkFromHead(ctx, repo, tagCommit, func(c *object.Commit) { exclude[c.Hash] = true })
		if err != nil {
			return nil, err
		}
	} else {
		slog.DebugContext(ctx, "Tag "+*tag+" not found, using ALL commits")
	}
	var commits []Commit
	err = walkFromHead(ctx, repo, head.Hash(), func(c *object.Commit) {
		if !exclude[c.Hash] {
			commits = append(commits, Commit{Message: c.Message, Info: newCommitInfo(c.Hash.String(), c.Author.Name)})
		}
	})
	if err != nil {
		return nil, err
	}
	slices.Reverse(commits)
	return commits, nil
}

// resolveTagCommit returns the commit that refs/tags/<tag> peels to.
func resolveTagCommit(repo *gogit.Repository, tag string) (plumbing.Hash, bool) {
	ref, err := repo.Reference(plumbing.NewTagReferenceName(tag), true)
	if err != nil {
		return plumbing.ZeroHash, false
	}
	commit, ok, err := peelToCommit(repo, ref.Hash())
	return commit, ok && err == nil
}

// topoReverse returns the commits reachable from start, parents before children. It follows git's
// --topo-order: a commit is emitted once all its children were, and the last parent is followed first.
func topoReverse(ctx context.Context, repo *gogit.Repository, start plumbing.Hash) ([]*object.Commit, error) {
	byHash := map[plumbing.Hash]*object.Commit{}
	var dated []*object.Commit
	err := walkFromHead(ctx, repo, start, func(c *object.Commit) {
		byHash[c.Hash] = c
		dated = append(dated, c)
	})
	if err != nil {
		return nil, err
	}
	children := make(map[plumbing.Hash]int, len(dated))
	for _, c := range dated {
		for _, p := range c.ParentHashes {
			children[p]++
		}
	}
	stack := []*object.Commit{byHash[start]}
	order := make([]*object.Commit, 0, len(dated))
	for len(stack) > 0 {
		c := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		order = append(order, c)
		for _, p := range c.ParentHashes {
			if children[p]--; children[p] == 0 {
				stack = append(stack, byHash[p])
			}
		}
	}
	slices.Reverse(order)
	return order, nil
}

// FirstCommitsForFiles finds, for each path, the first commit in reverse topological order whose tree contains
// it, with the same result as the CLI backend. A directory is never found. Errors are swallowed.
func (g *GoGit) FirstCommitsForFiles(ctx context.Context, paths []string) map[string]*CommitInfo {
	results := make(map[string]*CommitInfo, len(paths))
	repo, err := g.open()
	if err != nil {
		return map[string]*CommitInfo{}
	}
	head, err := repo.Head()
	if err != nil {
		return map[string]*CommitInfo{}
	}
	order, err := topoReverse(ctx, repo, head.Hash())
	if err != nil {
		return map[string]*CommitInfo{}
	}
	pending := slices.Clone(paths)
	for _, c := range order {
		if len(pending) == 0 {
			break
		}
		tree, err := c.Tree()
		if err != nil {
			continue
		}
		pending = slices.DeleteFunc(pending, func(path string) bool {
			entry, err := tree.FindEntry(path)
			if err != nil || entry.Mode == filemode.Dir {
				return false
			}
			info := newCommitInfo(c.Hash.String(), c.Author.Name)
			results[path] = &info
			return true
		})
	}
	return results
}
