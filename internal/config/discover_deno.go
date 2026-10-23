package config

import (
	"bytes"
	"encoding/json"
	"errors"
	"io/fs"
	"path"
	"path/filepath"
	"runtime"
	"slices"
	"strings"

	"github.com/celest-io/remeris/internal/diag"
	"github.com/celest-io/remeris/internal/glob"
	"github.com/celest-io/remeris/pkg/versioning"
	"github.com/celest-io/remeris/pkg/versioning/versionedfile"
)

const (
	// Knope's workspaces::deno_discover and workspaces::deno_lockfile_path; the third is the size cap of the Cargo
	// detector, shared.
	codeDenoDiscover     = "remeris::workspace::deno_discover"
	codeDenoLockfilePath = "remeris::workspace::deno_lockfile_path"
	codeDenoTooLarge     = "remeris::workspace::too_large"

	denoJSONFile   = "deno.json"
	denoJSONCFile  = "deno.jsonc"
	packageJSON    = "package.json"
	denoLockFile   = "deno.lock"
	denoDiscover   = "Failed to discover Deno workspace: "
	denoConfigKey  = "workspace"
	prefixJSR      = "jsr:"
	prefixNPM      = "npm:"
	rootDir        = "."
	parentDirNames = ".."
)

// denoWindows is true when paths are Windows paths; the checks take it as a parameter so that tests can run both.
var denoWindows = runtime.GOOS == "windows"

// denoConfigNames are the files that make a directory a workspace member, in deno_config's order.
var denoConfigNames = []string{denoJSONFile, denoJSONCFile, packageJSON}

// denoWorkspace is the Deno detector (spec F20.4, Knope's deno_workspaces).
//
// It follows deno_config 0.97 for the working directory only: remeris does not walk up to a parent workspace, and
// the `workspaces` of a root package.json are the npm detector's, not folded in here. A root deno.json (or
// deno.jsonc) is required; its `workspace` key is optional. The root folder comes first, then the members sorted by
// their directory URL. A folder is a package when its deno.json(c) has a name and a version, or else its package.json
// has both. Errors are "Failed to discover Deno workspace: ..." diagnostics.
func denoWorkspace(root fs.StatFS) ([]PackageConfig, error) {
	// Without a root deno.json(c) this is not a Deno workspace, and a package.json that npm owns is not read here.
	if _, err := root.Stat(denoJSONFile); err != nil {
		if _, err := root.Stat(denoJSONCFile); err != nil {
			return nil, nil //nolint:nilerr // a root without a Deno config is no Deno workspace, whatever the reason
		}
	}
	rootFolder, err := loadDenoFolder(root, rootDir)
	if err != nil {
		return nil, err
	}
	if rootFolder == nil || rootFolder.deno == nil {
		return nil, nil
	}
	folders, err := denoMemberFolders(root, rootFolder)
	if err != nil {
		return nil, err
	}
	folders = append([]*denoFolder{rootFolder}, folders...)
	if err := checkDenoDuplicateNames(folders); err != nil {
		return nil, err
	}

	lock, err := denoLockfile(root, rootFolder)
	if err != nil {
		return nil, err
	}
	var pkgs []*denoPackage
	for _, f := range folders {
		if p := f.asPackage(); p != nil {
			pkgs = append(pkgs, p)
		}
	}
	out := make([]PackageConfig, 0, len(pkgs))
	for _, target := range pkgs {
		cfg, err := target.packageConfig(pkgs, lock)
		if err != nil {
			return nil, err
		}
		out = append(out, cfg)
	}
	return out, nil
}

// denoFolder holds the configuration files of one directory: its deno.json (or else deno.jsonc) and its package.json.
type denoFolder struct {
	dir      string
	denoPath string
	deno     map[string]any
	pkgPath  string
	pkg      map[string]any
}

// denoPackage is a folder that is a package (it has a name and a version), with the file that says so.
type denoPackage struct {
	name   string
	config string
	dir    string
	folder *denoFolder
}

func denoFailure(format string, args ...any) *diag.Error {
	return diag.Newf(denoDiscover+format, args...).WithCode(codeDenoDiscover)
}

