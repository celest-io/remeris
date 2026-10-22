package denosemver

import (
	"cmp"
	"strings"
)

// Ported from deno_semver 0.9.1, src/package.rs and src/jsr.rs (MIT licence, Copyright 2018-2025 the Deno
// authors): the package name and version, the package requirement and the JSR dependency requirement. The
// package references with a sub path (jsr:/npm: specifiers in code) are not ported: deno.lock never stores them.

// PackageKind is the registry of a package: JSR or npm.
type PackageKind int

// The registries a Deno lockfile knows. Jsr sorts before Npm.
const (
	Jsr PackageKind = iota
	Npm
)

// SchemeWithColon returns "jsr:" or "npm:".
func (k PackageKind) SchemeWithColon() string {
	if k == Jsr {
		return "jsr:"
	}
	return "npm:"
}

// PackageNv is a package name and an exact version, as in the keys "@std/path@1.0.0" of the jsr and npm
// sections of deno.lock.
type PackageNv struct {
	Name    string
	Version Version
}

// String returns "name@version".
func (nv PackageNv) String() string { return nv.Name + "@" + nv.Version.String() }

// Compare orders by name (bytewise) and then by version precedence. The Rust type derives Ord with the fields in
// that order, so the name comes first. Versions that differ only by build metadata compare equal.
func (nv PackageNv) Compare(o PackageNv) int {
	return cmp.Or(strings.Compare(nv.Name, o.Name), nv.Version.Compare(o.Version))
}

// ParsePackageNv parses "name@version" (PackageNv::from_str). The name may start with '@' (a scope). The
// version is everything up to a '_' or '/' and is parsed with the loose npm grammar; the whole text must be
// consumed, so a peer-dependency suffix such as "_react@18.0.0" makes it fail. On failure the error is a
// *PackageNvParseError.
func ParsePackageNv(text string) (PackageNv, error) {
	nv, f := withFailureHandling(parseNv)(text)
	if f != nil {
		return PackageNv{}, &PackageNvParseError{Message: f.Error(), Text: text}
	}
	return nv, nil
}

func parseNv(in string) (string, PackageNv, *parseErr) {
	// the name runs up to the first '@' that is not the first character (which may start a scope)
	name := func(in string) (string, string, *parseErr) {
		for pos, c := range in {
			if pos > 0 && c == '@' {
				return in[pos:], in[:pos], nil
			}
		}
		return in, "", errBacktrace
	}
	rest, n, err := name(in)
	if err != nil {
		return in, PackageNv{}, err
	}
	rest, _, err = ch('@')(rest)
	if err != nil {
		return in, PackageNv{}, err
	}
	atVersion := rest
	versionText := ifTrue(takeWhile(func(c rune) bool { return c != '_' && c != '/' }), func(s string) bool { return s != "" })
	rest, text, err := versionText(rest)
	if err != nil {
		return in, PackageNv{}, err
	}
	v, perr := parseNpmVersion(text)
	if perr != nil {
		return in, PackageNv{}, fail(atVersion, perr.Error())
	}
	return rest, PackageNv{Name: n, Version: v}, nil
}

// PackageReq is a package name and a version requirement, as in "@std/path@^1.0.0".
type PackageReq struct {
	Name       string
	VersionReq VersionReq
}

// ParsePackageReq parses "name[@req]" with the strict specifier grammar (PackageReq::from_str). A missing
// requirement is the wildcard "*". The name may be scoped ("@scope/name"). Text after the name, such as a
// sub path, is an error. On failure the error is a *PackageReqParseError.
func ParsePackageReq(text string) (PackageReq, error) {
	return parsePackageReq(text, ParseVersionReqSpecifier)
}

// ParsePackageReqLoose parses "name[@req]" with the npm grammar (PackageReq::from_str_loose), the one the
// lockfile model uses for the keys of its specifiers. On failure the error is a *PackageReqParseError.
func ParsePackageReqLoose(text string) (PackageReq, error) {
	return parsePackageReq(text, ParseVersionReqLoose)
}

