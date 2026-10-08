// Package action defines the actions the versioning core asks the caller to run (spec F6.1, F7.5, F12.6, F13.4).
//
// The core does no I/O: it returns Actions and the CLI executes them, for real or as a dry run. The package
// is a leaf (it imports only semver) so that versioned files, packages and release notes can all produce
// actions without import cycles.
package action

import (
	"strings"

	"github.com/celest-io/remeris/pkg/versioning/semver"
)

// Kind says which fields of an Action are meaningful.
type Kind int

// The kinds of action.
const (
	// KindWriteToFile writes Content to Path. Diff describes the change for dry runs.
	KindWriteToFile Kind = iota
	// KindRemoveFile deletes Path.
	KindRemoveFile
	// KindAddTag creates the Git tag Tag.
	KindAddTag
	// KindCreateRelease publishes Release.
	KindCreateRelease
)

// Action is one thing to do to finish updating a package. Build one with the constructor functions below.
// Only the fields that belong to the Kind are set.
type Action struct {
	Kind    Kind
	Path    string   // KindWriteToFile, KindRemoveFile
	Content string   // KindWriteToFile: the full new file content, written as is
	Diff    string   // KindWriteToFile: what a dry run prints
	Tag     string   // KindAddTag
	Release *Release // KindCreateRelease
}

// Release is a release to publish: the data of a changelog section for one package version.
type Release struct {
	// Title is the title of the release without any markdown header prefix.
	Title   string
	Version semver.Version
	// Notes is the full release notes in Markdown, at header level 1.
	Notes string
	// PackageName is the custom package name; nil means the default package.
	PackageName *string
}

// Equal reports whether two releases have identical contents.
func (r Release) Equal(o Release) bool {
	return r.Title == o.Title && r.Version.Equal(o.Version) && r.Notes == o.Notes && equalName(r.PackageName, o.PackageName)
}

func equalName(a, b *string) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}

// Equal reports whether two actions are identical, comparing a Release by contents rather than by pointer.
func (a Action) Equal(o Action) bool {
	if a.Kind != o.Kind || a.Path != o.Path || a.Content != o.Content || a.Diff != o.Diff || a.Tag != o.Tag {
		return false
	}
	if a.Release == nil || o.Release == nil {
		return a.Release == o.Release
	}
	return a.Release.Equal(*o.Release)
}

// WriteToFile returns an action that writes content to path. diff is shown in dry runs.
func WriteToFile(path, content, diff string) Action {
	return Action{Kind: KindWriteToFile, Path: path, Content: content, Diff: diff}
}

// RemoveFile returns an action that deletes path.
func RemoveFile(path string) Action { return Action{Kind: KindRemoveFile, Path: path} }

// AddTag returns an action that creates a Git tag.
func AddTag(tag string) Action { return Action{Kind: KindAddTag, Tag: tag} }

// CreateRelease returns an action that publishes r. The action keeps r's slices and pointers, so treat r as
// immutable afterwards.
func CreateRelease(r Release) Action { return Action{Kind: KindCreateRelease, Release: &r} }

// Dedupe removes actions that are exactly equal to an earlier one, keeping the first (F13), like Knope's unique().
// The input is not modified. It compares every pair, which is fine for the handful of actions a release has.
func Dedupe(actions []Action) []Action {
	var out []Action
	for _, a := range actions {
		dup := false
		for _, kept := range out {
			if kept.Equal(a) {
				dup = true
				break
			}
		}
		if !dup {
			out = append(out, a)
		}
	}
	return out
}

// tagPrefix is "v" for the default package and "{name}/v" for a named one (F7.5).
func tagPrefix(customName *string) string {
	if customName == nil {
		return "v"
	}
	return *customName + "/v"
}

// ReleaseTag returns the tag a version of a package gets: "v{version}", or "{name}/v{version}" for a named package.
func ReleaseTag(v semver.Version, customName *string) string {
	return tagPrefix(customName) + v.String()
}

// IsReleaseTag reports whether tag has the package's prefix followed by a valid version.
func IsReleaseTag(tag string, customName *string) bool {
	rest, ok := strings.CutPrefix(tag, tagPrefix(customName))
	if !ok {
		return false
	}
	_, err := semver.Parse(rest)
	return err == nil
}
