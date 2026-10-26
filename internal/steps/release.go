package steps

import (
	"context"
	"log/slog"
	"strings"

	"github.com/celest-io/remeris/internal/forge/gitea"
	"github.com/celest-io/remeris/internal/forge/github"
	"github.com/celest-io/remeris/internal/git"
	"github.com/celest-io/remeris/internal/workflow"
	"github.com/celest-io/remeris/pkg/versioning"
	"github.com/celest-io/remeris/pkg/versioning/action"
	"github.com/celest-io/remeris/pkg/versioning/trace"
)

// Release tags and publishes the prepared release (spec F14). It executes the AddTag and CreateRelease actions
// that PrepareRelease left in State.PendingActions. When there are none, it looks for a release that an earlier
// workflow prepared but did not publish: a package whose version is newer than its last tag.
//
// A release becomes a Git tag, unless [github] or [gitea] is configured: then the forge creates the tag, and no
// local tag is made. With both configured, each release is made on GitHub first, then on Gitea, as in Knope, and
// the first failure stops everything after it: nothing is rolled back.
type Release struct{}

// Type returns "Release".
func (*Release) Type() string { return "Release" }

// NeedsPackages is true: the step works on the packages.
func (*Release) NeedsPackages() bool { return true }

// Run creates the tags and releases of the pending actions (spec F14). Afterwards State.PendingActions is empty.
func (*Release) Run(
	ctx context.Context, rt workflow.RunType[workflow.State],
) (workflow.RunType[workflow.State], error) {
	state := rt.State
	if len(state.PendingActions) == 0 {
		state.PendingActions = findPreparedReleases(ctx, &state)
	}
	pending := state.PendingActions
	state.PendingActions = nil
	rt.State = state

	var head headSHA
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
			if rt.State.GitHub != nil {
				if err := releaseOnGitHub(ctx, &rt, a.Release, tag, &head); err != nil {
					return rt, err
				}
			}
			if rt.State.Gitea != nil {
				if err := releaseOnGitea(ctx, &rt, a.Release, tag, &head); err != nil {
					return rt, err
				}
			}
			if rt.State.GitHub != nil || rt.State.Gitea != nil {
				continue
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
		pr, ok := pkg.FindPreparedRelease(state.AllGitTags, trace.New(slog.Default()))
		if !ok {
			continue
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
	g, err := ensureGit(ctx, &rt.State)
	if err != nil {
		return err
	}
	return g.CreateTag(ctx, tag, git.TagOptions{})
}

// headSHA reads the commit HEAD points to once per step run.
type headSHA struct {
	value string
	read  bool
}

// get returns the SHA, or "" when it cannot be read: like Knope, a failure is not an error, and the release then
// has no target_commitish. Reading Git is allowed in a dry run.
func (h *headSHA) get(ctx context.Context, state *workflow.State) string {
	if h.read {
		return h.value
	}
	h.read = true
	g, err := ensureGit(ctx, state)
	if err != nil {
		slog.DebugContext(ctx, "Could not open the Git repository: "+err.Error())
		return ""
	}
	sha, err := g.HeadCommitSHA(ctx)
	if err != nil {
		slog.DebugContext(ctx, "Could not read the HEAD commit: "+err.Error())
		return ""
	}
	h.value = sha
	return sha
}

// releaseName is the title of the release: "{package} {title}" for a named package, else the title.
func releaseName(rel *action.Release) string {
	if rel.PackageName != nil {
		return *rel.PackageName + " " + rel.Title
	}
	return rel.Title
}

// releaseOnGitHub creates the GitHub release of rel (spec F16.2). A dry run logs what would happen: it builds no
// client, so it neither reads the credential store nor touches the network.
func releaseOnGitHub(
	ctx context.Context, rt *workflow.RunType[workflow.State], rel *action.Release, tag string, head *headSHA,
) error {
	in := github.ReleaseInput{
		TagName:         tag,
		Name:            releaseName(rel),
		Body:            strings.TrimSpace(rel.Notes),
		Prerelease:      rel.Version.IsPrerelease(),
		TargetCommitish: head.get(ctx, &rt.State),
	}
	assets := rt.State.AssetsOf(packageName(rel))
	if rt.DryRun {
		lines, err := github.DescribeRelease(in, assets, "")
		if err != nil {
			return err
		}
		for _, line := range lines {
			slog.InfoContext(ctx, line)
		}
		return nil
	}
	client, err := rt.State.GitHub.RequireAuthentication(ctx)
	if err != nil {
		return err
	}
	return client.CreateRelease(ctx, in, assets, "")
}

// releaseOnGitea creates the Gitea release of rel (spec F16.2). A dry run logs what would happen and nothing
// else: it builds no client, so it neither reads the credential store, nor prompts, nor touches the network.
// Gitea has no asset uploads: a configuration that has some is refused when it is loaded.
func releaseOnGitea(
	ctx context.Context, rt *workflow.RunType[workflow.State], rel *action.Release, tag string, head *headSHA,
) error {
	in := gitea.ReleaseInput{
		TagName:         tag,
		Name:            releaseName(rel),
		Body:            strings.TrimSpace(rel.Notes),
		Prerelease:      rel.Version.IsPrerelease(),
		TargetCommitish: head.get(ctx, &rt.State),
	}
	if rt.DryRun {
		slog.InfoContext(ctx, gitea.DescribeRelease(rt.State.Gitea.Config(), in))
		return nil
	}
	client, err := rt.State.Gitea.RequireAuthentication(ctx)
	if err != nil {
		return err
	}
	return client.CreateRelease(ctx, in)
}

func packageName(rel *action.Release) versioning.PackageName {
	if rel.PackageName != nil {
		return versioning.CustomName(*rel.PackageName)
	}
	return versioning.DefaultName()
}