// loadDenoFolder reads the configuration files of dir. It returns nil when there is none, and an error when one does
// not parse. deno.json wins over deno.jsonc.
func loadDenoFolder(root fs.StatFS, dir string) (*denoFolder, error) {
	f := &denoFolder{dir: dir}
	for _, name := range []string{denoJSONFile, denoJSONCFile} {
		doc, found, err := readDenoManifest(root, path.Join(dir, name), true)
		if err != nil {
			return nil, err
		}
		if found {
			f.denoPath, f.deno = path.Join(dir, name), doc
			break
		}
	}
	doc, found, err := readDenoManifest(root, path.Join(dir, packageJSON), false)
	if err != nil {
		return nil, err
	}
	if found {
		f.pkgPath, f.pkg = path.Join(dir, packageJSON), doc
	}
	if f.deno == nil && f.pkg == nil {
		return nil, nil
	}
	return f, nil
}

// readDenoManifest reads a JSON object (with comments and trailing commas when jsonc). A missing file is not an
// error. A file that cannot be read, is not a regular file, is larger than 8 MiB, is nested deeper than
// maxJSONDepth, is not an object or has a field of the wrong type (`name` or `version` that is not a string, a
// `lock.path` that is not a string) is. The messages name the file and never quote it. As in Deno, an empty
// deno.json(c) and one that holds `null` are the default (empty) configuration.
func readDenoManifest(root fs.StatFS, name string, jsonc bool) (doc map[string]any, found bool, err error) {
	data, err := readManifestFile(root, name)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return nil, false, nil
	case errors.Is(err, errManifestTooLarge):
		return nil, false, denoFailure("%q is larger than 8 MiB", name).WithCode(codeDenoTooLarge)
	case err != nil:
		return nil, false, denoFailure("Could not read %q", name)
	}
	if jsonc {
		if data, err = standardizeJSONC(data); err != nil {
			return nil, false, denoParseFailure(name, err)
		}
		if len(bytes.TrimSpace(data)) == 0 {
			return map[string]any{}, true, nil
		}
	} else {
		data = bytes.TrimPrefix(data, []byte("\xef\xbb\xbf"))
		if jsonDepthExceeded(data, maxJSONDepth) {
			return nil, false, denoParseFailure(name, errJSONTooDeep)
		}
	}
	if err := json.Unmarshal(data, &doc); err != nil || (doc == nil && !jsonc) || !denoFieldTypesOK(doc) {
		return nil, false, denoParseFailure(name, nil)
	}
	if doc == nil {
		doc = map[string]any{}
	}
	return doc, true, nil
}

func denoParseFailure(name string, cause error) *diag.Error {
	if errors.Is(cause, errJSONTooDeep) {
		return denoFailure("Could not parse %q: %s", name, cause)
	}
	return denoFailure("Could not parse %q", name)
}

// denoFieldTypesOK checks the typed fields deno_config deserializes at load time: `name` and `version` are optional
// strings, and `lock`, when an object, has an optional string `path`.
func denoFieldTypesOK(doc map[string]any) bool {
	for _, key := range []string{"name", "version"} {
		if v, ok := doc[key]; ok && v != nil {
			if _, isString := v.(string); !isString {
				return false
			}
		}
	}
	if lock, ok := doc["lock"].(map[string]any); ok {
		if v, ok := lock["path"]; ok && v != nil {
			if _, isString := v.(string); !isString {
				return false
			}
		}
	}
	return true
}

// denoMembers parses the `workspace` key: absent or null is no members, an array of strings is the members, and an
// object must be exactly {"members": [strings]}.
func denoMembers(doc map[string]any) ([]string, error) {
	bad := denoFailure("Failed to parse \"workspace\" configuration.")
	var list []any
	switch w := doc[denoConfigKey].(type) {
	case nil:
		return nil, nil
	case []any:
		list = w
	case map[string]any:
		if len(w) != 1 {
			return nil, bad
		}
		var ok bool
		if list, ok = w["members"].([]any); !ok {
			return nil, bad
		}
	default:
		return nil, bad
	}
	members := make([]string, 0, len(list))
	for _, m := range list {
		s, ok := m.(string)
		if !ok {
			return nil, bad
		}
		members = append(members, s)
	}
	return members, nil
}

