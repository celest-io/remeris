package config

import (
	"regexp"
	"strconv"
	"strings"
)

const (
	schemeFile      = "file"
	schemeJSR       = "jsr"
	schemeNPM       = "npm"
	schemeWorkspace = "workspace"
	jsrNPMPrefix    = "@jsr/"
	versionAny      = "*"
)

// pkgJSONDepKind is the kind of a parsed package.json dependency value (deno_package_json's PackageJsonDepValue).
type pkgJSONDepKind int

const (
	pkgDepFile pkgJSONDepKind = iota
	pkgDepReq
	pkgDepWorkspace
)

// pkgJSONDep is one parsed dependency entry: a `file:` path, a package requirement (only its name matters), or a
// `workspace:` requirement (only the key matters).
type pkgJSONDep struct {
	kind pkgJSONDepKind
	// value is the path of a file dependency, or the package name of a requirement.
	value string
}

// denoPackageJSONDependsOn is Knope's dependent_depends_on for the package.json next to dependent's config: the
// entries of its `dependencies`, then of its `devDependencies` (deno_package_json's resolve_local_package_json_deps;
// peerDependencies and optionalDependencies are not read), each one tried with package_json_dep_matches. An entry
// that does not parse is skipped.
func denoPackageJSONDependsOn(dependent, target *denoPackage) bool {
	pkg := dependent.folder.pkg
	if pkg == nil {
		return false
	}
	for _, field := range []string{"dependencies", "devDependencies"} {
		deps, _ := pkg[field].(map[string]any)
		for key, raw := range deps {
			value, ok := packageJSONStringValue(raw)
			if !ok {
				continue
			}
			if dep, ok := parsePackageJSONDep(key, value); ok && dep.matches(key, dependent, target) {
				return true
			}
		}
	}
	return false
}

// packageJSONStringValue is deno_package_json's map_string: strings and numbers count, anything else is dropped.
func packageJSONStringValue(raw any) (string, bool) {
	switch v := raw.(type) {
	case string:
		return v, true
	case float64:
		return strconv.FormatFloat(v, 'f', -1, 64), true
	}
	return "", false
}

// matches is Knope's package_json_dep_matches. A `file:` path matches the directory of target or its config file
// (Knope also tries the three config names below the path, which only matches when the path is that directory).
func (d pkgJSONDep) matches(key string, dependent, target *denoPackage) bool {
	switch d.kind {
	case pkgDepFile:
		resolved, ok := logicalJoin(dependent.dir, d.value)
		return ok && (resolved == target.dir || resolved == target.config)
	case pkgDepReq:
		if strings.HasPrefix(d.value, jsrNPMPrefix) {
			scope, name, ok := strings.Cut(strings.TrimPrefix(target.name, "@"), "/")
			return ok && strings.HasPrefix(target.name, "@") && target.folder.deno != nil &&
				d.value == jsrNPMPrefix+scope+"__"+name
		}
		return target.folder.pkg != nil && d.value == target.name
	default:
		return key == target.name
	}
}

// logicalJoin resolves p against dir the way the relative-path crate's to_logical_path does, without touching the file
// system: empty and `.` segments are dropped and `..` removes the last segment. dir is relative to the project root;
// false means the result would be outside it. A path starting with `/` never matches.
func logicalJoin(dir, p string) (string, bool) {
	if strings.HasPrefix(p, "/") {
		return "", false
	}
	var parts []string
	if dir != rootDir {
		parts = strings.Split(dir, "/")
	}
	for _, seg := range strings.Split(p, "/") {
		switch seg {
		case "", ".":
		case parentDirNames:
			if len(parts) == 0 {
				return "", false
			}
			parts = parts[:len(parts)-1]
		default:
			parts = append(parts, seg)
		}
	}
	if len(parts) == 0 {
		return rootDir, true
	}
	return strings.Join(parts, "/"), true
}

