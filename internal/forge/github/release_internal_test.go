//go:build unix

package github

import (
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

// A FIFO matched as an asset is refused instead of blocking the upload.
func TestOpenAssetRefusesSpecialFiles(t *testing.T) {
	dir := t.TempDir()
	if err := syscall.Mkfifo(filepath.Join(dir, "pipe"), 0o600); err != nil {
		t.Skip("cannot create a FIFO:", err)
	}
	// Without a writer, opening the FIFO would block: the check must happen before the open.
	f, err := openAsset(dir, "pipe")
	if err == nil {
		_ = f.Close()
		t.Fatal("a FIFO was accepted as an asset")
	}
	if !strings.Contains(err.Error(), "not a regular file") {
		t.Errorf("error = %v", err)
	}
}