// ParsePackageReqNormalized parses "name[@req]" with the normalised grammar (PackageReq::from_str_normalized).
// On failure the error is a *PackageReqParseError.
func ParsePackageReqNormalized(text string) (PackageReq, error) {
	return parsePackageReq(text, ParseVersionReqNormalized)
}

func parsePackageReq(text string, parseVersionReq func(string) (VersionReq, error)) (PackageReq, error) {
	req, err := parsePackageReqInner(text, parseVersionReq)
	if err != nil {
		if !strings.HasPrefix(text, "@") && strings.Contains(text, "/") {
			err = ErrMissingAtSymbol
		}
		return PackageReq{}, &PackageReqParseError{Text: text, Source: err}
	}
	return req, nil
}

func parsePackageReqInner(text string, parseVersionReq func(string) (VersionReq, error)) (PackageReq, error) {
	req, path, err := parseWithPath(text, parseVersionReq)
	if err != nil {
		return PackageReq{}, err
	}
	if path != "" {
		return PackageReq{}, &ParseError{
			msg: "Invalid specifier version requirement",
			src: newFailure(text[len(text)-len(path)-1:], "Unexpected character '/'"),
		}
	}
	return req, nil
}

// parseWithPath splits "[/]name[@req][/path]" and returns the request and the path.
func parseWithPath(input string, parseVersionReq func(string) (VersionReq, error)) (PackageReq, string, error) {
	// strip the leading slash, which might come from an import map
	input = strings.TrimPrefix(input, "/")
	first, input := cutSlash(input)
	if first == "" {
		return PackageReq{}, "", ErrNoPackageName
	}
	scope, last, subPath := "", first, input
	// if it starts with an @, the second part is the rest of the name
	if strings.HasPrefix(first, "@") {
		second, rest := cutSlash(input)
		if second == "" {
			return PackageReq{}, "", ErrInvalidPackageName
		}
		scope, last, subPath = first, second, rest
	}
	versionReq := WildcardVersionReq()
	if i := strings.LastIndexByte(last, '@'); i >= 0 {
		vr, err := parseVersionReq(last[i+1:])
		if err != nil {
			return PackageReq{}, "", err
		}
		last, versionReq = last[:i], vr
	}
	name := last
	if scope != "" {
		name = scope + "/" + last
	}
	return PackageReq{Name: name, VersionReq: versionReq}, subPath, nil
}

// cutSlash is Rust's split_once('/').unwrap_or((s, "")).
func cutSlash(s string) (string, string) {
	before, after, _ := strings.Cut(s, "/")
	return before, after
}

// String returns "name@raw requirement", or just the name for the wildcard "*".
func (r PackageReq) String() string {
	if r.VersionReq.VersionText() == "*" {
		return r.Name
	}
	return r.Name + "@" + r.VersionReq.VersionText()
}

// Normalized returns "name@" followed by the normalised requirement; the wildcard prints "name@*". This is the
// form deno.lock stores.
func (r PackageReq) Normalized() string { return r.Name + "@" + r.VersionReq.Normalized() }

// Equal compares the names and the parsed requirements.
func (r PackageReq) Equal(o PackageReq) bool {
	return r.Name == o.Name && r.VersionReq.Equal(o.VersionReq)
}

// Compare orders by name and then by requirement, like the Rust Ord implementation: it sorts so that resolving
// the requirements in this order leads to the fewest duplicate packages. A tag sorts before a range, tags sort
// descending, ranges with a higher lower bound and then a lower upper bound sort first, and the wildcard sorts
// last. The raw text is ignored.
func (r PackageReq) Compare(o PackageReq) int {
	if c := strings.Compare(r.Name, o.Name); c != 0 {
		return c
	}
	return compareVersionReq(r.VersionReq, o.VersionReq)
}

func compareVersionReq(a, b VersionReq) int {
	switch {
	case a.inner.isTag && b.inner.isTag:
		return strings.Compare(b.inner.tag, a.inner.tag) // descending
	case a.inner.isTag:
		return -1 // prefer a, since it is a tag
	case b.inner.isTag:
		return 1 // prefer b, since it is a tag
	}
	for i := range min(len(a.inner.set), len(b.inner.set)) {
		if c := compareRange(a.inner.set[i], b.inner.set[i]); c != 0 {
			return c
		}
	}
	// prefer the one with the fewest items
	return cmp.Compare(len(a.inner.set), len(b.inner.set))
}

