package versionedfile

import (
	"fmt"
	"strings"

	"github.com/celest-io/remeris/internal/diag"
	"github.com/celest-io/remeris/pkg/versioning/action"
	"github.com/celest-io/remeris/pkg/versioning/semver"
)

// File is a parsed versioned file. Files are immutable values: SetVersion returns a new File and leaves the
// receiver unchanged. Parse once, then apply SetVersion once for each config entry that matches the path (F6.1).
type File interface {
	// Path is the file's location, as configured.
	Path() string
	// Version returns the package version held by the file, or ErrNoVersion for files that cannot hold one.
	Version() (semver.Version, error)
	// SetVersion sets the package version, or the version of dependency when it is not nil.
	SetVersion(v semver.Version, dependency *string, gv GoVersioning) (File, error)
	// Write returns the actions that save the pending edits, or nil if nothing changed.
	Write() []action.Action
}

// New parses content as the format named by cfg. gitTags is used by go.mod only.
func New(cfg Config, content string, gitTags []string) (File, error) {
	_, _ = content, gitTags
	switch cfg.Format {
	case RegexFile:
		return newRegexFile(cfg, content)
	case Cargo, PyProject, PackageJson, GoMod:
		return nil, notImplemented(cfg.Format, "M2")
	default:
		return nil, notImplemented(cfg.Format, "M9")
	}
}

func notImplemented(f Format, milestone string) error {
	return diag.New(fmt.Sprintf("Versioned file format %s is not implemented yet (milestone %s)", f, milestone)).
		WithCode(codeNotImplemented)
}

// Diff accumulates the dry-run descriptions of successive SetVersion calls (F6.1). It is an immutable value:
// the zero Diff is empty and Add returns a new Diff.
type Diff struct{ parts []string }

// Add returns a Diff with part appended.
func (d Diff) Add(part string) Diff {
	parts := make([]string, len(d.parts), len(d.parts)+1)
	copy(parts, d.parts)
	return Diff{parts: append(parts, part)}
}

// Empty reports whether no edit was recorded, that is, whether the file needs no write.
func (d Diff) Empty() bool { return len(d.parts) == 0 }

// String joins the parts with ", ".
func (d Diff) String() string { return strings.Join(d.parts, ", ") }
