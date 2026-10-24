package git

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	gogit "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing"
)

// appendRebaseReflogs writes the reflog lines that git writes for the move of the branch to and the switch to it:
// one in the reflog of the branch (in the common Git directory) and one in the reflog of HEAD (in the Git directory of
// this worktree). go-git writes no reflog itself. It is best effort: a failure is ignored, the references are moved.
func appendRebaseReflogs(repo *gogit.Repository, to, current string, oldHash, newHash plumbing.Hash) {
	common, own, ok := commonDir(repo)
	if !ok {
		return
	}
	name, email := "remeris", "remeris@localhost"
	if cfg, err := repo.ConfigScoped(config.SystemScope); err == nil {
		if cfg.User.Name != "" {
			name = cfg.User.Name
		}
		if cfg.User.Email != "" {
			email = cfg.User.Email
		}
	}
	now := time.Now()
	ident := fmt.Sprintf("%s <%s> %d %s", sanitizeIdent(name), sanitizeIdent(email), now.Unix(), now.Format("-0700"))
	appendReflog(filepath.Join(common, "logs", filepath.FromSlash(branchRefPrefix+to)), oldHash, newHash, ident,
		rebaseReflogMessage(to, current))
	appendReflog(filepath.Join(own, "logs", headRef), newHash, newHash, ident,
		fmt.Sprintf("checkout: moving from %s to %s", current, to))
}

// sanitizeIdent removes the characters that would break the format of a reflog line.
func sanitizeIdent(s string) string {
	return strings.Map(func(r rune) rune {
		if r == '<' || r == '>' || r == '\n' || r == '\x00' {
			return -1
		}
		return r
	}, s)
}

// appendReflog appends one line to a reflog file, creating it and its directories.
func appendReflog(path string, oldHash, newHash plumbing.Hash, ident, msg string) {
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600) //nolint:gosec // inside the Git directory
	if err != nil {
		return
	}
	_, _ = fmt.Fprintf(f, "%s %s %s\t%s\n", oldHash, newHash, ident, msg)
	_ = f.Close()
}
