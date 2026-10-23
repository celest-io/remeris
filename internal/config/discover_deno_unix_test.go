//go:build unix

package config

import (
	"os"
	"strings"
	"syscall"
	"testing"
	"time"
)

// denoWithin runs denoWorkspace on the current directory and fails the test if it blocks.
func denoWithin(t *testing.T) error {
	t.Helper()
	done := make(chan error, 1)
	go func() {
		_, err := denoWorkspace(osRoot{})
		done <- err
	}()
	select {
	case err := <-done:
		return err
	case <-time.After(5 * time.Second):
		t.Fatal("discovery blocks on a special file")
		return nil
	}
}

func TestDenoSpecialFiles(t *testing.T) {
	t.Run("deno.json as a FIFO", func(t *testing.T) {
		t.Chdir(t.TempDir())
		if err := syscall.Mkfifo(denoJSONFile, 0o600); err != nil {
			t.Skipf("no mkfifo: %v", err)
		}
		if err := denoWithin(t); err == nil || !strings.Contains(err.Error(), `Could not read "deno.json"`) {
			t.Errorf("error = %v", err)
		}
	})
	t.Run("deno.json as a link to /dev/zero", func(t *testing.T) {
		t.Chdir(t.TempDir())
		if err := os.Symlink("/dev/zero", denoJSONFile); err != nil {
			t.Skipf("no symlinks: %v", err)
		}
		if err := denoWithin(t); err == nil || !strings.Contains(err.Error(), `Could not read "deno.json"`) {
			t.Errorf("error = %v", err)
		}
	})
	t.Run("lock as a link to a FIFO", func(t *testing.T) {
		t.Chdir(t.TempDir())
		writeTemp(t, denoJSONFile, fsNameA)
		if err := syscall.Mkfifo("pipe", 0o600); err != nil {
			t.Skipf("no mkfifo: %v", err)
		}
		if err := os.Symlink("pipe", denoLockFile); err != nil {
			t.Skipf("no symlinks: %v", err)
		}
		if err := denoWithin(t); err == nil || !strings.Contains(err.Error(), `"deno.lock" is not a regular file`) {
			t.Errorf("error = %v", err)
		}
	})
}
