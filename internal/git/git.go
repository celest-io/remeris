// Package git gives remeris access to a Git repository (spec F9).
//
// The Git interface grows milestone by milestone. The primary backend runs the git command (CLI), with go-git
// as the fallback (GoGit); Open picks the backend.
package git

import (
	"context"
	"log/slog"
	"unicode/utf8"
)

// Git is the repository access remeris needs. Methods are added milestone by milestone.
type Git interface {
	// AllTagsOnBranch returns the tags reachable from HEAD, newest first (spec F9.1).
	AllTagsOnBranch(ctx context.Context) ([]string, error)
	// CommitMessagesAfterTag returns the commits reachable from HEAD but not from tag, oldest first (spec F9.2).
	// A nil or missing tag selects every commit. Like Knope, commits whose message is not valid UTF-8 are skipped.
	CommitMessagesAfterTag(ctx context.Context, tag *string) ([]Commit, error)
	// FirstCommitsForFiles returns, for each path, the first commit (oldest first, topological order) whose tree
	// contains it (spec F9.3). Paths that are never found are absent from the map and no value is ever nil.
	// Errors are swallowed and give an empty map.
	//
	// Only files are found. Knope's tree.get_path(path).is_ok() is also true for a directory, but both backends
	// treat a directory as absent; callers only pass the paths of change files, so this never matters.
	// Like Knope, the first commit found for a path ends its search: when that commit's author name is not valid
	// UTF-8 (Knope's commit info is None), the path is absent from the map and later commits are not tried.
	FirstCommitsForFiles(ctx context.Context, paths []string) map[string]*CommitInfo
	// AddFiles stages paths, which are slash-separated and relative to the root of the working tree (spec F9.4).
	// It is Knope's index.add_all with the default flags: a modified or untracked file is added, a tracked file
	// that was deleted is staged as a deletion, a directory adds the files under it, and nothing else is staged.
	// Like Knope, paths that match nothing, and untracked files that are ignored, are skipped without an error.
	// Paths are literal names, not patterns. An empty list does nothing, not even opening the repository.
	AddFiles(ctx context.Context, paths []string) error
	// CreateTag creates the tag name on HEAD (spec F9.5). Like Knope's repo.tag(name, HEAD, signature, "", false), it is
	// an annotated tag with an empty message, tagged by the committer from the Git configuration (user.name and
	// user.email only: GIT_COMMITTER_* is ignored), and it fails when the tag already exists.
	CreateTag(ctx context.Context, name string, opts TagOptions) error
	// HeadCommitSHA returns the full 40-character id of the commit HEAD points to (spec F9.6).
	HeadCommitSHA(ctx context.Context) (string, error)
	// CurrentBranch returns the short name of the branch HEAD points to, such as "main" or "feature/x" (spec F9.6,
	// Quirk Q3: Knope returns the full "refs/heads/..." name). A detached HEAD is Knope's "Not on the tip of a Git
	// branch." error, and an unborn branch fails with "reference 'refs/heads/main' not found", like libgit2's repo.head().
	CurrentBranch(ctx context.Context) (string, error)
	// FirstRemoteURL returns the configured URL of the first remote in git remote order (alphabetical by name, which is
	// also libgit2's order), with url.<base>.insteadOf applied like libgit2's remote lookup (with several url values the
	// first is used; libgit2 would take the last). It is false when there is no remote, or on any error, like Knope's
	// get_first_remote (spec F9.6).
	FirstRemoteURL(ctx context.Context) (string, bool)
}

// TagOptions are the options of CreateTag.
type TagOptions struct {
	// Sign asks for a signed tag. It is a Phase 1 hook for the signing extension (spec E2): both backends ignore it.
	Sign bool
}

// Commit is a commit with its full message, as returned by CommitMessagesAfterTag.
type Commit struct {
	// Message is the full raw message, including any trailing newline.
	Message string
	// Info is nil when the author name is not valid UTF-8, like Knope's commit info, which is then None.
	Info *CommitInfo
}

// CommitInfo identifies a commit and its author (spec F12.3).
type CommitInfo struct {
	// Hash is the first 7 hex characters of the commit id.
	Hash string
	// AuthorName is the name of the commit's author.
	AuthorName string
}

// shortHashLen is the length of CommitInfo.Hash.
const shortHashLen = 7

// commitInfoFor returns the info of a commit, or nil when the author name is not valid UTF-8.
func commitInfoFor(hash, author string) *CommitInfo {
	if !utf8.ValidString(author) {
		return nil
	}
	return &CommitInfo{Hash: hash[:min(shortHashLen, len(hash))], AuthorName: author}
}

// Open returns the Git backend for the repository at dir: the git command when it is on PATH, else the
// built-in go-git backend (spec F9.0).
func Open(ctx context.Context, dir string) (Git, error) {
	if cli, err := NewCLI(dir); err == nil {
		return cli, nil
	}
	slog.DebugContext(ctx, "git not found on PATH, using built-in Git support")
	return NewGoGit(dir)
}
