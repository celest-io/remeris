package versionedfile

import (
	"bytes"

	"github.com/celest-io/remeris/internal/diag"
	"github.com/celest-io/remeris/pkg/versioning/action"
	"github.com/celest-io/remeris/pkg/versioning/internal/tomledit"
	"github.com/celest-io/remeris/pkg/versioning/semver"
)

// Error codes of Cargo.lock files.
const (
	codeCargoLockTOML              = "remeris::cargo_lock::toml"
	codeCargoLockMissingDependency = "remeris::cargo_lock::missing_dependency"
	codeCargoLockMissingPackages   = "remeris::cargo_lock::missing_package_array"
	codeCargoLockMissingName       = "remeris::cargo_lock::missing_package_name"
	codeCargoLockEdit              = "remeris::cargo_lock::edit"

	cargoLockMalformedHelp = "The Cargo.lock may be malformed, or remeris may not yet support a newer format."

	// The lowest and highest versions of Cargo.lock that remeris knows (Knope's 3..=4).
	cargoLockMinVersion = 3
	cargoLockMaxVersion = 4
)

// cargoLockFile is a Cargo.lock (F6.2). It is an immutable value: SetVersion edits a copy of the document.
type cargoLockFile struct {
	path string
	doc  *tomledit.Document
	diff Diff
}

func newCargoLockFile(path, content string) (File, error) {
	doc, err := tomledit.Parse([]byte(content))
	if err != nil {
		return nil, diag.New("Invalid TOML in " + path + ": " + err.Error()).WithCode(codeCargoLockTOML).WithCause(err)
	}
	return cargoLockFile{path: path, doc: doc}, nil
}

func (f cargoLockFile) Path() string { return f.path }

// Version always fails: a lock file holds the versions of many packages, not the version of one.
func (cargoLockFile) Version() (semver.Version, error) {
	return semver.Version{}, ErrNoVersion
}

// SetVersion sets the version of every [[package]] named dependency. Without dependency it fails. A lock file whose
// own version is not 3 or 4 triggers a warning on every call, as in Knope. When no package matches, nothing is
// recorded and Write returns no action.
func (f cargoLockFile) SetVersion(v semver.Version, dependency *string, _ GoVersioning) (File, error) {
	if dependency == nil {
		return nil, diag.New("Dependency was not specified when setting the version").
			WithCode(codeCargoLockMissingDependency).
			WithHelp("This is likely a bug, please report it.")
	}
	f.warnLockVersion()

	count := f.doc.ArrayOfTablesLen(keyPackage)
	if pkg, ok := f.doc.Lookup(keyPackage); !ok || pkg.Kind != tomledit.ArrayOfTables {
		return nil, diag.New(f.path + " is expected to have an array of packages, but it does not").
			WithCode(codeCargoLockMissingPackages).WithHelp(cargoLockMalformedHelp)
	}
	doc, err := tomledit.Parse(f.doc.Bytes())
	if err != nil {
		return nil, diag.New("Invalid TOML in " + f.path + ": " + err.Error()).WithCode(codeCargoLockTOML).WithCause(err)
	}
	out := f
	out.doc = doc
	version := v.String()
	for i := range count {
		name, ok := doc.LookupIn([]tomledit.Key{{Name: keyPackage, Index: i}, {Name: keyName}})
		if !ok || name.Kind != tomledit.String {
			return nil, diag.New("Every package in " + f.path +
				" is expected to have a 'name' field containing a string, but one does not").
				WithCode(codeCargoLockMissingName).WithHelp(cargoLockMalformedHelp)
		}
		if name.Str != *dependency {
			continue
		}
		out.diff = out.diff.Add(name.Str + " = " + version)
		path := []tomledit.Key{{Name: keyPackage, Index: i}, {Name: keyVersion}}
		if err := doc.SetString(path, version); err != nil {
			return nil, diag.New("Could not update " + f.path + ": " + err.Error()).WithCode(codeCargoLockEdit).WithCause(err)
		}
	}
	return out, nil
}

// warnLockVersion warns about a lock file format that remeris does not know. Knope prints the toml_edit item of the
// top-level version key with its decor, so the text keeps the whitespace after the "=" and the trailing whitespace
// and comment of the line: `version = 5` reads "Unsupported version of Cargo.lock:  5. Outcome may be unexpected".
func (f cargoLockFile) warnLockVersion() {
	v, ok := f.doc.Lookup(keyVersion)
	if !ok {
		warn("Unknown version of Cargo.lock, outcome may be unexpected")
		return
	}
	if v.Kind == tomledit.Integer && v.Int >= cargoLockMinVersion && v.Int <= cargoLockMaxVersion {
		return
	}
	warn("Unsupported version of Cargo.lock: " + f.itemText(v) + ". Outcome may be unexpected")
}

// itemText renders a value like toml_edit's Display of an Item: the raw value with the spaces before it and the
// rest of its line (whitespace and comment) after it. A table has no single value span and renders empty.
func (f cargoLockFile) itemText(v tomledit.Value) string {
	if v.Kind == tomledit.Table || v.Kind == tomledit.ArrayOfTables {
		return ""
	}
	src := f.doc.Bytes()
	start, end := v.Span[0], v.Span[1]
	for start > 0 && (src[start-1] == ' ' || src[start-1] == '\t') {
		start--
	}
	if i := bytes.IndexByte(src[end:], '\n'); i >= 0 {
		end += i
	} else {
		end = len(src)
	}
	return string(bytes.TrimSuffix(src[start:end], []byte("\r")))
}

// Write returns the write action, or nil if no package was changed.
func (f cargoLockFile) Write() []action.Action {
	if f.diff.Empty() {
		return nil
	}
	return []action.Action{action.WriteToFile(f.path, string(f.doc.Bytes()), f.diff.String())}
}
