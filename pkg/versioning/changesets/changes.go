package changesets

import (
	"strings"

	"github.com/celest-io/remeris/pkg/versioning/changes"
)

const (
	// DefaultPackageName is the name change files use for the single "[package]" of a configuration (F11.1).
	DefaultPackageName = "default"
	// Dir is the directory of the change files, relative to the repository root.
	Dir = ".changeset"
)

// Path returns the slash-separated path of the change file with the given id: ".changeset/{id}.md". It is the key
// of the git info map passed to [PackageChanges].
func Path(id UniqueID) string {
	return Dir + "/" + string(id) + fileSuffix
}

// PackageChanges returns the changes recorded in change files for one package (F11.2), sorted by id (the order
// of the [ChangeSet]). name is the package name as written in change files; use [DefaultPackageName] for the
// single package. The package is mapped to the release whose package name equals name (case-sensitive); no such
// release gives no change. gitInfo maps [Path] to the first commit of the file; a missing entry (an uncommitted
// file) gives a nil GitInfo.
func PackageChanges(set ChangeSet, name string, gitInfo map[string]*changes.GitInfo) []changes.Change {
	for _, release := range set.Releases {
		if release.Package != name {
			continue
		}
		out := make([]changes.Change, 0, len(release.Changes))
		for _, pc := range release.Changes {
			out = append(out, ToChange(pc, gitInfo[Path(pc.ID)]))
		}
		return out
	}
	return nil
}

// ToChange converts one entry of a release into a [changes.Change] (F11.2), like Knope's
// Change::from_package_change_and_commit. info may be nil.
//
// The summary of the change is the first line of the trimmed text without its leading "#" and " " characters
// (not tabs); the details are the lines after it, without leading empty lines, joined with "\n", or nil when
// there is nothing.
func ToChange(pc PackageRelease, info *changes.GitInfo) changes.Change {
	lines := splitLines(strings.TrimSpace(pc.Summary))
	var summary string
	if len(lines) > 0 {
		summary = strings.TrimLeft(lines[0], "# ")
		lines = lines[1:]
	}
	for len(lines) > 0 && lines[0] == "" {
		lines = lines[1:]
	}
	var details *string
	if len(lines) > 0 {
		d := strings.Join(lines, "\n")
		details = &d
	}
	return changes.Change{
		Type:    changeType(pc.Type),
		Summary: summary,
		Details: details,
		GitInfo: info,
		Origin:  changes.Origin{Kind: changes.ChangeFile, ID: string(pc.ID)},
	}
}

func changeType(t ChangeType) changes.ChangeType {
	switch t.Kind {
	case Major:
		return changes.ChangeType{Kind: changes.Breaking}
	case Minor:
		return changes.ChangeType{Kind: changes.Feature}
	case Patch:
		return changes.ChangeType{Kind: changes.Fix}
	default:
		return changes.CustomChangeType(t.Custom)
	}
}
