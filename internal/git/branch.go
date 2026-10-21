package git

import "github.com/celest-io/remeris/internal/diag"

// branchRefPrefix is the namespace of the local branches.
const branchRefPrefix = "refs/heads/"

// notOnBranchError is Knope's git::not_a_branch error (ErrorKind::NotOnAGitBranch), returned when HEAD is detached.
func notOnBranchError() error {
	return diag.New("Not on the tip of a Git branch.").
		WithCode("remeris::git::not_a_branch").
		WithHelp("In order to run this step, you need to be on the very tip of a Git branch.")
}
