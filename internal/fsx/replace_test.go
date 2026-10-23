package fsx

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

const original = "original\n"

func writeOriginal(t *testing.T, mode os.FileMode) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "knope.toml")
	if err := os.WriteFile(path, []byte(original), mode); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, mode); err != nil {
		t.Fatal(err)
	}
	return path
}

func assertOnly(t *testing.T, path string, content string) {
	t.Helper()
	if got, _ := os.ReadFile(path); string(got) != content {
		t.Errorf("content = %q, want %q", got, content)
	}
	entries, err := os.ReadDir(filepath.Dir(path))
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if e.Name() != filepath.Base(path) && e.Name() != "target.toml" {
			t.Errorf("leftover file %s", e.Name())
		}
	}
}

func TestReplaceFileKeepsTheMode(t *testing.T) {
	for name, mode := range map[string]os.FileMode{"private": 0o600, "shared": 0o644, "executable": 0o755} {
		t.Run(name, func(t *testing.T) {
			path := writeOriginal(t, mode)
			if err := ReplaceFile(path, []byte("new\n")); err != nil {
				t.Fatal(err)
			}
			info, err := os.Stat(path)
			if err != nil {
				t.Fatal(err)
			}
			if info.Mode().Perm() != mode {
				t.Errorf("mode = %v, want %v", info.Mode().Perm(), mode)
			}
			assertOnly(t, path, "new\n")
		})
	}
}

func TestReplaceFileRefusals(t *testing.T) {
	t.Run("symlink", func(t *testing.T) {
		dir := t.TempDir()
		target := filepath.Join(dir, "target.toml")
		if err := os.WriteFile(target, []byte(original), 0o600); err != nil {
			t.Fatal(err)
		}
		link := filepath.Join(dir, "knope.toml")
		if err := os.Symlink(target, link); err != nil {
			t.Skipf("no symlinks here: %v", err)
		}
		if err := ReplaceFile(link, []byte("new\n")); !errors.Is(err, ErrSymlink) {
			t.Fatalf("err = %v, want ErrSymlink", err)
		}
		if got, _ := os.ReadFile(target); string(got) != original {
			t.Errorf("the target changed: %q", got)
		}
		if info, err := os.Lstat(link); err != nil || info.Mode()&os.ModeSymlink == 0 {
			t.Errorf("the link was replaced: %v, %v", info, err)
		}
		assertOnly(t, link, original)
	})
	t.Run("directory", func(t *testing.T) {
		if err := ReplaceFile(t.TempDir(), []byte("x")); !errors.Is(err, ErrNotRegular) {
			t.Errorf("err = %v, want ErrNotRegular", err)
		}
	})
	t.Run("missing", func(t *testing.T) {
		if err := ReplaceFile(filepath.Join(t.TempDir(), "none"), []byte("x")); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("err = %v, want not exist", err)
		}
	})
	t.Run("empty output", func(t *testing.T) {
		path := writeOriginal(t, 0o644)
		if err := ReplaceFile(path, nil); !errors.Is(err, ErrEmpty) {
			t.Fatalf("err = %v, want ErrEmpty", err)
		}
		assertOnly(t, path, original)
	})
	t.Run("empty over empty", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "knope.toml")
		if err := os.WriteFile(path, nil, 0o600); err != nil {
			t.Fatal(err)
		}
		if err := ReplaceFile(path, nil); err != nil {
			t.Errorf("err = %v", err)
		}
	})
}

func TestReplaceFileCleansUpWhenTheRenameFails(t *testing.T) {
	path := writeOriginal(t, 0o644)
	failure := errors.New("rename refused")
	old := rename
	rename = func(string, string) error { return failure }
	t.Cleanup(func() { rename = old })

	if err := ReplaceFile(path, []byte("new\n")); !errors.Is(err, failure) {
		t.Fatalf("err = %v, want the rename failure", err)
	}
	assertOnly(t, path, original)
}

func TestReplaceFileCleansUpWhenTheDirectoryIsNotWritable(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root ignores directory permissions")
	}
	path := writeOriginal(t, 0o644)
	dir := filepath.Dir(path)
	if err := os.Chmod(dir, 0o500); err != nil { //nolint:gosec // a directory must be searchable
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o700) }) //nolint:gosec // restored so the test directory can be removed
	if err := ReplaceFile(path, []byte("new\n")); err == nil {
		t.Fatal("expected an error")
	}
	assertOnly(t, path, original)
}
