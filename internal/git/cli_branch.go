package git

import (
	"context"
	"errors"
	"os/exec"
	"strings"
)

// CurrentBranch returns the short name of the branch HEAD points to (spec F9.6, Quirk Q3). Knope returns the full
// reference name; remeris returns "main", or "feature/x" for a branch with slashes.
//
// A detached HEAD, or one that points outside refs/heads, is Knope's "Not on the tip of a Git branch." error. An unborn
// branch (no commit yet) fails like libgit2's repo.head(): reference 'refs/heads/main' not found.
//
// Commands: git rev-parse --git-dir; git symbolic-ref -q HEAD (exit 1 when detached); git show-ref --verify --quiet ref.
func (c *CLI) CurrentBranch(ctx context.Context) (string, error) {
	if _, err := c.run(ctx, "", "rev-parse", "--git-dir"); err != nil {
		return "", fail(err, openRepoError)
	}
	out, err := c.run(ctx, "", "symbolic-ref", "-q", headRef)
	if err != nil {
		var exit *exec.ExitError
		if errors.As(err, &exit) && exit.ExitCode() == 1 {
			return "", notOnBranchError()
		}
		return "", fail(err, unknownGitError)
	}
	ref := strings.TrimSpace(out)
	if !strings.HasPrefix(ref, branchRefPrefix) {
		return "", notOnBranchError()
	}
	if _, err := c.run(ctx, "", "show-ref", "--verify", "--quiet", ref); err != nil {
		var exit *exec.ExitError
		if errors.As(err, &exit) && exit.ExitCode() == 1 {
			return "", unknownGitError(refNotFoundError(ref))
		}
		return "", fail(err, unknownGitError)
	}
	return strings.TrimPrefix(ref, branchRefPrefix), nil
}

// FirstRemoteURL returns the fetch URL of the first remote, like Knope's get_first_remote. It is false when there is no
// remote, or on any error.
//
// Like libgit2's repo.remotes(), only the remotes that have a url or pushurl key are listed, sorted by name in byte
// order (git remote would list every remote section). If the first one has no url, the result is false. The
// url.<base>.insteadOf rewriting is applied, like libgit2's remote lookup does. With several url values the first is
// used (libgit2 would take the last).
//
// Commands: git config --null --get-regexp ^remote\..*\.(push)?url$; git remote get-url -- name.
func (c *CLI) FirstRemoteURL(ctx context.Context) (string, bool) {
	out, err := c.run(ctx, "", "config", "--null", "--get-regexp", `^remote\..*\.(push)?url$`)
	if err != nil {
		return "", false
	}
	var first string
	found := false
	for entry := range strings.SplitSeq(out, "\x00") {
		key, _, _ := strings.Cut(entry, "\n")
		name, ok := strings.CutPrefix(key, "remote.")
		if !ok {
			continue
		}
		if i := strings.LastIndex(name, "."); i > 0 {
			name = name[:i]
		} else {
			continue
		}
		if !found || name < first {
			first, found = name, true
		}
	}
	if !found {
		return "", false
	}
	if _, ok, err := c.configValue(ctx, "remote."+first+".url"); err != nil || !ok {
		return "", false
	}
	out, err = c.run(ctx, "", "remote", "get-url", "--", first)
	url := strings.TrimSpace(out)
	if err != nil || url == "" {
		return "", false
	}
	return url, true
}
