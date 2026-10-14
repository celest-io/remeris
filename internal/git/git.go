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
