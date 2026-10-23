package denolock

import (
	"slices"
	"strings"

	"github.com/celest-io/remeris/pkg/versioning/internal/denosemver"
)

// Ported from deno_lockfile 0.49.0 (MIT licence, Copyright 2018-2026 the Deno authors), src/lib.rs:
// LockfileContent::from_json and the serde derives of the types it reads. Deserialisation errors keep the texts
// that serde and serde_json give the Rust code.

// Version returns the top-level "version" of a lockfile when it is a string, the way Knope reads it
// (json.get("version").and_then(Value::as_str)). The model reads version 5 only: Knope answers a missing
// version, "1", "2", "3" and "4" with "Lockfile version {v} in {path} requires npm package metadata which is not
// supported yet" ("1" when there is none), and any other value with "Unsupported lockfile version {v} in {path}".
func Version(root Value) (string, bool) {
	v, ok := root.Get("version")
	if !ok {
		return "", false
	}
	return v.StringValue()
}

// Parse reads the text of a version 5 deno.lock: ParseJSON, then FromJSON. It does not look at "version"; use
// ParseJSON and Version first to decide whether the file is one this package understands. A JSON syntax error is
// returned as it is; every other error is a *DeserializationError.
func Parse(src []byte) (*Content, error) {
	root, err := ParseJSON(src)
	if err != nil {
		return nil, err
	}
	return FromJSON(root)
}

// FromJSON is LockfileContent::from_json. A value that is not an object gives an empty Content. Sections are read in the
// order of the Rust code (specifiers, npm, jsr, redirects, remote, workspace), so the first error is the same.
// Unknown top-level keys are ignored.
func FromJSON(root Value) (*Content, error) {
	c := &Content{Redirects: map[string]string{}, Remote: map[string]string{}, Workspace: newWorkspace()}
	if root.Kind != KindObject {
		return c, nil
	}
	var specifiers, npm, jsr, redirects, remote, workspace *Value
	for i := range root.Obj {
		m := &root.Obj[i]
		switch m.Key {
		case "specifiers":
			specifiers = &m.Value
		case "npm":
			npm = &m.Value
		case "jsr":
			jsr = &m.Value
		case "redirects":
			redirects = &m.Value
		case "remote":
			remote = &m.Value
		case "workspace":
			workspace = &m.Value
		}
	}
	if err := c.readSpecifiers(specifiers); err != nil {
		return nil, err
	}
	if err := c.readNPM(npm); err != nil {
		return nil, err
	}
	if err := c.readJSR(jsr); err != nil {
		return nil, err
	}
	var err error
	if c.Redirects, err = readURLMap("redirects", redirects); err != nil {
		return nil, err
	}
	if c.Remote, err = readURLMap("remote", remote); err != nil {
		return nil, err
	}
	if c.Workspace, err = readWorkspace(workspace); err != nil {
		return nil, err
	}
	return c, nil
}

// The names of the keys that several structs of a lockfile share.
const (
	fieldDependencies         = "dependencies"
	fieldOptionalDependencies = "optionalDependencies"
)

func newWorkspace() Workspace {
	return Workspace{Members: map[string]*WorkspaceMember{}, Links: map[string]*Link{}}
}

type stringEntry struct{ key, value string }

// decodeStringMap is the deserialisation of a BTreeMap<String, String>, in the order of the file.
func decodeStringMap(v Value) ([]stringEntry, error) {
	if v.Kind != KindObject {
		return nil, invalidType(v, "a map")
	}
	out := make([]stringEntry, 0, len(v.Obj))
	for _, m := range v.Obj {
		if m.Value.Kind != KindString {
			return nil, invalidType(m.Value, "a string")
		}
		out = append(out, stringEntry{m.Key, m.Value.Str})
	}
	return out, nil
}

func readURLMap(section string, v *Value) (map[string]string, error) {
	out := map[string]string{}
	if v == nil {
		return out, nil
	}
	entries, err := decodeStringMap(*v)
	if err != nil {
		return nil, failedDeserializing(section, err)
	}
	for _, e := range entries {
		out[e.key] = e.value
	}
	return out, nil
}

