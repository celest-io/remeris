package git

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	gogit "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/format/gitignore"
	"github.com/go-git/go-git/v5/storage/filesystem"
)

// commonDir returns the Git directory shared by all worktrees (the one with refs/ and worktrees/), and the Git
// directory of this worktree. They are equal in the main worktree.
func commonDir(repo *gogit.Repository) (common, own string, ok bool) {
	st, isFS := repo.Storer.(*filesystem.Storage)
	if !isFS {
		return "", "", false
	}
	own = filepath.Clean(st.Filesystem().Root())
	common = own
	if b, err := os.ReadFile(filepath.Join(own, "commondir")); err == nil {
		common = strings.TrimSpace(string(b))
		if !filepath.IsAbs(common) {
			common = filepath.Join(own, common)
		}
		common = filepath.Clean(common)
	}
	return common, own, true
}

// readIgnoreLines parses a gitignore-format file into root-relative patterns. A missing file gives none.
func readIgnoreLines(path string) []gitignore.Pattern {
	f, err := os.Open(path) //nolint:gosec // a path from the Git configuration or the Git directory
	if err != nil {
		return nil
	}
	defer f.Close()
	var ps []gitignore.Pattern
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := sc.Text()
		if strings.TrimSpace(line) != "" && !strings.HasPrefix(line, "#") {
			ps = append(ps, gitignore.ParsePattern(line, nil))
		}
	}
	return ps
}

// excludePatterns returns the ignore sources that go-git's status does not read: info/exclude of the common Git
// directory (go-git only finds it in a main worktree), core.excludesFile, and the default global ignore file
// ($XDG_CONFIG_HOME/git/ignore or ~/.config/git/ignore) when core.excludesFile is not set.
func excludePatterns(repo *gogit.Repository) []gitignore.Pattern {
	var ps []gitignore.Pattern
	if common, _, ok := commonDir(repo); ok {
		ps = append(ps, readIgnoreLines(filepath.Join(common, "info", "exclude"))...)
	}
	file := ""
	if cfg, err := repo.ConfigScoped(config.SystemScope); err == nil {
		file = cfg.Raw.Section("core").Options.Get("excludesfile")
	}
	home, _ := os.UserHomeDir()
	switch {
	case file == "":
		xdg := os.Getenv("XDG_CONFIG_HOME")
		if xdg == "" && home != "" {
			xdg = filepath.Join(home, ".config")
		}
		if xdg != "" {
			file = filepath.Join(xdg, "git", "ignore")
		}
	case file == "~" || strings.HasPrefix(file, "~/"):
		file = filepath.Join(home, strings.TrimPrefix(file, "~"))
	}
	if file != "" {
		ps = append(ps, readIgnoreLines(file)...)
	}
	return ps
}

// checkedOutElsewhere returns an error when the branch is checked out in another worktree, as git switch refuses to
// switch to it. Every HEAD of the common Git directory is read: the main one and worktrees/*/HEAD. The check is not
// atomic with the checkout (another process may check the branch out in between).
func checkedOutElsewhere(repo *gogit.Repository, name string) error {
	common, own, ok := commonDir(repo)
	if !ok {
		return nil
	}
	if head, err := repo.Reference(plumbing.HEAD, false); err == nil && head.Target() == plumbing.NewBranchReferenceName(name) {
		return nil // already on it
	}
	heads, _ := filepath.Glob(filepath.Join(common, "worktrees", "*", "HEAD"))
	heads = append(heads, filepath.Join(common, "HEAD"))
	want := "ref: " + branchRefPrefix + name
	for _, h := range heads {
		dir := filepath.Dir(h)
		if dir == own {
			continue
		}
		if b, err := os.ReadFile(h); err == nil && strings.TrimSpace(string(b)) == want {
			where := dir
			if dir == common {
				where = filepath.Dir(common)
			} else if gd, err := os.ReadFile(filepath.Join(dir, "gitdir")); err == nil {
				where = filepath.Dir(strings.TrimSpace(string(gd)))
			}
			return unknownGitError(fmt.Errorf("'%s' is already used by worktree at '%s'", name, where))
		}
	}
	return nil
}
