package steps

import (
	"context"
	"errors"
	"io/fs"
	"log/slog"
	"path/filepath"

	"github.com/celest-io/remeris/internal/diag"
	"github.com/celest-io/remeris/internal/fsx"
	"github.com/celest-io/remeris/internal/git"
	"github.com/celest-io/remeris/pkg/versioning/action"
)

// codeFsWrite is the code of a failed write.
const codeFsWrite = "fs::write"

// ExecuteActions runs the file actions in order (spec F13.4) and returns the others, AddTag and CreateRelease,
// in order. The caller decides what to do with them: PrepareRelease (M5) keeps them as State.PendingActions for a
// later Release step; BumpVersion drops them.
//
// In a dry run nothing is changed: a write logs "Would add the following to {path}: {diff}" and a removal logs
// "Would delete {path}". A real write stores the content as given. A failed removal is ignored, because the same
// change file can be removed once per package. Staging the touched files in Git is not done here.
func ExecuteActions(dryRun bool, actions []action.Action) ([]action.Action, error) {
	remainder, _, err := executeActions(dryRun, actions)
	return remainder, err
}

// ExecuteAndStage is ExecuteActions followed by staging the path of every write and removal, in order (spec
// F13.4). A dry run logs "Would add files to git:" and one "  {path}" line per path, when there is any; a real
// run calls g.AddFiles. g may be nil only when nothing is to be staged.
func ExecuteAndStage(ctx context.Context, g git.Git, dryRun bool, actions []action.Action) ([]action.Action, error) {
	remainder, paths, err := executeActions(dryRun, actions)
	if err != nil || len(paths) == 0 {
		return remainder, err
	}
	if dryRun {
		slog.InfoContext(ctx, "Would add files to git:")
		for _, p := range paths {
			slog.InfoContext(ctx, "  "+p)
		}
		return remainder, nil
	}
	if g == nil {
		return nil, errNoGit
	}
	return remainder, g.AddFiles(ctx, paths)
}

// errNoGit is returned when files must be staged and the repository could not be opened.
var errNoGit = diag.New("Could not open Git repository: not a Git repository").
	WithCode("remeris::git::open_repo").
	WithHelp("Make sure you are in a Git repository and that you have permission to access it.")

func executeActions(dryRun bool, actions []action.Action) (remainder []action.Action, paths []string, err error) {
	for _, a := range actions {
		switch a.Kind {
		case action.KindWriteToFile:
			if err := writeFile(dryRun, a); err != nil {
				return nil, nil, err
			}
			paths = append(paths, a.Path)
		case action.KindRemoveFile:
			paths = append(paths, a.Path)
			if dryRun {
				slog.Info("Would delete " + a.Path)
				continue
			}
			// The error is ignored on purpose, like Knope (step/releases/package.rs: "Ignore errors since we remove
			// changesets per-package") and spec F13.3: another package may have removed the file already.
			_ = fsx.Remove(false, filepath.FromSlash(a.Path))
		case action.KindAddTag, action.KindCreateRelease:
			remainder = append(remainder, a)
		}
	}
	return remainder, paths, nil
}

func writeFile(dryRun bool, a action.Action) error {
	if dryRun {
		slog.Info("Would add the following to " + a.Path + ": " + a.Diff)
		return nil
	}
	if err := fsx.WriteFile(false, filepath.FromSlash(a.Path), []byte(a.Content)); err != nil {
		cause := err
		var pe *fs.PathError
		if errors.As(err, &pe) {
			cause = pe.Err
		}
		return diag.New("Error writing to " + a.Path + ": " + cause.Error()).
			WithCode(codeFsWrite).
			WithHelp("Make sure you have permission to write to this file.").
			WithCause(err)
	}
	return nil
}
