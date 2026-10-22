package denolock

import (
	"slices"
	"strings"

	"github.com/celest-io/remeris/pkg/versioning/internal/denosemver"
)

// Ported from deno_lockfile 0.49.0 (MIT licence, Copyright 2018-2026 the Deno authors): src/printer.rs
// (print_v5_content and its Formatter) and Lockfile::as_json_string in src/lib.rs.

// Print returns the text of the lockfile as Deno writes it: print_v5_content, then a newline (as_json_string).
func (c *Content) Print() string { return c.PrintContent() + "\n" }

// PrintContent is print_v5_content: the canonical version 5 text without the final newline. The keys come in the
// order version, specifiers, jsr, npm, redirects, remote, workspace; an empty section is left out; every map is
// sorted (see the fields of Content); objects are indented with two spaces; the "os" and "cpu" arrays of an npm package
// are written on one line. It does not change c.
func (c *Content) PrintContent() string {
	top := objectBuilder{}
	top.add("version", String("5"))
	if len(c.Specifiers) > 0 {
		top.add("specifiers", c.specifiersValue())
	}
	if len(c.JSR) > 0 {
		top.add("jsr", c.jsrValue())
	}
	if len(c.NPM) > 0 {
		top.add("npm", npmValue(c.NPM))
	}
	if len(c.Redirects) > 0 {
		top.add("redirects", sortedStringMap(c.Redirects))
	}
	if len(c.Remote) > 0 {
		top.add("remote", sortedStringMap(c.Remote))
	}
	if ws, ok := c.Workspace.value(); ok {
		top.add("workspace", ws)
	}
	e := emitter{buf: make([]byte, 0, 1<<12)}
	e.value(top.value())
	return string(e.buf)
}

// objectBuilder collects the members of an object.
type objectBuilder struct{ members []Member }

func (b *objectBuilder) add(key string, v Value) {
	b.members = append(b.members, Member{Key: key, Value: v})
}

func (b *objectBuilder) value() Value { return Value{Kind: KindObject, Obj: b.members} }

func stringArray(items []string) Value {
	arr := Value{Kind: KindArray, Arr: make([]Value, len(items))}
	for i, s := range items {
		arr.Arr[i] = String(s)
	}
	return arr
}

// sortedStringMap is a BTreeMap<String, String>: keys in bytewise order.
func sortedStringMap(m map[string]string) Value {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	obj := Value{Kind: KindObject, Obj: make([]Member, len(keys))}
	for i, k := range keys {
		obj.Obj[i] = Member{Key: k, Value: String(m[k])}
	}
	return obj
}

// specifiersValue is a BTreeMap<SerializedJsrDepPackageReq, &str>: the key is the normalised requirement text and the
// order is that text's bytes. Two requirements with the same text keep the last value.
func (c *Content) specifiersValue() Value {
	type entry struct{ key, value string }
	entries := make([]entry, len(c.Specifiers))
	for i, s := range c.Specifiers {
		entries[i] = entry{s.Req.Normalized(), s.Resolved}
	}
	slices.SortStableFunc(entries, func(a, b entry) int { return strings.Compare(a.key, b.key) })
	obj := Value{Kind: KindObject, Obj: make([]Member, 0, len(entries))}
	for i, e := range entries {
		if i+1 < len(entries) && entries[i+1].key == e.key {
			continue
		}
		obj.Obj = append(obj.Obj, Member{Key: e.key, Value: String(e.value)})
	}
	return obj
}

// sortedReqs returns the normalised texts of a set of requirements, sorted bytewise and without repeats
// (SerializedJsrDepPackageReq derives Ord on the final text, and the Rust collections are sets).
func sortedReqs(reqs []denosemver.JsrDepPackageReq) []string {
	out := make([]string, len(reqs))
	for i, r := range reqs {
		out[i] = r.Normalized()
	}
	slices.Sort(out)
	return slices.Compact(out)
}

