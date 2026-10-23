package config

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"path"
	"slices"
	"strings"

	"github.com/tailscale/hujson"

	"github.com/celest-io/remeris/internal/diag"
	"github.com/celest-io/remeris/internal/glob"
	"github.com/celest-io/remeris/pkg/versioning"
	"github.com/celest-io/remeris/pkg/versioning/versionedfile"
)

const (
	// Knope's workspaces::deno_discover and workspaces::deno_lockfile_path.
	codeDenoDiscover     = "remeris::workspace::deno_discover"
	codeDenoLockfilePath = "remeris::workspace::deno_lockfile_path"

	// maxDenoManifestBytes caps every manifest read: a configuration file larger than this is an error.
	maxDenoManifestBytes = 8 << 20

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
func loadDenoFolder(root fs.FS, dir string) (*denoFolder, error) {
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
// error. A file that cannot be read, is larger than maxDenoManifestBytes or is not an object is; the message names
// the file and never quotes it. An empty deno.json(c) is an empty object, as in Deno.
func readDenoManifest(root fs.FS, name string, jsonc bool) (doc map[string]any, found bool, err error) {
	f, err := root.Open(name)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, false, nil
		}
		return nil, false, denoFailure("Could not read %s", name)
	}
	defer func() { _ = f.Close() }()
	data, err := io.ReadAll(io.LimitReader(f, maxDenoManifestBytes+1))
	if err != nil {
		return nil, false, denoFailure("Could not read %s", name)
	}
	if len(data) > maxDenoManifestBytes {
		return nil, false, denoFailure("%s is larger than 8 MiB", name)
	}
	data = bytes.TrimPrefix(data, []byte("\xef\xbb\xbf"))
	if jsonc {
		if data, err = hujson.Standardize(data); err != nil {
			return nil, false, denoFailure("Could not parse %s", name)
		}
		if len(bytes.TrimSpace(data)) == 0 {
			return map[string]any{}, true, nil
		}
	}
	if err := json.Unmarshal(data, &doc); err != nil || doc == nil {
		return nil, false, denoFailure("Could not parse %s", name)
	}
	return doc, true, nil
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

func denoMemberFolders(root fs.FS, rootFolder *denoFolder) ([]*denoFolder, error) {
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
		dir, err := resolveDenoMember(raw)
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
			return nil, denoFailure("Cannot specify a workspace member twice ('%s').", m.raw)
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
		return denoFailure("Could not find config file for workspace member in '%s/'. Ensure you specify the directory "+
			"and not the configuration file in the workspace member.", m.dir)
	}
	return denoFailure("Could not find config file for workspace member in '%s/'.", m.dir)
}

// resolveDenoMember joins a member written as a path to the root and checks it stays below it.
func resolveDenoMember(raw string) (string, error) {
	if raw == "" || strings.HasPrefix(raw, "/") {
		return "", denoNonDescendant(raw)
	}
	dir := path.Clean(raw)
	if dir == parentDirNames || strings.HasPrefix(dir, parentDirNames+"/") {
		return "", denoNonDescendant(raw)
	}
	return dir, nil
}

func denoNonDescendant(raw string) error {
	return denoFailure("Workspace member must be nested in a directory under the workspace. Member: %s", raw)
}

// hasDenoGlobChars is deno_config's is_glob_pattern: only `*` and `?` count, and URLs are never patterns.
func hasDenoGlobChars(s string) bool {
	for _, p := range []string{"http://", "https://", "file://", prefixNPM} {
		if strings.HasPrefix(s, p) {
			return false
		}
	}
	return strings.ContainsAny(s, "*?")
}

// globDenoMembers finds the directories under the root that hold a deno.json, deno.jsonc or package.json and match one
// of the patterns, minus those at or below a directory matched by a negated pattern. `.git` and `node_modules`
// directories are never members. Brackets are literal, as in deno_config.
func globDenoMembers(root fs.FS, patterns, negated []string) ([]denoMemberDir, error) {
	var excluded []string
	for _, q := range negated {
		matches, err := expandDenoPattern(root, q, "")
		if err != nil {
			return nil, err
		}
		excluded = append(excluded, matches...)
	}
	var out []denoMemberDir
	for _, p := range patterns {
		for _, name := range denoConfigNames {
			files, err := expandDenoPattern(root, p, name)
			if err != nil {
				return nil, err
			}
			for _, file := range files {
				dir := path.Dir(file)
				if slices.ContainsFunc(strings.Split(dir, "/"), isIgnoredDenoDir) ||
					slices.ContainsFunc(excluded, func(e string) bool { return dir == e || strings.HasPrefix(dir, e+"/") }) {
					continue
				}
				out = append(out, denoMemberDir{raw: dir, dir: dir, pattern: true})
			}
		}
	}
	return out, nil
}

func isIgnoredDenoDir(segment string) bool { return segment == ".git" || segment == "node_modules" }

// expandDenoPattern expands member/name (or member alone when name is empty) the way deno_config builds its patterns:
// a leading "./" and a trailing "/" are dropped. A member without glob characters is a plain path, which is not
// checked for existence here.
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
		return nil, denoFailure(
			"Failed converting Deno workspace member '%s' to pattern for config '%s'.",
			member,
			denoJSONFile,
		)
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
			return denoFailure("The '%s' package ('%s') cannot have the same name as the package at '%s'.",
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
// error, whether or not the file exists.
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
	lock = path.Clean(lock)
	if lock == parentDirNames || strings.HasPrefix(lock, parentDirNames+"/") || strings.HasPrefix(lock, "/") {
		return "", diag.Newf("Failed to resolve deno.lock path: %s is outside the project directory", lock).
			WithCode(codeDenoLockfilePath)
	}
	if info, err := root.Stat(lock); err != nil || info.IsDir() {
		return "", nil
	}
	return lock, nil
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

// denoPackageJSONDependsOn reports whether the package.json of dependent depends on target through `file:`,
// `workspace:`, `npm:@jsr/...` or a plain name. Milestone M10 PR 65b implements it; until then the package.json
// edges are not found.
func denoPackageJSONDependsOn(_, _ *denoPackage) bool { return false }

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

// parseDenoRequirement reads `jsr:@scope/name[@req][/path]` and `npm:[@scope/]name[@req][/path]`. This is the part of
// deno_semver's grammar that finds the package name; the version requirement is not parsed, so `jsr:@a/b@nonsense`
// counts as a requirement on @a/b, where Deno would reject it.
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
	scope := ""
	if strings.HasPrefix(rest, "@") {
		slash := strings.Index(rest, "/")
		if slash < 2 {
			return denoRequirement{}, false
		}
		scope, rest = rest[:slash+1], rest[slash+1:]
	} else if jsr {
		return denoRequirement{}, false
	}
	end := strings.IndexAny(rest, "@/")
	if end < 0 {
		end = len(rest)
	}
	if end == 0 {
		return denoRequirement{}, false
	}
	return denoRequirement{jsr: jsr, name: scope + rest[:end]}, true
}
