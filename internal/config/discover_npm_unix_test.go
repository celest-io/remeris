//go:build unix

package config

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

const npmDiskDeadline = 10 * time.Second

// TestNPMWorkspacesFIFOManifest: a package.json that is a symlink to a FIFO is skipped without being opened (opening
// it would block forever).
func TestNPMWorkspacesFIFOManifest(t *testing.T) {
	t.Chdir(t.TempDir())
	writeTemp(t, packageJSONName, npmRootDoc)
	writeTemp(t, npmPkgB, npmNameB)
	if err := os.MkdirAll(filepath.Dir(npmPkgA), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := syscall.Mkfifo("fifo", 0o600); err != nil {
		t.Skipf("no FIFO: %v", err)
	}
	if err := os.Symlink("../../fifo", npmPkgA); err != nil {
		t.Fatal(err)
	}

	done := make(chan []string, 1)
	go func() {
		pkgs, err := npmWorkspaces(osRoot{})
		if err != nil {
			t.Error(err)
		}
		var got []string
		for _, p := range pkgs {
			got = append(got, summary(p))
		}
		done <- got
	}()
	select {
	case got := <-done:
		if len(got) != 1 || got[0] != npmSummaryB {
			t.Errorf("packages = %v, want only b", got)
		}
	case <-time.After(npmDiskDeadline):
		t.Fatal("npmWorkspaces blocked on a FIFO")
	}
}

// TestNPMWorkspacesSymlinkedDirectory pins the policy: a workspace directory that is a symbolic link is followed,
// even when it points outside the project, as in Knope. Only the pattern is checked, not where links lead.
func TestNPMWorkspacesSymlinkedDirectory(t *testing.T) {
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, packageJSONName), []byte(npmNameA), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Chdir(t.TempDir())
	writeTemp(t, packageJSONName, npmRootDoc)
	if err := os.MkdirAll("packages", 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, "packages/a"); err != nil {
		t.Fatal(err)
	}

	pkgs, err := npmWorkspaces(osRoot{})
	if err != nil || len(pkgs) != 1 || summary(pkgs[0]) != npmSummaryA {
		t.Fatalf("packages = %+v, error = %v; want a, followed through the link", pkgs, err)
	}
}

// TestNPMWorkspacesPatternCannotEscapeOnDisk: lexical escapes are errors on the production root.
func TestNPMWorkspacesPatternCannotEscapeOnDisk(t *testing.T) {
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, packageJSONName), []byte(npmNameA), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Chdir(t.TempDir())
	for _, pattern := range []string{"../" + filepath.Base(outside), outside, outside + "/*"} {
		writeTemp(t, packageJSONName, `{"workspaces": ["`+pattern+`"]}`)
		pkgs, err := npmWorkspaces(osRoot{})
		if err == nil || pkgs != nil {
			t.Errorf("pattern %q: packages = %+v, error = %v; want an error", pattern, pkgs, err)
		}
	}
}
