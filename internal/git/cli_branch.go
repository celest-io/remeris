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
	// Outside a repository, git config would read remotes from the global config.
	if _, err := c.run(ctx, "", "rev-parse", "--git-dir"); err != nil {
		return "", false
	}
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
	// An empty url is no URL: newer git versions print the remote's name from get-url then.
	if raw, ok, err := c.configValue(ctx, "remote."+first+".url"); err != nil || !ok || raw == "" {
		return "", false
	}
	out, err = c.run(ctx, "", "remote", "get-url", "--", first)
	url := strings.TrimSpace(out)
	if err != nil || url == "" {
		return "", false
	}
	return url, true
}

// LocalBranches returns the short names of the local branches, sorted by name (spec F19.7).
//
// Command: git for-each-ref --format=%(refname:strip=2) refs/heads/.
func (c *CLI) LocalBranches(ctx context.Context) ([]string, error) {
	if _, err := c.run(ctx, "", "rev-parse", "--git-dir"); err != nil {
		return nil, fail(err, openRepoError)
	}
	out, err := c.run(ctx, "", "for-each-ref", "--format=%(refname:strip=2)", branchRefPrefix)
	if err != nil {
		return nil, fail(err, unknownGitError)
	}
	branches := []string{}
	for line := range strings.SplitSeq(out, "\n") {
		if line != "" {
			branches = append(branches, line)
		}
	}
	return branches, nil
}

// BranchExists reports whether the local branch name exists. An invalid name is an error.
//
// Command: git show-ref --verify --quiet refs/heads/<name>.
func (c *CLI) BranchExists(ctx context.Context, name string) (bool, error) {
	if err := checkBranchName(name); err != nil {
		return false, err
	}
	if _, err := c.run(ctx, "", "rev-parse", "--git-dir"); err != nil {
		return false, fail(err, openRepoError)
	}
	_, err := c.run(ctx, "", "show-ref", "--verify", "--quiet", branchRefPrefix+name)
	if err == nil {
		return true, nil
	}
	var exit *exec.ExitError
	if errors.As(err, &exit) && exit.ExitCode() == 1 {
		return false, nil
	}
	return false, fail(err, unknownGitError)
}

// CreateBranch creates the local branch name at the tip of the local branch from, without tracking and without
// switching to it. The name must be valid and free, and from must exist.
//
// Command: git branch --no-track --end-of-options <name> refs/heads/<from>.
func (c *CLI) CreateBranch(ctx context.Context, name, from string) error {
	if err := checkCreateBranch(ctx, c, name, from); err != nil {
		return err
	}
	if _, err := c.run(ctx, "", "branch", "--no-track", "--end-of-options", name, branchRefPrefix+from); err != nil {
		return fail(err, unknownGitError)
	}
	return nil
}

// HasUncommittedChanges reports whether the working tree or the index differ from HEAD. Untracked files count and
// ignored files do not.
//
// Command: git --no-optional-locks status --porcelain -z --untracked-files=all (no index refresh, so a read does not
// take locks).
func (c *CLI) HasUncommittedChanges(ctx context.Context) (bool, error) {
	if _, err := c.run(ctx, "", "rev-parse", "--git-dir"); err != nil {
		return false, fail(err, openRepoError)
	}
	out, err := c.run(ctx, "", "--no-optional-locks", "status", "--porcelain", "-z", "--untracked-files=all")
	if err != nil {
		return false, fail(err, unknownGitError)
	}
	return out != "", nil
}

// SwitchTo checks out the local branch name. The branch must exist and the working tree must be clean (Knope's
// switch_to_branch). A remote branch is never guessed.
//
// Command: git switch --no-guess --end-of-options <name>.
func (c *CLI) SwitchTo(ctx context.Context, name string) error {
	if err := checkSwitch(ctx, c, name); err != nil {
		return err
	}
	if _, err := c.run(ctx, "", "switch", "--no-guess", "--end-of-options", name); err != nil {
		return fail(err, unknownGitError)
	}
	return nil
}

// Rebase moves the local branch to onto HEAD and switches to it, fast-forward only (see Git.Rebase).
//
// Both commits are resolved once and the branch is moved with a compare-and-set, so a commit that another process adds
// to the branch in between is never lost: the update fails instead.
//
// Commands: git symbolic-ref -q refs/heads/<to> (an alias is refused); git rev-parse --verify <ref>^{commit};
// git worktree list --porcelain -z; git merge-base --is-ancestor <old> <new> (exit 1 when not a fast-forward);
// git update-ref -m <message> refs/heads/<to> <new> <old>; git switch --no-guess --end-of-options <to>.
func (c *CLI) Rebase(ctx context.Context, to string) error {
	current, err := checkRebase(ctx, c, to)
	if err != nil {
		return err
	}
	if current == to {
		return nil
	}
	ref := branchRefPrefix + to
	target, err := c.run(ctx, "", "symbolic-ref", "-q", ref)
	if err == nil {
		return branchAliasError(to, strings.TrimSpace(target))
	}
	var exit *exec.ExitError
	if !errors.As(err, &exit) || exit.ExitCode() != 1 {
		return fail(err, unknownGitError)
	}
	oldOut, err := c.run(ctx, "", "rev-parse", "--verify", ref+"^{commit}")
	if err != nil {
		return fail(err, unknownGitError)
	}
	newOut, err := c.run(ctx, "", "rev-parse", "--verify", headRef+"^{commit}")
	if err != nil {
		return fail(err, unknownGitError)
	}
	oldHash, newHash := strings.TrimSpace(oldOut), strings.TrimSpace(newOut)
	if err := c.checkNotInWorktree(ctx, to); err != nil {
		return err
	}
	if _, err := c.run(ctx, "", "merge-base", "--is-ancestor", oldHash, newHash); err != nil {
		if errors.As(err, &exit) && exit.ExitCode() == 1 {
			return notFastForwardError(to, current)
		}
		return fail(err, unknownGitError)
	}
	if _, err := c.run(ctx, "", "update-ref", "-m", rebaseReflogMessage(to, current), ref, newHash, oldHash); err != nil {
		return fail(err, unknownGitError)
	}
	// HEAD and to are the same commit now, so the switch leaves the files alone.
	if _, err := c.run(ctx, "", "switch", "--no-guess", "--end-of-options", to); err != nil {
		return fail(err, func(cause error) error { return movedButNotSwitchedError(to, current, cause) })
	}
	return nil
}

// checkNotInWorktree fails when the branch is checked out in a worktree. The caller has checked that HEAD of this
// worktree is on another branch.
func (c *CLI) checkNotInWorktree(ctx context.Context, name string) error {
	out, err := c.run(ctx, "", "worktree", "list", "--porcelain", "-z")
	if err != nil {
		return fail(err, unknownGitError)
	}
	var path string
	for field := range strings.SplitSeq(out, "\x00") {
		if p, ok := strings.CutPrefix(field, "worktree "); ok {
			path = p
		} else if field == "branch "+branchRefPrefix+name {
			return worktreeBranchError(name, path)
		}
	}
	return nil
}
