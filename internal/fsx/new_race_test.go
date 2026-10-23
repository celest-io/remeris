package fsx

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestWriteNewConcurrentWritersHaveOneWinner(t *testing.T) {
	const writers, rounds = 8, 50
	for range rounds {
		dir := t.TempDir()
		path := filepath.Join(dir, newName)
		start := make(chan struct{})
		errs := make(chan error, writers)
		for range writers {
			go func() {
				<-start
				errs <- WriteNew(path, []byte(newContent), 0o666)
			}()
		}
		close(start)
		wins := 0
		for range writers {
			switch err := <-errs; {
			case err == nil:
				wins++
			case !errors.Is(err, fs.ErrExist):
				t.Fatalf("unexpected error: %v", err)
			}
		}
		if wins != 1 {
			t.Fatalf("%d writers succeeded, want 1", wins)
		}
		if got, _ := os.ReadFile(path); string(got) != newContent {
			t.Fatalf("content = %q", got)
		}
		if names := entries(t, dir); len(names) != 1 || names[0] != newName {
			t.Fatalf("directory holds %v", names)
		}
	}
}

func TestWriteNewReadOnlyDirectory(t *testing.T) {
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("directory permissions do not stop this user")
	}
	dir := t.TempDir()
	if err := os.Chmod(dir, 0o500); err != nil { //nolint:gosec // a read-only directory is the point of the test
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o700) }) //nolint:gosec // restores the directory so it can be removed
	err := WriteNew(filepath.Join(dir, newName), []byte(newContent), 0o666)
	if err == nil || errors.Is(err, fs.ErrExist) {
		t.Fatalf("err = %v, want a permission error", err)
	}
	if names := entries(t, dir); len(names) != 0 {
		t.Errorf("leftovers: %v", names)
	}
}
