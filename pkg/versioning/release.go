package versioning

import (
	"github.com/celest-io/remeris/pkg/versioning/action"
	"github.com/celest-io/remeris/pkg/versioning/semver"
	"github.com/celest-io/remeris/pkg/versioning/versionedfile"
)

// PreparedRelease is a release that an earlier workflow prepared (version bumped, changelog written) but did not
// publish. It carries what the Release step needs to log, in Knope's order, since the pure core cannot log.
type PreparedRelease struct {
	// Release is the release to publish, with PackageName set to the package's custom name.
	Release action.Release
	// LastTag is the newest tag of the package, nil when it has no tag. The caller logs "Last tag is {LastTag}".
	LastTag *semver.Version
	// FromChangelog is false when the release has no changelog entry and was made with empty notes. The caller
	// then logs "Release has previously been prepared without a changelog, will create a release with no notes."
	FromChangelog bool
}

// FindPreparedRelease is Knope's find_prepared_release (spec F14): it reports the release of the package's current
// version when that version is not yet tagged. allTags are all Git tags of the repository, newest first (as
// Git.AllTagsOnBranch returns them).
//
// The bool is false when the package has no version, or when its latest tag is the current version (compared with
// SemVer precedence, so a tag that differs only in build metadata is the same release: Knope cannot parse build
// metadata at all and skips such tags, which spec D8 replaces with full SemVer). Otherwise the release comes from
// the changelog entry of the current version, or has empty notes and the version as title.
//
// The caller logs these debug messages, in this order: "Searching for last package tag to determine if there's a
// release to release" (once the package has a version); "Last tag is {v}" when the package has a tag (LastTagOf
// gives it, also when nothing is returned because it equals the version); and, when FromChangelog is false, "Release
// has previously been prepared without a changelog, will create a release with no notes."
func (p *Package) FindPreparedRelease(allTags []string) (PreparedRelease, bool) {
	current, ok := p.LatestVersion()
	if !ok {
		return PreparedRelease{}, false
	}
	last := p.LastTagOf(allTags)
	if last != nil && semver.Compare(*last, current) == 0 {
		return PreparedRelease{}, false
	}
	pr := PreparedRelease{LastTag: last}
	name := p.Name.Custom()
	if cl := p.ReleaseNotes.Changelog; cl != nil {
		if rel, found := cl.GetRelease(current); found {
			rel.PackageName = name
			pr.Release, pr.FromChangelog = rel, true
			return pr, true
		}
	}
	pr.Release = action.Release{Title: current.String(), Version: current, PackageName: name}
	return pr, true
}

// LastTagOf returns the newest release tag of the package among allTags, or nil when there is none. Tags of other
// packages are ignored.
func (p *Package) LastTagOf(allTags []string) *semver.Version {
	v, ok := semver.FromTags(p.Name.Custom(), allTags, nil).Latest()
	if !ok {
		return nil
	}
	return &v
}

// PreparedReleaseActions returns the pending actions of the Release step for rel, a release found by
// FindPreparedRelease when no PrepareRelease ran in the workflow (spec F14): a CreateRelease, then the first AddTag
// that each file gives once rel's version is set in it, which is how go.mod files get their module tag.
//
// Like Knope's set_version, this sets the version in the files and resets the package's history to it; the files
// themselves are values, so all is not changed. A SetVersion error gives only the CreateRelease.
func (p *Package) PreparedReleaseActions(rel action.Release, all []versionedfile.File) []action.Action {
	actions := []action.Action{action.CreateRelease(rel)}
	files, err := p.SetVersion(rel.Version, p.GoVersioning, all)
	if err != nil {
		return actions
	}
	for _, f := range files {
		for _, a := range f.Write() {
			if a.Kind == action.KindAddTag {
				actions = append(actions, a)
				break
			}
		}
	}
	return actions
}