// readSpecifiers reads "specifiers". The keys are parsed with the loose grammar, in the sorted order of the BTreeMap
// that deno_lockfile deserialises them into. Two keys that are the same requirement keep the first key and the last
// value (HashMap::insert).
func (c *Content) readSpecifiers(v *Value) error {
	if v == nil {
		return nil
	}
	entries, err := decodeStringMap(*v)
	if err != nil {
		return failedDeserializing("specifiers", err)
	}
	slices.SortFunc(entries, func(a, b stringEntry) int { return strings.Compare(a.key, b.key) })
	index := make(map[string]int, len(entries))
	c.Specifiers = make([]Specifier, 0, len(entries))
	for _, e := range entries {
		req, err := denosemver.ParseJsrDepPackageReqLoose(e.key)
		if err != nil {
			return &DeserializationError{Kind: InvalidPackageSpecifier, msg: err.Error(), cause: err}
		}
		if i, ok := index[req.Key()]; ok {
			c.Specifiers[i].Resolved = e.value
			continue
		}
		index[req.Key()] = len(c.Specifiers)
		c.Specifiers = append(c.Specifiers, Specifier{Req: req, Resolved: e.value})
	}
	return nil
}

// extractNVFromID splits "name@version" at the first '@' after the first character, so a scope survives.
func extractNVFromID(value string) (name, version string, ok bool) {
	if value == "" {
		return "", "", false
	}
	// The Rust code slices value[1..], which panics when the first character is not one byte; here the search is
	// on bytes, which gives the same answer whenever Rust does not panic.
	i := strings.IndexByte(value[1:], '@')
	if i < 0 {
		return "", "", false
	}
	at := i + 1
	return value[:at], value[at+1:], true
}

type rawNPM struct {
	integrity                                       *string
	dependencies, optionalDependencies, optionalPrs []string
	os, cpu                                         []string
	tarball                                         *string
	deprecated, scripts, bin                        bool
}

func decodeOptionalString(v Value) (*string, error) {
	switch v.Kind {
	case KindNull:
		return nil, nil
	case KindString:
		s := v.Str
		return &s, nil
	}
	return nil, invalidType(v, "a string")
}

func decodeStringSeq(v Value) ([]string, error) {
	if v.Kind != KindArray {
		return nil, invalidType(v, "a sequence")
	}
	out := make([]string, 0, len(v.Arr))
	for _, e := range v.Arr {
		if e.Kind != KindString {
			return nil, invalidType(e, "a string")
		}
		out = append(out, e.Str)
	}
	return out, nil
}

func decodeBool(v Value) (bool, error) {
	if v.Kind != KindBool {
		return false, invalidType(v, "a boolean")
	}
	return v.Str == "true", nil
}

// decodeRawNPM is the derived Deserialize of RawNpmPackageInfo: keys in the order of the file, unknown keys ignored.
func decodeRawNPM(v Value) (rawNPM, error) {
	var r rawNPM
	if v.Kind != KindObject {
		return r, invalidType(v, "struct RawNpmPackageInfo")
	}
	for _, m := range v.Obj {
		var err error
		switch m.Key {
		case "integrity":
			r.integrity, err = decodeOptionalString(m.Value)
		case fieldDependencies:
			r.dependencies, err = decodeStringSeq(m.Value)
		case fieldOptionalDependencies:
			r.optionalDependencies, err = decodeStringSeq(m.Value)
		case "optionalPeers":
			r.optionalPrs, err = decodeStringSeq(m.Value)
		case "os":
			r.os, err = decodeStringSeq(m.Value)
		case "cpu":
			r.cpu, err = decodeStringSeq(m.Value)
		case "tarball":
			r.tarball, err = decodeOptionalString(m.Value)
		case "deprecated":
			r.deprecated, err = decodeBool(m.Value)
		case "scripts":
			r.scripts, err = decodeBool(m.Value)
		case "bin":
			r.bin, err = decodeBool(m.Value)
		}
		if err != nil {
			return r, err
		}
	}
	return r, nil
}

