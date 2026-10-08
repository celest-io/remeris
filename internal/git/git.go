// Package git gives remeris access to a Git repository (spec F9).
//
// The Git interface grows milestone by milestone. The primary backend runs the git command (CLI), with go-git
// as the fallback (GoGit); Open picks the backend.
package git

import (
	"context"
	"log/slog"
)

// Git is the repository access remeris needs. Methods are added milestone by milestone.
type Git interface {
	// AllTagsOnBranch returns the tags reachable from HEAD, newest first (spec F9.1).
	AllTagsOnBranch(ctx context.Context) ([]string, error)
	// CommitMessagesAfterTag returns the commits reachable from HEAD but not from tag, oldest first (spec F9.2).
	// A nil or missing tag selects every commit.
	CommitMessagesAfterTag(ctx context.Context, tag *string) ([]Commit, error)
	// FirstCommitsForFiles returns, for each path, the first commit (oldest first, topological order) whose tree
	// contains it (spec F9.3). Paths that are never found are absent from the map and no value is ever nil.
	// Errors are swallowed and give an empty map.
	FirstCommitsForFiles(ctx context.Context, paths []string) map[string]*CommitInfo
}

// Commit is a commit with its full message, as returned by CommitMessagesAfterTag.
type Commit struct {
	// Message is the full raw message, including any trailing newline.
	Message string
	Info    CommitInfo
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

func newCommitInfo(hash, author string) CommitInfo {
	return CommitInfo{Hash: hash[:min(shortHashLen, len(hash))], AuthorName: author}
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
