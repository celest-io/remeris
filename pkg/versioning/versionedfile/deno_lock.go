package versionedfile

import (
	"slices"

	"github.com/celest-io/remeris/internal/diag"
	"github.com/celest-io/remeris/pkg/versioning/action"
	"github.com/celest-io/remeris/pkg/versioning/internal/denolock"
	"github.com/celest-io/remeris/pkg/versioning/internal/denosemver"
	"github.com/celest-io/remeris/pkg/versioning/semver"
)

// Error codes of deno.lock files.
const (
	codeDenoLockDeserialize    = "remeris::deno_lock::deserialize"
	codeDenoLockLegacy         = "remeris::deno_lock::legacy_version"
	codeDenoLockUnsupported    = "remeris::deno_lock::unsupported_version"
	codeDenoLockInvalidVersion = "remeris::deno_lock::invalid_version"
	codeDenoLockUpdate         = "remeris::deno_lock::update"

	denoLockSupportedVersion = "5"
	denoLockMissingVersion   = "1" // the number Knope names when the file has no string version

	keyDenoSpecifiers = "specifiers"
	keyDenoWorkspace  = "workspace"
	keyDenoMembers    = "members"
	keyDenoLinks      = "links"
	keyDenoDeps       = "dependencies"
	keyDenoPackageJSN = "packageJson"
	keyDenoOptional   = "optionalDependencies"
	keyDenoPeer       = "peerDependencies"
	sectionJSR        = "jsr"
	sectionNPM        = "npm"
)

// denoLockLegacyVersions are the lockfile versions that need npm package metadata.
var denoLockLegacyVersions = []string{"1", "2", "3", "4"}

// denoLockFile is a deno.lock (F6.2), ported from Knope's versioned_file/deno_lock.rs. It keeps the lockfile as an
// ordered JSON tree and edits that tree, like Knope; the model of denolock is only used when writing, to print the
// canonical text. It is an immutable value: SetVersion edits a copy of the tree.
type denoLockFile struct {
	path string
	json denolock.Value
	diff Diff
	// text is the canonical print of json, set by the SetVersion that changed something.
	text string
}

// newDenoLockFile reads the JSON and checks the lockfile version. Like Knope it does not build the model yet:
// a lockfile that the model rejects fails at Write time.
func newDenoLockFile(path, content string) (File, error) {
	root, err := denolock.ParseJSON([]byte(content))
	if err != nil {
		return nil, diag.New("Error deserializing " + path + ": " + err.Error()).
			WithCode(codeDenoLockDeserialize).WithCause(err)
	}
	version, hasVersion := denolock.Version(root)
	switch {
	case hasVersion && version == denoLockSupportedVersion:
	case !hasVersion || slices.Contains(denoLockLegacyVersions, version):
		if !hasVersion {
			version = denoLockMissingVersion
		}
		return nil, diag.New("Lockfile version " + version + " in " + path +
			" requires npm package metadata which is not supported yet").WithCode(codeDenoLockLegacy)
	default:
		return nil, diag.New("Unsupported lockfile version " + version + " in " + path).WithCode(codeDenoLockUnsupported)
	}
	return denoLockFile{path: path, json: root}, nil
}

func (f denoLockFile) Path() string { return f.path }

// Version always fails: a lock file holds the versions of many packages, not the version of one.
func (denoLockFile) Version() (semver.Version, error) { return semver.Version{}, ErrNoVersion }

// denoRewrite carries what one SetVersion call knows while it rewrites the tree.
type denoRewrite struct {
	dependency string
	// old is the set of old resolved values and old requirement texts of the matching specifiers.
	old      map[string]struct{}
	resolved string
}

