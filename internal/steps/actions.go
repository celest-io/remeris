package steps

import (
	"errors"
	"io/fs"
	"log/slog"
	"path/filepath"

	"github.com/celest-io/remeris/internal/diag"
	"github.com/celest-io/remeris/internal/fsx"
	"github.com/celest-io/remeris/pkg/versioning/action"
)

// codeFsWrite is the code of a failed write.
const codeFsWrite = "fs::write"

// ExecuteActions runs the file actions in order (spec F13.4) and returns the others, AddTag and CreateRelease,
// in order, for the caller to keep as pending actions.
//
// In a dry run nothing is changed: a write logs "Would add the following to {path}: {diff}" and a removal logs
// "Would delete {path}". A real write stores the content as given. A failed removal is ignored, because the same
// change file can be removed once per package. Staging the touched files in Git is not done here.
func ExecuteActions(dryRun bool, actions []action.Action) ([]action.Action, error) {
	var remainder []action.Action
	for _, a := range actions {
		switch a.Kind {
		case action.KindWriteToFile:
			if err := writeFile(dryRun, a); err != nil {
				return nil, err
			}
		case action.KindRemoveFile:
			if dryRun {
				slog.Info("Would delete " + a.Path)
				continue
			}
			_ = fsx.Remove(false, filepath.FromSlash(a.Path)) // ignored on purpose: another package may have removed the file
		case action.KindAddTag, action.KindCreateRelease:
			remainder = append(remainder, a)
		}
	}
	return remainder, nil
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
