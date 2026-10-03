package fsx

import (
	"os"
	"path/filepath"
	"testing"
)

func TestWriteFileAndRemove(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sub", "file.txt")

	if err := WriteFile(true, path, []byte("x")); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("a dry run must not write, stat err = %v", err)
	}

	if err := WriteFile(false, path, []byte("x")); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(path); string(got) != "x" {
		t.Fatalf("file content = %q", got)
	}

	if err := Remove(true, path); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("a dry run must not delete: %v", err)
	}
	if err := Remove(false, path); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("file should be gone, stat err = %v", err)
	}
}
