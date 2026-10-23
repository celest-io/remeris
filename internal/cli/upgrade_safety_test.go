package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func noTempFiles(t *testing.T, path string) {
	t.Helper()
	entries, err := os.ReadDir(filepath.Dir(path))
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if e.Name() != filepath.Base(path) {
			t.Errorf("leftover file %s", e.Name())
		}
	}
}

func TestUpgradeLeavesNoTemporaryFile(t *testing.T) {
	cfg, path := upgradeFixture(t, oldIgnore)
	if _, err := run(t, cfg, upgradeArg); err != nil {
		t.Fatal(err)
	}
	noTempFiles(t, path)
}

func TestUpgradeDryRunKeepsBytesAndModTime(t *testing.T) {
	cfg, path := upgradeFixture(t, oldIgnore)
	before, _ := os.Stat(path)
	if _, err := run(t, cfg, "--dry-run", upgradeArg); err != nil {
		t.Fatal(err)
	}
	after, _ := os.Stat(path)
	if !after.ModTime().Equal(before.ModTime()) || readFile(t, path) != oldIgnore {
		t.Error("the dry run touched the file")
	}
	noTempFiles(t, path)
}

func TestUpgradeRefusesKeysItWouldDrop(t *testing.T) {
	const content = "[changes]\nignore_conventional_merge_commits = true\n\n[package]\nbogus = 1\n\n" +
		"[[workflows]]\nname = \"prepare\"\n\n[[workflows.steps]]\ntype = \"PrepareRelease\"\n" +
		"ignore_conventional_commits = true\n"
	const want = "knope.toml has keys that --upgrade would drop: changes.ignore_conventional_merge_commits, package.bogus"
	for _, args := range [][]string{{upgradeArg}, {"--dry-run", upgradeArg}} {
		cfg, path := upgradeFixture(t, content)
		_, err := run(t, cfg, args...)
		if err == nil || err.Error() != want {
			t.Fatalf("err = %v, want %q", err, want)
		}
		if readFile(t, path) != content {
			t.Error("the file changed")
		}
		noTempFiles(t, path)
	}
	// Nothing to rewrite: the file is not refused.
	cfg, _ := upgradeFixture(
		t,
		"[changes]\nignore_conventional_merge_commits = true\n\n[[workflows]]\nname = \"x\"\nsteps = []\n",
	)
	if out, err := run(t, cfg, upgradeArg); err != nil || out != nothingToUpgrade+"\n" {
		t.Errorf("out = %q, err = %v", out, err)
	}
}

func TestUpgradeKeepsALoneNamedPackage(t *testing.T) {
	const content = "[packages.foo]\nchangelog = \"CHANGELOG.md\"\n\n[[workflows]]\nname = \"prepare\"\n\n" +
		"[[workflows.steps]]\ntype = \"PrepareRelease\"\nignore_conventional_commits = true\n"
	cfg, path := upgradeFixture(t, content)
	if _, err := run(t, cfg, upgradeArg); err != nil {
		t.Fatal(err)
	}
	if got := readFile(t, path); !strings.Contains(got, "[packages.foo]") || strings.Contains(got, "[package]") {
		t.Errorf("file = %q", got)
	}
}

func TestUpgradeWinsOverValidateAndKeepsTheLabelOut(t *testing.T) {
	cfg, path := upgradeFixture(t, oldIgnore)
	if _, err := run(t, cfg, "--validate", upgradeArg, wfPrepare, "--prerelease-label", "rc"); err != nil {
		t.Fatal(err)
	}
	if got := readFile(t, path); got != upgradedIgnore {
		t.Errorf("file = %q", got)
	}
}

func TestUpgradeRefusesAReadOnlyFile(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root writes read-only files")
	}
	cfg, path := upgradeFixture(t, oldIgnore)
	if err := os.Chmod(path, 0o400); err != nil {
		t.Fatal(err)
	}
	_, err := run(t, cfg, upgradeArg)
	if err == nil || !strings.Contains(err.Error(), "is read-only") {
		t.Fatalf("err = %v", err)
	}
	if readFile(t, path) != oldIgnore {
		t.Error("the file changed")
	}
	noTempFiles(t, path)
}
