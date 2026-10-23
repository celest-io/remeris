package versionedfile

import (
	"strconv"

	"github.com/celest-io/remeris/internal/diag"
	"github.com/celest-io/remeris/pkg/versioning/action"
	"github.com/celest-io/remeris/pkg/versioning/internal/jsonedit"
	"github.com/celest-io/remeris/pkg/versioning/semver"
)

const (
	codePackageLockDeserialize    = "remeris::package_lock_json::deserialize"
	codePackageLockMissingVersion = "remeris::package_lock_json::missing_version"
	codePackageLockInvalidVersion = "remeris::package_lock_json::invalid_version"

	helpPackageLockMissingVersion = "Either add a `version` field or specify a `dependency` to update"
	helpPackageLockInvalidVersion = "Only plain version numbers are currently supported, e.g. `1.2.3`"
	warnPackageLockfileVersion    = "package-lock.json lockfileVersion is not 2 or 3, errors may occur"

	keyPackages        = "packages"
	keyLockfileVersion = "lockfileVersion"

	lockfileVersion2 = 2
	lockfileVersion3 = 3
)

// packageLockJSONFile is a package-lock.json (F6.2). It is an immutable value. Like package.json, it keeps the text
// of the file: every edit parses it again and replaces it with the pretty-printed result, which has no trailing
// newline (Q10).
type packageLockJSONFile struct {
	path string
	raw  string
	diff Diff
}

func newPackageLockJSONFile(path, content string) (File, error) {
	if _, err := jsonedit.Parse([]byte(content)); err != nil {
		return nil, packageLockDeserialize(path, err)
	}
	return packageLockJSONFile{path: path, raw: content}, nil
}

func packageLockDeserialize(path string, err error) error {
	return diag.New("Error deserializing " + path + ": " + err.Error()).WithCode(codePackageLockDeserialize)
}

func (f packageLockJSONFile) Path() string { return f.path }

// Version is the top level version. A file without a string version is an error, even if every edit names a
// dependency.
func (f packageLockJSONFile) Version() (semver.Version, error) {
	root, err := jsonedit.Parse([]byte(f.raw))
	if err != nil {
		return semver.Version{}, packageLockDeserialize(f.path, err)
	}
	var text string
	ok := false
	if value, found := root.Get(keyVersion); found {
		text, ok = value.Str()
	}
	if !ok {
		return semver.Version{}, diag.New(f.path + " did not have a `version` field").
			WithCode(codePackageLockMissingVersion).WithHelp(helpPackageLockMissingVersion)
	}
	v, err := semver.Parse(text)
	if err != nil {
		return semver.Version{}, diag.New("Error parsing " + f.path + ": " + err.Error()).
			WithCode(codePackageLockInvalidVersion).WithHelp(helpPackageLockInvalidVersion)
	}
	return v, nil
}

// SetVersion sets the top level version and packages[""].version, or, with a dependency, updates what refers to
// it. Every call warns when lockfileVersion is not the integer 2 or 3. With a dependency the diff is added and the
// file is rewritten whether or not anything matched. The diff is "version = {v}" or "{dependency} = {v}", joined
// with ", " over successive calls.
func (f packageLockJSONFile) SetVersion(v semver.Version, dependency *string, _ GoVersioning) (File, error) {
	root, err := jsonedit.Parse([]byte(f.raw))
	if err != nil {
		return nil, packageLockDeserialize(f.path, err)
	}
	if !supportedLockfileVersion(root) {
		warn(warnPackageLockfileVersion)
	}
	text := jsonedit.String(v.String())
	var entry string
	packages, hasPackages := root.Get(keyPackages)
	hasPackages = hasPackages && packages.Kind() == jsonedit.KindObject
	if dependency != nil {
		entry = *dependency + " = " + v.String()
		if hasPackages {
			for _, member := range packages.Members() {
				updatePackageLockEntry(member.Value, *dependency, text)
			}
		}
	} else {
		entry = "version = " + v.String()
		root.Set(keyVersion, text)
		if hasPackages {
			if rootPackage, ok := packages.Get(""); ok {
				rootPackage.Set(keyVersion, text)
			}
		}
	}
	out := f
	out.raw = string(jsonedit.Marshal(root))
	out.diff = f.diff.Add(entry)
	return out, nil
}

// supportedLockfileVersion reports whether lockfileVersion is the integer 2 or 3. A float such as 3.0 does not count.
func supportedLockfileVersion(root *jsonedit.Value) bool {
	value, ok := root.Get(keyLockfileVersion)
	if !ok {
		return false
	}
	text, ok := value.NumberText()
	if !ok {
		return false
	}
	n, err := strconv.ParseInt(text, 10, 64)
	return err == nil && (n == lockfileVersion2 || n == lockfileVersion3)
}

// updatePackageLockEntry changes one value of packages. An entry whose name field is dependency gets the version,
// and so does the member called dependency of its dependencies and devDependencies, whatever it holds. Entries that
// are not objects are skipped. Set on a missing member of the entry appends it, as serde_json's insert does.
func updatePackageLockEntry(entry *jsonedit.Value, dependency string, version *jsonedit.Value) {
	if entry.Kind() != jsonedit.KindObject {
		return
	}
	if name, ok := entry.Get(keyName); ok {
		if s, isString := name.Str(); isString && s == dependency {
			entry.Set(keyVersion, version)
		}
	}
	for _, section := range []string{keyDeps, keyNpmDevDeps} {
		deps, ok := entry.Get(section)
		if !ok || deps.Kind() != jsonedit.KindObject {
			continue
		}
		if _, has := deps.Get(dependency); has {
			deps.Set(dependency, version)
		}
	}
}

// Write returns the write action, or nil if nothing was edited.
func (f packageLockJSONFile) Write() []action.Action {
	if f.diff.Empty() {
		return nil
	}
	return []action.Action{action.WriteToFile(f.path, f.raw, f.diff.String())}
}
