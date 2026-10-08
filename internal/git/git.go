// Package git gives remeris access to a Git repository (spec F9).
//
// The Git interface grows milestone by milestone. The primary backend runs the git command (CLI);
// Open picks the backend.
package git

import (
	"context"

	"github.com/celest-io/remeris/internal/diag"
)

// Git is the repository access remeris needs. Methods are added milestone by milestone.
type Git interface {
	// AllTagsOnBranch returns the tags reachable from HEAD, newest first (spec F9.1).
	AllTagsOnBranch(ctx context.Context) ([]string, error)
}

// Open returns the Git backend for the repository at dir: the git command when it is on PATH.
func Open(_ context.Context, dir string) (Git, error) {
	cli, err := NewCLI(dir)
	if err != nil {
		return nil, diag.New("Could not find the git command").
			WithCode("remeris::git::no_backend").
			WithHelp("Install git and make sure it is on PATH.").
			WithCause(err)
	}
	return cli, nil
}
