package config

import (
	"encoding/json"
	"errors"
	"io/fs"
	"path"
	"runtime"
	"strconv"
	"strings"

	"github.com/celest-io/remeris/internal/diag"
	"github.com/celest-io/remeris/internal/glob"
	"github.com/celest-io/remeris/pkg/versioning"
	"github.com/celest-io/remeris/pkg/versioning/versionedfile"
)

// Knope's workspaces::npm_glob and workspaces::npm_no_name.
const (
	codeNPMGlob   = "remeris::workspaces::npm_glob"
	codeNPMNoName = "remeris::workspaces::npm_no_name"

	npmPackageJSON  = "package.json"
	npmLockFile     = "package-lock.json"
	codeNPMTooLarge = "remeris::workspace::too_large"
)

// npmWorkspace is a directory matched by a `workspaces` pattern that holds a valid package.json.
type npmWorkspace struct {
	file string // the package.json, relative to the root and slash-separated
	dir  string // its directory, "." for the root
	doc  map[string]any
}

// npmWorkspaces is the npm detector (spec F20.3, Knope's npm_workspaces).
//
// The `workspaces` array of ./package.json is expanded pattern by pattern, in order. A match that is a file, or a
// directory whose package.json cannot be read or parsed, is skipped. A pattern that does not compile is an error,
// and so is a workspace without a name, but only once every pattern has been expanded. Every workspace becomes a
// package; see npmPackage.
func npmWorkspaces(root fs.StatFS) ([]PackageConfig, error) {
	workspaces, err := collectNPMWorkspaces(root)
	if err != nil {
		return nil, err
	}
	names := make([]string, len(workspaces))
	for i, w := range workspaces {
		name, ok := w.doc["name"].(string)
		if !ok {
			return nil, diag.Newf("Could not find a name in %s", w.file).WithCode(codeNPMNoName)
		}
		names[i] = name
	}
	if len(workspaces) == 0 {
		return nil, nil
	}
	_, lockErr := root.Stat(npmLockFile)
	lock := lockErr == nil

	pkgs := make([]PackageConfig, 0, len(workspaces))
	for i := range workspaces {
		pkg, err := npmPackage(workspaces, names, i, lock)
		if err != nil {
			return nil, err
		}
		pkgs = append(pkgs, pkg)
	}
	return pkgs, nil
}

// collectNPMWorkspaces expands the patterns of the root package.json. Unlike Knope, a directory matched by several
// patterns appears once, at its first position.
func collectNPMWorkspaces(root fs.StatFS) ([]npmWorkspace, error) {
	doc, _, err := readManifest(root, npmPackageJSON)
	if err != nil {
		return nil, err
	}
	patterns, _ := doc["workspaces"].([]any)

	var found []npmWorkspace
	seen := map[string]bool{}
	for _, p := range patterns {
		pattern, ok := p.(string)
		if !ok {
			continue
		}
		if !patternAllowed(pattern, runtime.GOOS == "windows") {
			return nil, npmGlobError(pattern, "the pattern must be a relative path without `..` that stays in the project")
		}
		matches, err := glob.Glob(root, pattern)
		if err != nil {
			var readErr *glob.ReadError
			if !errors.As(err, &readErr) { // unreadable directories are skipped, as in Knope
				var patErr *glob.PatternError
				if errors.As(err, &patErr) {
					return nil, npmGlobError(pattern, patErr).WithCause(err)
				}
				return nil, npmGlobError(pattern, "the pattern is not supported").WithCause(err)
			}
		}
		for _, dir := range matches {
			if seen[dir] {
				continue
			}
			file := path.Join(dir, npmPackageJSON)
			manifest, ok, err := readManifest(root, file)
			if err != nil {
				return nil, err
			}
			if ok {
				seen[dir] = true
				found = append(found, npmWorkspace{file: file, dir: dir, doc: manifest})
			}
		}
	}
	return found, nil
}

