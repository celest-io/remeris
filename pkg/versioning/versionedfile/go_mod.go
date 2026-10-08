package versionedfile

import (
	"strconv"
	"strings"

	"github.com/celest-io/remeris/internal/diag"
	"github.com/celest-io/remeris/pkg/versioning/action"
	"github.com/celest-io/remeris/pkg/versioning/semver"
)

const (
	codeGoNoModuleLine     = "remeris::go::no_module_line"
	codeGoMissingModule    = "remeris::go::missing_module_path"
	codeGoNoMatchingTag    = "remeris::go::no_matching_tag"
	codeGoBumpingToV2      = "remeris::go::cannot_increase_major_version"
	codeGoMajorDirectories = "remeris::go::major_version_directory_based"

	modulePrefix = "module "
)

// ErrBumpingToV2 is returned by SetVersion when a module without a /vN suffix would get a major version above 1
// and the mode is Standard.
var ErrBumpingToV2 = diag.New("Will not bump Go modules to 2.0.0").
	WithCode(codeGoBumpingToV2).
	WithHelp("Go recommends a directory-based versioning strategy for major versions above v1. " +
		"See the docs for more details.")

// ErrMajorVersionDirectoryBased is returned by SetVersion when the module lives in a v{N} directory and the
// major version would change.
var ErrMajorVersionDirectoryBased = diag.New("Cannot bump major versions of directory-based go modules").
	WithCode(codeGoMajorDirectories).
	WithHelp("You are using directory-based major versions—remeris cannot create a new major version directory " +
		"for you. Create the new directory manually and add it as a new package in knope.toml.")

// moduleLine is the module line of go.mod, such as "module github.com/owner/repo/v2 // v2.1.4". The /vN suffix is
// kept apart so it can be changed and rebuilt, and the comment holds the version that remeris maintains.
type moduleLine struct {
	module   string
	major    uint64
	hasMajor bool
	version  *semver.Version
}

func parseModuleLine(line string) (moduleLine, error) {
	parts := strings.Fields(line)
	if len(parts) < 2 {
		return moduleLine{}, diag.New("missing module path").
			WithCode(codeGoMissingModule).
			WithHelp("The module line in go.mod must contain a module path, usually the URI of the repository.")
	}
	ml := moduleLine{module: parts[1]}
	if i := strings.LastIndexByte(ml.module, '/'); i >= 0 {
		if digits, ok := strings.CutPrefix(ml.module[i+1:], "v"); ok {
			if n, err := strconv.ParseUint(digits, 10, 64); err == nil {
				ml.major, ml.hasMajor = n, true
				ml.module = ml.module[:i]
			}
		}
	}
	if len(parts) > 3 && parts[2] == "//" {
		if text, ok := strings.CutPrefix(parts[3], "v"); ok {
			if v, err := semver.Parse(text); err == nil {
				ml.version = &v
			}
		}
	}
	return ml, nil
}

// String rebuilds the line. The /vN suffix is written only for N above 1, so "module x/v1" becomes "module x".
func (m moduleLine) String() string {
	var b strings.Builder
	b.WriteString(modulePrefix + m.module)
	if m.hasMajor && m.major > 1 {
		b.WriteString("/v" + strconv.FormatUint(m.major, 10))
	}
	if m.version != nil {
		b.WriteString(" // v" + m.version.String())
	}
	return b.String()
}

// findModuleLine returns the first line that starts with "module ", without its line ending.
func findModuleLine(raw string) (string, bool) {
	for line := range strings.SplitSeq(raw, "\n") {
		line = strings.TrimSuffix(line, "\r")
		if strings.HasPrefix(line, modulePrefix) {
			return line, true
		}
	}
	return "", false
}

// goModFile is a go.mod (F6.2). It is an immutable value.
type goModFile struct {
	path       string
	raw        string
	line       moduleLine
	version    semver.Version
	newVersion *semver.Version
}

