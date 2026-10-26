package steps

import (
	"context"
	"log/slog"
	"os"
	"time"

	"github.com/celest-io/remeris/internal/diag"
	"github.com/celest-io/remeris/internal/git"
	"github.com/celest-io/remeris/internal/workflow"
	"github.com/celest-io/remeris/pkg/versioning"
	"github.com/celest-io/remeris/pkg/versioning/action"
	"github.com/celest-io/remeris/pkg/versioning/changes"
	"github.com/celest-io/remeris/pkg/versioning/changes/conventional"
	"github.com/celest-io/remeris/pkg/versioning/changesets"
	"github.com/celest-io/remeris/pkg/versioning/releasenotes"
	"github.com/celest-io/remeris/pkg/versioning/semver"
)

// Names, codes and texts of PrepareRelease, word for word as Knope has them (spec F13).
const (
	dateLayout = "2006-01-02"

	codeNoPackages = "remeris::no_defined_packages"
	codeNoRelease  = "remeris::releases::no_release"
	codeNeedsForge = "remeris::releases::github_required_for_variable"
	codeReadChange = "remeris::changesets::could_not_read_changeset"

	deprecatedIgnoreWarning = "The `ignore_conventional_commits` option on the PrepareRelease step is deprecated. " +
		"Use `ignore_conventional_commits` in the `[changes]` config section instead. " +
		"Run `remeris --upgrade` to automatically migrate your config."
)

// PrepareRelease updates versions and changelogs from the pending changes (spec F13): for every package it
// collects the changes since the last stable release, calculates the new version, edits the versioned files and
// the changelog, removes the change files and stages all of that in Git. The AddTag and CreateRelease actions are
// left in State.PendingActions for a Release step.
type PrepareRelease struct {
	// PrereleaseLabel makes the new versions pre-releases with this label; "" means stable versions. The
	// --prerelease-label flag sets it on every PrepareRelease step.
	PrereleaseLabel string `toml:"prerelease_label"`
	// AllowEmpty lets a real run succeed when no package is ready to release.
	AllowEmpty bool `toml:"allow_empty"`
	// IgnoreConventionalCommits is deprecated in favour of `[changes]`: commits are not read.
	IgnoreConventionalCommits bool `toml:"ignore_conventional_commits"`
}

// Type returns "PrepareRelease".
func (*PrepareRelease) Type() string { return "PrepareRelease" }

// NeedsPackages is true: the step works on the packages.
func (*PrepareRelease) NeedsPackages() bool { return true }

// Run prepares the release of every package (spec F13).
func (s *PrepareRelease) Run(
	ctx context.Context, rt workflow.RunType[workflow.State],
) (workflow.RunType[workflow.State], error) {
	state := rt.State
	if len(state.Packages) == 0 {
		return rt, errNoPackages()
	}

	forgeVariable := ""
	for _, pkg := range state.Packages {
		if forgeVariable = releasenotes.ForgeVariable(pkg.ReleaseNotes.ChangeTemplates); forgeVariable != "" {
			break
		}
	}
	set, err := loadChangeSet()
	if err != nil {
		return rt, err
	}

	date := Today()
	for _, pkg := range state.Packages {
		list, err := s.gatherChanges(ctx, state, pkg, set)
		if err != nil {
			return rt, err
		}
		if len(list) > 0 && forgeVariable != "" {
			if state.GitHub == nil {
				return rt, diag.Newf("You must configure [github] to use the variable %s in a change template", forgeVariable).
					WithCode(codeNeedsForge)
			}
			enrichGitInfo(ctx, state.GitHub, list, rt.DryRun)
		}

		logImpliedRules(ctx, pkg, list)
		cfg := versioning.ChangeConfig{Force: pkg.TakeOverride(), GoVersioning: pkg.GoVersioning}
		if s.PrereleaseLabel != "" {
			label := s.PrereleaseLabel
			cfg.PrereleaseLabel = &label
		}
		files, actions, err := pkg.ApplyChanges(list, state.AllVersionedFiles, cfg, date)
		if err != nil {
			return rt, err
		}
		state.AllVersionedFiles = files
		state.PendingActions = append(state.PendingActions, actions...)
	}

	var all []action.Action
	for _, f := range state.AllVersionedFiles {
		all = append(all, f.Write()...)
	}
	state.AllVersionedFiles = nil
	all = action.Dedupe(append(all, state.PendingActions...))
	if state.PendingActions, err = ExecuteAndStage(ctx, state.Git, rt.DryRun, all); err != nil {
		return rt, err
	}
	rt.State = state

	if !rt.DryRun && !s.AllowEmpty && len(state.PendingActions) == 0 {
		return rt, diag.New("No packages are ready to release").
			WithCode(codeNoRelease).
			WithHelp("The `PrepareRelease` step will not complete if no changes cause a package's version to be increased.")
	}
	return rt, nil
}