func (c *Content) jsrValue() Value {
	// handle_jsr: a name with more than one specifier (in either registry) is written with its requirement.
	count := make(map[string]int, len(c.Specifiers))
	seen := make(map[string]struct{}, len(c.Specifiers))
	for _, s := range c.Specifiers {
		if _, dup := seen[s.Req.Key()]; dup {
			continue
		}
		seen[s.Req.Key()] = struct{}{}
		count[s.Req.Req.Name]++
	}

	pkgs := slices.Clone(c.JSR)
	slices.SortStableFunc(pkgs, func(a, b JSRPackage) int { return a.Nv.Compare(b.Nv) })
	obj := Value{Kind: KindObject, Obj: make([]Member, 0, len(pkgs))}
	for i, p := range pkgs {
		if i+1 < len(pkgs) && pkgs[i+1].Nv.Compare(p.Nv) == 0 {
			continue // a BTreeMap holds one entry per key
		}
		pkg := objectBuilder{}
		pkg.add("integrity", String(p.Integrity))
		deps := make([]string, len(p.Dependencies))
		for j, d := range p.Dependencies {
			if n, ok := count[d.Req.Name]; ok && n == 1 {
				deps[j] = d.Kind.SchemeWithColon() + d.Req.Name
			} else {
				deps[j] = d.Normalized()
			}
		}
		slices.Sort(deps)
		deps = slices.Compact(deps)
		if len(deps) > 0 {
			pkg.add(fieldDependencies, stringArray(deps))
		}
		obj.Obj = append(obj.Obj, Member{Key: p.Nv.String(), Value: pkg.value()})
	}
	return obj
}

// npmValue is handle_npm.
func npmValue(all []NPMPackage) Value {
	pkgs := slices.Clone(all)
	slices.SortStableFunc(pkgs, func(a, b NPMPackage) int { return strings.Compare(a.ID, b.ID) })
	unique := pkgs[:0]
	for i, p := range pkgs {
		if i+1 < len(pkgs) && pkgs[i+1].ID == p.ID {
			continue // a BTreeMap holds one entry per key
		}
		unique = append(unique, p)
	}
	pkgs = unique

	versions := make(map[string]int, len(pkgs))
	for _, p := range pkgs {
		if name, _, ok := extractNVFromID(p.ID); ok { // otherwise it is corrupt
			versions[name]++
		}
	}

	obj := Value{Kind: KindObject, Obj: make([]Member, 0, len(pkgs))}
	for _, p := range pkgs {
		pkg := objectBuilder{}
		if p.Integrity != nil {
			pkg.add("integrity", String(*p.Integrity))
		}
		for _, f := range []struct {
			name string
			deps []NPMDep
		}{{fieldDependencies, p.Dependencies}, {fieldOptionalDependencies, p.OptionalDependencies}, {"optionalPeers", p.OptionalPeers}} {
			if deps := npmDepTexts(f.deps, versions); len(deps) > 0 {
				pkg.add(f.name, stringArray(deps))
			}
		}
		if len(p.OS) > 0 {
			pkg.add("os", stringArray(p.OS))
		}
		if len(p.CPU) > 0 {
			pkg.add("cpu", stringArray(p.CPU))
		}
		for _, f := range []struct {
			name  string
			value bool
		}{{"deprecated", p.Deprecated}, {"scripts", p.Scripts}, {"bin", p.Bin}} {
			if f.value {
				pkg.add(f.name, Bool(true))
			}
		}
		if p.Tarball != nil {
			pkg.add("tarball", String(*p.Tarball))
		}
		obj.Obj = append(obj.Obj, Member{Key: p.ID, Value: pkg.value()})
	}
	return obj
}

