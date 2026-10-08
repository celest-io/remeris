package versionedfile_test

import (
	"errors"
	"slices"
	"testing"

	"github.com/celest-io/remeris/internal/diag"
	"github.com/celest-io/remeris/pkg/versioning/action"
	"github.com/celest-io/remeris/pkg/versioning/semver"
	"github.com/celest-io/remeris/pkg/versioning/versionedfile"
)

const (
	goRepo       = "module github.com/owner/repo"
	goRepoV2     = goRepo + "/v2"
	goRepoV1     = goRepo + " // v1.1.0"
	goBody       = "\n\ngo 1.19\n\nrequire (\n\tgithub.com/a/b v1.0.0\n)\n"
	goV101       = "1.0.1"
	goV210       = "2.1.0"
	goSubMod     = "sub_dir/go.mod"
	goSubV2Mod   = "sub_dir/v2/go.mod"
	goV2Mod      = "v2/go.mod"
	goTagV123    = "v1.2.3"
	goTagV200    = "v2.0.0"
	goTagV100    = "v1.0.0"
	goTagV130    = "v1.3.0"
	goTagV210    = "v2.1.0"
	goSubTagV100 = "sub_dir/v1.0.0"
	goSubTagV200 = "sub_dir/v2.0.0"
	goCodeNoTag  = "remeris::go::no_matching_tag"
	goHelpNoTag  = "The go.mod file must have a matching tag in the repository."
	goNoTagError = "No matching tag found for the go.mod file. Searched for a tag with the prefix "
)

func newGoMod(t *testing.T, path, content string, tags []string) versionedfile.File {
	t.Helper()
	f, err := versionedfile.New(versionedfile.Config{Path: path, Format: versionedfile.GoMod}, content, tags)
	if err != nil {
		t.Fatal(err)
	}
	return f
}

func asDiagnostic(t *testing.T, err error) diag.Diagnostic {
	t.Helper()
	var d diag.Diagnostic
	if !errors.As(err, &d) {
		t.Fatalf("want a diagnostic, got %v", err)
	}
	return d
}

func TestGoModVersion(t *testing.T) {
	t.Parallel()
	tests := map[string]struct {
		path    string
		content string
		tags    []string
		want    string
	}{
		"comment needs no tags":     {goMod, goRepo + " // v2.1.4", []string{""}, "2.1.4"},
		"comment wins over tags":    {goMod, goRepo + " // v1.0.0", []string{goTagV123}, pyOne},
		"comment with major suffix": {goMod, goRepoV2 + " // v3.1.4", nil, "3.1.4"},
		"comment with prerelease":   {goMod, goRepo + " // v1.0.0-rc.1", nil, "1.0.0-rc.1"},
		"tag at the root":           {goMod, goRepo, []string{goTagV123}, v123},
		"v1 tags only":              {goMod, goRepo, []string{goTagV123, goTagV200}, v123},
		"v0 tag":                    {goMod, goRepo, []string{"v0.4.0"}, "0.4.0"},
		"first match wins":          {goMod, goRepo, []string{goTagV100, "v1.5.0"}, pyOne},
		"newest first, skip v2":     {goMod, goRepo, []string{goTagV210, "v1.9.0", "v1.10.0"}, "1.9.0"},
		"major tag":                 {goMod, goRepoV2, []string{goTagV123, goTagV200, "v3.0.0"}, pyV200},
		"tag for submodule":         {goSubMod, goRepo + "/sub_dir", []string{goTagV123, "sub_dir/v0.2.0", "v1.2.4"}, v020},
		"nested submodule":          {"a/b/go.mod", goRepo + "/a/b", []string{"a/v1.0.0", "a/b/v1.4.0"}, "1.4.0"},
		"major subdirectory":        {goV2Mod, goRepoV2, []string{goTagV123, "v2.3.0"}, "2.3.0"},
		"major subdirectory in submodule": {
			goSubV2Mod, goRepo + "/sub_dir/v2", []string{goSubTagV100, "sub_dir/v2.4.0"}, "2.4.0",
		},
		"v1 directory for a v1 suffix": {"v1/go.mod", goRepo + "/v1", []string{goTagV123, goTagV200}, v123},
		"directory is not a prefix without a suffix": {
			"v2/go.mod", goRepo, []string{"v2/v1.0.0"}, pyOne,
		},
		"prerelease tag matches": {goMod, goRepo, []string{"v1.1.0-rc.1"}, "1.1.0-rc.1"},
		"build metadata kept":    {goMod, goRepo, []string{"v1.1.0+meta"}, "1.1.0+meta"},
		"skips invalid tags": {
			goMod, goRepo, []string{"v1.2", "vx", pyOne, "sub_dir/v1.8.0", "release-1", goTagV123}, v123,
		},
		"module line is not the first line": {goMod, "// header\n\n" + goRepo + goBody, []string{goTagV123}, v123},
		"first module line wins":            {goMod, goRepo + " // v1.0.0\nmodule other // v9.9.9\n", nil, pyOne},
		"crlf line endings":                 {goMod, goRepo + " // v1.7.0\r\n\r\ngo 1.19\r\n", nil, "1.7.0"},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			f := newGoMod(t, tc.path, tc.content, tc.tags)
			got, err := f.Version()
			if err != nil || got.String() != tc.want {
				t.Errorf("Version() = %v, %v; want %s", got, err, tc.want)
			}
			if f.Path() != tc.path {
				t.Errorf("Path() = %q, want %q", f.Path(), tc.path)
			}
			if f.Write() != nil {
				t.Errorf("Write() before SetVersion = %v, want nil", f.Write())
			}
		})
	}
}

