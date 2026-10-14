// Package changes defines what a release is made of: [Change] values, where they come from ([Origin]) and the
// bump rule they imply. It mirrors knope-versioning's changes module. The package is pure: building the changes
// from commits is in the conventional subpackage, and reading change files is done elsewhere.
package changes

import (
	"context"

	"github.com/celest-io/remeris/pkg/versioning/semver"
)

// Kind tells which variant a [ChangeType] is.
type Kind int

// The change type kinds. Breaking is the zero value.
const (
	// Breaking is a breaking change (change file type "major", or a commit with "!" or a BREAKING CHANGE footer).
	Breaking Kind = iota
	// Feature is a new feature ("minor", or a "feat" commit).
	Feature
	// Fix is a bug fix ("patch", or a "fix" commit).
	Fix
	// Custom is a change type added by the configuration; ChangeType.Custom says which.
	Custom
)

// SectionKind tells what a custom [SectionSource] names.
type SectionKind int

// The section source kinds.
const (
	// CustomFooter names a commit footer token, such as "Changelog-Note".
	CustomFooter SectionKind = iota
	// CustomType names a change file type, such as "docs".
	CustomType
)

// SectionSource is what a custom changelog section lists: Knope's SectionSource.
type SectionSource struct {
	Kind SectionKind
	// Name is the footer token or the change type, as written.
	Name string
}

// ChangeType is the type of a change: Knope's ChangeType. Custom is only meaningful when Kind is [Custom].
type ChangeType struct {
	Kind   Kind
	Custom SectionSource
}

// Rule returns the stable bump rule the type implies (F1.2): Breaking is major, Feature is minor, and Fix and
// every custom type are patch.
func (t ChangeType) Rule() semver.StableRule {
	switch t.Kind {
	case Breaking:
		return semver.StableMajor
	case Feature:
		return semver.StableMinor
	default:
		return semver.StablePatch
	}
}

// FooterType returns the custom type of a commit footer token.
func FooterType(token string) ChangeType {
	return ChangeType{Kind: Custom, Custom: SectionSource{Kind: CustomFooter, Name: token}}
}

// CustomChangeType returns the custom type of a change file type.
func CustomChangeType(name string) ChangeType {
	return ChangeType{Kind: Custom, Custom: SectionSource{Kind: CustomType, Name: name}}
}

// GitInfo is the information about the commit that introduced a change.
type GitInfo struct {
	// Hash is the abbreviated commit hash (the first 7 hex digits).
	Hash       string
	AuthorName string
	// PRNumber is the number of the pull request that introduced the commit, when a forge was asked.
	PRNumber *int
	// PRAuthorLogin is the forge login of that pull request's author, when a forge was asked.
	PRAuthorLogin *string
}

// OriginKind tells which variant an [Origin] is.
type OriginKind int

// The origin kinds.
const (
	// ConventionalCommit is a change made from a commit message.
	ConventionalCommit OriginKind = iota
	// ChangeFile is a change made from a file in .changeset/.
	ChangeFile
)

// Origin says where a change came from: Knope's ChangeSource. (The name Source is kept for the interface of E3.)
type Origin struct {
	Kind OriginKind
	// Description is the commit description, for ConventionalCommit: see F10.2 step 5.
	Description string
	// ID is the change file name without ".md", for ChangeFile.
	ID string
}

// String is Knope's Display: "commit {description}" or "changeset {id}.md".
func (o Origin) String() string {
	if o.Kind == ChangeFile {
		return "changeset " + o.ID + ".md"
	}
	return "commit " + o.Description
}

// Change is one change to a package.
type Change struct {
	Type    ChangeType
	Summary string
	// Details is the text after the summary, for change files; nil when there is none.
	Details *string
	// GitInfo is nil when the change has no commit (an uncommitted change file).
	GitInfo *GitInfo
	Origin  Origin
}

// HasDetails reports whether the change has details; Knope sorts changes without details first.
func (c Change) HasDetails() bool { return c.Details != nil }

// FooterLookup answers whether a changelog section lists a commit footer. releasenotes.Sections implements it.
// The comparison is ASCII case-insensitive, like Knope's eq_ignore_ascii_case: use [EqualASCIIFold].
type FooterLookup interface {
	ContainsFooter(token string) bool
}

// PackageInfo is what a [Source] needs to know about a package.
type PackageInfo struct {
	// Name is nil for the default package.
	Name *string
	// Scopes limits the commits to these scopes. nil means no limit; a non-nil empty slice matches no scope.
	Scopes []string
	// Sections tells which footers are listed. nil lists none.
	Sections FooterLookup
}

// Source is a pluggable source of changes (spec E3). Phase 1 has two: conventional commits and change files,
// run in that order. The `since ReleaseRef` argument of the spec is added when a source needs it.
type Source interface {
	Name() string
	// Changes returns the changes for one package.
	Changes(ctx context.Context, pkg PackageInfo) ([]Change, error)
}

// EqualASCIIFold reports whether a and b are equal when only the ASCII letters are case-folded. Unlike
// [strings.EqualFold] it never folds other characters (the Kelvin sign is not "k"), like Rust's
// eq_ignore_ascii_case.
func EqualASCIIFold(a, b string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range len(a) {
		if lowerASCII(a[i]) != lowerASCII(b[i]) {
			return false
		}
	}
	return true
}

func lowerASCII(b byte) byte {
	if 'A' <= b && b <= 'Z' {
		return b + 'a' - 'A'
	}
	return b
}

// ContainsASCIIFold reports whether list holds s under [EqualASCIIFold].
func ContainsASCIIFold(list []string, s string) bool {
	for _, item := range list {
		if EqualASCIIFold(item, s) {
			return true
		}
	}
	return false
}
