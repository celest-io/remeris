package versionedfile

import (
	"fmt"
	"strings"

	"github.com/celest-io/remeris/internal/diag"
	"github.com/celest-io/remeris/pkg/versioning/action"
	"github.com/celest-io/remeris/pkg/versioning/internal/tomledit"
	"github.com/celest-io/remeris/pkg/versioning/semver"
)

// Error codes and keys of pyproject.toml files.
const (
	codePyProjectInvalid      = "remeris::pyproject::invalid"
	codePyProjectInconsistent = "remeris::pyproject::inconsistent"
	codePyProjectNoVersions   = "remeris::pyproject::no_versions"
	codePyProjectEdit         = "remeris::pyproject::edit"

	keyProject = "project"
	keyTool    = "tool"
	keyPoetry  = "poetry"
)

// pyProjectFile is a pyproject.toml (F6.2). It is an immutable value: SetVersion edits a copy of the document.
type pyProjectFile struct {
	path    string
	doc     *tomledit.Document
	version semver.Version
	diff    Diff
}

// pyProjectPaths are the key paths of the two places a version can live: PEP 621 and Poetry.
func pyProjectPaths() (project, poetry []string) {
	return []string{keyProject, keyVersion}, []string{keyTool, keyPoetry, keyVersion}
}

func newPyProjectFile(path, content string) (File, error) {
	doc, err := tomledit.Parse([]byte(content))
	if err != nil {
		return nil, pyProjectInvalid(path, err)
	}
	version, err := pyProjectVersion(path, doc)
	if err != nil {
		return nil, err
	}
	return pyProjectFile{path: path, doc: doc, version: version}, nil
}

func pyProjectInvalid(path string, cause error) error {
	return diag.New("Could not deserialize " + path + " as a pyproject.toml: " + cause.Error()).
		WithCode(codePyProjectInvalid).
		WithHelp("remeris expects the pyproject.toml file to have a `tool.poetry.version` or `project.version` " +
			"property. If you use a different location for your version, please open an issue to add support.").
		WithCause(cause)
}

// pyProjectString returns the string at keys, or nil if a table on the way or the version is missing. A table or
// a version of the wrong type is an error, like Knope's typed deserialization.
func pyProjectString(path string, doc *tomledit.Document, keys []string) (*string, error) {
	last := len(keys) - 1
	for i := range keys {
		v, ok := doc.Lookup(keys[:i+1]...)
		if !ok {
			return nil, nil
		}
		switch {
		case i < last && v.Kind != tomledit.Table && v.Kind != tomledit.InlineTable:
			return nil, pyProjectInvalid(
				path,
				fmt.Errorf("invalid type for `%s`, expected a table", strings.Join(keys[:i+1], ".")),
			)
		case i == last && v.Kind != tomledit.String:
			return nil, pyProjectInvalid(path, fmt.Errorf("invalid type for `%s`, expected a string", strings.Join(keys, ".")))
		case i == last:
			return &v.Str, nil
		}
	}
	return nil, nil
}

// pyProjectVersion is the version both locations agree on (Knope's Toml::version).
func pyProjectVersion(path string, doc *tomledit.Document) (semver.Version, error) {
	projectPath, poetryPath := pyProjectPaths()
	poetry, err := pyProjectString(path, doc, poetryPath)
	if err != nil {
		return semver.Version{}, err
	}
	project, err := pyProjectString(path, doc, projectPath)
	if err != nil {
		return semver.Version{}, err
	}
	switch {
	case project != nil && poetry != nil && *project != *poetry:
		return semver.Version{}, diag.New("Found conflicting versions " + *project + " and " + *poetry + " in " + path).
			WithCode(codePyProjectInconsistent).
			WithHelp("Make sure [project.version] and [tool.poetry.version] are the same.")
	case poetry != nil:
		return semver.Parse(*poetry)
	case project != nil:
		return semver.Parse(*project)
	}
	return semver.Version{}, diag.New("No versions were found in " + path).
		WithCode(codePyProjectNoVersions).
		WithHelp("Make sure [project.version] or [tool.poetry.version] is set.")
}

func (f pyProjectFile) Path() string { return f.path }

// Version returns the version checked when the file was parsed, or the one SetVersion set.
func (f pyProjectFile) Version() (semver.Version, error) { return f.version, nil }

// SetVersion replaces the text between the quotes of project.version and tool.poetry.version, whichever exist.
// Unlike Knope, whose second replacement reuses byte offsets computed before the first, it is correct when the
// new version has another length than the old one. The diff is the version alone and replaces the previous one.
func (f pyProjectFile) SetVersion(v semver.Version, dependency *string, _ GoVersioning) (File, error) {
	if dependency != nil {
		return nil, UnsupportedDependency(f.path)
	}
	doc, err := tomledit.Parse(f.doc.Bytes())
	if err != nil {
		return nil, pyProjectInvalid(f.path, err)
	}
	projectPath, poetryPath := pyProjectPaths()
	for _, keys := range [][]string{poetryPath, projectPath} {
		if _, ok := doc.Lookup(keys...); !ok {
			continue
		}
		if err = doc.ReplaceStringContents(tomledit.Keys(keys...), v.String()); err != nil {
			return nil, diag.New("Could not update " + f.path + ": " + err.Error()).
				WithCode(codePyProjectEdit).
				WithCause(err)
		}
	}
	out := f
	out.doc = doc
	out.version = v
	out.diff = Diff{}.Add(v.String())
	return out, nil
}

// Write returns the write action, or nil if SetVersion was never called.
func (f pyProjectFile) Write() []action.Action {
	if f.diff.Empty() {
		return nil
	}
	return []action.Action{action.WriteToFile(f.path, string(f.doc.Bytes()), f.diff.String())}
}
