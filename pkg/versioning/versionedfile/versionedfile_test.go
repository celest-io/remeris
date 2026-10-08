package versionedfile_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/celest-io/remeris/internal/diag"
	"github.com/celest-io/remeris/pkg/versioning/versionedfile"
)

const (
	cargoToml    = "Cargo.toml"
	packageJSON  = "package.json"
	setupPy      = "setup.py"
	unknownPref  = "Unknown file: "
	notImplement = "is not implemented yet"
	denoJSONC    = "deno.jsonc"
	goMod        = "go.mod"
	m2           = "M2"
	m9           = "M9"
)

func strp(s string) *string { return &s }

func TestFormatFromPath(t *testing.T) {
	t.Parallel()
	// The key is the path.
	exact := map[string]versionedfile.Format{
		cargoToml:                 versionedfile.Cargo,
		"Cargo.lock":              versionedfile.CargoLock,
		"deno.json":               versionedfile.DenoJson,
		denoJSONC:                 versionedfile.DenoJson,
		"deno.lock":               versionedfile.DenoLock,
		"gleam.toml":              versionedfile.Gleam,
		goMod:                     versionedfile.GoMod,
		packageJSON:               versionedfile.PackageJson,
		"package-lock.json":       versionedfile.PackageLockJson,
		"pom.xml":                 versionedfile.MavenPom,
		"pubspec.yaml":            versionedfile.PubSpec,
		"pyproject.toml":          versionedfile.PyProject,
		"tauri.conf.json":         versionedfile.TauriConf,
		"tauri.macos.conf.json":   versionedfile.TauriConf,
		"tauri.windows.conf.json": versionedfile.TauriConf,
		"tauri.linux.conf.json":   versionedfile.TauriConf,
	}
	if len(exact) != len(versionedfile.FileNames) {
		t.Fatalf("table has %d names, FileNames has %d", len(exact), len(versionedfile.FileNames))
	}
	for path, want := range exact {
		t.Run(path, func(t *testing.T) {
			t.Parallel()
			for _, p := range []string{path, "a/b/" + path} {
				if f, ok := versionedfile.FormatFromPath(p); !ok || f != want {
					t.Errorf("FormatFromPath(%q) = %v, %v; want %v", p, f, ok, want)
				}
			}
		})
	}
	rejected := map[string]string{
		"case sensitive":          "cargo.toml",
		"case sensitive 2":        "Package.json",
		"unknown":                 setupPy,
		"directory named like it": "package.json/other.txt",
		"suffix only":             "my-package.json",
		"trailing slash":          "dir/",
		"empty":                   "",
	}
	for name, path := range rejected {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if f, ok := versionedfile.FormatFromPath(path); ok {
				t.Errorf("FormatFromPath(%q) = %v, want no match", path, f)
			}
		})
	}
}

func TestFileNamesOrder(t *testing.T) {
	t.Parallel()
	names := make([]string, len(versionedfile.FileNames))
	for i, f := range versionedfile.FileNames {
		names[i] = f.Name
	}
	last := names[len(names)-1]
	if names[0] != cargoToml || names[2] != "deno.json" || names[3] != denoJSONC || last != "tauri.linux.conf.json" {
		t.Errorf("unexpected order: %v", names)
	}
	// Knope's default detection has no deno.jsonc (Q11 only adds it to the explicit file names).
	defaults := versionedfile.Defaults()
	if len(defaults) != len(names)-1 {
		t.Errorf("Defaults has %d entries, want %d", len(defaults), len(names)-1)
	}
	for _, d := range defaults {
		if d.Path == denoJSONC {
			t.Errorf("Defaults contains %s", denoJSONC)
		}
	}
}

