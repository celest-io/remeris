package git

import (
	"context"
	"errors"
	"slices"
	"strings"

	"github.com/celest-io/remeris/internal/diag"
)

var _ Committer = (*CLI)(nil)

// codeCommit is the code of a failed commit.
const (
	codeCommit   = "remeris::git::commit"
	literalPaths = "--literal-pathspecs"
	formatSSH    = "ssh"
	gitCommit    = "commit"
)

// SigningConfig reads commit.gpgsign, user.signingkey, gpg.format and the program of that format with
// git config --get, which applies the system, global and local files in git's own order. A missing key is not an
// error: git config exits with 1 for it.
func (c *CLI) SigningConfig(ctx context.Context) (SigningConfig, error) {
	get := func(key string, extra ...string) (string, error) {
		args := append([]string{"config", "--get"}, extra...)
		out, err := c.run(ctx, "", append(args, key)...)
		if err != nil {
			var ee interface{ ExitCode() int }
			if errors.As(err, &ee) && ee.ExitCode() == 1 {
				return "", nil
			}
			return "", fail(err, unknownGitError)
		}
		return strings.TrimSpace(out), nil
	}

	var cfg SigningConfig
	gpgsign, err := get("commit.gpgsign", "--type=bool")
	if err != nil {
		return cfg, err
	}
	cfg.GPGSign = gpgsign == "true"
	if cfg.Key, err = get("user.signingkey"); err != nil {
		return cfg, err
	}
	if cfg.Format, err = get("gpg.format"); err != nil {
		return cfg, err
	}
	programKey := "gpg.program"
	switch cfg.Format {
	case formatSSH:
		programKey = "gpg.ssh.program"
	case "x509":
		programKey = "gpg.x509.program"
	}
	if cfg.Program, err = get(programKey); err != nil {
		return cfg, err
	}
	return cfg, nil
}

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
	switch {
	case opts.Sign && opts.NoSign:
		return "", diag.New("A commit cannot be both signed and unsigned").WithCode(codeCommit)
	case opts.Sign:
		args = append(args, "--gpg-sign")
	case opts.NoSign:
		args = append(args, "--no-gpg-sign")
	}
	args = append(args, "-m", message, "--pathspec-from-file=-", "--pathspec-file-nul")
	if _, err := top.run(ctx, strings.Join(opts.Paths, "\x00")+"\x00", args...); err != nil {
		return "", fail(err, func(cause error) error { return commitError(cause, opts.Sign) })
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

// commitError reports a failed git commit. A signing failure, which git reports through its own message, is
// told apart so the caller can show how to set signing up.
func commitError(cause error, signed bool) error {
	e := diag.Newf("Could not commit: %w", cause).WithCode(codeCommit)
	if signed {
		return e.WithHelp("Git could not sign the commit. Check user.signingkey, gpg.format and the signing program, " +
			"and try \"git commit -S\" by hand.")
	}
	return e.WithHelp("Try the same commit by hand with git to see why it failed.")
}

func noChangesError() error {
	return diag.New("Nothing to commit").
		WithCode(codeCommit).
		WithHelp("None of the selected files differ from HEAD.")
}

// Root is git rev-parse --show-toplevel.
func (c *CLI) Root(ctx context.Context) (string, error) {
	out, err := c.run(ctx, "", "rev-parse", "--show-toplevel")
	if err != nil {
		return "", fail(err, openRepoError)
	}
	return strings.TrimRight(out, "\n"), nil
}

// AdoptCommit fetches branch from its remote and soft-resets to sha, which must be the tip it fetched. The index and
// the working tree stay as they are, so the files that were committed through the API (and staged by the caller)
// are not changed. The remote is branch.<name>.remote, or origin. branch and sha are checked: a name that is not a
// valid branch name or an id that is not 40 hexadecimal digits never reaches git.
func (c *CLI) AdoptCommit(ctx context.Context, branch, sha string) error {
	if !validSHA(sha) {
		return unknownGitError(errors.New("not a commit id: " + sha))
	}
	if _, err := c.BranchExists(ctx, branch); err != nil {
		return err
	}
	remote := "origin"
	if out, err := c.run(ctx, "", "config", "--get", "branch."+branch+".remote"); err == nil && strings.TrimSpace(out) != "" {
		remote = strings.TrimSpace(out)
	}
	if strings.HasPrefix(remote, "-") {
		return unknownGitError(errors.New("bad remote name"))
	}
	if _, err := c.run(ctx, "", "fetch", "--quiet", remote, "--", "refs/heads/"+branch); err != nil {
		return fail(err, unknownGitError)
	}
	if _, err := c.run(ctx, "", "reset", "--soft", "--quiet", sha, "--"); err != nil {
		return fail(err, unknownGitError)
	}
	return nil
}

// validSHA reports whether s is a full SHA-1 or SHA-256 commit id.
func validSHA(s string) bool {
	if len(s) != 40 && len(s) != 64 { //nolint:mnd // the length of a SHA-1 and of a SHA-256 id
		return false
	}
	for _, r := range s {
		if (r < '0' || r > '9') && (r < 'a' || r > 'f') {
			return false
		}
	}
	return true
}