// SetVersion points every requirement on dependency to v: Knope's set_version. Without a dependency, or when no
// specifier names it, nothing happens.
func (f denoLockFile) SetVersion(v semver.Version, dependency *string, _ GoVersioning) (File, error) {
	if dependency == nil {
		return f, nil
	}
	root := cloneJSON(f.json)
	specifiers := jsonMember(&root, keyDenoSpecifiers)
	if specifiers == nil || specifiers.Kind != denolock.KindObject {
		return f, nil
	}
	rw := denoRewrite{dependency: *dependency, old: map[string]struct{}{}, resolved: v.String()}
	kinds := map[denosemver.PackageKind]bool{}
	for i := range specifiers.Obj {
		m := &specifiers.Obj[i]
		req, err := denosemver.ParseJsrDepPackageReq(m.Key)
		if err != nil || req.Req.Name != rw.dependency || m.Value.Kind != denolock.KindString {
			continue
		}
		rw.old[m.Value.Str] = struct{}{}
		rw.old[req.Req.VersionReq.VersionText()] = struct{}{}
		kinds[req.Kind] = true
		m.Value.Str = rw.resolved
	}
	if len(kinds) == 0 {
		return f, nil
	}
	newVersion, err := denosemver.ParseStandard(rw.resolved)
	if err != nil {
		return nil, diag.New("Invalid version '" + rw.resolved + "' for Deno dependency: " + err.Error()).
			WithCode(codeDenoLockInvalidVersion).WithCause(err)
	}
	if kinds[denosemver.Jsr] {
		rw.updateRegistry(jsonMember(&root, sectionJSR), newVersion)
	}
	if kinds[denosemver.Npm] {
		rw.updateRegistry(jsonMember(&root, sectionNPM), newVersion)
	}
	rw.updateWorkspace(jsonMember(&root, keyDenoWorkspace))
	model, err := denolock.FromJSON(root)
	if err != nil {
		return nil, diag.New("Could not update " + f.path + ": " + err.Error()).WithCode(codeDenoLockUpdate).WithCause(err)
	}
	f.json = root
	f.text = model.Print()
	f.diff = f.diff.Add(rw.dependency + " = " + rw.resolved)
	return f, nil
}

// updateRegistry renames the "name@old" keys of the jsr or npm section. A renamed entry is removed (the
// last member fills the hole) and inserted at the end, as in a serde_json map with preserve_order.
func (rw denoRewrite) updateRegistry(section *denolock.Value, newVersion denosemver.Version) {
	if section == nil || section.Kind != denolock.KindObject {
		return
	}
	var keys []string
	for _, m := range section.Obj {
		nv, err := denosemver.ParsePackageNv(m.Key)
		if err != nil || nv.Name != rw.dependency {
			continue
		}
		if _, ok := rw.old[nv.Version.String()]; ok {
			keys = append(keys, m.Key)
		}
	}
	for _, key := range keys {
		value, ok := removeMember(section, key)
		if !ok {
			continue
		}
		newKey := rw.dependency + "@" + rw.resolved
		if nv, err := denosemver.ParsePackageNv(key); err == nil {
			nv.Version = newVersion
			newKey = nv.String()
		}
		setMember(section, newKey, value)
	}
}

func (rw denoRewrite) updateWorkspace(workspace *denolock.Value) {
	if workspace == nil {
		return
	}
	rw.updateDependencyArrays(workspace)
	if members := jsonMember(workspace, keyDenoMembers); members != nil && members.Kind == denolock.KindObject {
		for i := range members.Obj {
			rw.updateDependencyArrays(&members.Obj[i].Value)
		}
	}
	links := jsonMember(workspace, keyDenoLinks)
	if links == nil || links.Kind != denolock.KindObject {
		return
	}
	type renamed struct {
		key   string
		value denolock.Value
	}
	var queue []renamed
	for _, key := range memberKeys(links) {
		newKey, ok := rw.updatedSpecifier(key)
		if !ok {
			continue
		}
		if value, found := removeMember(links, key); found {
			queue = append(queue, renamed{newKey, value})
		}
	}
	// Only a link whose own key matched has its dependency lists rewritten.
	for _, r := range queue {
		rw.updateDependencyArrays(&r.value)
		setMember(links, r.key, r.value)
	}
}

