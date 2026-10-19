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

// FirstRemoteURL returns the fetch URL of the first remote, in the order of git remote (alphabetical, which is also the
// order of libgit2's repo.remotes()). It is false when there is no remote, or on any error, like Knope's get_first_remote.
//
// The URL is remote.<name>.url as configured: insteadOf rewriting is not applied.
//
// Commands: git remote; git config --get remote.name.url.
func (c *CLI) FirstRemoteURL(ctx context.Context) (string, bool) {
	out, err := c.run(ctx, "", "remote")
	if err != nil {
		return "", false
	}
	names := strings.Fields(out)
	if len(names) == 0 {
		return "", false
	}
	url, ok, err := c.configValue(ctx, "remote."+names[0]+".url")
	if err != nil || !ok || url == "" {
		return "", false
	}
	return url, true
}
