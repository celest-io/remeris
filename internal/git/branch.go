package git

import (
	"context"
	"fmt"
	"strings"

	"github.com/celest-io/remeris/internal/diag"
)

// branchRefPrefix is the namespace of the local branches.
const branchRefPrefix = "refs/heads/"

// notOnBranchError is Knope's git::not_a_branch error (ErrorKind::NotOnAGitBranch), returned when HEAD is detached.
func notOnBranchError() error {
	return diag.New("Not on the tip of a Git branch.").
		WithCode("remeris::git::not_a_branch").
		WithHelp("In order to run this step, you need to be on the very tip of a Git branch.")
}

// badBranchNameError is Knope's git::bad_branch_name error, returned for a name git would refuse. The message is
// Knope's; the name can come from an issue title, so the help quotes it.
func badBranchNameError(name string) error {
	return diag.New("Bad branch name").
		WithCode("remeris::git::bad_branch_name").
		WithHelp(fmt.Sprintf("The branch name was not formatted correctly: %q", name))
}

// uncommittedChangesError is Knope's git::uncommitted_changes error.
func uncommittedChangesError() error {
	return diag.New("Uncommitted changes").
		WithCode("remeris::git::uncommitted_changes").
		WithHelp("You need to commit your changes before running this step.")
}

// incompleteCheckoutError is Knope's git::incomplete_checkout error: HEAD moved but the checkout did not finish.
func incompleteCheckoutError(cause error) error {
	return diag.New("Could not complete checkout").
		WithCode("remeris::git::incomplete_checkout").
		WithHelp("Switching branches failed, but HEAD was changed. You probably want to git switch back " +
			"to the branch you were on.").
		WithCause(cause)
}

// branchNotFoundError is libgit2's error for a local branch that does not exist.
func branchNotFoundError(name string) error {
	return unknownGitError(fmt.Errorf("cannot locate local branch '%s'", name))
}

// branchExistsError is libgit2's error for creating a branch that already exists.
func branchExistsError(name string) error {
	return unknownGitError(fmt.Errorf("a reference with that name already exists: refs/heads/%s", name))
}

// checkBranchName refuses the names that git check-ref-format would refuse for refs/heads/<name>, and the names that
// start with "-" or are HEAD, which git branch refuses too. It is pure so that both backends agree, and so that a
// hostile name (an issue title) can never be taken for an option by the git command.
func checkBranchName(name string) error {
	if name == "" || name == "@" || name == headRef || strings.HasPrefix(name, "-") ||
		strings.HasPrefix(name, "/") || strings.HasSuffix(name, "/") || strings.HasSuffix(name, ".") ||
		strings.Contains(name, "..") || strings.Contains(name, "@{") || strings.Contains(name, "//") {
		return badBranchNameError(name)
	}
	for _, r := range name {
		if r < ' ' || r == 0x7f || strings.ContainsRune(` ~^:?*[\`, r) {
			return badBranchNameError(name)
		}
	}
	for part := range strings.SplitSeq(name, "/") {
		if strings.HasPrefix(part, ".") || strings.HasSuffix(part, ".lock") {
			return badBranchNameError(name)
		}
	}
	return nil
}

// branchChecker is the part of Git that the checks before a branch operation need.
type branchChecker interface {
	BranchExists(ctx context.Context, name string) (bool, error)
	HasUncommittedChanges(ctx context.Context) (bool, error)
}

// checkCreateBranch validates the arguments of CreateBranch: a valid new name that is free, and an existing base branch.
func checkCreateBranch(ctx context.Context, g branchChecker, name, from string) error {
	if err := checkBranchName(name); err != nil {
		return err
	}
	if err := checkBranchName(from); err != nil {
		return err
	}
	if exists, err := g.BranchExists(ctx, from); err != nil {
		return err
	} else if !exists {
		return branchNotFoundError(from)
	}
	if exists, err := g.BranchExists(ctx, name); err != nil {
		return err
	} else if exists {
		return branchExistsError(name)
	}
	return nil
}

// checkSwitch validates the arguments of SwitchTo: the branch exists and the working tree is clean.
func checkSwitch(ctx context.Context, g branchChecker, name string) error {
	if err := checkBranchName(name); err != nil {
		return err
	}
	if exists, err := g.BranchExists(ctx, name); err != nil {
		return err
	} else if !exists {
		return branchNotFoundError(name)
	}
	if dirty, err := g.HasUncommittedChanges(ctx); err != nil {
		return err
	} else if dirty {
		return uncommittedChangesError()
	}
	return nil
}

// notFastForwardError is returned by Rebase when the branch is not an ancestor of HEAD. Knope would point the branch
// at HEAD and lose the commits that are only on the branch; remeris refuses (testdata/deviations.md).
func notFastForwardError(to, current string) error {
	return diag.New(fmt.Sprintf("Cannot fast-forward branch %s to %s: %s has commits that are not in %s",
		to, current, to, current)).
		WithCode("remeris::git::not_fast_forward").
		WithHelp(fmt.Sprintf("RebaseBranch only moves %s forward. Bring those commits into %s (git rebase %s or "+
			"git merge %s), then run this step again.", to, current, to, to))
}

// movedButNotSwitchedError is returned by Rebase when the branch was moved but HEAD could not be switched to it.
func movedButNotSwitchedError(to, current string, cause error) error {
	return diag.New(fmt.Sprintf("Branch %s was moved to %s but switching to it failed", to, current)).
		WithCode("remeris::git::switch_after_move").
		WithHelp(fmt.Sprintf("Nothing was lost. Run git switch %s.", to)).
		WithCause(cause)
}

// branchAliasError is returned by Rebase for a branch that is a symbolic reference: moving it would move its target.
func branchAliasError(to, target string) error {
	return diag.New(fmt.Sprintf("Cannot move branch %s: it is a symbolic reference to %s", to, target)).
		WithCode("remeris::git::branch_alias").
		WithHelp("Use the name of the branch it points to.")
}

// worktreeBranchError is the error for a branch that is checked out in another worktree, like git's own.
func worktreeBranchError(name, where string) error {
	return unknownGitError(fmt.Errorf("'%s' is already used by worktree at '%s'", name, where))
}

// rebaseReflogMessage is the reflog message of the branch that Rebase moves.
func rebaseReflogMessage(to, current string) string {
	return fmt.Sprintf("remeris: RebaseBranch fast-forward %s to %s", to, current)
}

// rebaseChecker is the part of Git that the checks before Rebase need.
type rebaseChecker interface {
	branchChecker
	CurrentBranch(ctx context.Context) (string, error)
}

// checkRebase validates the arguments of Rebase and returns the current branch: a valid name, HEAD on a branch, a
// clean working tree and an existing branch to.
func checkRebase(ctx context.Context, g rebaseChecker, to string) (string, error) {
	if err := checkBranchName(to); err != nil {
		return "", err
	}
	current, err := g.CurrentBranch(ctx)
	if err != nil {
		return "", err
	}
	if dirty, err := g.HasUncommittedChanges(ctx); err != nil {
		return "", err
	} else if dirty {
		return "", uncommittedChangesError()
	}
	if exists, err := g.BranchExists(ctx, to); err != nil {
		return "", err
	} else if !exists {
		return "", branchNotFoundError(to)
	}
	return current, nil
}