func (rw denoRewrite) updateDependencyArrays(object *denolock.Value) {
	rw.updateArray(jsonMember(object, keyDenoDeps))
	if packageJSON := jsonMember(object, keyDenoPackageJSN); packageJSON != nil {
		rw.updateArray(jsonMember(packageJSON, keyDenoDeps))
	}
	rw.updateArray(jsonMember(object, keyDenoOptional))
	rw.updateArray(jsonMember(object, keyDenoPeer))
}

func (rw denoRewrite) updateArray(array *denolock.Value) {
	if array == nil || array.Kind != denolock.KindArray {
		return
	}
	for i := range array.Arr {
		item := &array.Arr[i]
		if item.Kind != denolock.KindString {
			continue
		}
		if text, ok := rw.updatedSpecifier(item.Str); ok {
			item.Str = text
		}
	}
}

// updatedSpecifier returns "{scheme}{dependency}@{resolved}" when text is a requirement on the dependency whose
// requirement text is one of the old versions.
func (rw denoRewrite) updatedSpecifier(text string) (string, bool) {
	req, err := denosemver.ParseJsrDepPackageReq(text)
	if err != nil || req.Req.Name != rw.dependency {
		return "", false
	}
	if _, ok := rw.old[req.Req.VersionReq.VersionText()]; !ok {
		return "", false
	}
	return req.Kind.SchemeWithColon() + rw.dependency + "@" + rw.resolved, true
}

// Write returns the write action, or nil if no specifier was changed. The text is the canonical print of the model
// built from the edited tree (see SetVersion). Knope drops the write silently when the model cannot be built;
// remeris fails in SetVersion instead (testdata/deviations.md).
func (f denoLockFile) Write() []action.Action {
	if f.diff.Empty() {
		return nil
	}
	return []action.Action{action.WriteToFile(f.path, f.text, f.diff.String())}
}

// cloneJSON copies a tree so that an edit does not reach the value it came from.
func cloneJSON(v denolock.Value) denolock.Value {
	out := v
	if v.Arr != nil {
		out.Arr = make([]denolock.Value, len(v.Arr))
		for i, item := range v.Arr {
			out.Arr[i] = cloneJSON(item)
		}
	}
	if v.Obj != nil {
		out.Obj = make([]denolock.Member, len(v.Obj))
		for i, m := range v.Obj {
			out.Obj[i] = denolock.Member{Key: m.Key, Value: cloneJSON(m.Value)}
		}
	}
	return out
}

// jsonMember returns a pointer to the member of an object, or nil when v is not an object or has no such member.
func jsonMember(v *denolock.Value, key string) *denolock.Value {
	if v.Kind != denolock.KindObject {
		return nil
	}
	for i := range v.Obj {
		if v.Obj[i].Key == key {
			return &v.Obj[i].Value
		}
	}
	return nil
}

func memberKeys(v *denolock.Value) []string {
	keys := make([]string, len(v.Obj))
	for i, m := range v.Obj {
		keys[i] = m.Key
	}
	return keys
}

// removeMember removes a member like serde_json (swap remove: the order of the others changes).
func removeMember(v *denolock.Value, key string) (denolock.Value, bool) {
	i := slices.IndexFunc(v.Obj, func(m denolock.Member) bool { return m.Key == key })
	if i < 0 {
		return denolock.Value{}, false
	}
	value := v.Obj[i].Value
	// serde_json's Map::remove is IndexMap::swap_remove with preserve_order: the last member takes the place.
	last := len(v.Obj) - 1
	v.Obj[i] = v.Obj[last]
	v.Obj[last] = denolock.Member{}
	v.Obj = v.Obj[:last]
	return value, true
}

// setMember replaces the value of an existing member in place, or appends a new one, like serde_json's insert.
func setMember(v *denolock.Value, key string, value denolock.Value) {
	if i := slices.IndexFunc(v.Obj, func(m denolock.Member) bool { return m.Key == key }); i >= 0 {
		v.Obj[i].Value = value
		return
	}
	v.Obj = append(v.Obj, denolock.Member{Key: key, Value: value})
}
