package versionedfile

import (
	"github.com/celest-io/remeris/internal/diag"
	"github.com/celest-io/remeris/pkg/versioning/action"
	"github.com/celest-io/remeris/pkg/versioning/internal/tomledit"
	"github.com/celest-io/remeris/pkg/versioning/semver"
)

// Error codes of gleam.toml files.
const (
	codeGleamTOML            = "remeris::gleam::toml"
	codeGleamMissingProperty = "remeris::gleam::missing_property"
	codeGleamEdit            = "remeris::gleam::edit"
)

// gleamFile is a gleam.toml (F6.2). It is an immutable value: SetVersion edits a copy of the document.
type gleamFile struct {
	path string
	doc  *tomledit.Document
	diff Diff
}

func newGleamFile(path, content string) (File, error) {
	doc, err := parseGleam(path, content)
	if err != nil {
		return nil, err
	}
	return gleamFile{path: path, doc: doc}, nil
}

func parseGleam(path, content string) (*tomledit.Document, error) {
	doc, err := tomledit.Parse([]byte(content))
	if err != nil {
		return nil, diag.New("Invalid TOML in " + path + ": " + err.Error()).WithCode(codeGleamTOML).WithCause(err)
	}
	return doc, nil
}

func (f gleamFile) Path() string { return f.path }

// Version returns the top-level version, which must be a string.
func (f gleamFile) Version() (semver.Version, error) {
	v, ok := f.doc.Lookup(keyVersion)
	if !ok || v.Kind != tomledit.String {
		return semver.Version{}, diag.New(f.path + " was missing required property version").
			WithCode(codeGleamMissingProperty)
	}
	return semver.Parse(v.Str)
}

// SetVersion replaces the top-level version, whatever its type, or inserts it after the last top-level pair when
// there is none. The dependency is ignored, as in Knope.
func (f gleamFile) SetVersion(v semver.Version, _ *string, _ GoVersioning) (File, error) {
	doc, err := parseGleam(f.path, string(f.doc.Bytes()))
	if err != nil {
		return nil, err
	}
	version := v.String()
	if err := doc.SetString(tomledit.Keys(keyVersion), version); err != nil {
		return nil, diag.New("Could not update " + f.path + ": " + err.Error()).WithCode(codeGleamEdit).WithCause(err)
	}
	out := f
	out.doc = doc
	out.diff = f.diff.Add("version = " + version)
	return out, nil
}

// Write returns the write action, or nil if SetVersion was never called.
func (f gleamFile) Write() []action.Action {
	if f.diff.Empty() {
		return nil
	}
	return []action.Action{action.WriteToFile(f.path, string(f.doc.Bytes()), f.diff.String())}
}
