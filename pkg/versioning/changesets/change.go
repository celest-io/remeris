// Package changesets reads and writes change files (".changeset/<id>.md") and groups them into releases.
//
// It is a port of the Rust "changesets" crate v0.4.0 that Knope uses. The package is pure: reading a directory
// takes an [fs.FS], and nothing here touches the operating system.
package changesets

import (
	"errors"
	"fmt"
	"strings"
)

const (
	fence      = "---"
	fileSuffix = ".md"
	patchText  = "patch"
	minorText  = "minor"
	majorText  = "major"
)

// Errors returned by [ParseChange]. Use [errors.Is] to tell them apart.
var (
	// ErrMissingFrontMatter means the first line of the file is not "---".
	ErrMissingFrontMatter = errors.New("missing front matter")
	// ErrInvalidFrontMatter means a front-matter line has no ":", or the closing "---" is not where it is expected.
	ErrInvalidFrontMatter = errors.New("invalid front matter")
	// ErrEmptyVersioning means the front matter lists no package. It is wrapped in an "invalid front matter" error.
	ErrEmptyVersioning = errors.New( //nolint:staticcheck // Knope's text
		"Versioning needs to contain at least one item.",
	)
)

// ChangeKind is the kind of a [ChangeType].
type ChangeKind int

// The kinds of change type. The order is the precedence used by [ChangeType.Compare] and by the Rust crate.
const (
	// Custom is any change type that is not patch, minor or major.
	Custom ChangeKind = iota
	// Patch is the "patch" change type.
	Patch
	// Minor is the "minor" change type.
	Minor
	// Major is the "major" change type.
	Major
)

// ChangeType is how a change affects the version of a package.
type ChangeType struct {
	Kind ChangeKind
	// Custom is the label, only meaningful when Kind is [Custom].
	Custom string
}

// ParseChangeType turns the text of a front-matter value into a [ChangeType]. "patch", "minor" and "major" are
// matched case-sensitively; anything else is a custom type.
func ParseChangeType(s string) ChangeType {
	switch s {
	case patchText:
		return ChangeType{Kind: Patch}
	case minorText:
		return ChangeType{Kind: Minor}
	case majorText:
		return ChangeType{Kind: Major}
	default:
		return ChangeType{Kind: Custom, Custom: s}
	}
}

// String returns the text written in a change file.
func (c ChangeType) String() string {
	switch c.Kind {
	case Patch:
		return patchText
	case Minor:
		return minorText
	case Major:
		return majorText
	default:
		return c.Custom
	}
}

// Compare orders change types as the crate does: custom < patch < minor < major, and all custom types are equal.
// It returns -1, 0 or 1.
func (c ChangeType) Compare(other ChangeType) int {
	switch {
	case c.Kind < other.Kind:
		return -1
	case c.Kind > other.Kind:
		return 1
	default:
		return 0
	}
}

// UniqueID identifies a change: the name of its file without ".md".
type UniqueID string

// NormalizeID turns an arbitrary summary into a file-name safe identifier (spec F11.3): ASCII letters and digits
// are lower-cased, runs of space, "_" and "-" become a single "_", and everything else is dropped.
func NormalizeID(summary string) UniqueID {
	var b strings.Builder
	previousWasUnderscore := false
	for _, r := range summary {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			previousWasUnderscore = false
		case r >= 'A' && r <= 'Z':
			b.WriteRune(r + 'a' - 'A')
			previousWasUnderscore = false
		case (r == ' ' || r == '_' || r == '-') && !previousWasUnderscore:
			b.WriteByte('_')
			previousWasUnderscore = true
		}
	}
	return UniqueID(b.String())
}

// PackageChange is one front-matter line: how a change affects one package.
type PackageChange struct {
	// Package is the name as written in the file. It is not unquoted (quirk Q4).
	Package string
	Type    ChangeType
}

// Change is a single change file, applicable to any number of packages.
type Change struct {
	ID UniqueID
	// Versioning lists the packages in file order. It is never empty for a parsed change.
	Versioning []PackageChange
	// Summary is the markdown below the front matter.
	Summary string
}

// FileName returns "{id}.md".
func (c Change) FileName() string {
	return string(c.ID) + fileSuffix
}

// String returns the content of the change file, ending with a newline. Packages are written in the order of
// Versioning (quirk Q5).
func (c Change) String() string {
	var b strings.Builder
	b.WriteString(fence + "\n")
	for _, v := range c.Versioning {
		b.WriteString(v.Package + ": " + v.Type.String() + "\n")
	}
	b.WriteString(fence + "\n\n")
	b.WriteString(c.Summary + "\n")
	return b.String()
}

// ParseChange parses the content of a change file with the given id.
//
// The errors are [ErrMissingFrontMatter] and [ErrInvalidFrontMatter]; an empty front matter is
// [ErrEmptyVersioning] wrapped in an invalid front matter error.
//
// A package listed twice is an [ErrInvalidFrontMatter], although the spec's "last wins" would suggest otherwise.
// The changesets crate counts unique names when it skips to the closing fence, so a duplicate always lands on a
// front-matter line and fails; this returns the error at the duplicate line instead.
func ParseChange(id UniqueID, content string) (Change, error) {
	lines := splitLines(content)
	if len(lines) == 0 || strings.TrimSpace(lines[0]) != fence {
		return Change{}, ErrMissingFrontMatter
	}
	lines = lines[1:]

	var versioning []PackageChange
	seen := map[string]struct{}{}
	for _, line := range lines {
		if strings.TrimSpace(line) == fence {
			break
		}
		name, value, ok := strings.Cut(line, ":")
		if !ok {
			return Change{}, ErrInvalidFrontMatter
		}
		pc := PackageChange{Package: strings.TrimSpace(name), Type: ParseChangeType(strings.TrimSpace(value))}
		if _, dup := seen[pc.Package]; dup {
			return Change{}, ErrInvalidFrontMatter
		}
		seen[pc.Package] = struct{}{}
		versioning = append(versioning, pc)
	}
	if len(versioning) == 0 {
		return Change{}, fmt.Errorf("%w: %w", ErrInvalidFrontMatter, ErrEmptyVersioning)
	}

	// Without duplicates, there is one front-matter line per package, so this lands on the closing fence.
	lines = lines[len(versioning):]
	if len(lines) == 0 || strings.TrimSpace(lines[0]) != fence {
		return Change{}, ErrInvalidFrontMatter
	}
	lines = lines[1:]
	for len(lines) > 0 && strings.TrimSpace(lines[0]) == "" {
		lines = lines[1:]
	}
	return Change{ID: id, Versioning: versioning, Summary: strings.Join(lines, "\n")}, nil
}

// splitLines splits like Rust's str::lines: on "\n", dropping a "\r" right before it, and without a final empty
// line.
func splitLines(s string) []string {
	if s == "" {
		return nil
	}
	lines := strings.Split(s, "\n")
	terminated := lines[len(lines)-1] == ""
	if terminated {
		lines = lines[:len(lines)-1]
	}
	for i := range lines {
		if terminated || i < len(lines)-1 {
			lines[i] = strings.TrimSuffix(lines[i], "\r")
		}
	}
	return lines
}