func TestSupportsDependency(t *testing.T) {
	t.Parallel()
	tests := map[string]struct {
		format versionedfile.Format
		want   bool
	}{
		"Cargo":           {versionedfile.Cargo, true},
		"CargoLock":       {versionedfile.CargoLock, true},
		"DenoJson":        {versionedfile.DenoJson, true},
		"DenoLock":        {versionedfile.DenoLock, true},
		"PackageJson":     {versionedfile.PackageJson, true},
		"PackageLockJson": {versionedfile.PackageLockJson, true},
		"Gleam":           {versionedfile.Gleam, false},
		"GoMod":           {versionedfile.GoMod, false},
		"MavenPom":        {versionedfile.MavenPom, false},
		"PubSpec":         {versionedfile.PubSpec, false},
		"PyProject":       {versionedfile.PyProject, false},
		"TauriConf":       {versionedfile.TauriConf, false},
		"RegexFile":       {versionedfile.RegexFile, false},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if got := tc.format.SupportsDependency(); got != tc.want {
				t.Errorf("%v.SupportsDependency() = %v, want %v", tc.format, got, tc.want)
			}
			if tc.format.String() != name {
				t.Errorf("String() = %q, want %q", tc.format.String(), name)
			}
		})
	}
}

func TestNewConfig(t *testing.T) {
	t.Parallel()
	dep := strp("dep")
	tests := map[string]struct {
		path       string
		dependency *string
		regex      []string
		want       versionedfile.Config
		wantMsg    string
		wantCode   string
		helpPrefix string
	}{
		"known file": {path: cargoToml, want: versionedfile.Config{Path: cargoToml, Format: versionedfile.Cargo}},
		"known file with dependency": {
			path: packageJSON, dependency: dep,
			want: versionedfile.Config{Path: packageJSON, Format: versionedfile.PackageJson, Dependency: dep},
		},
		"unsupported dependency is not checked here": {
			path: goMod, dependency: dep,
			want: versionedfile.Config{Path: goMod, Format: versionedfile.GoMod, Dependency: dep},
		},
		"regex file, any name": {
			path: "README.md", regex: []string{`v(?<version>\d+)`},
			want: versionedfile.Config{Path: "README.md", Format: versionedfile.RegexFile, Regex: []string{`v(?<version>\d+)`}},
		},
		"regex on a known name": {
			path: cargoToml, regex: []string{"x"},
			want: versionedfile.Config{Path: cargoToml, Format: versionedfile.RegexFile, Regex: []string{"x"}},
		},
		"unknown file": {
			path:       setupPy,
			wantMsg:    unknownPref + setupPy,
			wantCode:   "remeris::versioned_file::unknown_file",
			helpPrefix: "remeris identifies the type of file based on its name. Supported file names: Cargo.toml, Cargo.lock, deno.json",
		},
		"no file name": {
			path: "", wantMsg: unknownPref, wantCode: "remeris::versioned_file::unknown_file",
			helpPrefix: "remeris identifies the type of file",
		},
		"conflicting options": {
			path: "x.txt", dependency: dep, regex: []string{"a"},
			wantMsg:  "Cannot specify both 'dependency' and 'regex' for the same file: x.txt",
			wantCode: "remeris::versioned_file::conflicting_options",
			helpPrefix: "Use 'dependency' to update a dependency version in a known file format, or 'regex' to match version " +
				"strings in arbitrary text files, but not both.",
		},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			got, err := versionedfile.NewConfig(tc.path, tc.dependency, tc.regex)
			if tc.wantMsg == "" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				if got.Path != tc.want.Path || got.Format != tc.want.Format ||
					(got.Dependency == nil) != (tc.want.Dependency == nil) || strings.Join(got.Regex, "|") != strings.Join(tc.want.Regex, "|") {
					t.Errorf("got %+v, want %+v", got, tc.want)
				}
				return
			}
			var d diag.Diagnostic
			if !errors.As(err, &d) {
				t.Fatalf("want a diagnostic, got %v", err)
			}
			if d.Error() != tc.wantMsg || d.Code() != tc.wantCode || !strings.HasPrefix(d.Help(), tc.helpPrefix) {
				t.Errorf("got message %q code %q help %q", d.Error(), d.Code(), d.Help())
			}
		})
	}
}