func TestGoModParseErrors(t *testing.T) {
	t.Parallel()
	tests := map[string]struct {
		path    string
		content string
		tags    []string
		msg     string
		code    string
		help    string
	}{
		"no tags at the root": {
			goMod, goRepo, nil, goNoTagError + " and a major version of [0, 1]",
			goCodeNoTag, goHelpNoTag,
		},
		"only v2 tags": {
			goMod, goRepo, []string{goTagV200}, goNoTagError + " and a major version of [0, 1]",
			goCodeNoTag, goHelpNoTag,
		},
		"submodule prefix": {
			goSubMod, goRepo, []string{goTagV123}, goNoTagError + "sub_dir/ and a major version of [0, 1]",
			goCodeNoTag, goHelpNoTag,
		},
		"major filter": {
			goMod, goRepoV2, []string{goTagV123}, goNoTagError + " and a major version of [2]",
			goCodeNoTag, goHelpNoTag,
		},
		"major subdirectory prefix drops the directory": {
			goSubV2Mod, goRepoV2, []string{goSubTagV100, "sub_dir/v2/v2.0.0"},
			goNoTagError + "sub_dir/ and a major version of [2]",
			goCodeNoTag, goHelpNoTag,
		},
		"no module line": {
			goMod, "go 1.19\n", nil, "No module line found in go.mod file", "remeris::go::no_module_line",
			"The go.mod file does not contain a module line. This is required for the step to work.",
		},
		"module must start the line": {
			goMod, "  module x\n", nil, "No module line found in go.mod file", "remeris::go::no_module_line",
			"The go.mod file does not contain a module line. This is required for the step to work.",
		},
		"missing module path": {
			goMod, "module \ngo 1.19\n", nil, "missing module path", "remeris::go::missing_module_path",
			"The module line in go.mod must contain a module path, usually the URI of the repository.",
		},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			f, err := versionedfile.New(versionedfile.Config{Path: tc.path, Format: versionedfile.GoMod}, tc.content, tc.tags)
			if f != nil || err == nil {
				t.Fatalf("got %v, %v", f, err)
			}
			d := asDiagnostic(t, err)
			if d.Error() != tc.msg || d.Code() != tc.code || d.Help() != tc.help {
				t.Errorf("got %q %q %q; want %q %q %q", d.Error(), d.Code(), d.Help(), tc.msg, tc.code, tc.help)
			}
		})
	}
}

