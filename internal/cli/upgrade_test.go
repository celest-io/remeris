package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/celest-io/remeris/internal/config"
)

const (
	oldIgnore = `# keep me
[package]
changelog = "CHANGELOG.md"

[[workflows]]
name = "prepare"

[[workflows.steps]]
type = "PrepareRelease"
ignore_conventional_commits = true
`
	upgradedIgnore = `[changes]
ignore_conventional_commits = true

[package]
changelog = "CHANGELOG.md"

[[workflows]]
name = "prepare"

[[workflows.steps]]
type = "PrepareRelease"
`
	nothingToDo = "[package]\nchangelog = \"CHANGELOG.md\"\n\n[[workflows]]\nname = \"No-op\"\nsteps = []\n"
	wfPrepare   = "prepare"
	upgradeArg  = "--" + flagUpgrade
)

// upgradeFixture writes content to a knope.toml in a temporary directory, loads it and makes --upgrade write there.
func upgradeFixture(t *testing.T, content string) (*config.Config, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "knope.toml")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	old := upgradePath
	upgradePath = path
	t.Cleanup(func() { upgradePath = old })
	return cfg, path
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestUpgradeRewritesTheFile(t *testing.T) {
	cfg, path := upgradeFixture(t, oldIgnore)
	out, err := run(t, cfg, upgradeArg)
	if err != nil {
		t.Fatal(err)
	}
	if want := upgradeWarning + "\n"; out != want {
		t.Errorf("stdout = %q, want %q", out, want)
	}
	if got := readFile(t, path); got != upgradedIgnore {
		t.Errorf("file = %q", got)
	}
	if info, _ := os.Stat(path); info.Mode().Perm() != 0o600 {
		t.Errorf("mode = %v", info.Mode().Perm())
	}
}

func TestUpgradeIsIdempotent(t *testing.T) {
	cfg, path := upgradeFixture(t, oldIgnore)
	if _, err := run(t, cfg, upgradeArg); err != nil {
		t.Fatal(err)
	}
	again, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	out, err := run(t, again, upgradeArg)
	if err != nil || out != nothingToUpgrade+"\n" {
		t.Errorf("out = %q, err = %v", out, err)
	}
	if got := readFile(t, path); got != upgradedIgnore {
		t.Errorf("file = %q", got)
	}
}

func TestUpgradeNothingLeavesTheFileUntouched(t *testing.T) {
	cfg, path := upgradeFixture(t, nothingToDo)
	before, _ := os.Stat(path)
	out, err := run(t, cfg, upgradeArg)
	if err != nil || out != nothingToUpgrade+"\n" {
		t.Errorf("out = %q, err = %v", out, err)
	}
	after, _ := os.Stat(path)
	if got := readFile(t, path); got != nothingToDo || !after.ModTime().Equal(before.ModTime()) {
		t.Errorf("the file was touched: %q", got)
	}
}

func TestUpgradeDryRunWritesNothing(t *testing.T) {
	cfg, path := upgradeFixture(t, oldIgnore)
	out, err := run(t, cfg, "--dry-run", upgradeArg)
	if err != nil {
		t.Fatal(err)
	}
	if want := wouldUpgrade + "\n" + upgradedIgnore; out != want {
		t.Errorf("stdout = %q, want %q", out, want)
	}
	if got := readFile(t, path); got != oldIgnore {
		t.Errorf("the file changed: %q", got)
	}

	cfg, _ = upgradeFixture(t, nothingToDo)
	if out, err := run(t, cfg, "--dry-run", upgradeArg); err != nil || out != nothingToUpgrade+"\n" {
		t.Errorf("out = %q, err = %v", out, err)
	}
}

func TestUpgradeNeverWritesThePrereleaseLabel(t *testing.T) {
	for name, args := range map[string][]string{
		"no workflow":   {upgradeArg},
		"with workflow": {upgradeArg, wfPrepare},
		"label flag":    {upgradeArg, wfPrepare, "--prerelease-label", "beta"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Setenv(knopePrereleaseLabelEnv, "rc")
			t.Setenv(prereleaseLabelEnv, "rc")
			cfg, path := upgradeFixture(t, oldIgnore)
			if _, err := run(t, cfg, args...); err != nil {
				t.Fatal(err)
			}
			got := readFile(t, path)
			if strings.Contains(got, "prerelease_label") || got != upgradedIgnore {
				t.Errorf("file = %q", got)
			}
		})
	}
}

func TestUpgradeRefusesASymlink(t *testing.T) {
	cfg, path := upgradeFixture(t, oldIgnore)
	target := filepath.Join(filepath.Dir(path), "real.toml")
	if err := os.Rename(path, target); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, path); err != nil {
		t.Skipf("no symlinks here: %v", err)
	}
	_, err := run(t, cfg, upgradeArg)
	if err == nil || !strings.Contains(err.Error(), "is a symbolic link") {
		t.Fatalf("err = %v", err)
	}
	if got := readFile(t, target); got != oldIgnore {
		t.Errorf("the target changed: %q", got)
	}
}

func TestUpgradeReportsTheDiscoveryErrorFirst(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	if err := os.WriteFile("Cargo.toml", []byte("[package\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, path := upgradeFixture(t, "[[workflows]]\nname = \"prepare\"\n\n[[workflows.steps]]\n"+
		"type = \"PrepareRelease\"\nignore_conventional_commits = true\n")
	before := readFile(t, path)
	if _, err := run(t, cfg, upgradeArg); err == nil || !strings.Contains(err.Error(), "Cargo.toml") {
		t.Fatalf("err = %v", err)
	}
	if got := readFile(t, path); got != before {
		t.Errorf("the file changed: %q", got)
	}
}