func TestUnknownFileHelpListsEveryName(t *testing.T) {
	t.Parallel()
	_, err := versionedfile.NewConfig(setupPy, nil, nil)
	var d diag.Diagnostic
	if !errors.As(err, &d) {
		t.Fatalf("want a diagnostic, got %v", err)
	}
	for _, f := range versionedfile.FileNames {
		if !strings.Contains(d.Help(), f.Name) {
			t.Errorf("help does not mention %s: %s", f.Name, d.Help())
		}
	}
}

func TestUnsupportedDependency(t *testing.T) {
	t.Parallel()
	tests := map[string]struct {
		path    string
		wantMsg string
	}{
		"plain":  {goMod, "Dependencies are not supported in go.mod files"},
		"nested": {"a/b/pyproject.toml", "Dependencies are not supported in pyproject.toml files"},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			err := versionedfile.UnsupportedDependency(tc.path)
			var d diag.Diagnostic
			if !errors.As(err, &d) {
				t.Fatalf("want a diagnostic, got %v", err)
			}
			if d.Error() != tc.wantMsg || d.Code() != "remeris::package::unsupported_dependency" ||
				d.Help() != "Dependencies aren't supported in every file type." {
				t.Errorf("got message %q code %q help %q", d.Error(), d.Code(), d.Help())
			}
		})
	}
}

func TestErrNoVersion(t *testing.T) {
	t.Parallel()
	if versionedfile.ErrNoVersion.Error() != "This file can't contain a version" ||
		versionedfile.ErrNoVersion.Help() != "This is likely a bug, please report it." ||
		versionedfile.ErrNoVersion.Code() != "remeris::versioned_file::no_version" {
		t.Errorf("unexpected ErrNoVersion: %v", versionedfile.ErrNoVersion)
	}
}

func TestNewNotImplemented(t *testing.T) {
	t.Parallel()
	tests := map[string]struct {
		format    versionedfile.Format
		milestone string
	}{
		"Cargo":           {versionedfile.Cargo, m2},
		"PyProject":       {versionedfile.PyProject, m2},
		"PackageJson":     {versionedfile.PackageJson, m2},
		"GoMod":           {versionedfile.GoMod, m2},
		"RegexFile":       {versionedfile.RegexFile, m2},
		"CargoLock":       {versionedfile.CargoLock, m9},
		"DenoJson":        {versionedfile.DenoJson, m9},
		"DenoLock":        {versionedfile.DenoLock, m9},
		"Gleam":           {versionedfile.Gleam, m9},
		"PackageLockJson": {versionedfile.PackageLockJson, m9},
		"MavenPom":        {versionedfile.MavenPom, m9},
		"PubSpec":         {versionedfile.PubSpec, m9},
		"TauriConf":       {versionedfile.TauriConf, m9},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			f, err := versionedfile.New(versionedfile.Config{Path: "x", Format: tc.format}, "", nil)
			if f != nil || err == nil {
				t.Fatalf("got %v, %v", f, err)
			}
			want := "Versioned file format " + name + " " + notImplement + " (milestone " + tc.milestone + ")"
			if err.Error() != want {
				t.Errorf("got %q, want %q", err.Error(), want)
			}
		})
	}
}

func TestDiff(t *testing.T) {
	t.Parallel()
	var empty versionedfile.Diff
	one := empty.Add("version = 1.0.0")
	two := one.Add("dep.version = 2.0.0")
	tests := map[string]struct {
		diff  versionedfile.Diff
		empty bool
		want  string
	}{
		"zero":  {empty, true, ""},
		"one":   {one, false, "version = 1.0.0"},
		"two":   {two, false, "version = 1.0.0, dep.version = 2.0.0"},
		"fork":  {one.Add("other"), false, "version = 1.0.0, other"},
		"empty": {empty, true, ""},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if tc.diff.Empty() != tc.empty || tc.diff.String() != tc.want {
				t.Errorf("got empty=%v %q, want empty=%v %q", tc.diff.Empty(), tc.diff.String(), tc.empty, tc.want)
			}
		})
	}
}
