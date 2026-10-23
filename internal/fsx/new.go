package fsx

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
)

// link makes a hard link; tests replace it to simulate a file system without them.
var link = os.Link

// fill writes data to a new file and closes it; tests replace it to simulate a failing disk.
var fill = writeSync

// tempAttempts bounds the search for an unused temporary name.
const tempAttempts = 16

// WriteNew creates the file at path with data and never replaces anything. It fails with an error that
// satisfies errors.Is(err, fs.ErrExist) when path exists, whatever it is: a file, a directory, a symbolic link
// (even a dangling one, which is not followed).
//
// The file appears complete or not at all. The data is written and synced to a temporary file in the same
// directory, which is then hard linked to path: link(2) fails if path exists and never follows or replaces a
// symbolic link, so two writers cannot both succeed and nobody sees a partial file. On a file system without hard
// links the file is created exclusively (O_EXCL) and written in place; a failure then removes the partial file.
//
// A process killed between creating the temporary file and removing it leaves `.<name>.<random>.tmp` behind (for
// knope.toml, `.knope.toml.*.tmp`, which shows as untracked in `git status`). On the fallback path a killed process
// can leave a partial file at path.
//
// perm is the mode before the umask is applied, as for os.WriteFile (use 0o666 for a regular file).
func WriteNew(path string, data []byte, perm fs.FileMode) error {
	dir, base := filepath.Split(path)
	tmp, f, err := createTemp(dir, base, perm)
	if err != nil {
		// The directory is missing or not writable: the plain exclusive create gives the right error, and
		// reports an existing path as such.
		return writeExclusive(path, data, perm)
	}
	defer func() { _ = os.Remove(tmp) }()
	if err := fill(f, data); err != nil {
		return err
	}
	err = link(tmp, path)
	switch {
	case err == nil:
		return nil
	case errors.Is(err, fs.ErrExist):
		return &fs.PathError{Op: "create", Path: path, Err: fs.ErrExist}
	default:
		// No hard links here (or none to that place): create it in place.
		return writeExclusive(path, data, perm)
	}
}

// createTemp opens an unused hidden file next to the target with the final permissions, so that the kernel applies
// the umask as for any other new file.
func createTemp(dir, base string, perm fs.FileMode) (string, *os.File, error) {
	var lastErr error
	for range tempAttempts {
		var b [6]byte
		if _, err := rand.Read(b[:]); err != nil {
			return "", nil, err
		}
		name := filepath.Join(dir, "."+base+"."+hex.EncodeToString(b[:])+".tmp")
		f, err := os.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, perm) //nolint:gosec // the caller chooses the mode
		if err == nil {
			return name, f, nil
		}
		if !errors.Is(err, fs.ErrExist) {
			return "", nil, err
		}
		lastErr = err
	}
	return "", nil, lastErr
}

// writeExclusive creates path with O_EXCL and writes data to it, removing the file again if that fails.
func writeExclusive(path string, data []byte, perm fs.FileMode) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, perm) //nolint:gosec // the caller chooses the mode
	if err != nil {
		return err
	}
	if err := fill(f, data); err != nil {
		_ = os.Remove(path)
		return err
	}
	return nil
}

// writeSync writes data to f, syncs it and closes it.
func writeSync(f *os.File, data []byte) error {
	_, err := f.Write(data)
	if err == nil {
		err = f.Sync()
	}
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	return err
}
