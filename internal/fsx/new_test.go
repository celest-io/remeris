package fsx

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

const (
	newName    = "knope.toml"
	newContent = "workflows = []\n"
)

// entries lists the names in dir, to check that no temporary file stays behind.
func entries(t *testing.T, dir string) []string {
	t.Helper()
	list, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range list {
		names = append(names, e.Name())
	}
	return names
}

func TestWriteNewCreatesTheFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, newName)
	if err := WriteNew(path, []byte(newContent), 0o666); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(path); string(got) != newContent {
		t.Errorf("content = %q", got)
	}
	if names := entries(t, dir); len(names) != 1 || names[0] != newName {
		t.Errorf("directory holds %v, want only %s", names, newName)
	}
}

func TestWriteNewKeepsWhatExists(t *testing.T) {
	setup := map[string]func(t *testing.T, path string){
		"file": func(t *testing.T, path string) {
			if err := os.WriteFile(path, []byte("mine"), 0o600); err != nil {
				t.Fatal(err)
			}
		},
		"empty file": func(t *testing.T, path string) {
			if err := os.WriteFile(path, nil, 0o600); err != nil {
				t.Fatal(err)
			}
		},
		"directory": func(t *testing.T, path string) {
			if err := os.Mkdir(path, 0o750); err != nil {
				t.Fatal(err)
			}
		},
		"symlink to a file": func(t *testing.T, path string) {
			other := filepath.Join(filepath.Dir(path), "other")
			if err := os.WriteFile(other, []byte("other"), 0o600); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink("other", path); err != nil {
				t.Skipf("no symbolic links: %v", err)
			}
		},
		"dangling symlink": func(t *testing.T, path string) {
			if err := os.Symlink("nowhere", path); err != nil {
				t.Skipf("no symbolic links: %v", err)
			}
		},
	}
	for name, arrange := range setup {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, newName)
			arrange(t, path)
			before, err := os.Lstat(path)
			if err != nil {
				t.Fatal(err)
			}
			err = WriteNew(path, []byte(newContent), 0o666)
			if !errors.Is(err, fs.ErrExist) {
				t.Fatalf("err = %v, want fs.ErrExist", err)
			}
			after, err := os.Lstat(path)
			if err != nil || !os.SameFile(before, after) || before.Mode() != after.Mode() || before.Size() != after.Size() {
				t.Errorf("the path changed: %v, %v", after, err)
			}
			for _, n := range entries(t, dir) {
				if n != newName && n != "other" {
					t.Errorf("unexpected leftover %q", n)
				}
			}
		})
	}
}

func TestWriteNewDoesNotCreateThroughADanglingSymlink(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, newName)
	if err := os.Symlink("target", path); err != nil {
		t.Skipf("no symbolic links: %v", err)
	}
	_ = WriteNew(path, []byte(newContent), 0o666)
	if _, err := os.Lstat(filepath.Join(dir, "target")); !os.IsNotExist(err) {
		t.Errorf("the symbolic link was followed: %v", err)
	}
}

func TestWriteNewHonoursTheMode(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("permission bits are not modelled on Windows")
	}
	for name, perm := range map[string]fs.FileMode{"private": 0o600, "shared": 0o666} {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, newName)
			if err := WriteNew(path, []byte(newContent), perm); err != nil {
				t.Fatal(err)
			}
			info, err := os.Stat(path)
			if err != nil {
				t.Fatal(err)
			}
			// The umask only removes bits, whatever it is.
			if got := info.Mode().Perm(); got&^perm != 0 || got&0o400 == 0 {
				t.Errorf("mode = %o for %o", got, perm)
			}
			// A file made by os.WriteFile with the same mode is the reference for the umask.
			ref := filepath.Join(dir, "ref")
			if err := os.WriteFile(ref, nil, perm); err != nil {
				t.Fatal(err)
			}
			refInfo, _ := os.Stat(ref)
			if info.Mode().Perm() != refInfo.Mode().Perm() {
				t.Errorf("mode = %o, os.WriteFile gives %o", info.Mode().Perm(), refInfo.Mode().Perm())
			}
		})
	}
}

func TestWriteNewWithoutHardLinks(t *testing.T) {
	old := link
	link = func(string, string) error { return &os.LinkError{Op: "link", Err: errors.ErrUnsupported} }
	t.Cleanup(func() { link = old })

	dir := t.TempDir()
	path := filepath.Join(dir, newName)
	if err := WriteNew(path, []byte(newContent), 0o666); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(path); string(got) != newContent {
		t.Errorf("content = %q", got)
	}
	if names := entries(t, dir); len(names) != 1 {
		t.Errorf("directory holds %v", names)
	}

	// The fallback never replaces either.
	if err := WriteNew(path, []byte("other"), 0o666); !errors.Is(err, fs.ErrExist) {
		t.Errorf("second write: %v", err)
	}
	if got, _ := os.ReadFile(path); string(got) != newContent {
		t.Errorf("content after the refusal = %q", got)
	}
}

func TestWriteNewLosesARaceCleanly(t *testing.T) {
	// The file appears between the temporary file and the link.
	dir := t.TempDir()
	path := filepath.Join(dir, newName)
	old := link
	link = func(oldname, newname string) error {
		if err := os.WriteFile(newname, []byte("winner"), 0o600); err != nil {
			t.Fatal(err)
		}
		return old(oldname, newname)
	}
	t.Cleanup(func() { link = old })

	if err := WriteNew(path, []byte(newContent), 0o666); !errors.Is(err, fs.ErrExist) {
		t.Fatalf("err = %v, want fs.ErrExist", err)
	}
	if got, _ := os.ReadFile(path); string(got) != "winner" {
		t.Errorf("content = %q", got)
	}
	if names := entries(t, dir); len(names) != 1 {
		t.Errorf("directory holds %v", names)
	}
}

func TestWriteNewMissingDirectory(t *testing.T) {
	path := filepath.Join(t.TempDir(), "missing", newName)
	err := WriteNew(path, []byte(newContent), 0o666)
	if !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("err = %v, want fs.ErrNotExist", err)
	}
}

func TestWriteNewRemovesTheFileItCouldNotFill(t *testing.T) {
	errDisk := errors.New("disk full")
	for name, noLinks := range map[string]bool{"temporary file": false, "in place": true} {
		t.Run(name, func(t *testing.T) {
			oldFill, oldLink := fill, link
			t.Cleanup(func() { fill, link = oldFill, oldLink })
			fill = func(f *os.File, _ []byte) error {
				_ = f.Close()
				return errDisk
			}
			if noLinks {
				link = func(string, string) error { return errors.ErrUnsupported }
			}
			dir := t.TempDir()
			if err := WriteNew(filepath.Join(dir, newName), []byte(newContent), 0o666); !errors.Is(err, errDisk) {
				t.Fatalf("err = %v, want the write error", err)
			}
			if names := entries(t, dir); len(names) != 0 {
				t.Errorf("leftovers: %v", names)
			}
		})
	}
}