// denoMemberDir is one resolved member: the directory, and the text it was written as. Two members with the same
// directory and the same text are one; with different text they are a duplicate (deno_config's IndexSet of pairs).
type denoMemberDir struct {
	raw, dir string
	pattern  bool
}

func denoMemberFolders(root fs.StatFS, rootFolder *denoFolder) ([]*denoFolder, error) {
	members, err := denoMembers(rootFolder.deno)
	if err != nil {
		return nil, err
	}
	var plain, patterns, negated []string
	for _, m := range members {
		switch {
		case strings.HasPrefix(m, "!"):
			negated = append(negated, m[1:])
		case hasDenoGlobChars(m):
			patterns = append(patterns, m)
		default:
			plain = append(plain, m)
		}
	}

	var dirs []denoMemberDir
	for _, raw := range plain {
		dir, err := resolveDenoMember(raw, denoWindows)
		if err != nil {
			return nil, err
		}
		dirs = append(dirs, denoMemberDir{raw: raw, dir: dir})
	}
	globbed, err := globDenoMembers(root, patterns, negated)
	if err != nil {
		return nil, err
	}
	dirs = append(dirs, globbed...)

	byDir := map[string]denoMemberDir{}
	var folders []*denoFolder
	for _, m := range dirs {
		if m.dir == rootDir {
			return nil, denoFailure("Remove the reference to the current config file (%q) in \"workspaces\".", m.raw)
		}
		if prev, seen := byDir[m.dir]; seen {
			if prev == m {
				continue
			}
			return nil, denoFailure("Cannot specify a workspace member twice (%q).", m.raw)
		}
		f, err := loadDenoFolder(root, m.dir)
		if err != nil {
			return nil, err
		}
		if f == nil {
			return nil, denoMemberNotFound(m)
		}
		byDir[m.dir] = m
		folders = append(folders, f)
	}
	// deno_config keeps the members in a BTreeMap keyed by the directory URL, which ends in a slash: "a-b/" < "a/".
	slices.SortFunc(folders, func(a, b *denoFolder) int { return strings.Compare(a.dir+"/", b.dir+"/") })
	return folders, nil
}

func denoMemberNotFound(m denoMemberDir) error {
	last := path.Base(m.dir)
	if !m.pattern && slices.Contains(denoConfigNames, last) {
		return denoFailure("Could not find config file for workspace member in %q. Ensure you specify the directory "+
			"and not the configuration file in the workspace member.", m.dir+"/")
	}
	return denoFailure("Could not find config file for workspace member in %q.", m.dir+"/")
}

