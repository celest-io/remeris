package config

import (
	"errors"
	"io"
	"io/fs"
)

// maxManifestSize caps what package discovery reads from one manifest (Cargo.toml, package.json, deno.json).
const maxManifestSize = 8 << 20

var (
	// errManifestTooLarge is returned by readManifestFile for a file of more than maxManifestSize bytes. Reading a
	// prefix would silently lose members or dependencies, so the discovery fails instead.
	errManifestTooLarge = errors.New("file is larger than 8 MiB")
	// errNotRegularFile is returned for a directory, a FIFO, a device or another file that is not a regular file.
	errNotRegularFile = errors.New("not a regular file")
)

// readManifestFile reads a manifest of package discovery. It stats the path first and refuses everything but a
// regular file (a symlink to one is fine): opening a FIFO would block forever. A missing file is an error that
// satisfies errors.Is(err, fs.ErrNotExist). The errors never carry file content.
func readManifestFile(root fs.StatFS, path string) ([]byte, error) {
	info, err := root.Stat(path)
	if err != nil {
		return nil, err
	}
	if info == nil || !info.Mode().IsRegular() {
		return nil, &fs.PathError{Op: "read", Path: path, Err: errNotRegularFile}
	}
	f, err := root.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	data, err := io.ReadAll(io.LimitReader(f, maxManifestSize+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxManifestSize {
		return nil, &fs.PathError{Op: "read", Path: path, Err: errManifestTooLarge}
	}
	return data, nil
}
