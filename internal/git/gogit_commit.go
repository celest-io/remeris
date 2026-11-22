package git

import (
	"context"
	"slices"
	"strings"

	gogit "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/config"

	"github.com/celest-io/remeris/internal/diag"
)

var _ Committer = (*GoGit)(nil)

// SigningConfig reads the same keys as the CLI backend from the merged system, global and local configuration.
func (g *GoGit) SigningConfig(ctx context.Context) (SigningConfig, error) {
	if err := ctx.Err(); err != nil {
		return SigningConfig{}, err
	}
	repo, err := g.open()
	if err != nil {
		return SigningConfig{}, err
	}
	cfg, err := repo.ConfigScoped(config.SystemScope)
	if err != nil {
		return SigningConfig{}, unknownGitError(err)
	}
	raw := cfg.Raw
	out := SigningConfig{
		GPGSign: isTrue(raw.Section("commit").Option("gpgsign")),
		Key:     raw.Section("user").Option("signingkey"),
		Format:  raw.Section("gpg").Option("format"),
	}
	switch out.Format {
	case formatSSH:
		out.Program = raw.Section("gpg").Subsection("ssh").Option("program")
	case "x509":
		out.Program = raw.Section("gpg").Subsection("x509").Option("program")
	default:
		out.Program = raw.Section("gpg").Option("program")
	}
	return out, nil
}

// Changes lists what differs from HEAD from go-git's worktree status, which leaves ignored files out. go-git does not
// detect renames, so a rename is a deleted path and a new one.
func (g *GoGit) Changes(ctx context.Context) ([]Change, error) {
	status, err := g.status(ctx)
	if err != nil {
		return nil, err
	}
	var changes []Change
	for name, st := range status {
		switch {
		case st.Staging == gogit.Unmodified && st.Worktree == gogit.Unmodified:
		case st.Worktree == gogit.Deleted || (st.Staging == gogit.Deleted && st.Worktree == gogit.Unmodified):
			changes = append(changes, Change{Path: name, Deleted: true})
		case st.Staging == gogit.Added && st.Worktree == gogit.Deleted:
		default:
			changes = append(changes, Change{Path: name})
		}
	}
	slices.SortFunc(changes, func(a, b Change) int { return strings.Compare(a.Path, b.Path) })
	return changes, nil
}

func (g *GoGit) status(ctx context.Context) (gogit.Status, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	repo, err := g.open()
	if err != nil {
		return nil, err
	}
	wt, err := repo.Worktree()
	if err != nil {
		return nil, openRepoError(err)
	}
	status, err := wt.Status()
	if err != nil {
		return nil, unknownGitError(err)
	}
	return status, nil
}

// Commit stages opts.Paths and commits what is staged. Unlike git commit --only, go-git cannot leave other staged
// changes out of the commit, so it refuses to commit when some exist: that needs the git command.
func (g *GoGit) Commit(ctx context.Context, message string, opts CommitOptions) (string, error) {
	if len(opts.Paths) == 0 {
		return "", noChangesError()
	}
	if err := g.AddFiles(ctx, opts.Paths); err != nil {
		return "", err
	}
	status, err := g.status(ctx)
	if err != nil {
		return "", err
	}
	roots := make([]string, len(opts.Paths))
	copy(roots, opts.Paths)
	staged := 0
	for name, st := range status {
		if st.Staging == gogit.Unmodified || st.Staging == gogit.Untracked {
			continue
		}
		if !underAny(name, roots) {
			return "", diag.Newf("%s is staged but not selected for the commit", name).
				WithCode(codeCommit).
				WithHelp("The built-in Git support commits everything that is staged. Unstage the file, " +
					"or install the git command so that only the selected files are committed.")
		}
		staged++
	}
	if staged == 0 {
		return "", noChangesError()
	}
	repo, err := g.open()
	if err != nil {
		return "", err
	}
	wt, err := repo.Worktree()
	if err != nil {
		return "", openRepoError(err)
	}
	hash, err := wt.Commit(message, &gogit.CommitOptions{})
	if err != nil {
		return "", diag.Newf("Could not commit: %w", err).
			WithCode(codeCommit).
			WithHelp("Check that user.name and user.email are set in the Git configuration.")
	}
	return hash.String(), nil
}

// isTrue reads a Git boolean the way git config --type=bool does for its true values.
func isTrue(v string) bool {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "true", "yes", "on", "1":
		return true
	}
	return false
}