func TestGoModSetVersion(t *testing.T) {
	t.Parallel()
	tests := map[string]struct {
		path    string
		content string
		tags    []string
		version string
		mode    versionedfile.GoVersioning
		want    string
		tag     string
		wantErr error
	}{
		"no comment yet": {goMod, goRepo + goBody, []string{goTagV123}, v130, versionedfile.Standard,
			goRepo + " // v1.3.0" + goBody, goTagV130, nil},
		"comment replaced": {goMod, goRepoV1 + goBody, nil, "1.2.0", versionedfile.Standard,
			goRepo + " // v1.2.0" + goBody, "v1.2.0", nil},
		"no trailing newline": {goMod, "module toplevel", []string{goTagV123}, v130, versionedfile.Standard,
			"module toplevel // v1.3.0", goTagV130, nil},
		"prerelease": {goMod, goRepo, []string{goTagV123}, "1.3.0-rc.0", versionedfile.Standard,
			goRepo + " // v1.3.0-rc.0", "v1.3.0-rc.0", nil},
		"standard refuses 2.0.0": {
			goMod,
			goRepoV1,
			nil,
			pyV200,
			versionedfile.Standard,
			"",
			"",
			versionedfile.ErrBumpingToV2,
		},
		"bump major allows 2.0.0": {goMod, goRepoV1 + goBody, nil, pyV200, versionedfile.BumpMajor,
			goRepoV2 + " // v2.0.0" + goBody, goTagV200, nil},
		"ignore major rules": {goMod, "module something/test", []string{goTagV123}, "2.0.1", versionedfile.IgnoreMajorRules,
			"module something/test // v2.0.1", "v2.0.1", nil},
		"module already v2": {goMod, goRepoV2 + " // v2.0.0", nil, goV210, versionedfile.Standard,
			goRepoV2 + " // v2.1.0", goTagV210, nil},
		"v2 to v3 with a suffix": {goMod, goRepoV2 + " // v2.0.0", nil, npmV300, versionedfile.Standard,
			goRepo + "/v3 // v3.0.0", "v3.0.0", nil},
		"v1 suffix is dropped": {goMod, goRepo + "/v1", []string{goTagV123}, v130, versionedfile.Standard,
			goRepo + " // v1.3.0", goTagV130, nil},
		"v2 suffix kept when going back to 1.x": {goMod, goRepoV2 + " // v2.0.0", nil, pyOne, versionedfile.Standard,
			goRepoV2 + " // v1.0.0", goTagV100, nil},
		"major subdirectory": {goV2Mod, "module knope/v2", []string{goTagV200}, goV210, versionedfile.Standard,
			"module knope/v2 // v2.1.0", goTagV210, nil},
		"major subdirectory refuses a new major": {goV2Mod, goRepoV2 + " // v2.0.0", nil, npmV300, versionedfile.Standard,
			"", "", versionedfile.ErrMajorVersionDirectoryBased},
		"major subdirectory, bump major refuses too": {goV2Mod, goRepoV2 + " // v2.0.0", nil, npmV300, versionedfile.BumpMajor,
			"", "", versionedfile.ErrMajorVersionDirectoryBased},
		"major subdirectory with ignored rules": {
			goV2Mod,
			goRepoV2 + " // v2.0.0",
			nil,
			npmV300,
			versionedfile.IgnoreMajorRules,
			goRepoV2 + " // v3.0.0",
			"v2/v3.0.0",
			nil,
		},
		"submodule tag": {goSubMod, "module sub_dir/knope", []string{goSubTagV100}, goV101, versionedfile.Standard,
			"module sub_dir/knope // v1.0.1", "sub_dir/v1.0.1", nil},
		"submodule with major directory": {goSubV2Mod, "module sub_dir/knope/v2", []string{goSubTagV200}, goV210,
			versionedfile.Standard, "module sub_dir/knope/v2 // v2.1.0", "sub_dir/v2.1.0", nil},
		"nested submodule": {"a/b/go.mod", "module x/a/b", []string{"a/b/v0.1.0"}, v020, versionedfile.Standard,
			"module x/a/b // v0.2.0", "a/b/v0.2.0", nil},
		"bump major in a submodule": {goSubMod, "module x/sub_dir // v1.0.0", nil, pyV200, versionedfile.BumpMajor,
			"module x/sub_dir/v2 // v2.0.0", goSubTagV200, nil},
		"every identical module line is replaced": {
			goMod,
			goRepo + "\nmodule " + goRepo[7:] + "\n",
			[]string{goTagV123},
			v130,
			versionedfile.Standard,
			goRepo + " // v1.3.0\n" + goRepo + " // v1.3.0\n",
			goTagV130,
			nil,
		},
		"crlf kept": {goMod, goRepo + "\r\n\r\ngo 1.19\r\n", []string{goTagV123}, v130, versionedfile.Standard,
			goRepo + " // v1.3.0\r\n\r\ngo 1.19\r\n", goTagV130, nil},
		"crlf kept with a comment and a bump": {goMod, goRepoV1 + "\r\ngo 1.19\r\n", nil, pyV200, versionedfile.BumpMajor,
			goRepoV2 + " // v2.0.0\r\ngo 1.19\r\n", goTagV200, nil},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			f := newGoMod(t, tc.path, tc.content, tc.tags)
			got, err := f.SetVersion(semver.MustParse(tc.version), nil, tc.mode)
			if tc.wantErr != nil {
				if !errors.Is(err, tc.wantErr) || got != nil {
					t.Fatalf("got %v, %v; want %v", got, err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			want := []action.Action{action.WriteToFile(tc.path, tc.want, tc.version), action.AddTag(tc.tag)}
			if actions := got.Write(); !slices.EqualFunc(actions, want, action.Action.Equal) {
				t.Errorf("Write() = %+v, want %+v", actions, want)
			}
			before, _ := f.Version()
			if after, _ := got.Version(); !after.Equal(before) {
				t.Errorf("Version() = %s after SetVersion, want the parsed version %s", after, before)
			}
		})
	}
}

func TestGoModSetVersionErrors(t *testing.T) {
	t.Parallel()
	bump := versionedfile.ErrBumpingToV2
	if bump.Error() != "Will not bump Go modules to 2.0.0" || bump.Code() != "remeris::go::cannot_increase_major_version" ||
		bump.Help() != "Go recommends a directory-based versioning strategy for major versions above v1. "+
			"See the docs for more details." {
		t.Errorf("unexpected ErrBumpingToV2: %v", bump)
	}
	dir := versionedfile.ErrMajorVersionDirectoryBased
	if dir.Error() != "Cannot bump major versions of directory-based go modules" ||
		dir.Code() != "remeris::go::major_version_directory_based" ||
		dir.Help() != "You are using directory-based major versions—remeris cannot create a new major version directory "+
			"for you. Create the new directory manually and add it as a new package in knope.toml." {
		t.Errorf("unexpected ErrMajorVersionDirectoryBased: %v", dir)
	}
	f := newGoMod(t, goMod, goRepoV1, nil)
	dep := "dep"
	got, err := f.SetVersion(semver.MustParse(goV101), &dep, versionedfile.Standard)
	if got != nil || err == nil || err.Error() != versionedfile.UnsupportedDependency(goMod).Error() {
		t.Errorf("dependency: got %v, %v", got, err)
	}
}

func TestGoModImmutableAndRepeated(t *testing.T) {
	t.Parallel()
	original := newGoMod(t, "sub_dir/go.mod", "module x/sub_dir // v1.0.0\n", nil)
	first, err := original.SetVersion(semver.MustParse(v110), nil, versionedfile.Standard)
	if err != nil {
		t.Fatal(err)
	}
	second, err := first.SetVersion(semver.MustParse("1.2.0"), nil, versionedfile.Standard)
	if err != nil {
		t.Fatal(err)
	}
	if original.Write() != nil {
		t.Errorf("the original changed: %+v", original.Write())
	}
	if v, _ := original.Version(); v.String() != pyOne {
		t.Errorf("original version = %s", v)
	}
	// Like Knope, the diff, the content and the tag come from the last call only.
	want := []action.Action{
		action.WriteToFile("sub_dir/go.mod", "module x/sub_dir // v1.2.0\n", "1.2.0"), action.AddTag("sub_dir/v1.2.0"),
	}
	if got := second.Write(); !slices.EqualFunc(got, want, action.Action.Equal) {
		t.Errorf("second Write() = %+v, want %+v", got, want)
	}
	wantFirst := []action.Action{
		action.WriteToFile("sub_dir/go.mod", "module x/sub_dir // v1.1.0\n", v110), action.AddTag("sub_dir/v1.1.0"),
	}
	if got := first.Write(); !slices.EqualFunc(got, wantFirst, action.Action.Equal) {
		t.Errorf("first Write() = %+v, want %+v", got, wantFirst)
	}
}

func TestGoModRepeatedWithBump(t *testing.T) {
	t.Parallel()
	f := newGoMod(t, goMod, goRepoV1, nil)
	f, err := f.SetVersion(semver.MustParse(pyV200), nil, versionedfile.BumpMajor)
	if err != nil {
		t.Fatal(err)
	}
	// The module already has /v2, so a second 2.x call needs no override.
	f, err = f.SetVersion(semver.MustParse(goV210), nil, versionedfile.Standard)
	if err != nil {
		t.Fatal(err)
	}
	want := []action.Action{action.WriteToFile(goMod, goRepoV2+" // v2.1.0", goV210), action.AddTag(goTagV210)}
	if got := f.Write(); !slices.EqualFunc(got, want, action.Action.Equal) {
		t.Errorf("Write() = %+v, want %+v", got, want)
	}
}

// The cases below are the in/out pairs of Knope's tests/prepare_release/go_modules fixtures.
func TestGoModKnopeFixtures(t *testing.T) {
	t.Parallel()
	tests := map[string]struct {
		path    string
		in      string
		tags    []string
		version string
		mode    versionedfile.GoVersioning
		out     string
		tag     string
	}{
		"ignore_go_major_versioning": {goMod, "module something/test", []string{goTagV100}, "2.0.1",
			versionedfile.IgnoreMajorRules, "module something/test // v2.0.1", "v2.0.1"},
		"version_determination": {goMod, "module toplevel", []string{"v1.2.3"}, v130,
			versionedfile.Standard, "module toplevel // v1.3.0", goTagV130},
		"major_versions v1": {goMod, "module github.com/knope-dev/knope" + goBody, []string{goTagV100}, v110,
			versionedfile.Standard, "module github.com/knope-dev/knope // v1.1.0" + goBody, "v1.1.0"},
		"major_versions v2": {goMod, "module github.com/knope-dev/knope // v1.1.0" + goBody, nil, pyV200,
			versionedfile.BumpMajor, "module github.com/knope-dev/knope/v2 // v2.0.0" + goBody, goTagV200},
		"major_version_directories root": {goMod, "module knope", []string{goTagV100}, goV101, versionedfile.Standard,
			"module knope // v1.0.1", "v1.0.1"},
		"major_version_directories sub_dir": {
			goSubMod,
			"module sub_dir/knope",
			[]string{goSubTagV100},
			goV101,
			versionedfile.Standard,
			"module sub_dir/knope // v1.0.1",
			"sub_dir/v1.0.1",
		},
		"major_version_directories v2": {goV2Mod, "module knope/v2", []string{goTagV200}, goV210, versionedfile.Standard,
			"module knope/v2 // v2.1.0", goTagV210},
		"major_version_directories sub_dir/v2": {
			goSubV2Mod,
			"module sub_dir/knope/v2",
			[]string{goSubTagV200},
			goV210,
			versionedfile.Standard,
			"module sub_dir/knope/v2 // v2.1.0",
			"sub_dir/v2.1.0",
		},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			f := newGoMod(t, tc.path, tc.in, tc.tags)
			got, err := f.SetVersion(semver.MustParse(tc.version), nil, tc.mode)
			if err != nil {
				t.Fatal(err)
			}
			want := []action.Action{action.WriteToFile(tc.path, tc.out, tc.version), action.AddTag(tc.tag)}
			if actions := got.Write(); !slices.EqualFunc(actions, want, action.Action.Equal) {
				t.Errorf("Write() = %+v, want %+v", actions, want)
			}
		})
	}
}
