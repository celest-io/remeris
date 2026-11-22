package git

import (
	"context"
	"slices"
	"strings"

	"github.com/celest-io/remeris/internal/diag"
)

var _ Committer = (*CLI)(nil)

// codeCommit is the code of a failed commit.
const (
	codeCommit   = "remeris::git::commit"
	literalPaths = "--literal-pathspecs"
	gitCommit    = "commit"
)

// Changes lists the changes with git status --porcelain=v1 -z --untracked-files=all, which is relative to the root of
// the working tree whatever the directory is, and leaves out ignored files.
func (c *CLI) Changes(ctx context.Context) ([]Change, error) {
	out, err := c.run(ctx, "", "status", "--porcelain=v1", "-z", "--untracked-files=all")
	if err != nil {
		return nil, fail(err, openRepoError)
	}
	return parsePorcelain(out), nil
}

// parsePorcelain reads "XY path" records separated by NUL. A rename or copy is followed by its old path in a record of
// its own: the old path of a rename is deleted, the old path of a copy is not.
func parsePorcelain(out string) []Change {
	records := strings.Split(out, "\x00")
	byPath := map[string]Change{}
	for i := 0; i < len(records); i++ {
		rec := records[i]
		if len(rec) < 4 { //nolint:mnd // "XY " and at least one character of the path
			continue
		}
		index, tree, path := rec[0], rec[1], rec[3:]
		switch {
		case index == 'R' || index == 'C' || tree == 'R' || tree == 'C':
			if i+1 < len(records) {
				i++
				if index == 'R' || tree == 'R' {
					byPath[records[i]] = Change{Path: records[i], Deleted: true}
				}
			}
			byPath[path] = Change{Path: path}
		case index == 'D' && tree == ' ', tree == 'D' && index != 'A':
			byPath[path] = Change{Path: path, Deleted: true}
		case index == 'A' && tree == 'D':
			// Added to the index and deleted again: there is nothing to commit.
		default:
			byPath[path] = Change{Path: path}
		}
	}
	changes := make([]Change, 0, len(byPath))
	for _, ch := range byPath {
		changes = append(changes, ch)
	}
	slices.SortFunc(changes, func(a, b Change) int { return strings.Compare(a.Path, b.Path) })
	return changes
}

// Commit stages opts.Paths with AddFiles and then commits only those paths with
// git commit --only, which leaves any other staged change staged and out of the commit. The paths are
// passed on stdin as literal NUL-separated pathspecs, so a name is never an option or a pattern.
func (c *CLI) Commit(ctx context.Context, message string, opts CommitOptions) (string, error) {
	if len(opts.Paths) == 0 {
		return "", noChangesError()
	}
	if err := c.requireChanges(ctx, opts.Paths); err != nil {
		return "", err
	}
	if err := c.AddFiles(ctx, opts.Paths); err != nil {
		return "", err
	}
	root, err := c.run(ctx, "", "rev-parse", "--show-toplevel")
	if err != nil {
		return "", fail(err, openRepoError)
	}
	top := &CLI{Dir: strings.TrimRight(root, "\n"), path: c.path}

	args := []string{literalPaths, gitCommit, "--quiet", "--only"}
	args = append(args, "-m", message, "--pathspec-from-file=-", "--pathspec-file-nul")
	if _, err := top.run(ctx, strings.Join(opts.Paths, "\x00")+"\x00", args...); err != nil {
		return "", fail(err, func(cause error) error { return commitError(cause) })
	}
	sha, err := top.run(ctx, "", "rev-parse", "HEAD")
	if err != nil {
		return "", fail(err, unknownGitError)
	}
	return strings.TrimSpace(sha), nil
}

// requireChanges fails with "Nothing to commit" when no change is under paths: git commit --only would fail too,
// but with a message on its standard output that the error would not carry.
func (c *CLI) requireChanges(ctx context.Context, paths []string) error {
	changes, err := c.Changes(ctx)
	if err != nil {
		return err
	}
	for _, ch := range changes {
		if underAny(ch.Path, paths) {
			return nil
		}
	}
	return noChangesError()
}

// commitError reports a failed git commit.
func commitError(cause error) error {
	return diag.Newf("Could not commit: %w", cause).
		WithCode(codeCommit).
		WithHelp("Try the same commit by hand with git to see why it failed.")
}

func noChangesError() error {
	return diag.New("Nothing to commit").
		WithCode(codeCommit).
		WithHelp("None of the selected files differ from HEAD.")
}