func newGoModFile(path, content string, gitTags []string) (File, error) {
	line, ok := findModuleLine(content)
	if !ok {
		return nil, diag.New("No module line found in go.mod file").
			WithCode(codeGoNoModuleLine).
			WithHelp("The go.mod file does not contain a module line. This is required for the step to work.")
	}
	ml, err := parseModuleLine(line)
	if err != nil {
		return nil, err
	}
	if ml.version != nil {
		return goModFile{path: path, raw: content, line: ml, version: *ml.version}, nil
	}

	parent := parentDir(path)
	filter := []uint64{0, 1}
	if ml.hasMajor {
		if hasLastComponent(parent, "v"+strconv.FormatUint(ml.major, 10)) {
			// Major version directories are not tag prefixes.
			parent = parentDir(parent)
		}
		filter = []uint64{ml.major}
	}
	prefix := ""
	if parent != "" {
		prefix = parent + "/"
	}
	for _, tag := range gitTags {
		rest, ok := strings.CutPrefix(tag, prefix)
		if !ok {
			continue
		}
		rest, ok = strings.CutPrefix(rest, "v")
		if !ok {
			continue
		}
		v, err := semver.Parse(rest)
		if err != nil {
			continue
		}
		for _, major := range filter {
			if v.Major == major {
				return goModFile{path: path, raw: content, line: ml, version: v}, nil
			}
		}
	}
	return nil, noMatchingTag(prefix, filter)
}

func noMatchingTag(prefix string, filter []uint64) error {
	majors := make([]string, len(filter))
	for i, m := range filter {
		majors[i] = strconv.FormatUint(m, 10)
	}
	return diag.New("No matching tag found for the go.mod file. Searched for a tag with the prefix " + prefix +
		" and a major version of [" + strings.Join(majors, ", ") + "]").
		WithCode(codeGoNoMatchingTag).
		WithHelp("The go.mod file must have a matching tag in the repository.")
}

// parentDir is the directory of a slash-separated relative path, "" at the root.
func parentDir(path string) string {
	if i := strings.LastIndexByte(path, '/'); i >= 0 {
		return path[:i]
	}
	return ""
}

// hasLastComponent reports whether the last path component of dir is name.
func hasLastComponent(dir, name string) bool {
	return dir == name || strings.HasSuffix(dir, "/"+name)
}

func (f goModFile) Path() string { return f.path }

// Version returns the version read from the module comment or the tags. Like Knope, SetVersion does not change it.
func (f goModFile) Version() (semver.Version, error) { return f.version, nil }

// SetVersion sets the version in the module comment and, in Standard and BumpMajor modes, the /vN suffix of
// a major version above 1. It refuses a major bump Go's rules do not allow (ErrBumpingToV2,
// ErrMajorVersionDirectoryBased). The diff is the new version alone, so a repeated call replaces the previous one.
func (f goModFile) SetVersion(v semver.Version, dependency *string, gv GoVersioning) (File, error) {
	if dependency != nil {
		return nil, UnsupportedDependency(f.path)
	}
	original, _ := findModuleLine(f.raw)
	line := f.line
	line.version = &v

	newMajor := v.Major
	oldMajor := uint64(0)
	if line.hasMajor {
		oldMajor = line.major
	}
	if newMajor > 1 && newMajor != oldMajor && gv != IgnoreMajorRules {
		if !line.hasMajor && gv != BumpMajor {
			return nil, ErrBumpingToV2
		}
		if line.hasMajor && hasLastComponent(parentDir(f.path), "v"+strconv.FormatUint(line.major, 10)) {
			return nil, ErrMajorVersionDirectoryBased
		}
		line.major, line.hasMajor = newMajor, true
	}

	out := f
	out.line = line
	out.raw = strings.ReplaceAll(f.raw, original, line.String())
	out.newVersion = &v
	return out, nil
}

// Write returns two actions after SetVersion: the file write (diff = the version) and the Git tag of the module,
// "{dir}/v{version}" or "v{version}" at the root. The directory leaves out a trailing v{major} directory. It
// returns nil if no version was set.
func (f goModFile) Write() []action.Action {
	if f.newVersion == nil {
		return nil
	}
	v := *f.newVersion
	prefix, _ := strings.CutSuffix(parentDir(f.path), "v"+strconv.FormatUint(v.Major, 10))
	prefix = strings.TrimSuffix(prefix, "/")
	tag := "v" + v.String()
	if prefix != "" {
		tag = prefix + "/" + tag
	}
	return []action.Action{action.WriteToFile(f.path, f.raw, v.String()), action.AddTag(tag)}
}