func errNoPackages() error {
	return diag.New("No packages to operate on").
		WithCode(codeNoPackages).
		WithHelp("There must be at least one package for remeris to work with, " +
			"no supported package files were found in this directory.")
}

// Today returns the date of a release: today's date in UTC as YYYY-MM-DD, like Knope. It is a variable so that
// tests can fix the date; the golden tests set it from REMERIS_FAKE_DATE in cmd/remeris/main_test.go, and the
// binary itself reads no such variable.
var Today = func() string { return time.Now().UTC().Format(dateLayout) }

// loadChangeSet reads ".changeset/" when it exists. A file that cannot be read or parsed fails the step.
func loadChangeSet() (changesets.ChangeSet, error) {
	if info, err := os.Stat(changesets.Dir); err != nil || !info.IsDir() {
		return changesets.ChangeSet{}, nil
	}
	set, err := changesets.FromFS(os.DirFS(changesets.Dir))
	if err != nil {
		return changesets.ChangeSet{}, diag.New(err.Error()).
			WithCode(codeReadChange).
			WithHelp("This could be a file-system issue or a problem with the formatting of a change file.").
			WithCause(err)
	}
	return set, nil
}

// gatherChanges is spec F13.1: the changes of the commits since the last stable tag of the package, followed by
// the changes of its change files.
func (s *PrepareRelease) gatherChanges(
	ctx context.Context, state workflow.State, pkg *versioning.Package, set changesets.ChangeSet,
) ([]changes.Change, error) {
	if s.IgnoreConventionalCommits {
		slog.WarnContext(ctx, deprecatedIgnoreWarning)
	}
	var list []changes.Change
	if !s.IgnoreConventionalCommits && !state.IgnoreConventionalCommits {
		commits, err := commitsSinceLastStable(ctx, state, pkg)
		if err != nil {
			return nil, err
		}
		sections := pkg.ReleaseNotes.Sections
		if sections == nil {
			sections = releasenotes.DefaultSections()
		}
		list = conventional.Changes(commits, pkg.Scopes, sections)
	}
	return append(list, changeFileChanges(ctx, state.Git, pkg, set)...), nil
}

// commitsSinceLastStable lists the commits after the last stable tag of the package, or all of them when it has
// none (spec F9.2, F13.2).
func commitsSinceLastStable(
	ctx context.Context, state workflow.State, pkg *versioning.Package,
) ([]conventional.CommitMessage, error) {
	name := ""
	if custom := pkg.Name.Custom(); custom != nil {
		name = *custom
	}
	slog.DebugContext(ctx, "Getting conventional commits since last release of package "+name)
	var tag *string
	if v, ok := semver.FromTags(pkg.Name.Custom(), state.AllGitTags, nil).LastStable(); ok {
		t := pkg.ReleaseTag(v)
		tag = &t
	}
	if state.Git == nil {
		return nil, errNoGit
	}
	found, err := state.Git.CommitMessagesAfterTag(ctx, tag)
	if err != nil {
		return nil, err
	}
	out := make([]conventional.CommitMessage, 0, len(found))
	for _, c := range found {
		// Knope drops a commit with more than one parent, whatever its message says.
		if state.IgnoreConventionalMergeCommits && c.IsMerge() {
			continue
		}
		out = append(out, conventional.CommitMessage{Message: c.Message, Info: gitInfo(c.Info)})
	}
	return out, nil
}

// changeFileChanges is the changes of the package's change files, sorted by id, each with the first commit that
// has the file (spec F11.2).
func changeFileChanges(ctx context.Context, g git.Git, pkg *versioning.Package, set changesets.ChangeSet) []changes.Change {
	name := pkg.Name.String()
	var paths []string
	for _, release := range set.Releases {
		if release.Package != name {
			continue
		}
		for _, c := range release.Changes {
			paths = append(paths, changesets.Path(c.ID))
		}
	}
	infos := map[string]*changes.GitInfo{}
	if g != nil && len(paths) > 0 {
		for path, info := range g.FirstCommitsForFiles(ctx, paths) {
			infos[path] = gitInfo(info)
		}
	}
	return changesets.PackageChanges(set, name, infos)
}

func gitInfo(info *git.CommitInfo) *changes.GitInfo {
	if info == nil {
		return nil
	}
	return &changes.GitInfo{Hash: info.Hash, AuthorName: info.AuthorName}
}

// logImpliedRules prints, in verbose mode, the rule each change implies (spec F10.2).
func logImpliedRules(ctx context.Context, pkg *versioning.Package, list []changes.Change) {
	if len(list) == 0 {
		return
	}
	slog.DebugContext(ctx, "Determining new version for "+pkg.Name.String())
	for _, c := range list {
		slog.DebugContext(ctx, c.Origin.String()+"\n\timplies rule "+c.Type.Rule().String())
	}
}
