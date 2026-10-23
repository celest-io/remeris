package fsx

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

var (
	// ErrSymlink is returned by ReplaceFile when the file is a symbolic link: the link is never followed or replaced.
	ErrSymlink = errors.New("the file is a symbolic link")
	// ErrNotRegular is returned by ReplaceFile when the path exists and is not a regular file.
	ErrNotRegular = errors.New("the path is not a regular file")
	// ErrEmpty is returned by ReplaceFile when it is asked to replace a file that has content with nothing.
	ErrEmpty = errors.New("refusing to replace a file that has content with an empty one")
)

// rename is replaced by the tests to make the last step fail.
var rename = os.Rename

// ReplaceFile replaces the content of the existing file at path with data, atomically: the data is written to a
// temporary file in the same directory (mode of the original file), synced, and renamed over path, then the
// directory is synced (best effort). On any failure the temporary file is removed and path is left as it was.
// It refuses a symbolic link (ErrSymlink), anything that is not a regular file (ErrNotRegular) and an empty
// replacement for a file that has content (ErrEmpty).
func ReplaceFile(path string, data []byte) (err error) {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	switch {
	case info.Mode()&fs.ModeSymlink != 0:
		return ErrSymlink
	case !info.Mode().IsRegular():
		return ErrNotRegular
	case len(data) == 0 && info.Size() > 0:
		return ErrEmpty
	}

	dir := filepath.Dir(path)
	// CreateTemp creates the file with mode 0600; the original mode is applied before anything is renamed.
	tmp, err := os.CreateTemp(dir, "."+filepath.Base(path)+".*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer func() {
		if err != nil {
			_ = tmp.Close() // already closed on the late failures; the error is of no interest
			_ = os.Remove(tmpName)
		}
	}()
	if err = tmp.Chmod(info.Mode().Perm()); err != nil {
		return err
	}
	if _, err = tmp.Write(data); err != nil {
		return err
	}
	if err = tmp.Sync(); err != nil {
		return err
	}
	if err = tmp.Close(); err != nil {
		return err
	}
	if err = rename(tmpName, path); err != nil {
		return fmt.Errorf("replace %s: %w", path, err)
	}
	syncDir(dir)
	return nil
}

// syncDir flushes the directory entry of a rename. Some platforms cannot open or sync a directory, so a failure
// is ignored: the file itself is already in place.
func syncDir(dir string) {
	d, err := os.Open(dir) //nolint:gosec // the directory of the file being replaced
	if err != nil {
		return
	}
	_ = d.Sync()
	_ = d.Close()
}