// readNPM reads "npm". Dependencies are written as "name@version" or, when the package has one version, as the
// bare name; reading resolves both to a name and an id.
func (c *Content) readNPM(v *Value) error {
	if v == nil {
		return nil
	}
	if v.Kind != KindObject {
		return failedDeserializing("npm", invalidType(*v, "a map"))
	}
	type rawEntry struct {
		id  string
		pkg rawNPM
	}
	raw := make([]rawEntry, 0, len(v.Obj))
	for _, m := range v.Obj {
		pkg, err := decodeRawNPM(m.Value)
		if err != nil {
			return failedDeserializing("npm", err)
		}
		raw = append(raw, rawEntry{m.Key, pkg})
	}
	if len(raw) == 0 {
		return nil
	}
	slices.SortFunc(raw, func(a, b rawEntry) int { return strings.Compare(a.id, b.id) })

	// collect the versions
	versionByName := make(map[string]string, len(raw))
	for _, e := range raw {
		name, version, ok := extractNVFromID(e.id)
		if !ok {
			return &DeserializationError{Kind: InvalidNpmPackageID, msg: "Invalid npm package '" + e.id + "'"}
		}
		versionByName[name] = version
	}

	c.NPM = make([]NPMPackage, 0, len(raw))
	for _, e := range raw {
		pkg := NPMPackage{
			ID: e.id, Integrity: e.pkg.integrity, OS: e.pkg.os, CPU: e.pkg.cpu, Tarball: e.pkg.tarball,
			Deprecated: e.pkg.deprecated, Scripts: e.pkg.scripts, Bin: e.pkg.bin,
		}
		var err error
		if pkg.Dependencies, err = resolveNPMDeps(e.pkg.dependencies, versionByName); err != nil {
			return err
		}
		if pkg.OptionalDependencies, err = resolveNPMDeps(e.pkg.optionalDependencies, versionByName); err != nil {
			return err
		}
		if pkg.OptionalPeers, err = resolveNPMDeps(e.pkg.optionalPrs, versionByName); err != nil {
			return err
		}
		c.NPM = append(c.NPM, pkg)
	}
	return nil
}

// resolveNPMDeps is handle_dep for a list: a later dependency with the same name replaces an earlier one, and the
// result is sorted by name (a BTreeMap).
func resolveNPMDeps(deps []string, versionByName map[string]string) ([]NPMDep, error) {
	if len(deps) == 0 {
		return nil, nil
	}
	out := make([]NPMDep, 0, len(deps))
	for _, dep := range deps {
		left, right, ok := extractNVFromID(dep)
		if !ok {
			version, found := versionByName[dep]
			if !found {
				return nil, &DeserializationError{
					Kind: MissingPackage,
					msg:  "npm package '" + dep + "' was not found and could not have its version resolved",
				}
			}
			left, right = dep, version
		}
		packageName, version := left, right
		if aliased, ok := strings.CutPrefix(right, "npm:"); ok {
			// ex. key@npm:package-a@version
			packageName, version, ok = extractNVFromID(aliased)
			if !ok {
				return nil, &DeserializationError{
					Kind: InvalidNpmPackageDependency,
					msg:  "Invalid npm package dependency '" + dep + "'",
				}
			}
		}
		out = append(out, NPMDep{Name: left, ID: packageName + "@" + version})
	}
	return sortNPMDeps(out), nil
}

// sortNPMDeps sorts by name and keeps the last entry of each name.
func sortNPMDeps(deps []NPMDep) []NPMDep {
	slices.SortStableFunc(deps, func(a, b NPMDep) int { return strings.Compare(a.Name, b.Name) })
	out := deps[:0]
	for i := range deps {
		if i+1 < len(deps) && deps[i+1].Name == deps[i].Name {
			continue
		}
		out = append(out, deps[i])
	}
	return out
}

type rawJSR struct {
	integrity    string
	dependencies []string
}

func decodeRawJSR(v Value) (rawJSR, error) {
	var r rawJSR
	if v.Kind != KindObject {
		return r, invalidType(v, "struct RawJsrPackageInfo")
	}
	hasIntegrity := false
	for _, m := range v.Obj {
		var err error
		switch m.Key {
		case "integrity":
			if m.Value.Kind != KindString {
				err = invalidType(m.Value, "a string")
			} else {
				r.integrity, hasIntegrity = m.Value.Str, true
			}
		case fieldDependencies:
			r.dependencies, err = decodeStringSeq(m.Value)
		}
		if err != nil {
			return r, err
		}
	}
	if !hasIntegrity {
		return r, serdef("missing field `integrity`")
	}
	return r, nil
}

