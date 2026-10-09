package versionedfile

import (
	"github.com/celest-io/remeris/internal/diag"
	"github.com/celest-io/remeris/pkg/versioning/action"
	"github.com/celest-io/remeris/pkg/versioning/internal/tomledit"
	"github.com/celest-io/remeris/pkg/versioning/semver"
)

// Error codes and keys of Cargo.toml files.
const (
	codeCargoTOML            = "remeris::cargo::toml"
	codeCargoMissingProperty = "remeris::cargo::missing_property"
	codeCargoEdit            = "remeris::cargo::edit"

	keyPackage   = "package"
	keyVersion   = "version"
	keyWorkspace = "workspace"
	keyDeps      = "dependencies"
	keyDevDeps   = "dev-dependencies"
	keyName      = "name"

	cargoMissingVersion = " was missing required property `package.version` or `workspace.package.version`"
)

// cargoFile is a Cargo.toml (F6.2). It is an immutable value: SetVersion edits a copy of the document.
type cargoFile struct {
	path string
	doc  *tomledit.Document
	diff Diff
}

func newCargoFile(path, content string) (File, error) {
	doc, err := parseCargo(path, content)
	if err != nil {
		return nil, err
	}
	return cargoFile{path: path, doc: doc}, nil
}

func parseCargo(path, content string) (*tomledit.Document, error) {
	doc, err := tomledit.Parse([]byte(content))
	if err != nil {
		return nil, diag.New("Invalid TOML in " + path + ": " + err.Error()).WithCode(codeCargoTOML).WithCause(err)
	}
	return doc, nil
}

// CargoPackageName returns the package.name of a Cargo.toml and whether there is one (Knope's name_from_document).
// Cargo.lock's default dependency is the name of the first non-dependency Cargo.toml of a package (F7.2). The
// error reports invalid TOML.
func CargoPackageName(content string) (name string, ok bool, err error) {
	doc, err := parseCargo("Cargo.toml", content)
	if err != nil {
		return "", false, err
	}
	v, found := doc.Lookup(keyPackage, keyName)
	if !found || v.Kind != tomledit.String {
		return "", false, nil
	}
	return v.Str, true, nil
}

// CargoContainsDependency reports whether any of dependencies, dev-dependencies and workspace.dependencies of a
// Cargo.toml has an entry named dependency, whatever its form (Knope's contains_dependency). The error reports
// invalid TOML. Build and target-specific dependency tables are not looked at, as in Knope.
func CargoContainsDependency(content, dependency string) (bool, error) {
	doc, err := parseCargo("Cargo.toml", content)
	if err != nil {
		return false, err
	}
	for _, table := range cargoDependencyTables() {
		if _, found := doc.Lookup(append(table, dependency)...); found {
			return true, nil
		}
	}
	return false, nil
}

// cargoDependencyTables returns the key paths of the tables SetVersion edits dependencies in. Each call returns
// fresh slices, so callers may append to them.
func cargoDependencyTables() [][]string {
	return [][]string{{keyDeps}, {keyDevDeps}, {keyWorkspace, keyDeps}}
}

func (f cargoFile) Path() string { return f.path }

// Version returns package.version, else workspace.package.version.
func (f cargoFile) Version() (semver.Version, error) {
	for _, path := range [][]string{{keyPackage, keyVersion}, {keyWorkspace, keyPackage, keyVersion}} {
		if v, ok := f.doc.Lookup(path...); ok && v.Kind == tomledit.String {
			return semver.Parse(v.Str)
		}
	}
	return semver.Version{}, diag.New(f.path + cargoMissingVersion).
		WithCode(codeCargoMissingProperty)
}

// SetVersion sets package.version (or workspace.package.version when the package inherits it), or the version of
// dependency in every table that has it. The diff is recorded even when nothing matched, like Knope.
func (f cargoFile) SetVersion(v semver.Version, dependency *string, _ GoVersioning) (File, error) {
	doc, err := parseCargo(f.path, string(f.doc.Bytes()))
	if err != nil {
		return nil, err
	}
	out := f
	out.doc = doc
	version := v.String()
	if dependency != nil {
		err = out.setDependency(*dependency, version)
		out.diff = f.diff.Add(*dependency + ".version = " + version)
	} else {
		err = out.setOwnVersion(version)
		out.diff = f.diff.Add("version = " + version)
	}
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (f cargoFile) setOwnVersion(version string) error {
	if v, ok := f.doc.Lookup(keyPackage, keyVersion); ok && v.Kind == tomledit.String {
		return f.edit(tomledit.Keys(keyPackage, keyVersion), version)
	}
	if _, ok := f.doc.Lookup(keyWorkspace, keyPackage, keyVersion); ok {
		return f.edit(tomledit.Keys(keyWorkspace, keyPackage, keyVersion), version)
	}
	return nil
}

func (f cargoFile) setDependency(dependency, version string) error {
	for _, table := range cargoDependencyTables() {
		path := append(table, dependency)
		entry, ok := f.doc.Lookup(path...)
		switch {
		case !ok || entry.Kind == tomledit.ArrayOfTables:
		case entry.Kind == tomledit.Table || entry.Kind == tomledit.InlineTable:
			if err := f.edit(tomledit.Keys(append(path, keyVersion)...), version); err != nil {
				return err
			}
		default:
			if err := f.edit(tomledit.Keys(path...), version); err != nil {
				return err
			}
		}
	}
	return nil
}

// edit sets the string at path, turning editor errors into diagnostics.
func (f cargoFile) edit(path []tomledit.Key, version string) error {
	if err := f.doc.SetString(path, version); err != nil {
		return diag.New("Could not update " + f.path + ": " + err.Error()).WithCode(codeCargoEdit).WithCause(err)
	}
	return nil
}

// Write returns the write action, or nil if SetVersion was never called.
func (f cargoFile) Write() []action.Action {
	if f.diff.Empty() {
		return nil
	}
	return []action.Action{action.WriteToFile(f.path, string(f.doc.Bytes()), f.diff.String())}
}
