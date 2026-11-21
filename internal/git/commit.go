package git

import (
	"context"
	"strings"
)

// Change is one path that differs from HEAD in the index or the working tree (an ignored file never is).
type Change struct {
	// Path is slash-separated and relative to the root of the working tree.
	Path string
	// Deleted is true when the path is gone from the working tree (or is staged as a deletion, or is the old name of a
	// rename). It is a tracked file that is no longer there.
	Deleted bool
}

// SigningConfig is the commit signing setup of the Git configuration (system, global, then local).
type SigningConfig struct {
	// GPGSign is commit.gpgsign: every commit is signed.
	GPGSign bool
	// Key is user.signingkey: a key id, or for gpg.format=ssh a public key or the path of one.
	Key string
	// Format is gpg.format: "openpgp" (the default, reported as ""), "ssh" or "x509".
	Format string
	// Program is the program of the format: gpg.program, gpg.ssh.program or gpg.x509.program, for the format in use.
	Program string
}

// Configured reports whether Git is set up to sign commits: it is asked to sign every commit, or has a signing key.
// It does not check that the key is usable; a failing signature is reported by the commit itself.
func (c SigningConfig) Configured() bool { return c.GPGSign || strings.TrimSpace(c.Key) != "" }

// CommitOptions are the options of Committer.Commit.
type CommitOptions struct {
	// Sign signs the commit (git commit -S). When false the commit follows the user's own Git configuration, like
	// a plain "git commit" does.
	Sign bool
	// Paths are the files to commit: slash-separated and relative to the root of the working tree, as in Change.Path.
	// Other staged changes are left out of the commit and stay staged. An empty list commits nothing and is an error.
	Paths []string
}

// Committer is the part of a Git backend that makes commits. It is separate from Git so that the many fakes of Git
// need not implement it; Open returns a backend that implements both.
type Committer interface {
	// SigningConfig reads the signing setup of the Git configuration.
	SigningConfig(ctx context.Context) (SigningConfig, error)
	// Changes lists what differs from HEAD, sorted by path: modified and untracked files (ignored ones are left out) and
	// deleted tracked files. A rename is a new path and a deleted old one.
	Changes(ctx context.Context) ([]Change, error)
	// Root returns the absolute path of the root of the working tree. Paths of Change and CommitOptions are relative to it.
	Root(ctx context.Context) (string, error)
	// AdoptCommit takes a commit that was made elsewhere (through a forge API) on the remote branch: it fetches the
	// branch from the remote it tracks (origin when it tracks none) and moves HEAD and the branch to sha without
	// touching the index or the working tree (git reset --soft). Only the git command can do it.
	AdoptCommit(ctx context.Context, branch, sha string) error
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
