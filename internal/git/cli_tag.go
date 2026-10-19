package git

import (
	"context"
	"errors"
	"os/exec"
	"strings"
)

// configValue returns the value of a Git configuration key (repository, global and system scopes), and false when
// the key is not set.
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
// git show-ref --verify --quiet refs/tags/name; git tag -a --cleanup=verbatim -m "" -- name HEAD (the "--" keeps a name
// such as --force from being an option). GIT_COMMITTER_DATE is dropped so that the tag gets the current time.
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
		value = strings.TrimSpace(value) // git and libgit2 strip the surrounding whitespace of an identity
		if !ok || value == "" {
			// An empty name or email is no identity either: libgit2 refuses it, and so does the go-git backend.
			return noCommitterError(missingConfigError(key))
		}
		identity[i] = value
	}
	exists, err := c.tagExists(ctx, name)
	if err != nil {
		return err
	}
	if exists {
		return unknownGitError(tagExistsError(name)) // the same text as the go-git backend, and no TAG_EDITMSG left behind
	}
	_, err = c.runEnv(ctx, "", []string{"GIT_COMMITTER_NAME=" + identity[0], "GIT_COMMITTER_EMAIL=" + identity[1]},
		[]string{"GIT_COMMITTER_DATE"},
		"-c", "tag.gpgSign=false", "tag", "-a", "--cleanup=verbatim", "-m", "", "--", name, headRef)
	if err != nil {
		return fail(err, unknownGitError)
	}
	return nil
}

// tagExists reports whether refs/tags/name exists.
func (c *CLI) tagExists(ctx context.Context, name string) (bool, error) {
	_, err := c.run(ctx, "", "show-ref", "--verify", "--quiet", "refs/tags/"+name)
	if err == nil {
		return true, nil
	}
	var exit *exec.ExitError
	if errors.As(err, &exit) && exit.ExitCode() == 1 {
		return false, nil
	}
	return false, fail(err, unknownGitError)
}

// HeadCommitSHA returns the full id of the commit HEAD points to. A repository without commits is an error.
func (c *CLI) HeadCommitSHA(ctx context.Context) (string, error) {
	if _, err := c.run(ctx, "", "rev-parse", "--git-dir"); err != nil {
		return "", fail(err, openRepoError)
	}
	out, err := c.run(ctx, "", "rev-parse", "--verify", "--quiet", "HEAD^{commit}")
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return "", ctxErr
		}
		// Name the reference as libgit2 does: the branch HEAD points to, or HEAD itself.
		ref := headRef
		if target, serr := c.run(ctx, "", "symbolic-ref", "-q", headRef); serr == nil && strings.TrimSpace(target) != "" {
			ref = strings.TrimSpace(target)
		}
		return "", fail(err, func(error) error { return unknownGitError(refNotFoundError(ref)) })
	}
	return strings.TrimSpace(out), nil
}
