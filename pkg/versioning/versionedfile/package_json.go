package versionedfile

import (
	"strings"

	"github.com/celest-io/remeris/internal/diag"
	"github.com/celest-io/remeris/pkg/versioning/action"
	"github.com/celest-io/remeris/pkg/versioning/internal/jsonedit"
	"github.com/celest-io/remeris/pkg/versioning/semver"
)

const (
	codePackageJSONDeserialize = "remeris::package_json::deserialize"

	keyNpmDevDeps   = "devDependencies"
	workspacePrefix = "workspace:"
)

// packageJSONFile is a package.json (F6.2). It is an immutable value. Like Knope, it keeps the text of the file:
// every edit parses it again and replaces it with the pretty-printed result, which has no trailing newline (Q10).
type packageJSONFile struct {
	path    string
	raw     string
	version semver.Version
	diff    Diff
}

func newPackageJSONFile(path, content string) (File, error) {
	root, err := jsonedit.Parse([]byte(content))
	if err != nil {
		return nil, packageJSONInvalid(path, err.Error())
	}
	if root.Kind() != jsonedit.KindObject {
		return nil, packageJSONInvalid(path, "invalid type: expected a JSON object")
	}
	value, ok := root.Get(keyVersion)
	if !ok {
		return nil, packageJSONInvalid(path, "missing field `version`")
	}
	text, ok := value.Str()
	if !ok {
		return nil, packageJSONInvalid(path, "invalid type for `version`: expected a string")
	}
	version, err := semver.Parse(text)
	if err != nil {
		return nil, packageJSONInvalid(path, err.Error())
	}
	return packageJSONFile{path: path, raw: content, version: version}, nil
}

// packageJSONInvalid is Knope's Error::Deserialize. Knope prints serde_json's text after the colon, this prints
// the text of the Go error.
func packageJSONInvalid(path, detail string) error {
	return diag.New("Error deserializing " + path + ": " + detail).
		WithCode(codePackageJSONDeserialize).
		WithHelp("remeris expects the package.json file to be an object with a top level `version` property")
}

func (f packageJSONFile) Path() string { return f.path }

// Version returns the version read when the file was parsed, or the one SetVersion set.
func (f packageJSONFile) Version() (semver.Version, error) { return f.version, nil }

// SetVersion sets the top level version, or updates dependency in dependencies and devDependencies. Setting the
// package version always rewrites the file, even to the same version. A dependency edit rewrites it only when a
// value changes. The diff is "version = {v}" or "{dependency} = {value}", joined with ", " over successive calls.
func (f packageJSONFile) SetVersion(v semver.Version, dependency *string, _ GoVersioning) (File, error) {
	root, err := jsonedit.Parse([]byte(f.raw))
	if err != nil {
		return nil, packageJSONInvalid(f.path, err.Error())
	}
	out := f
	if dependency == nil {
		root.Set(keyVersion, jsonedit.String(v.String()))
		out.raw = string(jsonedit.Marshal(root))
		out.version = v
		out.diff = f.diff.Add("version = " + v.String())
		return out, nil
	}
	changed := ""
	found := false
	for _, section := range []string{keyDeps, keyNpmDevDeps} {
		if value, ok := updateNpmDependency(root, section, *dependency, v); ok {
			changed, found = value, true
		}
	}
	if !found {
		return f, nil
	}
	out.raw = string(jsonedit.Marshal(root))
	out.diff = f.diff.Add(*dependency + " = " + changed)
	return out, nil
}

// updateNpmDependency changes the string value of name in the object called section. It returns the new value
// and true only if the value differs from the old one.
func updateNpmDependency(root *jsonedit.Value, section, name string, v semver.Version) (string, bool) {
	deps, ok := root.Get(section)
	if !ok || deps.Kind() != jsonedit.KindObject {
		return "", false
	}
	entry, ok := deps.Get(name)
	if !ok {
		return "", false
	}
	existing, ok := entry.Str()
	if !ok {
		return "", false
	}
	updated := npmDependencyValue(existing, v)
	if updated == existing {
		return "", false
	}
	deps.Set(name, jsonedit.String(updated))
	return updated, true
}

// npmDependencyValue is the new value of a dependency: a workspace specifier keeps everything before its first
// ASCII digit (and is unchanged if it has no digit), any other string becomes the bare version.
func npmDependencyValue(existing string, v semver.Version) string {
	rest, isWorkspace := strings.CutPrefix(existing, workspacePrefix)
	if !isWorkspace {
		return v.String()
	}
	i := strings.IndexFunc(rest, func(r rune) bool { return r >= '0' && r <= '9' })
	if i < 0 {
		return existing
	}
	return workspacePrefix + rest[:i] + v.String()
}

// Write returns the write action, or nil if nothing was edited.
func (f packageJSONFile) Write() []action.Action {
	if f.diff.Empty() {
		return nil
	}
	return []action.Action{action.WriteToFile(f.path, f.raw, f.diff.String())}
}
