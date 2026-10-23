//go:build unix

package config

import (
	"errors"
	"syscall"
	"testing"
	"time"
)

func TestReadManifestFileFIFO(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := syscall.Mkfifo("pipe.toml", 0o600); err != nil {
		t.Skipf("no mkfifo: %v", err)
	}
	done := make(chan error, 1)
	go func() {
		_, err := readManifestFile(osRoot{}, "pipe.toml")
		done <- err
	}()
	select {
	case err := <-done:
		if !errors.Is(err, errNotRegularFile) {
			t.Fatalf("error = %v, want %v", err, errNotRegularFile)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("reading a FIFO blocks")
	}
}