// unsafeDenoPath reports whether p, a member or lock path of deno.json, is absolute or names another volume, which
// would leave the project: a rooted path, a drive or UNC path, and on Windows (windows true) any backslash, since it
// separates components there. The check is lexical and runs before the path is cleaned.
func unsafeDenoPath(p string, windows bool) bool {
	if p == "" || strings.HasPrefix(p, "/") || filepath.IsAbs(p) || filepath.VolumeName(p) != "" {
		return true
	}
	if windows {
		if strings.Contains(p, `\`) {
			return true
		}
		if len(p) >= 2 && p[1] == ':' && (p[0]|0x20 >= 'a' && p[0]|0x20 <= 'z') {
			return true
		}
	}
	return false
}

// resolveDenoMember joins a member written as a path to the root and checks it stays below it.
func resolveDenoMember(raw string, windows bool) (string, error) {
	if unsafeDenoPath(raw, windows) {
		return "", denoNonDescendant(raw)
	}
	dir := path.Clean(raw)
	if dir == parentDirNames || strings.HasPrefix(dir, parentDirNames+"/") {
		return "", denoNonDescendant(raw)
	}
	return dir, nil
}

func denoNonDescendant(raw string) error {
	return denoFailure("Workspace member must be nested in a directory under the workspace. Member: %q", raw)
}

// hasDenoGlobChars is deno_config's is_glob_pattern: only `*` and `?` count, and URLs are never patterns.
func hasDenoGlobChars(s string) bool {
	for _, p := range []string{"http://", "https://", "file://", prefixNPM, prefixJSR} {
		if strings.HasPrefix(s, p) {
			return false
		}
	}
	return strings.ContainsAny(s, "*?")
}

// globDenoMembers finds the directories under the root that hold a deno.json, deno.jsonc or package.json and match one
// of the patterns. deno_config turns `!p` into the exclusion of `p/deno.json` (and the other two names), so a
// directory is excluded when it is matched by `p` itself, never because it is below a match. Brackets are literal.
//
// The walk is internal/glob's: it cannot prune `.git` and `node_modules` while walking, nor keep out of linked
// directories, so those matches are dropped afterwards, as are matches that deno_config's hidden-file rule refuses.
func globDenoMembers(root fs.StatFS, patterns, negated []string) ([]denoMemberDir, error) {
	excluded := map[string]bool{}
	for _, q := range uniqueStrings(negated) {
		matches, err := expandDenoPattern(root, q, "")
		if err != nil {
			return nil, err
		}
		for _, m := range matches {
			excluded[m] = true
		}
	}
	linked := map[string]bool{}
	var out []denoMemberDir
	for _, p := range uniqueStrings(patterns) {
		for _, name := range denoConfigNames {
			files, err := expandDenoPattern(root, p, name)
			if err != nil {
				return nil, err
			}
			components := denoPatternComponents(p, name)
			for _, file := range files {
				dir := path.Dir(file)
				if excluded[dir] || slices.ContainsFunc(strings.Split(dir, "/"), isIgnoredDenoDir) ||
					!hiddenAllowed(components, strings.Split(file, "/")) || throughSymlink(root, dir, linked) {
					continue
				}
				out = append(out, denoMemberDir{raw: dir, dir: dir, pattern: true})
			}
		}
	}
	return out, nil
}

func uniqueStrings(in []string) []string {
	var out []string
	for _, s := range in {
		if !slices.Contains(out, s) {
			out = append(out, s)
		}
	}
	return out
}

func isIgnoredDenoDir(segment string) bool { return segment == ".git" || segment == "node_modules" }

// throughSymlink reports whether a component of dir is a symbolic link. deno_config reads lstat metadata and never
// enters a linked directory. Results are cached in seen.
func throughSymlink(root fs.FS, dir string, seen map[string]bool) bool {
	if dir == rootDir {
		return false
	}
	prefix := ""
	for _, c := range strings.Split(dir, "/") {
		prefix = path.Join(prefix, c)
		link, known := seen[prefix]
		if !known {
			info, err := fs.Lstat(root, prefix)
			link = err == nil && info.Mode()&fs.ModeSymlink != 0
			seen[prefix] = link
		}
		if link {
			return true
		}
	}
	return false
}

// denoPatternComponents splits member/name the way expandDenoPattern builds its pattern, without `.` components.
func denoPatternComponents(member, name string) []string {
	var out []string
	for _, c := range strings.Split(member+"/"+name, "/") {
		if c != "" && c != "." {
			out = append(out, c)
		}
	}
	return out
}

// hiddenAllowed applies the leading-dot rule deno_config matches with (the glob crate's require_literal_leading_dot):
// a component of the match that starts with `.` must be matched by a pattern component that starts with a literal `.`,
// and `**` never matches such a component. internal/glob lets `*` match a leading dot, so the matches are filtered
// here. The pattern components are assumed to match already; only the dots are checked.
func hiddenAllowed(pattern, match []string) bool {
	type key struct{ p, m int }
	memo := map[key]bool{}
	var ok func(p, m int) bool
	ok = func(p, m int) bool {
		if p == len(pattern) {
			return m == len(match)
		}
		k := key{p, m}
		if v, done := memo[k]; done {
			return v
		}
		memo[k] = false
		res := false
		if pattern[p] == "**" {
			for j := m; ; j++ {
				if ok(p+1, j) {
					res = true
					break
				}
				if j >= len(match) || strings.HasPrefix(match[j], ".") {
					break
				}
			}
		} else if m < len(match) && (!strings.HasPrefix(match[m], ".") || strings.HasPrefix(pattern[p], ".")) {
			res = ok(p+1, m+1)
		}
		memo[k] = res
		return res
	}
	return ok(0, 0)
}

// expandDenoPattern expands member/name (or member alone when name is empty) the way deno_config builds its patterns:
// a leading "./" and a trailing "/" are dropped. A member without glob characters is a plain path, which is not
// checked for existence here. The matching is case sensitive; deno_config's is not.
func expandDenoPattern(root fs.FS, member, name string) ([]string, error) {
	member = strings.TrimSuffix(strings.TrimPrefix(member, "./"), "/")
	pattern := strings.NewReplacer("[", "[[]", "]", "[]]").Replace(member)
	if name != "" {
		pattern += "/" + name
	}
	if !hasDenoGlobChars(member) && name == "" {
		return []string{path.Clean(member)}, nil
	}
	files, err := glob.Glob(root, pattern)
	var readErr *glob.ReadError
	if err != nil && !errors.As(err, &readErr) {
		return nil, denoFailure("Failed converting Deno workspace member %q to pattern for config %q.", member, denoJSONFile)
	}
	return files, nil
}

// checkDenoDuplicateNames: two deno.json(c) files of the workspace with the same `name` are an error. The names of
// package.json files are not compared.
func checkDenoDuplicateNames(folders []*denoFolder) error {
	seen := map[string]string{}
	for _, f := range folders {
		name, _ := f.deno["name"].(string)
		if f.deno == nil || name == "" {
			continue
		}
		if other, dup := seen[name]; dup {
			return denoFailure("The %q package (%q) cannot have the same name as the package at %q.",
				name, f.denoPath, other)
		}
		seen[name] = f.denoPath
	}
	return nil
}

// asPackage applies Knope's folder rule: the deno.json(c) with a name and a version, else the package.json with both,
// else nothing.
func (f *denoFolder) asPackage() *denoPackage {
	if name, ok := nameAndVersion(f.deno); ok {
		return &denoPackage{name: name, config: f.denoPath, dir: f.dir, folder: f}
	}
	if name, ok := nameAndVersion(f.pkg); ok {
		return &denoPackage{name: name, config: f.pkgPath, dir: f.dir, folder: f}
	}
	return nil
}

func nameAndVersion(doc map[string]any) (string, bool) {
	name, _ := doc["name"].(string)
	version, _ := doc["version"].(string)
	return name, name != "" && version != ""
}

// denoLockfile is the lockfile of the root config: the `lock` setting (false: none; a path, or an object with a
// `path`; otherwise deno.lock next to the config), only when the file exists. A path that leaves the project is an
// error, whether or not the file exists, and so is a lock that is not a regular file (the release would write to it).
func denoLockfile(root fs.StatFS, rootFolder *denoFolder) (string, error) {
	lock := denoLockFile
	switch v := rootFolder.deno["lock"].(type) {
	case nil:
	case bool:
		if !v {
			return "", nil
		}
	case string:
		lock = v
	case map[string]any:
		if p, ok := v["path"].(string); ok {
			lock = p
		}
	default:
		return "", diag.New("Failed to resolve deno.lock path: the \"lock\" setting is not a boolean, a path or an object").
			WithCode(codeDenoLockfilePath)
	}
	if unsafeDenoPath(lock, denoWindows) {
		return "", denoLockOutside(lock)
	}
	clean := path.Clean(lock)
	if clean == parentDirNames || strings.HasPrefix(clean, parentDirNames+"/") {
		return "", denoLockOutside(lock)
	}
	info, err := root.Stat(clean)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return "", nil
	case err != nil:
		return "", diag.Newf("Failed to resolve deno.lock path: could not read %q", clean).WithCode(codeDenoLockfilePath)
	case !info.Mode().IsRegular():
		return "", diag.Newf("Failed to resolve deno.lock path: %q is not a regular file", clean).
			WithCode(codeDenoLockfilePath)
	}
	return clean, nil
}

func denoLockOutside(lock string) error {
	return diag.Newf("Failed to resolve deno.lock path: %q is outside the project directory", lock).
		WithCode(codeDenoLockfilePath)
}

// packageConfig builds the package of t (spec F20.4): its config, the lockfile entry, and the config of every other
// package that depends on it.
func (t *denoPackage) packageConfig(all []*denoPackage, lock string) (PackageConfig, error) {
	files := []string{t.config}
	deps := []string{""}
	if lock != "" {
		files, deps = append(files, lock), append(deps, t.name)
	}
	for _, d := range all {
		if d.config != t.config && d.dependsOn(t) {
			files, deps = append(files, d.config), append(deps, t.name)
		}
	}
	configs := make([]versionedfile.Config, 0, len(files))
	for i, file := range files {
		var dependency *string
		if deps[i] != "" {
			dependency = &deps[i]
		}
		cfg, err := versionedfile.NewConfig(file, dependency, nil)
		if err != nil {
			return PackageConfig{}, err
		}
		configs = append(configs, cfg)
	}
	return PackageConfig{
		Name:           versioning.CustomName(t.name),
		VersionedFiles: configs,
		Changelog:      path.Join(t.dir, defaultChangelog),
		Scopes:         []string{t.name},
	}, nil
}

// dependsOn is Knope's dependent_depends_on: d's deno.json has a `jsr:` requirement named like target (and target has
// a deno.json), or an `npm:` requirement (and target has a package.json); or d's package.json depends on target.
func (d *denoPackage) dependsOn(target *denoPackage) bool {
	for _, dep := range denoImportRequirements(d.folder.deno) {
		switch {
		case dep.name != target.name:
		case dep.jsr && target.folder.deno != nil, !dep.jsr && target.folder.pkg != nil:
			return true
		}
	}
	return denoPackageJSONDependsOn(d, target)
}

// denoRequirement is a `jsr:` or `npm:` package requirement; only its name matters here.
type denoRequirement struct {
	jsr  bool
	name string
}

// denoImportRequirements lists the requirements that deno_config's ConfigFile::dependencies finds: the values of
// `imports`, of every scope of `scopes`, and the `jsxImportSource`, `jsxImportSourceTypes` and `types` entries of
// `compilerOptions`.
func denoImportRequirements(doc map[string]any) []denoRequirement {
	var specs []string
	collect := func(v any) {
		obj, _ := v.(map[string]any)
		for _, s := range obj {
			if str, ok := s.(string); ok {
				specs = append(specs, str)
			}
		}
	}
	collect(doc["imports"])
	scopes, _ := doc["scopes"].(map[string]any)
	for _, scope := range scopes {
		collect(scope)
	}
	if opts, ok := doc["compilerOptions"].(map[string]any); ok {
		for _, key := range []string{"jsxImportSource", "jsxImportSourceTypes"} {
			if s, ok := opts[key].(string); ok {
				specs = append(specs, s)
			}
		}
		types, _ := opts["types"].([]any)
		for _, t := range types {
			if s, ok := t.(string); ok {
				specs = append(specs, s)
			}
		}
	}
	var out []denoRequirement
	for _, s := range specs {
		if req, ok := parseDenoRequirement(s); ok {
			out = append(out, req)
		}
	}
	return out
}

// parseDenoRequirement reads `jsr:[@scope/]name[@req][/path]` and `npm:[@scope/]name[@req][/path]` the way
// deno_semver finds the package name: the path starts at the first `/` (the second for a name that starts with `@`),
// and the requirement is split at the last `@` after the first character. jsr names need no scope, and the scope may
// be empty (`jsr:@/a` is a requirement on `@/a`). The version requirement is not parsed, so `jsr:@a/b@nonsense`
// counts as a requirement on `@a/b`, where Deno would reject it.
func parseDenoRequirement(spec string) (denoRequirement, bool) {
	var jsr bool
	var rest string
	switch {
	case strings.HasPrefix(spec, prefixJSR):
		jsr, rest = true, strings.TrimPrefix(spec, prefixJSR)
	case strings.HasPrefix(spec, prefixNPM):
		rest = strings.TrimPrefix(spec, prefixNPM)
	default:
		return denoRequirement{}, false
	}
	rest = strings.TrimPrefix(rest, "/")
	nameReq := rest
	if strings.HasPrefix(rest, "@") {
		slash := strings.IndexByte(rest, '/')
		if slash < 0 {
			return denoRequirement{}, false
		}
		if next := strings.IndexByte(rest[slash+1:], '/'); next >= 0 {
			nameReq = rest[:slash+1+next]
		}
	} else if slash := strings.IndexByte(rest, '/'); slash >= 0 {
		nameReq = rest[:slash]
	}
	name := nameReq
	if at := strings.LastIndexByte(nameReq, '@'); at > 0 {
		name = nameReq[:at]
	}
	if name == "" || strings.HasSuffix(name, "/") {
		return denoRequirement{}, false
	}
	return denoRequirement{jsr: jsr, name: name}, true
}
