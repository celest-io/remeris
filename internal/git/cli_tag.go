package git

import (
	"context"
	"errors"
	"os/exec"
	"strings"
)

// configValue returns the value of a Git configuration key (repository, global and system scopes), and false when
// the key is not set. Like libgit2, an empty value counts as set.
func (c *CLI) configValue(ctx context.Context, key string) (string, bool, error) {
	out, err := c.run(ctx, "", "config", "--null", "--get", key)
	if err != nil {
		var exit *exec.ExitError
		if errors.As(err, &exit) && exit.ExitCode() == 1 {
			return "", false, nil // git config exits with 1 when the key is not set
		}
		return "", false, fail(err, unknownGitError)
	}
	return strings.TrimSuffix(out, "\x00"), true, nil
}

// CreateTag creates an annotated tag with an empty message on HEAD (spec F9.5). Sign is ignored.
//
// The tagger is the identity of user.name and user.email, passed as GIT_COMMITTER_NAME and GIT_COMMITTER_EMAIL so
// that the environment of the caller cannot change it, as with libgit2. tag.gpgSign is switched off for the same reason.
//
// Commands: git config --get user.name; git config --get user.email; git rev-parse --git-dir;
// git tag -a --cleanup=verbatim -m "" name HEAD.
func (c *CLI) CreateTag(ctx context.Context, name string, _ TagOptions) error {
	if _, err := c.run(ctx, "", "rev-parse", "--git-dir"); err != nil {
		return fail(err, openRepoError)
	}
	// libgit2 resolves HEAD before it looks for the committer, so a repository without commits fails first.
	if _, err := c.HeadCommitSHA(ctx); err != nil {
		return err
	}
	var identity [2]string
	for i, key := range []string{configUserName, configUserEmail} {
		value, ok, err := c.configValue(ctx, key)
		if err != nil {
			return err
		}
		if !ok {
			return noCommitterError(missingConfigError(key))
		}
		identity[i] = value
	}
	_, err := c.runEnv(ctx, "", []string{"GIT_COMMITTER_NAME=" + identity[0], "GIT_COMMITTER_EMAIL=" + identity[1]},
		"-c", "tag.gpgSign=false", "tag", "-a", "--cleanup=verbatim", "-m", "", name, "HEAD")
	if err != nil {
		return fail(err, unknownGitError)
	}
	return nil
}

// HeadCommitSHA returns the full id of the commit HEAD points to. A repository without commits is an error.
func (c *CLI) HeadCommitSHA(ctx context.Context) (string, error) {
	if _, err := c.run(ctx, "", "rev-parse", "--git-dir"); err != nil {
		return "", fail(err, openRepoError)
	}
	out, err := c.run(ctx, "", "rev-parse", "--verify", "--quiet", "HEAD^{commit}")
	if err != nil {
		return "", fail(err, unknownGitError)
	}
	return strings.TrimSpace(out), nil
}
