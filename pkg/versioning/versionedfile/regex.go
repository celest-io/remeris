package versionedfile

import (
	"errors"
	"regexp"
	"strings"

	"github.com/celest-io/remeris/internal/diag"
	"github.com/celest-io/remeris/pkg/versioning/action"
	"github.com/celest-io/remeris/pkg/versioning/semver"
)

// Error codes of regex files.
const (
	codeInvalidPattern     = "remeris::text_file::invalid_pattern"
	codeMissingVersionGrp  = "remeris::text_file::missing_version_group"
	codeNoMatch            = "remeris::text_file::no_match"
	codeInvalidMatchedVers = "remeris::text_file::invalid_version"

	regexSyntaxURL = "https://pkg.go.dev/regexp/syntax"
	versionGroup   = "version"
)

// regexFile is a text file whose versions are found by regular expressions (F6.2). Each pattern has a named
// capture group "version". It is an immutable value.
type regexFile struct {
	path    string
	content string
	res     []*regexp.Regexp
	diff    string
}

// newRegexFile compiles cfg.Regex and checks every pattern has a "version" group. Patterns use Go's RE2 syntax,
// which differs from Rust's regex crate used by Knope: \d, \w, \s and \b are ASCII-only, and Rust-only syntax such
// as (?x) or class set operations is rejected. Like Knope, a nil pattern
// list is reported as a pattern that did not match.
func newRegexFile(cfg Config, content string) (File, error) {
	if cfg.Regex == nil {
		return nil, noMatchError("", cfg.Path)
	}
	res := make([]*regexp.Regexp, 0, len(cfg.Regex))
	for _, pattern := range cfg.Regex {
		re, err := regexp.Compile(pattern)
		if err == nil {
			err = duplicateGroupName(re)
		}
		if err != nil {
			return nil, diag.New("Invalid regex pattern '" + pattern + "' for " + cfg.Path + ": " + err.Error()).
				WithCode(codeInvalidPattern).
				WithHelp("Check the regex pattern syntax").
				WithURL(regexSyntaxURL).
				WithCause(err)
		}
		if re.SubexpIndex(versionGroup) < 0 {
			return nil, diag.New("Regex pattern '" + pattern + "' must contain at least one named capture group called 'version'").
				WithCode(codeMissingVersionGrp).
				WithHelp(`Use a named capture group like (?<version>\d+\.\d+\.\d+) in your regex`)
		}
		res = append(res, re)
	}
	return regexFile{path: cfg.Path, content: content, res: res}, nil
}

// duplicateGroupName rejects a pattern that names two groups alike. Rust's regex crate refuses such patterns, while
// Go's accepts them and SubexpIndex would silently use the first group.
func duplicateGroupName(re *regexp.Regexp) error {
	seen := make(map[string]bool)
	for _, name := range re.SubexpNames() {
		if name == "" {
			continue
		}
		if seen[name] {
			return errors.New("duplicate capture group name " + name)
		}
		seen[name] = true
	}
	return nil
}

func noMatchError(pattern, path string) error {
	return diag.New("Regex '" + pattern + "' did not match any content in " + path).
		WithCode(codeNoMatch).
		WithHelp("Ensure the regex matches the version string in the file")
}

func (f regexFile) Path() string { return f.path }

// Version returns the version matched by the first pattern. Every pattern must match and capture a valid version.
func (f regexFile) Version() (semver.Version, error) {
	if len(f.res) == 0 {
		return semver.Version{}, noMatchError("", f.path)
	}
	var first semver.Version
	for i, re := range f.res {
		start, end := versionSpan(re, re.FindStringSubmatchIndex(f.content))
		if start < 0 {
			return semver.Version{}, noMatchError(re.String(), f.path)
		}
		text := f.content[start:end]
		v, err := semver.Parse(text)
		if err != nil {
			return semver.Version{}, diag.New("Matched version '" + text + "' in " + f.path +
				" is not a valid semantic version: " + err.Error()).
				WithCode(codeInvalidMatchedVers).
				WithHelp("The matched string must be a valid semantic version").
				WithCause(err)
		}
		if i == 0 {
			first = v
		}
	}
	return first, nil
}

// versionSpan returns the byte span of the "version" group in match m, or -1, -1 if there is no match or the
// group did not take part in it.
func versionSpan(re *regexp.Regexp, m []int) (start, end int) {
	if m == nil {
		return -1, -1
	}
	i := re.SubexpIndex(versionGroup) * 2
	return m[i], m[i+1]
}

// SetVersion replaces the version in every match of every pattern. Dependencies are not supported.
func (f regexFile) SetVersion(v semver.Version, dependency *string, _ GoVersioning) (File, error) {
	if dependency != nil {
		return nil, UnsupportedDependency(f.path)
	}
	newVersion := v.String()
	content := f.content
	for _, re := range f.res {
		content = replaceVersions(re, content, newVersion)
	}
	out := f
	out.content = content
	if changed := changedLines(f.content, content); changed != "" {
		out.diff = changed
	}
	return out, nil
}

// replaceVersions replaces, in each match of re, every occurrence of the captured version text with newVersion
// (Quirk Q8). The text between matches is kept.
func replaceVersions(re *regexp.Regexp, content, newVersion string) string {
	var b strings.Builder
	last := 0
	for _, m := range re.FindAllStringSubmatchIndex(content, -1) {
		b.WriteString(content[last:m[0]])
		matched := content[m[0]:m[1]]
		if start, end := versionSpan(re, m); start >= 0 {
			matched = strings.ReplaceAll(matched, content[start:end], newVersion)
		}
		b.WriteString(matched)
		last = m[1]
	}
	b.WriteString(content[last:])
	return b.String()
}

// changedLines compares the lines of oldContent and newContent pairwise and returns the new, trimmed lines that
// differ, joined with "\n".
func changedLines(oldContent, newContent string) string {
	oldLines, newLines := splitLines(oldContent), splitLines(newContent)
	var changed []string
	for i := 0; i < len(oldLines) && i < len(newLines); i++ {
		if oldLines[i] != newLines[i] {
			changed = append(changed, strings.TrimSpace(newLines[i]))
		}
	}
	return strings.Join(changed, "\n")
}

// splitLines splits like Rust's str::lines: on "\n", dropping one "\r" before it and a final empty line.
func splitLines(s string) []string {
	if s == "" {
		return nil
	}
	// Like Rust's str::lines: a "\r" is part of the line ending only before "\n", so an unterminated last line keeps it.
	terminated := strings.HasSuffix(s, "\n")
	lines := strings.Split(strings.TrimSuffix(s, "\n"), "\n")
	for i, l := range lines {
		if i < len(lines)-1 || terminated {
			lines[i] = strings.TrimSuffix(l, "\r")
		}
	}
	return lines
}

// Write returns the write action, or nil if no line changed.
func (f regexFile) Write() []action.Action {
	if f.diff == "" {
		return nil
	}
	return []action.Action{action.WriteToFile(f.path, f.content, f.diff)}
}