// npmPackage builds the package of workspaces[i]: its own package.json, then the root lock file when there is one,
// then the package.json of every other workspace that depends on it (`dependencies` or `devDependencies`). The
// changelog is always set (Q13), even when the file does not exist.
func npmPackage(workspaces []npmWorkspace, names []string, i int, lock bool) (PackageConfig, error) {
	w, name := workspaces[i], names[i]
	var files []versionedfile.Config
	add := func(file string, dependency *string) error {
		cfg, err := versionedfile.NewConfig(file, dependency, nil)
		if err != nil {
			return err
		}
		files = append(files, cfg)
		return nil
	}

	if err := add(w.file, nil); err != nil {
		return PackageConfig{}, err
	}
	if lock {
		if err := add(npmLockFile, &name); err != nil {
			return PackageConfig{}, err
		}
	}
	for j, other := range workspaces {
		if j != i &&
			(hasNPMDependency(other.doc, "dependencies", name) || hasNPMDependency(other.doc, "devDependencies", name)) {
			if err := add(other.file, &name); err != nil {
				return PackageConfig{}, err
			}
		}
	}
	return PackageConfig{
		Name:           versioning.CustomName(name),
		VersionedFiles: files,
		Changelog:      path.Join(w.dir, defaultChangelog),
		Scopes:         []string{name},
	}, nil
}

// hasNPMDependency: doc[section] is an object with the key name, whatever its value.
func hasNPMDependency(doc map[string]any, section, name string) bool {
	deps, _ := doc[section].(map[string]any)
	_, ok := deps[name]
	return ok
}

// readManifest returns the JSON object at name and true. The file is skipped (false, no error) when it is missing,
// unreadable, not a regular file or not JSON, as in Knope. A file of more than 8 MiB is an error instead of a silent
// skip. Valid JSON that is not an object (null, an array) is a manifest without any key, as with serde_json's
// Value::get in Knope. File content never reaches an error message.
func readManifest(root fs.StatFS, name string) (map[string]any, bool, error) {
	data, err := readManifestFile(root, name)
	if errors.Is(err, errManifestTooLarge) {
		return nil, false, diag.New("npm manifest " + strconv.Quote(name) + " is larger than 8 MiB").
			WithCode(codeNPMTooLarge).
			WithCause(err)
	}
	if err != nil {
		return nil, false, nil
	}
	var v any
	if json.Unmarshal(data, &v) != nil {
		return nil, false, nil
	}
	doc, _ := v.(map[string]any)
	return doc, true, nil
}

// patternAllowed reports whether a workspaces pattern stays lexically inside the project: relative, without `..`,
// without NUL, and on Windows without a volume (`C:`, UNC), a backslash (a separator there, but one literal
// character for internal/glob), a colon or a reserved device name. It does not stop a symbolic link from pointing
// out of the project: links are followed, as in Knope.
func patternAllowed(pattern string, windows bool) bool {
	if strings.ContainsRune(pattern, 0) || strings.HasPrefix(pattern, "/") {
		return false
	}
	if windows && strings.ContainsAny(pattern, `\:`) {
		return false
	}
	for part := range strings.SplitSeq(pattern, "/") {
		if part == ".." || windows && reservedDeviceName(part) {
			return false
		}
	}
	return true
}

// reservedDeviceName: a Windows device name (CON, NUL, COM1, ...), with or without an extension.
func reservedDeviceName(part string) bool {
	base, _, _ := strings.Cut(part, ".")
	switch strings.ToUpper(strings.TrimRight(base, " ")) {
	case "CON", "PRN", "AUX", "NUL", "CONIN$", "CONOUT$":
		return true
	}
	upper := strings.ToUpper(base)
	return len(upper) == 4 && (strings.HasPrefix(upper, "COM") || strings.HasPrefix(upper, "LPT")) &&
		upper[3] >= '1' && upper[3] <= '9'
}

func npmGlobError(pattern string, reason any) *diag.Error {
	return diag.Newf("Could not process workspaces glob pattern %s in package.json: %v", strconv.Quote(pattern), reason).
		WithCode(codeNPMGlob)
}
