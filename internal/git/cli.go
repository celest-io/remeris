package git

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"slices"
	"strings"
	"time"

	"github.com/celest-io/remeris/internal/diag"
)

// CLI is the Git backend that runs the git command (spec F9.0).
type CLI struct {
	// Dir is the working directory passed to git with -C.
	Dir string
	// path is the resolved git executable.
	path string
}

var _ Git = (*CLI)(nil)

// NewCLI returns a CLI backend for dir. It fails when git is not on PATH.
func NewCLI(dir string) (*CLI, error) {
	path, err := exec.LookPath("git")
	if err != nil {
		return nil, fmt.Errorf("git was not found on PATH: %w", err)
	}
	return &CLI{Dir: dir, path: path}, nil
}

// waitDelay bounds how long run waits for git's I/O after a cancelled context.
const waitDelay = 2 * time.Second

// repoLocationVars are the variables that make git act on another repository than Dir. libgit2's
// Repository::open ignores them, so they are dropped to keep the same behaviour.
var repoLocationVars = []string{
	"GIT_DIR", "GIT_WORK_TREE", "GIT_INDEX_FILE", "GIT_OBJECT_DIRECTORY", "GIT_ALTERNATE_OBJECT_DIRECTORIES",
	"GIT_COMMON_DIR", "GIT_NAMESPACE", "GIT_CEILING_DIRECTORIES", "GIT_PREFIX", "GIT_INDEX_VERSION",
}

// gitEnv returns the inherited environment without the repository-location variables, plus the settings
// that make git's output stable. The user's other git settings (config, credentials) are kept.
func gitEnv(environ []string) []string {
	env := make([]string, 0, len(environ)+2)
	for _, kv := range environ {
		name, _, _ := strings.Cut(kv, "=")
		if !slices.Contains(repoLocationVars, name) {
			env = append(env, kv)
		}
	}
	return append(env, "GIT_TERMINAL_PROMPT=0", "LC_ALL=C")
}

// run executes git with args in c.Dir and returns its standard output.
//
// --no-replace-objects keeps refs/replace out of the picture, as libgit2 ignores it.
func (c *CLI) run(ctx context.Context, stdin string, args ...string) (string, error) {
	full := append([]string{"--no-replace-objects", "-C", c.Dir}, args...)
	cmd := exec.CommandContext(ctx, c.path, full...) //nolint:gosec // path comes from LookPath
	cmd.Env = gitEnv(os.Environ())
	cmd.WaitDelay = waitDelay
	if stdin != "" {
		cmd.Stdin = strings.NewReader(stdin)
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		if msg := strings.TrimSpace(stderr.String()); msg != "" {
			err = fmt.Errorf("%s: %w", msg, err)
		}
		return "", err
	}
	return stdout.String(), nil
}

// fail turns a command error into a diagnostic made by wrap, except a context error, which is returned as is.
func fail(err error, wrap func(error) error) error {
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	return wrap(err)
}

func openRepoError(cause error) error {
	return diag.Newf("Could not open Git repository: %w", cause).
		WithCode("remeris::git::open_repo").
		WithHelp("Make sure you are in a Git repository and that you have permission to access it.")
}

func unknownGitError(cause error) error {
	return diag.Newf("Unknown Git error: %w", cause).
		WithCode("remeris::git::command").
		WithHelp("Something went wrong when interacting with Git that we don't have an explanation for. " +
			"Maybe try performing the operation manually?")
}

// AllTagsOnBranch lists the tags reachable from HEAD, newest first (spec F9.1).
//
// Tags are peeled to commits (annotated, lightweight and tags of tags). A tag that does not peel to a
// commit is ignored, like Knope does. Tags on the same commit keep git's reference order (sorted by name).
// A cancelled context returns the context error itself. A repository without commits is an error, as in Knope,
// whose revwalk cannot push an unborn HEAD.
//
// Commands: git rev-parse --git-dir; git for-each-ref --format=%(refname) refs/tags;
// git cat-file --batch-check (stdin: refs/tags/<name>^{commit}); git rev-list HEAD -- (the "--" avoids a clash with a file
// named HEAD).
func (c *CLI) AllTagsOnBranch(ctx context.Context) ([]string, error) {
	if _, err := c.run(ctx, "", "rev-parse", "--git-dir"); err != nil {
		return nil, fail(err, openRepoError)
	}
	byCommit, err := c.tagsByCommit(ctx)
	if err != nil {
		return nil, err
	}

	walk, err := c.run(ctx, "", "rev-list", "HEAD", "--")
	if err != nil {
		return nil, fail(err, unknownGitError)
	}
	tags := make([]string, 0, len(byCommit))
	for _, sha := range strings.Fields(walk) {
		if t, ok := byCommit[sha]; ok {
			tags = append(tags, t...)
			delete(byCommit, sha)
		}
	}
	if len(byCommit) > 0 {
		var skipped []string
		for _, ts := range byCommit {
			skipped = append(skipped, ts...)
		}
		slices.Sort(skipped)
		slog.DebugContext(ctx, "Skipping relevant tags that are not on the current branch: "+strings.Join(skipped, ", "))
	}
	return tags, nil
}

// tagsByCommit maps each commit id to the names of the tags that peel to it, in reference order.
func (c *CLI) tagsByCommit(ctx context.Context) (map[string][]string, error) {
	refs, err := c.run(ctx, "", "for-each-ref", "--format=%(refname)", "refs/tags")
	if err != nil {
		return nil, fail(err, unknownGitError)
	}
	names := strings.Fields(refs)
	byCommit := make(map[string][]string, len(names))
	if len(names) == 0 {
		return byCommit, nil
	}
	var in strings.Builder
	for _, n := range names {
		in.WriteString(n + "^{commit}\n")
	}
	out, err := c.run(ctx, in.String(), "cat-file", "--batch-check")
	if err != nil {
		return nil, fail(err, unknownGitError)
	}
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	if len(lines) != len(names) {
		return nil, unknownGitError(fmt.Errorf("unexpected cat-file output: %d lines for %d tags", len(lines), len(names)))
	}
	for i, line := range lines {
		fields := strings.Fields(line)
		if len(fields) != 3 || fields[1] != "commit" {
			continue // does not peel to a commit
		}
		byCommit[fields[0]] = append(byCommit[fields[0]], strings.TrimPrefix(names[i], "refs/tags/"))
	}
	return byCommit, nil
}
