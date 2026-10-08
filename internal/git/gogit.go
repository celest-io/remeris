package git

import (
	"container/heap"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"time"

	gogit "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
)

// GoGit is the built-in Git backend, used when the git command is not installed (spec F9.0).
type GoGit struct {
	// Dir is the directory the repository is discovered from, like git -C.
	Dir string
}

var _ Git = (*GoGit)(nil)

// NewGoGit returns a go-git backend for dir. The repository is opened on each call, so it is not checked here.
func NewGoGit(dir string) (*GoGit, error) {
	return &GoGit{Dir: dir}, nil
}

// open finds the repository from Dir or one of its parents. Like libgit2 and the CLI backend, it ignores GIT_DIR
// and the other location variables: go-git does not read them.
//
// Linked worktrees (git worktree add) are supported through EnableDotGitCommonDir. Bare repositories are not
// supported: releases run in a working tree.
func (g *GoGit) open() (*gogit.Repository, error) {
	repo, err := gogit.PlainOpenWithOptions(g.Dir, &gogit.PlainOpenOptions{
		DetectDotGit:          true,
		EnableDotGitCommonDir: true,
	})
	if err != nil {
		return nil, openRepoError(err)
	}
	return repo, nil
}

// AllTagsOnBranch lists the tags reachable from HEAD, newest first, with the same result as the CLI backend.
//
// go-git does not read refs/replace, so replaced objects are ignored as they are with the CLI. The walk is
// implemented here so that equal commit times keep git's order.
//
// A cancelled context returns the context error itself. In a shallow clone the walk stops at the shallow
// commits, as git does.
func (g *GoGit) AllTagsOnBranch(ctx context.Context) ([]string, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	repo, err := g.open()
	if err != nil {
		return nil, err
	}
	byCommit, err := tagsByCommit(ctx, repo)
	if err != nil {
		return nil, err
	}
	head, err := repo.Head()
	if err != nil {
		return nil, unknownGitError(err) // an unborn HEAD, as in Knope's revwalk
	}
	tags := make([]string, 0, len(byCommit))
	err = walkFromHead(ctx, repo, head.Hash(), func(c *object.Commit) {
		if t, ok := byCommit[c.Hash]; ok {
			tags = append(tags, t...)
			delete(byCommit, c.Hash)
		}
	})
	if err != nil {
		return nil, err
	}
	if len(byCommit) > 0 {
		var skipped []string
		for _, ts := range byCommit {
			skipped = append(skipped, ts...)
		}
		slices.Sort(skipped)
		slog.DebugContext(ctx, "Skipping relevant tags that are not on the current branch: "+strings.Join(skipped, ", "))
	}
	return tags, nil
}

// tagsByCommit maps each commit to the tags that peel to it, in reference order (sorted by name).
func tagsByCommit(ctx context.Context, repo *gogit.Repository) (map[plumbing.Hash][]string, error) {
	iter, err := repo.References()
	if err != nil {
		return nil, unknownGitError(err)
	}
	var refs []*plumbing.Reference
	err = iter.ForEach(func(ref *plumbing.Reference) error {
		if ref.Name().IsTag() && ref.Type() == plumbing.HashReference {
			refs = append(refs, ref)
		}
		return nil
	})
	if err != nil {
		return nil, unknownGitError(err)
	}
	slices.SortFunc(refs, func(a, b *plumbing.Reference) int { return strings.Compare(string(a.Name()), string(b.Name())) })

	byCommit := make(map[plumbing.Hash][]string, len(refs))
	for _, ref := range refs {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		commit, ok, err := peelToCommit(repo, ref.Hash())
		if err != nil {
			return nil, unknownGitError(err)
		}
		if ok {
			byCommit[commit] = append(byCommit[commit], ref.Name().Short())
		}
	}
	return byCommit, nil
}

// peelToCommit follows tag objects until it finds a commit. It reports false for trees, blobs and missing objects.
func peelToCommit(repo *gogit.Repository, h plumbing.Hash) (plumbing.Hash, bool, error) {
	for {
		obj, err := repo.Storer.EncodedObject(plumbing.AnyObject, h)
		if errors.Is(err, plumbing.ErrObjectNotFound) {
			return plumbing.ZeroHash, false, nil
		}
		if err != nil {
			return plumbing.ZeroHash, false, err
		}
		switch obj.Type() {
		case plumbing.CommitObject:
			return h, true, nil
		case plumbing.TagObject:
			tag, err := object.DecodeTag(repo.Storer, obj)
			if err != nil {
				return plumbing.ZeroHash, false, err
			}
			h = tag.Target
		default:
			return plumbing.ZeroHash, false, nil
		}
	}
}

// queued is a commit waiting in the walk's frontier.
type queued struct {
	commit *object.Commit
	when   time.Time
	seq    int
}

// frontier orders commits by committer time, newest first, and first in first out among equal times.
type frontier []queued

func (f frontier) Len() int { return len(f) }
func (f frontier) Less(i, j int) bool {
	if !f[i].when.Equal(f[j].when) {
		return f[i].when.After(f[j].when)
	}
	return f[i].seq < f[j].seq
}
func (f frontier) Swap(i, j int) { f[i], f[j] = f[j], f[i] }
func (f *frontier) Push(x any)   { *f = append(*f, x.(queued)) } //nolint:forcetypeassert // heap.Interface
func (f *frontier) Pop() any {
	old := *f
	last := old[len(old)-1]
	*f = old[:len(old)-1]
	return last
}

// walkFromHead calls visit for every commit reachable from start, in the order of git rev-list and libgit2's
// default revwalk: the newest commit of the frontier first, parents in parent order. The parents of the shallow
// commits are not followed: a shallow clone does not have them.
func walkFromHead(ctx context.Context, repo *gogit.Repository, start plumbing.Hash, visit func(*object.Commit)) error {
	shallow, err := repo.Storer.Shallow()
	if err != nil {
		return unknownGitError(err)
	}
	seen := map[plumbing.Hash]bool{start: true}
	var queue frontier
	seq := 0
	push := func(h plumbing.Hash) error {
		c, err := repo.CommitObject(h)
		if err != nil {
			return unknownGitError(fmt.Errorf("reading commit %s: %w", h, err))
		}
		heap.Push(&queue, queued{commit: c, when: c.Committer.When, seq: seq})
		seq++
		return nil
	}
	if err := push(start); err != nil {
		return err
	}
	for queue.Len() > 0 {
		if err := ctx.Err(); err != nil {
			return err
		}
		c := heap.Pop(&queue).(queued).commit //nolint:forcetypeassert // only queued values are pushed
		visit(c)
		if slices.Contains(shallow, c.Hash) {
			continue
		}
		for _, p := range c.ParentHashes {
			if seen[p] {
				continue
			}
			seen[p] = true
			if err := push(p); err != nil {
				return err
			}
		}
	}
	return nil
}
