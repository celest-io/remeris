package versionedfile

import (
	"strings"

	"github.com/celest-io/remeris/internal/diag"
)

// Config is the validated configuration of one versioned file.
type Config struct {
	// Path is the slash-separated, relative location of the file.
	Path string
	// Format is the kind of file.
	Format Format
	// Dependency, if set, versions that dependency within the file rather than the whole package.
	Dependency *string
	// Regex, if non-nil, holds the patterns that find versions in an arbitrary text file (Format is RegexFile).
	Regex []string
}

// Error codes.
const (
	codeUnknownFile           = "remeris::versioned_file::unknown_file"
	codeConflictingOptions    = "remeris::versioned_file::conflicting_options"
	codeNoVersion             = "remeris::versioned_file::no_version"
	codeUnsupportedDependency = "remeris::package::unsupported_dependency"
	codeNotImplemented        = "remeris::versioned_file::not_implemented"
)

// NewConfig validates a versioned_files entry (F2.2 step 2). A non-nil regex (even empty) selects RegexFile.
// It returns a diagnostic for an unknown file name, or when dependency and regex are both given.
//
// A nil regex means the key was absent; a non-nil empty slice means it was given, as Knope's Some(vec![]): the file
// is then a regex file. Decoders must keep the two apart.
func NewConfig(path string, dependency *string, regex []string) (Config, error) {
	if dependency != nil && regex != nil {
		return Config{}, diag.New("Cannot specify both 'dependency' and 'regex' for the same file: " + path).
			WithCode(codeConflictingOptions).
			WithHelp("Use 'dependency' to update a dependency version in a known file format, or 'regex' to match " +
				"version strings in arbitrary text files, but not both.")
	}
	if regex != nil {
		return Config{Path: path, Format: RegexFile, Regex: regex}, nil
	}
	format, ok := FormatFromPath(path)
	if !ok {
		return Config{}, diag.New("Unknown file: " + path).WithCode(codeUnknownFile).WithHelp(unknownFileHelp())
	}
	return Config{Path: path, Format: format, Dependency: dependency}, nil
}

func unknownFileHelp() string {
	names := make([]string, len(FileNames))
	for i, f := range FileNames {
		names[i] = f.Name
	}
	return "remeris identifies the type of file based on its name. Supported file names: " + strings.Join(names, ", ")
}

// Defaults returns one Config per supported file name, in FileNames order (default detection, F20). Like Knope's
// Config::defaults it has no deno.jsonc: Quirk Q11 adds that name for explicit versioned_files only, and Deno
// workspace discovery (F20.4) finds deno.jsonc files by itself.
func Defaults() []Config {
	out := make([]Config, 0, len(FileNames))
	for _, f := range FileNames {
		if f.Name != denoJSONCName {
			out = append(out, Config{Path: f.Name, Format: f.Format})
		}
	}
	return out
}

// UnsupportedDependency is the error for a dependency on a file whose format does not support one (F6.3, F7.2).
func UnsupportedDependency(path string) error {
	return diag.New("Dependencies are not supported in " + baseName(path) + " files").
		WithCode(codeUnsupportedDependency).
		WithHelp("Dependencies aren't supported in every file type.")
}

// ErrNoVersion is returned by File.Version for files that cannot hold a package version, such as lockfiles.
var ErrNoVersion = diag.New("This file can't contain a version").
	WithCode(codeNoVersion).
	WithHelp("This is likely a bug, please report it.")