func compareRange(a, b versionRange) int {
	if c := compareBound(a.start, b.start, func(x, y versionBound) int {
		// prefer higher versions first
		return cmp.Or(y.version.Compare(x.version), compareBoundKind(x.kind, y.kind))
	}); c != 0 {
		return c
	}
	return compareBound(a.end, b.end, func(x, y versionBound) int {
		// prefer lower versions first
		return cmp.Or(x.version.Compare(y.version), compareBoundKind(y.kind, x.kind)) // reversed
	})
}

func compareBound(a, b rangeBound, compareVersionBound func(x, y versionBound) int) int {
	switch {
	case a.unbounded && b.unbounded:
		return 0
	case a.unbounded:
		return 1
	case b.unbounded:
		return -1
	}
	return compareVersionBound(a.bound, b.bound)
}

// compareBoundKind sorts an exclusive bound before an inclusive one.
func compareBoundKind(a, b boundKind) int {
	switch a {
	case b:
		return 0
	case exclusive:
		return -1
	}
	return 1
}

// JsrDepPackageReq is a requirement on a package of either registry, written "jsr:name@req" or "npm:name@req".
// Deno uses it for the specifiers of a lockfile and the dependencies of a workspace.
type JsrDepPackageReq struct {
	Kind PackageKind
	Req  PackageReq
}

// ParseJsrDepPackageReq parses a "jsr:" or "npm:" requirement with the strict grammar. A text with another
// scheme fails with a *NotExpectedSchemeError; a bad requirement fails with a *PackageReqParseError.
func ParseJsrDepPackageReq(text string) (JsrDepPackageReq, error) {
	return parseJsrDep(text, ParsePackageReq)
}

// ParseJsrDepPackageReqLoose is ParseJsrDepPackageReq with the npm grammar for the requirement.
func ParseJsrDepPackageReqLoose(text string) (JsrDepPackageReq, error) {
	return parseJsrDep(text, ParsePackageReqLoose)
}

// ParseJsrDepPackageReqNormalized is ParseJsrDepPackageReq with the normalised grammar for the requirement.
func ParseJsrDepPackageReqNormalized(text string) (JsrDepPackageReq, error) {
	return parseJsrDep(text, ParsePackageReqNormalized)
}

func parseJsrDep(text string, parseReq func(string) (PackageReq, error)) (JsrDepPackageReq, error) {
	kind := Jsr
	rest, ok := strings.CutPrefix(text, "jsr:")
	if !ok {
		kind = Npm
		rest, ok = strings.CutPrefix(text, "npm:")
	}
	if !ok {
		scheme, _, _ := strings.Cut(text, ":")
		return JsrDepPackageReq{}, &NotExpectedSchemeError{Scheme: scheme}
	}
	req, err := parseReq(rest)
	if err != nil {
		return JsrDepPackageReq{}, err
	}
	return JsrDepPackageReq{Kind: kind, Req: req}, nil
}

// String returns the scheme and the requirement as written: "jsr:@std/path@^1.0.0", or "jsr:@std/path" for the
// wildcard.
func (d JsrDepPackageReq) String() string { return d.Kind.SchemeWithColon() + d.Req.String() }

// Normalized returns the scheme, the name and the normalised requirement, such as "jsr:@std/path@1" or
// "npm:left-pad@*". deno.lock stores this form, and its maps are sorted by it.
func (d JsrDepPackageReq) Normalized() string {
	return d.Kind.SchemeWithColon() + d.Req.Name + "@" + d.Req.VersionReq.Normalized()
}

// Equal compares the kinds and the requirements.
func (d JsrDepPackageReq) Equal(o JsrDepPackageReq) bool {
	return d.Kind == o.Kind && d.Req.Equal(o.Req)
}

// Compare orders by kind (JSR first) and then by requirement.
func (d JsrDepPackageReq) Compare(o JsrDepPackageReq) int {
	return cmp.Or(cmp.Compare(d.Kind, o.Kind), d.Req.Compare(o.Req))
}