// readJSR reads "jsr". Every dependency is a requirement that must point at an entry of "specifiers", either
// exactly or, for a bare "jsr:name", at the one entry of that name.
func (c *Content) readJSR(v *Value) error {
	if v == nil {
		return nil
	}
	if v.Kind != KindObject {
		return failedDeserializing("jsr", invalidType(*v, "a map"))
	}
	type rawEntry struct {
		nv  denosemver.PackageNv
		pkg rawJSR
	}
	raw := make([]rawEntry, 0, len(v.Obj))
	for _, m := range v.Obj {
		nv, err := denosemver.ParsePackageNv(m.Key)
		if err != nil {
			return failedDeserializing("jsr", err)
		}
		pkg, err := decodeRawJSR(m.Value)
		if err != nil {
			return failedDeserializing("jsr", err)
		}
		raw = append(raw, rawEntry{nv, pkg})
	}
	if len(raw) == 0 {
		return nil
	}
	// A BTreeMap keyed by PackageNv: keys that compare equal (they differ by build metadata only) keep the first key
	// and the last value.
	slices.SortStableFunc(raw, func(a, b rawEntry) int { return a.nv.Compare(b.nv) })
	merged := raw[:0]
	for i := 0; i < len(raw); {
		j := i + 1
		for j < len(raw) && raw[j].nv.Compare(raw[i].nv) == 0 {
			j++
		}
		merged = append(merged, rawEntry{nv: raw[i].nv, pkg: raw[j-1].pkg})
		i = j
	}
	raw = merged

	// The specifiers with their version requirements, then the same names without a requirement. The Rust code walks
	// a HashMap for the second step, so which of several specifiers of one name a bare dependency means is
	// unspecified there; here it is the first in sorted order (Print never writes a bare name for such a name).
	resolved := make(map[string]int, 2*len(c.Specifiers))
	for i, s := range c.Specifiers {
		resolved[s.Req.Key()] = i
	}
	for i, s := range c.Specifiers {
		bare, err := denosemver.ParseJsrDepPackageReq(s.Req.Kind.SchemeWithColon() + s.Req.Req.Name)
		if err != nil {
			continue // should never happen
		}
		if _, taken := resolved[bare.Key()]; !taken {
			resolved[bare.Key()] = i
		}
	}

	c.JSR = make([]JSRPackage, 0, len(raw))
	for _, e := range raw {
		pkg := JSRPackage{Nv: e.nv, Integrity: e.pkg.integrity}
		seen := make(map[int]struct{}, len(e.pkg.dependencies))
		for _, text := range e.pkg.dependencies {
			dep, err := denosemver.ParseJsrDepPackageReq(text)
			if err != nil {
				continue // should never happen
			}
			i, ok := resolved[dep.Key()]
			if !ok {
				return &DeserializationError{
					Kind: InvalidJSRDependency,
					msg:  "Invalid jsr dependency '" + text + "' for '" + e.nv.String() + "'",
				}
			}
			if _, dup := seen[i]; dup {
				continue
			}
			seen[i] = struct{}{}
			pkg.Dependencies = append(pkg.Dependencies, c.Specifiers[i].Req)
		}
		c.JSR = append(c.JSR, pkg)
	}
	return nil
}

// decodeReqSet is the deserialisation of a HashSet<JsrDepPackageReq>: the normalised grammar, one entry per
// requirement, in the order of the file.
func decodeReqSet(v Value) ([]denosemver.JsrDepPackageReq, error) {
	if v.Kind != KindArray {
		return nil, invalidType(v, "a sequence")
	}
	out := make([]denosemver.JsrDepPackageReq, 0, len(v.Arr))
	seen := make(map[string]struct{}, len(v.Arr))
	for _, e := range v.Arr {
		if e.Kind != KindString {
			return nil, invalidType(e, "a string")
		}
		req, err := denosemver.ParseJsrDepPackageReqNormalized(e.Str)
		if err != nil {
			return nil, serdeError(err.Error())
		}
		if _, dup := seen[req.Key()]; dup {
			continue
		}
		seen[req.Key()] = struct{}{}
		out = append(out, req)
	}
	return out, nil
}

