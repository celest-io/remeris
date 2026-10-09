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
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

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

// CommitMessagesAfterTag returns the commits reachable from HEAD but not from the tag, oldest first (spec F9.2).
// A nil tag, or a tag that is missing or does not peel to a commit, selects every commit. Commits whose message is
// not valid UTF-8 are skipped, and a commit whose author name is not valid UTF-8 has no Info, like in Knope.
//
// Commands: git show-ref --verify --quiet refs/tags/<tag>; git rev-parse --verify --quiet refs/tags/<tag>^{commit};
// git rev-list --reverse refs/tags/<tag>..HEAD --, whose order is the date-ordered walk of libgit2's revwalk;
// git cat-file --batch for the raw commit objects. git log is not used because it re-encodes the message and the
// author name to UTF-8 using the commit's encoding header, while Knope (libgit2) reads the stored bytes.
func (c *CLI) CommitMessagesAfterTag(ctx context.Context, tag *string) ([]Commit, error) {
	rng := "HEAD"
	if tag == nil {
		slog.DebugContext(ctx, "Using ALL commits")
	} else {
		ref := "refs/tags/" + *tag
		if _, err := c.run(ctx, "", "show-ref", "--verify", "--quiet", ref); err != nil {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			slog.DebugContext(ctx, "Tag "+*tag+" not found, using ALL commits")
		} else {
			// Knope logs this even when the tag does not peel to a commit, and then uses every commit.
			slog.DebugContext(ctx, "Using commits since tag "+*tag)
			if _, err := c.run(ctx, "", "rev-parse", "--verify", "--quiet", ref+"^{commit}"); err == nil {
				rng = ref + "..HEAD"
			} else if ctx.Err() != nil {
				return nil, ctx.Err()
			}
		}
	}
	out, err := c.run(ctx, "", "rev-list", "--reverse", rng, "--")
	if err != nil {
		return nil, unknownGitError(err)
	}
	raws, err := c.rawCommits(ctx, strings.Fields(out))
	if err != nil {
		return nil, err
	}
	commits := make([]Commit, 0, len(raws))
	for _, raw := range raws {
		if utf8.ValidString(raw.message) {
			commits = append(commits, Commit{Message: raw.message, Info: commitInfoFor(raw.hash, raw.author)})
		}
	}
	return commits, nil
}

// rawCommit is a commit as stored: the message and author name are the stored bytes, not re-encoded.
type rawCommit struct {
	hash, author, message string
}

// rawCommits reads the commit objects of hashes with git cat-file --batch, in the order given.
func (c *CLI) rawCommits(ctx context.Context, hashes []string) ([]rawCommit, error) {
	if len(hashes) == 0 {
		return nil, nil
	}
	out, err := c.run(ctx, strings.Join(hashes, "\n")+"\n", "cat-file", "--batch")
	if err != nil {
		return nil, unknownGitError(err)
	}
	raws := make([]rawCommit, 0, len(hashes))
	for range hashes {
		header, rest, ok := strings.Cut(out, "\n")
		fields := strings.Fields(header)
		if !ok || len(fields) != 3 || fields[1] != "commit" {
			return nil, unknownGitError(fmt.Errorf("unexpected cat-file header %q", header))
		}
		size, err := strconv.Atoi(fields[2])
		if err != nil || size < 0 || size >= len(rest) {
			return nil, unknownGitError(fmt.Errorf("unexpected cat-file header %q", header))
		}
		raws = append(raws, parseRawCommit(fields[0], rest[:size]))
		out = rest[size+1:] // skip the newline that ends the object
	}
	return raws, nil
}

// parseRawCommit splits a raw commit object into its author name and message. The headers end at the first empty
// line; continuation lines (gpgsig, mergetag) start with a space and never match.
func parseRawCommit(hash, content string) rawCommit {
	headers, message, _ := strings.Cut(content, "\n\n")
	raw := rawCommit{hash: hash, message: message}
	for line := range strings.SplitSeq(headers, "\n") {
		if rest, ok := strings.CutPrefix(line, "author "); ok {
			name, _, _ := strings.Cut(rest, "<")
			raw.author = strings.TrimSpace(name)
			break
		}
	}
	return raw
}

// FirstCommitsForFiles finds, for each path, the first commit whose tree contains it (spec F9.3).
//
// The oldest commit whose tree has a path always adds it relative to every parent, so one
// git log --topo-order --reverse -m --root --diff-filter=A over the paths lists it first. Merges are diffed
// against each parent (-m) to catch a file that only a merge brings in. Paths are literal, not patterns, and
// a directory is never found (see Git.FirstCommitsForFiles). Errors are swallowed.
func (c *CLI) FirstCommitsForFiles(ctx context.Context, paths []string) map[string]*CommitInfo {
	results := make(map[string]*CommitInfo, len(paths))
	if len(paths) == 0 {
		return results
	}
	args := []string{"--literal-pathspecs", "log", "--topo-order", "--reverse", "-m", "--root", "--no-renames",
		"--full-history", "--diff-filter=A", "--name-status", "-z", "--format=%x01%H", "HEAD", "--"}
	out, err := c.run(ctx, "", append(args, paths...)...)
	if err != nil {
		return map[string]*CommitInfo{}
	}
	wanted := make(map[string]bool, len(paths))
	for _, p := range paths {
		wanted[p] = true
	}
	firstHash := make(map[string]string, len(paths))
	var hashes []string
	for _, chunk := range strings.Split(out, "\x01")[1:] {
		fields := strings.Split(chunk, "\x00")
		for rest := fields[1:]; len(rest) >= 2; rest = rest[2:] {
			if path := rest[1]; wanted[path] && firstHash[path] == "" {
				firstHash[path] = fields[0]
				if !slices.Contains(hashes, fields[0]) {
					hashes = append(hashes, fields[0])
				}
			}
		}
	}
	raws, err := c.rawCommits(ctx, hashes)
	if err != nil {
		return map[string]*CommitInfo{}
	}
	infos := make(map[string]*CommitInfo, len(raws))
	for _, raw := range raws {
		infos[raw.hash] = commitInfoFor(raw.hash, raw.author)
	}
	for path, hash := range firstHash {
		if info := infos[hash]; info != nil {
			results[path] = info
		}
	}
	return results
}
