package git

import "context"

// Change is one path that differs from HEAD in the index or the working tree (an ignored file never is).
type Change struct {
	// Path is slash-separated and relative to the root of the working tree.
	Path string
	// Deleted is true when the path is gone from the working tree (or is staged as a deletion, or is the old name of a
	// rename). It is a tracked file that is no longer there.
	Deleted bool
}

// CommitOptions are the options of Committer.Commit.
type CommitOptions struct {
	// Paths are the files to commit: slash-separated and relative to the root of the working tree, as in Change.Path.
	// Other staged changes are left out of the commit and stay staged. An empty list commits nothing and is an error.
	Paths []string
}

// Committer is the part of a Git backend that makes commits. It is separate from Git so that the many fakes of Git
// need not implement it; Open returns a backend that implements both.
type Committer interface {
	// Changes lists what differs from HEAD, sorted by path: modified and untracked files (ignored ones are left out) and
	// deleted tracked files. A rename is a new path and a deleted old one.
	Changes(ctx context.Context) ([]Change, error)
	// Commit commits exactly opts.Paths with message on the current branch and returns the full id of the new commit.
	// Files that are modified or untracked are staged first and deleted ones are staged as deletions. Other staged
	// changes are not part of the commit. It fails when nothing in Paths differs from HEAD.
	Commit(ctx context.Context, message string, opts CommitOptions) (string, error)
}

// AsCommitter returns the commit support of g, which every backend of Open has. Fakes that only implement Git have none.
func AsCommitter(g Git) (Committer, bool) {
	c, ok := g.(Committer)
	return c, ok
}