// npmDepTexts is handle_deps: the entries in the order of their names, as a bare name when the package has a single
// version, as "name@version" when it has several, and as "alias@npm:name@version" when the names differ.
func npmDepTexts(deps []NPMDep, versions map[string]int) []string {
	deps = sortNPMDeps(slices.Clone(deps))
	out := make([]string, 0, len(deps))
	for _, d := range deps {
		name, version, ok := extractNVFromID(d.ID)
		if !ok {
			continue
		}
		switch {
		case name != d.Name:
			out = append(out, d.Name+"@npm:"+name+"@"+version)
		case versions[name] == 1:
			out = append(out, name)
		default:
			out = append(out, d.ID)
		}
	}
	return out
}

// value returns the "workspace" object, and false when the section is empty and left out.
func (w *Workspace) value() (Value, bool) {
	obj := objectBuilder{}
	// the root is flattened into the section
	root := memberValue(&w.Root, w.NpmOverrides)
	obj.members = append(obj.members, root.Obj...)
	if len(w.Members) > 0 {
		keys := sortedKeys(w.Members)
		members := Value{Kind: KindObject, Obj: make([]Member, len(keys))}
		for i, k := range keys {
			m := memberValue(w.Members[k], nil)
			members.Obj[i] = Member{Key: k, Value: m}
		}
		obj.add("members", members)
	}
	if len(w.Links) > 0 {
		keys := sortedKeys(w.Links)
		links := Value{Kind: KindObject, Obj: make([]Member, len(keys))}
		for i, k := range keys {
			links.Obj[i] = Member{Key: k, Value: linkValue(w.Links[k])}
		}
		obj.add("links", links)
	}
	return obj.value(), len(obj.members) > 0
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	return keys
}

// memberValue is SerializedWorkspaceMemberConfigContent: "dependencies" and "packageJson", each left out when empty.
// A nil member is an empty one. The overrides are passed for the root only.
func memberValue(m *WorkspaceMember, overrides *Value) Value {
	obj := objectBuilder{}
	if m == nil {
		return obj.value()
	}
	if deps := sortedReqs(m.Dependencies); len(deps) > 0 {
		obj.add(fieldDependencies, stringArray(deps))
	}
	pkgJSON := objectBuilder{}
	if deps := sortedReqs(m.PackageJSONDependencies); len(deps) > 0 {
		pkgJSON.add(fieldDependencies, stringArray(deps))
	}
	if overrides != nil {
		pkgJSON.add("overrides", *overrides)
	}
	if len(pkgJSON.members) > 0 {
		obj.add("packageJson", pkgJSON.value())
	}
	return obj.value()
}

// linkValue is SerializedLockfileLinkContent. A link with nothing in it is written as {}.
func linkValue(l *Link) Value {
	obj := objectBuilder{}
	if l == nil {
		return obj.value()
	}
	for _, f := range []struct {
		name string
		deps []denosemver.JsrDepPackageReq
	}{{fieldDependencies, l.Dependencies}, {fieldOptionalDependencies, l.OptionalDependencies}, {"peerDependencies", l.PeerDependencies}} {
		if deps := sortedReqs(f.deps); len(deps) > 0 {
			obj.add(f.name, stringArray(deps))
		}
	}
	if len(l.PeerDependenciesMeta) > 0 {
		keys := sortedKeys(l.PeerDependenciesMeta)
		meta := Value{Kind: KindObject, Obj: make([]Member, len(keys))}
		for i, k := range keys {
			meta.Obj[i] = Member{Key: k, Value: l.PeerDependenciesMeta[k]}
		}
		obj.add("peerDependenciesMeta", meta)
	}
	return obj.value()
}

// emitter writes a Value the way serde_json does with the Formatter of printer.rs: serde_json's PrettyFormatter,
// except that the arrays under the keys "os" and "cpu" are written on one line, as ["a", "b"]. The state machine is
// the Rust one, hook for hook, so that the odd cases (an "os" key in free-form JSON) come out the same.
type emitter struct {
	buf []byte
	// lastKey is the text of the object key being written or whose value is being written (the Rust Option<String>
	// last_key); hasLastKey is false for None. Only the unescaped runs of a key are added to it.
	lastKey    []byte
	hasLastKey bool
	inKey      bool
	indent     int
	hasValue   bool
}

