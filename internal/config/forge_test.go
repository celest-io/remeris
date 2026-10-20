package config

import (
	"os"
	"path/filepath"
	"testing"
)

const githubSSH = "git@github.com:o/r.git"

func TestGitHubFromRemote(t *testing.T) {
	tests := map[string]struct {
		url   string
		owner string
		repo  string
		ok    bool
	}{
		"https with .git":          {"https://github.com/o/r.git", "o", "r", true},
		"https without .git":       {"https://github.com/o/r", "o", "r", true},
		"scp style":                {githubSSH, "o", "r", true},
		"ssh url":                  {"ssh://git@github.com/o/r.git", "o", "r", true},
		"single part has no owner": {"git@github.com:r.git", "", "", false},
		"owner is the host":        {"https://github.com/r", "github.com", "r", true},
		"trailing slash":           {"https://github.com/o/r/", "r", "", true},
		"github.com anywhere":      {"https://example.com/github.com/x/r.git", "x", "r", true},
		"gitlab":                   {"https://gitlab.com/o/r.git", "", "", false},
		"codeberg is M8":           {"https://codeberg.org/o/r.git", "", "", false},
		"local path":               {"/srv/git/r.git", "", "", false},
		"empty":                    {"", "", "", false},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			got, ok := GitHubFromRemote(tt.url)
			if ok != tt.ok {
				t.Fatalf("GitHubFromRemote(%q) ok = %t, want %t", tt.url, ok, tt.ok)
			}
			if ok && (got.Owner != tt.owner || got.Repo != tt.repo) {
				t.Errorf("GitHubFromRemote(%q) = %s/%s, want %s/%s", tt.url, got.Owner, got.Repo, tt.owner, tt.repo)
			}
		})
	}
}

func TestLoadFSWithRemote(t *testing.T) {
	root := rootOf(map[string]string{cargoTomlName: packageHeader})
	remote := func(url string, ok bool) RemoteFunc { return func() (string, bool) { return url, ok } }
	workflowSteps := func(cfg *Config) int { return len(cfg.Workflows[0].Steps) }

	t.Run("detected without a file", func(t *testing.T) {
		cfg, err := LoadFSWithRemote(filepath.Join(t.TempDir(), "knope.toml"), root, remote(githubSSH, true))
		if err != nil || cfg.GitHub == nil || cfg.GitHub.Owner != "o" || cfg.GitHub.Repo != "r" || !cfg.HasForge() {
			t.Fatalf("config = %+v, %v", cfg, err)
		}
		if workflowSteps(cfg) != 4 {
			t.Errorf("release has %d steps, want the forge order of 4", workflowSteps(cfg))
		}
	})
	t.Run("no remote, no github remote or no getter", func(t *testing.T) {
		for _, r := range []RemoteFunc{remote("", false), remote("https://gitlab.com/o/r.git", true), nil} {
			cfg, err := LoadFSWithRemote(filepath.Join(t.TempDir(), "knope.toml"), root, r)
			if err != nil || cfg.HasForge() || workflowSteps(cfg) != 5 {
				t.Errorf("config = %+v, %v", cfg, err)
			}
		}
	})
	t.Run("a file is not given a forge", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "knope.toml")
		if err := os.WriteFile(path, []byte("[package]\nversioned_files = [\"Cargo.toml\"]\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		called := false
		cfg, err := LoadFSWithRemote(path, root, func() (string, bool) { called = true; return githubSSH, true })
		if err != nil || cfg.HasForge() || called {
			t.Errorf("config = %+v, %v, remote read = %t", cfg, err, called)
		}
	})
}