// parsePackageJSONDep is deno_package_json's PackageJsonDepValue::parse: the scheme is the text before the first
// colon. `file:` keeps its path, `jsr:` becomes a requirement on the npm name `@jsr/{scope}__{name}`, `npm:` on the
// text before its last `@`, `workspace:` needs `~`, `^` or a version requirement, other schemes do not parse, and a
// value without a scheme is a requirement on the key. A false result means the entry is skipped.
func parsePackageJSONDep(key, value string) (pkgJSONDep, bool) {
	scheme, rest, hasScheme := strings.Cut(value, ":")
	if !hasScheme {
		return reqDep(key, value)
	}
	switch scheme {
	case schemeFile:
		return pkgJSONDep{kind: pkgDepFile, value: rest}, true
	case schemeJSR:
		name, version, ok := parseJSRDepValue(key, rest)
		if !ok {
			return pkgJSONDep{}, false
		}
		return reqDep(name, version)
	case schemeNPM:
		if at := strings.LastIndexByte(rest, '@'); at > 0 {
			return reqDep(rest[:at], rest[at+1:])
		}
		return reqDep(rest, versionAny)
	case schemeWorkspace:
		if rest != "~" && rest != "^" && !validNPMVersionReq(rest) {
			return pkgJSONDep{}, false
		}
		return pkgJSONDep{kind: pkgDepWorkspace}, true
	}
	return pkgJSONDep{}, false
}

func reqDep(name, version string) (pkgJSONDep, bool) {
	if !validNPMVersionReq(version) {
		return pkgJSONDep{}, false
	}
	return pkgJSONDep{kind: pkgDepReq, value: name}, true
}

// parseJSRDepValue is deno_package_json's parse_jsr_dep_value: the npm name of a JSR package and its version text.
// The name must be scoped (`@scope/name`), or else the entry does not parse.
func parseJSRDepValue(fallback, value string) (name, version string, ok bool) {
	jsrName, version := fallback, value
	switch at := strings.LastIndexByte(value, '@'); {
	case strings.HasPrefix(value, "@"):
		jsrName, version = value, versionAny
		if at > 0 {
			jsrName, version = value[:at], value[at+1:]
		}
	case at >= 0:
		jsrName, version, _ = strings.Cut(value, "@")
	}
	scope, pkg, found := strings.Cut(strings.TrimPrefix(jsrName, "@"), "/")
	if !strings.HasPrefix(jsrName, "@") || !found {
		return "", "", false
	}
	return jsrNPMPrefix + scope + "__" + pkg, version, true
}

// The npm version requirement grammar of deno_semver (parse_from_npm), reduced to whether a text parses. A text with
// several `||` alternatives always does (an alternative that does not parse is dropped); a single one parses when it
// is a valid tag or a range. Numbers are not range checked.
var (
	npmPartialRe = `v?(?:[xX*]|\d+)(?:\.(?:[xX*]|\d+)(?:\.(?:[xX*]|\d+)` +
		`(?:-?[0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*)?(?:\+[0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*)?)?)?`
	npmSimpleRe = `(?:(?:(?:>=|<=|<|>|=)\s+)?(?:~>|~|\^)=*\s*v?\s*|(?:>=|<=|<|>|=)\s*)?` + npmPartialRe
	npmRangeRe  = regexp.MustCompile(`^(?:` + npmPartialRe + `\s+-\s+` + npmPartialRe + `|` +
		npmSimpleRe + `(?:(?:\s*&&\s*|\s*,\s*|\s+)` + npmSimpleRe + `)*)$`)
)

func validNPMVersionReq(text string) bool {
	text = strings.TrimSpace(text)
	if text == "" || strings.Contains(text, "||") {
		return true
	}
	return validNPMTag(text) || npmRangeRe.MatchString(text)
}

// validNPMTag: letters, numbers, `-`, `_`, `.` and `~`.
func validNPMTag(text string) bool {
	for _, c := range text {
		if !isTagRune(c) {
			return false
		}
	}
	return true
}

func isTagRune(c rune) bool {
	return c == '-' || c == '_' || c == '.' || c == '~' ||
		(c >= '0' && c <= '9') || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || c > 127
}
