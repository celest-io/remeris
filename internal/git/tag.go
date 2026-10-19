package git

import (
	"fmt"

	"github.com/celest-io/remeris/internal/diag"
)

// Names of the Git configuration keys that make up the committer, as libgit2's repo.signature() reads them.
const (
	configUserName  = "user.name"
	configUserEmail = "user.email"
)

// noCommitterError is Knope's git::no_committer error. cause is libgit2's error for the missing key.
func noCommitterError(cause error) error {
	return diag.Newf("Could not determine Git committer to commit changes: %w", cause).
		WithCode("remeris::git::no_committer").
		WithHelp("We couldn't determine who to commit the changes as. Please set the `user.name` and " +
			"`user.email` Git config options.")
}

// missingConfigError is libgit2's error for a configuration key that is not set.
func missingConfigError(key string) error {
	return fmt.Errorf("config value '%s' was not found", key)
}

// tagExistsError is libgit2's error for a tag that already exists.
func tagExistsError(name string) error {
	return fmt.Errorf("tag '%s' already exists", name)
}