// decodePackageJSON is LockfilePackageJsonContent: "dependencies" and "overrides" (null is no overrides).
func decodePackageJSON(v Value) (deps []denosemver.JsrDepPackageReq, overrides *Value, err error) {
	if v.Kind != KindObject {
		return nil, nil, invalidType(v, "struct LockfilePackageJsonContent")
	}
	for _, m := range v.Obj {
		switch m.Key {
		case fieldDependencies:
			if deps, err = decodeReqSet(m.Value); err != nil {
				return nil, nil, err
			}
		case "overrides":
			overrides = nil
			if m.Value.Kind != KindNull {
				o := m.Value
				overrides = &o
			}
		}
	}
	return deps, overrides, nil
}

// decodeMemberFields reads the fields of WorkspaceMemberConfigContent from members. The root of the workspace passes
// the members that the flattened struct left over; a member passes its own.
func decodeMemberFields(members []Member) (m WorkspaceMember, overrides *Value, err error) {
	for _, f := range members {
		switch f.Key {
		case fieldDependencies:
			if m.Dependencies, err = decodeReqSet(f.Value); err != nil {
				return m, nil, err
			}
		case "packageJson":
			if m.PackageJSONDependencies, overrides, err = decodePackageJSON(f.Value); err != nil {
				return m, nil, err
			}
		}
	}
	return m, overrides, nil
}

func decodeLink(v Value) (*Link, error) {
	if v.Kind != KindObject {
		return nil, invalidType(v, "struct LockfileLinkContent")
	}
	l := &Link{}
	for _, f := range v.Obj {
		var err error
		switch f.Key {
		case fieldDependencies:
			l.Dependencies, err = decodeReqSet(f.Value)
		case fieldOptionalDependencies:
			l.OptionalDependencies, err = decodeReqSet(f.Value)
		case "peerDependencies":
			l.PeerDependencies, err = decodeReqSet(f.Value)
		case "peerDependenciesMeta":
			if f.Value.Kind != KindObject {
				err = invalidType(f.Value, "a map")
				break
			}
			l.PeerDependenciesMeta = make(map[string]Value, len(f.Value.Obj))
			for _, e := range f.Value.Obj {
				l.PeerDependenciesMeta[e.Key] = e.Value
			}
		}
		if err != nil {
			return nil, err
		}
	}
	return l, nil
}

// readWorkspace is the derived Deserialize of WorkspaceConfigContent: "members", "links" (or its deprecated alias
// "patches") and "npmOverrides" are its own fields, and every other key goes to the flattened root.
func readWorkspace(v *Value) (Workspace, error) {
	w := newWorkspace()
	if v == nil {
		return w, nil
	}
	fail := func(err error) (Workspace, error) { return newWorkspace(), failedDeserializing("workspace", err) }
	if v.Kind != KindObject {
		return fail(invalidType(*v, "struct WorkspaceConfigContent"))
	}
	var rest []Member
	seenLinks := false
	for _, f := range v.Obj {
		switch f.Key {
		case "members":
			if f.Value.Kind != KindObject {
				return fail(invalidType(f.Value, "a map"))
			}
			for _, m := range f.Value.Obj {
				if m.Value.Kind != KindObject {
					return fail(invalidType(m.Value, "struct WorkspaceMemberConfigContent"))
				}
				member, _, err := decodeMemberFields(m.Value.Obj)
				if err != nil {
					return fail(err)
				}
				w.Members[m.Key] = &member
			}
		case "links", "patches":
			if seenLinks {
				return fail(serdef("duplicate field `links`"))
			}
			seenLinks = true
			if f.Value.Kind != KindObject {
				return fail(invalidType(f.Value, "a map"))
			}
			for _, m := range f.Value.Obj {
				link, err := decodeLink(m.Value)
				if err != nil {
					return fail(err)
				}
				w.Links[m.Key] = link
			}
		case "npmOverrides":
			if f.Value.Kind != KindNull {
				o := f.Value
				w.NpmOverrides = &o
			}
		default:
			rest = append(rest, f)
		}
	}
	root, overrides, err := decodeMemberFields(rest)
	if err != nil {
		return fail(err)
	}
	w.Root = root
	// copy overrides from packageJson section to npm_overrides field
	if w.NpmOverrides == nil {
		w.NpmOverrides = overrides
	}
	return w, nil
}