func (e *emitter) inlineArray() bool {
	return e.hasLastKey && (string(e.lastKey) == "os" || string(e.lastKey) == "cpu")
}

func (e *emitter) writeIndent() {
	for range e.indent {
		e.buf = append(e.buf, ' ', ' ')
	}
}

func (e *emitter) value(v Value) {
	switch v.Kind {
	case KindNull:
		e.buf = append(e.buf, "null"...)
	case KindBool, KindNumber:
		e.buf = append(e.buf, v.Str...)
	case KindString:
		e.str(v.Str)
	case KindArray:
		e.beginArray()
		for i, el := range v.Arr {
			e.beginArrayValue(i == 0)
			e.value(el)
			e.hasValue = true // end_array_value
		}
		e.endArray()
	case KindObject:
		e.beginObject()
		for i, m := range v.Obj {
			// begin_object_key
			e.lastKey, e.hasLastKey, e.inKey = e.lastKey[:0], true, true
			if i == 0 {
				e.buf = append(e.buf, '\n')
			} else {
				e.buf = append(e.buf, ',', '\n')
			}
			e.writeIndent()
			e.str(m.Key)
			e.inKey = false                 // end_object_key
			e.buf = append(e.buf, ':', ' ') // begin_object_value
			e.value(m.Value)
			e.hasValue, e.hasLastKey = true, false // end_object_value
		}
		e.endObject()
	}
}

func (e *emitter) beginObject() {
	e.indent++
	e.hasValue = false
	e.buf = append(e.buf, '{')
}

func (e *emitter) endObject() {
	e.indent--
	if e.hasValue {
		e.buf = append(e.buf, '\n')
		e.writeIndent()
	}
	e.buf = append(e.buf, '}')
}

func (e *emitter) beginArray() {
	if !e.inlineArray() {
		e.indent++
	}
	e.hasValue = false
	e.buf = append(e.buf, '[')
}

func (e *emitter) endArray() {
	dedent := !e.inlineArray()
	if dedent {
		e.indent--
	}
	if e.hasValue && dedent {
		e.buf = append(e.buf, '\n')
		e.writeIndent()
	}
	e.buf = append(e.buf, ']')
}

func (e *emitter) beginArrayValue(first bool) {
	if e.inlineArray() {
		if !first {
			e.buf = append(e.buf, ',', ' ')
		}
		return
	}
	if first {
		e.buf = append(e.buf, '\n')
	} else {
		e.buf = append(e.buf, ',', '\n')
	}
	e.writeIndent()
}

const hexDigits = "0123456789abcdef"

// str writes s as serde_json does: only the quote, the backslash and the control characters are escaped. The runs
// between escapes go through write_string_fragment, which is where a key is recorded; the escapes do not.
func (e *emitter) str(s string) {
	e.buf = append(e.buf, '"')
	start := 0
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c >= 0x20 && c != '"' && c != '\\' {
			continue
		}
		e.fragment(s[start:i])
		start = i + 1
		switch c {
		case '"':
			e.buf = append(e.buf, '\\', '"')
		case '\\':
			e.buf = append(e.buf, '\\', '\\')
		case '\b':
			e.buf = append(e.buf, '\\', 'b')
		case '\f':
			e.buf = append(e.buf, '\\', 'f')
		case '\n':
			e.buf = append(e.buf, '\\', 'n')
		case '\r':
			e.buf = append(e.buf, '\\', 'r')
		case '\t':
			e.buf = append(e.buf, '\\', 't')
		default:
			e.buf = append(e.buf, '\\', 'u', '0', '0', hexDigits[c>>4], hexDigits[c&0xf])
		}
	}
	e.fragment(s[start:])
	e.buf = append(e.buf, '"')
}

func (e *emitter) fragment(s string) {
	if s == "" {
		return
	}
	if e.inKey && e.hasLastKey {
		e.lastKey = append(e.lastKey, s...)
	}
	e.buf = append(e.buf, s...)
}
