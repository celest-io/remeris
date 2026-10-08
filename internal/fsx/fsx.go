// Package fsx wraps the file system writes remeris makes so that a dry run never touches disk
// (spec 0.1, RunType). Milestone M0 needs no writes yet; the helpers here are the seam later
// milestones build on.
package fsx

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
)

// WriteFile writes data to path, creating parent directories. In a dry run it only logs what
// would happen.
func WriteFile(dryRun bool, path string, data []byte) error {
	if dryRun {
		slog.Info(fmt.Sprintf("Would write to %s", path))
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644) //nolint:gosec // files in a repository are meant to be readable
}

// Remove deletes the file at path. In a dry run it only logs what would happen.
func Remove(dryRun bool, path string) error {
	if dryRun {
		slog.Info(fmt.Sprintf("Would delete %s", path))
		return nil
	}
	return os.Remove(path)
}
