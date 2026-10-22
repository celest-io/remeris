package versionedfile

import (
	"github.com/celest-io/remeris/internal/diag"
	"github.com/celest-io/remeris/pkg/versioning/action"
	"github.com/celest-io/remeris/pkg/versioning/internal/jsonedit"
	"github.com/celest-io/remeris/pkg/versioning/semver"
)

const (
	codeTauriConfDeserialize = "remeris::tauri_conf_json::deserialize"
	helpTauriConf            = "remeris expects the tauri.conf.json file to be an object with a top level `version` property"
)

// tauriConfFile is tauri.conf.json or one of its platform variants (F6.2); they share this one implementation. It is
// an immutable value. Like Knope, it keeps the text of the file: an edit parses it again and replaces it with the
// pretty-printed result, which has no trailing newline (Q10). Tauri v2 allows "version": "../package.json", which is
// not a version and fails here as it does in Knope.
type tauriConfFile struct {
	path    string
	raw     string
	version semver.Version
	diff    Diff
}

func newTauriConfFile(path, content string) (File, error) {
	root, err := jsonedit.Parse([]byte(content))
	if err != nil {
		return nil, tauriConfInvalid(path, err.Error())
	}
	if root.Kind() != jsonedit.KindObject {
		return nil, tauriConfInvalid(path, "invalid type: expected a JSON object")
	}
	value, ok := root.Get(keyVersion)
	if !ok {
		return nil, tauriConfInvalid(path, "missing field `version`")
	}
	text, ok := value.Str()
	if !ok {
		return nil, tauriConfInvalid(path, "invalid type for `version`: expected a string")
	}
	version, err := semver.Parse(text)
	if err != nil {
		return nil, tauriConfInvalid(path, err.Error())
	}
	return tauriConfFile{path: path, raw: content, version: version}, nil
}

func tauriConfInvalid(path, detail string) error {
	return diag.New("Error deserializing " + path + ": " + detail).
		WithCode(codeTauriConfDeserialize).WithHelp(helpTauriConf)
}

func (f tauriConfFile) Path() string { return f.path }

// Version is the version read when the file was parsed; SetVersion does not change it, as in Knope.
func (f tauriConfFile) Version() (semver.Version, error) { return f.version, nil }

// SetVersion sets the top level version, appending the member if it is missing (it cannot be, after parsing).
// Dependencies are not supported and are ignored. The diff is the bare version, and a later call replaces it.
func (f tauriConfFile) SetVersion(v semver.Version, _ *string, _ GoVersioning) (File, error) {
	root, err := jsonedit.Parse([]byte(f.raw))
	if err != nil {
		return nil, tauriConfInvalid(f.path, err.Error())
	}
	root.Set(keyVersion, jsonedit.String(v.String()))
	out := f
	out.raw = string(jsonedit.Marshal(root))
	out.diff = Diff{}.Add(v.String())
	return out, nil
}

// Write returns the write action, or nil if nothing was edited.
func (f tauriConfFile) Write() []action.Action {
	if f.diff.Empty() {
		return nil
	}
	return []action.Action{action.WriteToFile(f.path, f.raw, f.diff.String())}
}
