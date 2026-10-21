package steps

import (
	"context"
	"log/slog"

	"github.com/celest-io/remeris/internal/git"
	"github.com/celest-io/remeris/internal/workflow"
	"github.com/celest-io/remeris/pkg/versioning/action"
)

// Release tags and publishes the prepared release (spec F14). It executes the AddTag and CreateRelease actions
// that PrepareRelease left in State.PendingActions. When there are none, it looks for a release that an earlier
// workflow prepared but did not publish: a package whose version is newer than its last tag.
//
// A release becomes a Git tag, unless [github] or [gitea] is configured; publishing on a forge arrives in
// milestones M7 and M8, and the step fails with "not implemented" until then, in a dry run too.
type Release struct{}

// Type returns "Release".
func (*Release) Type() string { return "Release" }

// Run creates the tags and releases of the pending actions (spec F14). Afterwards State.PendingActions is empty.
func (s *Release) Run(
	ctx context.Context, rt workflow.RunType[workflow.State],
) (workflow.RunType[workflow.State], error) {
	state := rt.State
	if len(state.PendingActions) == 0 {
		state.PendingActions = findPreparedReleases(ctx, &state)
	}
	pending := state.PendingActions
	state.PendingActions = nil
	rt.State = state

	for _, a := range pending {
		switch a.Kind {
		case action.KindAddTag:
			if isPackageTag(&rt.State, a.Tag) {
				continue // the CreateRelease of that package creates the tag
			}
			if err := createTag(ctx, &rt, a.Tag); err != nil {
				return rt, err
			}
		case action.KindCreateRelease:
			tag := action.ReleaseTag(a.Release.Version, a.Release.PackageName)
			switch {
			case rt.State.GitHub != nil:
				return rt, notImplemented(s, "M7")
			case rt.State.GiteaConfigured:
				return rt, notImplemented(s, "M8")
			}
			if err := createTag(ctx, &rt, tag); err != nil {
				return rt, err
			}
		case action.KindWriteToFile, action.KindRemoveFile:
			continue
		}
	}
	return rt, nil
}

// findPreparedReleases is the no-PrepareRelease path: for every package whose current version has no tag yet, a
// CreateRelease and the extra tags of its versioned files (the module tag of a go.mod in a subdirectory).
func findPreparedReleases(ctx context.Context, state *workflow.State) []action.Action {
	var actions []action.Action
	for _, pkg := range state.Packages {
		if _, ok := pkg.LatestVersion(); !ok {
			continue
		}
		slog.DebugContext(ctx, "Searching for last package tag to determine if there's a release to release")
		if last := pkg.LastTagOf(state.AllGitTags); last != nil {
			slog.DebugContext(ctx, "Last tag is "+last.String())
		}
		pr, ok := pkg.FindPreparedRelease(state.AllGitTags)
		if !ok {
			continue
		}
		if !pr.FromChangelog {
			slog.DebugContext(ctx,
				"Release has previously been prepared without a changelog, will create a release with no notes.")
		}
		actions = append(actions, pkg.PreparedReleaseActions(pr.Release, state.AllVersionedFiles)...)
	}
	return actions
}

func isPackageTag(state *workflow.State, tag string) bool {
	for _, pkg := range state.Packages {
		if pkg.IsReleaseTag(tag) {
			return true
		}
	}
	return false
}

// createTag creates tag on HEAD. A dry run only logs. Like Knope, it opens the repository itself when the
// state has none (no package, or opening failed when the project was loaded).
func createTag(ctx context.Context, rt *workflow.RunType[workflow.State], tag string) error {
	if rt.DryRun {
		slog.InfoContext(ctx, "Would create Git tag "+tag)
		return nil
	}
	if rt.State.Git == nil {
		g, err := git.Open(ctx, ".")
		if err != nil {
			return err
		}
		rt.State.Git = g
	}
	return rt.State.Git.CreateTag(ctx, tag, git.TagOptions{})
}
