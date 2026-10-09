package versioning_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/celest-io/remeris/internal/diag"
	"github.com/celest-io/remeris/pkg/versioning"
	"github.com/celest-io/remeris/pkg/versioning/action"
	"github.com/celest-io/remeris/pkg/versioning/semver"
	"github.com/celest-io/remeris/pkg/versioning/versionedfile"
)

const (
	cargoToml   = "Cargo.toml"
	cargoLock   = "Cargo.lock"
	otherCargo  = "other/Cargo.toml"
	packageJSON = "package.json"
	v100        = "1.0.0"
	v123        = "1.2.3"
	v200        = "2.0.0"
	helpSame    = "All files in a package must have the same version"
	codeIncons  = "remeris::inconsistent_versions"
	cargoBody   = "[package]\nname = \"tester\"\nversion = \"1.2.3\"\n\n[dependencies]\nfoo = \"1.0.0\"\n"
	fooDep      = "foo"
	tagV123     = "v1.2.3"
	fooTagV123  = "foo/v1.2.3"
	tagV100     = "v1.0.0"
	valChanged  = "changed"
)

func strp(s string) *string { return &s }

func mustVersion(t *testing.T, s string) semver.Version {
	t.Helper()
	v, err := semver.Parse(s)
	if err != nil {
		t.Fatal(err)
	}
	return v
}

// fakeFile stands in for formats that are not built yet (Cargo.lock, package.json). It is immutable like the
// real files: SetVersion returns a copy that records the call.
type fakeFile struct {
	path    string
	version string // empty: ErrNoVersion
	calls   []string
	setErr  error
}

func (f fakeFile) Path() string { return f.path }

func (f fakeFile) Version() (semver.Version, error) {
	if f.version == "" {
		return semver.Version{}, versionedfile.ErrNoVersion
	}
	return semver.Parse(f.version)
}

func (f fakeFile) SetVersion(v semver.Version, dependency *string, _ versionedfile.GoVersioning) (versionedfile.File, error) {
	if f.setErr != nil {
		return nil, f.setErr
	}
	call := v.String()
	if dependency != nil {
		call = *dependency + "=" + call
	}
	f.calls = append(append([]string(nil), f.calls...), call)
	return f, nil
}

func (f fakeFile) Write() []action.Action { return nil }

func realCargo(t *testing.T, path, content string) versionedfile.File {
	t.Helper()
	f, err := versionedfile.New(versionedfile.Config{Path: path, Format: versionedfile.Cargo}, content, nil)
	if err != nil {
		t.Fatal(err)
	}
	return f
}

func cfg(path string, format versionedfile.Format, dep *string) versionedfile.Config {
	return versionedfile.Config{Path: path, Format: format, Dependency: dep}
}

func TestValidateVersionedFiles(t *testing.T) {
	t.Parallel()
	pkgJSON := func(v string) versionedfile.File { return fakeFile{path: packageJSON, version: v} }
	tests := map[string]struct {
		configs     []versionedfile.Config
		files       func(t *testing.T) []versionedfile.File
		wantVersion string
		wantDeps    []string // dependency of each returned config, "" for none
		wantErr     string
		wantCode    string
		wantHelp    string
		wantIs      error
	}{
		"no files": {
			files: func(*testing.T) []versionedfile.File { return nil },
		},
		"consistent": {
			configs: []versionedfile.Config{
				cfg(cargoToml, versionedfile.Cargo, nil),
				cfg(packageJSON, versionedfile.PackageJson, nil),
			},
			files: func(t *testing.T) []versionedfile.File {
				return []versionedfile.File{realCargo(t, cargoToml, cargoBody), pkgJSON(v123)}
			},
			wantVersion: v123,
			wantDeps:    []string{"", ""},
		},
		"inconsistent": {
			configs: []versionedfile.Config{
				cfg(cargoToml, versionedfile.Cargo, nil),
				cfg(packageJSON, versionedfile.PackageJson, nil),
			},
			files: func(t *testing.T) []versionedfile.File {
				return []versionedfile.File{realCargo(t, cargoToml, cargoBody), pkgJSON(v200)}
			},
			wantErr:  "Found inconsistent versions in package: Cargo.toml had 1.2.3 and package.json had 2.0.0",
			wantCode: codeIncons,
			wantHelp: helpSame,
		},
		"build metadata counts": {
			configs: []versionedfile.Config{
				cfg(cargoToml, versionedfile.Cargo, nil),
				cfg(packageJSON, versionedfile.PackageJson, nil),
			},
			files: func(t *testing.T) []versionedfile.File {
				return []versionedfile.File{realCargo(t, cargoToml, cargoBody), pkgJSON("1.2.3+b1")}
			},
			wantErr:  "Found inconsistent versions in package: Cargo.toml had 1.2.3 and package.json had 1.2.3+b1",
			wantCode: codeIncons,
			wantHelp: helpSame,
		},
		"dependency entries are ignored": {
			configs: []versionedfile.Config{
				cfg(cargoToml, versionedfile.Cargo, nil), cfg(packageJSON, versionedfile.PackageJson, strp(fooDep)),
			},
			files: func(t *testing.T) []versionedfile.File {
				return []versionedfile.File{realCargo(t, cargoToml, cargoBody), pkgJSON(v200)}
			},
			wantVersion: v123,
			wantDeps:    []string{"", fooDep},
		},
		"only dependency entries": {
			configs: []versionedfile.Config{cfg(cargoToml, versionedfile.Cargo, strp(fooDep))},
			files: func(t *testing.T) []versionedfile.File {
				return []versionedfile.File{realCargo(t, cargoToml, cargoBody)}
			},
			wantDeps: []string{fooDep},
		},
		"cargo lock defaults to the cargo toml name": {
			configs: []versionedfile.Config{
				cfg(cargoLock, versionedfile.CargoLock, nil),
				cfg(cargoToml, versionedfile.Cargo, nil),
			},
			files: func(t *testing.T) []versionedfile.File {
				return []versionedfile.File{fakeFile{path: cargoLock}, realCargo(t, cargoToml, cargoBody)}
			},
			wantVersion: v123,
			wantDeps:    []string{"tester", ""},
		},
		"cargo lock skips a cargo toml that is a dependency entry": {
			configs: []versionedfile.Config{
				cfg(cargoLock, versionedfile.CargoLock, nil),
				cfg(cargoToml, versionedfile.Cargo, strp(fooDep)),
				cfg(otherCargo, versionedfile.Cargo, nil),
			},
			files: func(t *testing.T) []versionedfile.File {
				return []versionedfile.File{
					fakeFile{path: cargoLock}, realCargo(t, cargoToml, cargoBody),
					realCargo(t, otherCargo, "[package]\nname = \"second\"\nversion = \"1.2.3\"\n"),
				}
			},
			wantVersion: v123,
			wantDeps:    []string{"second", fooDep, ""},
		},
		"cargo lock skips a cargo toml without a name": {
			configs: []versionedfile.Config{
				cfg(cargoLock, versionedfile.CargoLock, nil), cfg(cargoToml, versionedfile.Cargo, nil),
				cfg(otherCargo, versionedfile.Cargo, nil),
			},
			files: func(t *testing.T) []versionedfile.File {
				return []versionedfile.File{
					fakeFile{path: cargoLock}, realCargo(t, cargoToml, "[workspace.package]\nversion = \"1.2.3\"\n"),
					realCargo(t, otherCargo, cargoBody),
				}
			},
			wantVersion: v123,
			wantDeps:    []string{"tester", "", ""},
		},
		"cargo lock with its own dependency": {
			configs:  []versionedfile.Config{cfg(cargoLock, versionedfile.CargoLock, strp(fooDep))},
			files:    func(*testing.T) []versionedfile.File { return []versionedfile.File{fakeFile{path: cargoLock}} },
			wantDeps: []string{fooDep},
		},
		"cargo lock without a dependency or a cargo toml": {
			configs:  []versionedfile.Config{cfg(cargoLock, versionedfile.CargoLock, nil)},
			files:    func(*testing.T) []versionedfile.File { return []versionedfile.File{fakeFile{path: cargoLock}} },
			wantErr:  "Cargo.lock must specify a dependency",
			wantCode: "remeris::package::cargo_lock_no_dependency",
			wantIs:   versioning.ErrCargoLockNoDependency,
		},
		"unsupported dependency": {
			configs: []versionedfile.Config{cfg("go.mod", versionedfile.GoMod, strp(fooDep))},
			files: func(*testing.T) []versionedfile.File {
				return []versionedfile.File{fakeFile{path: "go.mod", version: v100}}
			},
			wantErr:  "Dependencies are not supported in go.mod files",
			wantCode: "remeris::package::unsupported_dependency",
			wantHelp: "Dependencies aren't supported in every file type.",
		},
		"file not found": {
			configs:  []versionedfile.Config{cfg(cargoToml, versionedfile.Cargo, nil)},
			files:    func(*testing.T) []versionedfile.File { return nil },
			wantErr:  "Versioned file not found: Cargo.toml",
			wantCode: "remeris::package::versioned_file_not_found",
			wantHelp: "this is likely a bug, please report it",
		},
		"a file that cannot hold a version is an error without dependency": {
			configs: []versionedfile.Config{cfg(packageJSON, versionedfile.PackageJson, nil)},
			files:   func(*testing.T) []versionedfile.File { return []versionedfile.File{pkgJSON("")} },
			wantErr: versionedfile.ErrNoVersion.Error(),
			wantIs:  versionedfile.ErrNoVersion,
		},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			got, version, err := versioning.ValidateVersionedFiles(tc.configs, tc.files(t))
			if tc.wantErr != "" {
				checkDiag(t, err, tc.wantErr, tc.wantCode, tc.wantHelp, tc.wantIs)
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if tc.wantVersion == "" && version != nil ||
				tc.wantVersion != "" && (version == nil || version.String() != tc.wantVersion) {
				t.Errorf("version = %v, want %q", version, tc.wantVersion)
			}
			if len(got) != len(tc.configs) {
				t.Fatalf("got %d configs, want %d", len(got), len(tc.configs))
			}
			for i, c := range got {
				dep := ""
				if c.Dependency != nil {
					dep = *c.Dependency
				}
				if dep != tc.wantDeps[i] {
					t.Errorf("config %d (%s) dependency = %q, want %q", i, c.Path, dep, tc.wantDeps[i])
				}
				if c.Path != tc.configs[i].Path {
					t.Errorf("config %d path = %q, want %q", i, c.Path, tc.configs[i].Path)
				}
			}
		})
	}
}

func checkDiag(t *testing.T, err error, msg, code, help string, is error) {
	t.Helper()
	if err == nil {
		t.Fatalf("expected error %q", msg)
	}
	if err.Error() != msg {
		t.Errorf("error = %q, want %q", err.Error(), msg)
	}
	var d diag.Diagnostic
	if !errors.As(err, &d) {
		t.Fatalf("%T is not a diagnostic", err)
	}
	if code != "" && d.Code() != code {
		t.Errorf("code = %q, want %q", d.Code(), code)
	}
	if help != "" && d.Help() != help {
		t.Errorf("help = %q, want %q", d.Help(), help)
	}
	if is != nil && !errors.Is(err, is) {
		t.Errorf("error is not %v", is)
	}
}

func TestValidateVersionedFilesDoesNotModifyInput(t *testing.T) {
	t.Parallel()
	configs := []versionedfile.Config{cfg(cargoLock, versionedfile.CargoLock, nil), cfg(cargoToml, versionedfile.Cargo, nil)}
	files := []versionedfile.File{fakeFile{path: cargoLock}, realCargo(t, cargoToml, cargoBody)}
	if _, _, err := versioning.ValidateVersionedFiles(configs, files); err != nil {
		t.Fatal(err)
	}
	if configs[0].Dependency != nil {
		t.Errorf("input config was modified: dependency %q", *configs[0].Dependency)
	}
}

func TestNewPackage(t *testing.T) {
	t.Parallel()
	tests := map[string]struct {
		name     versioning.PackageName
		fileVer  string
		tags     []string
		want     string
		wantNone bool
	}{
		"version from files":  {versioning.DefaultName(), v123, nil, v123, false},
		"files ahead of tags": {versioning.DefaultName(), "1.3.0", []string{"v1.2.0", tagV100}, "1.3.0", false},
		"tags ahead of files": {versioning.DefaultName(), v100, []string{"v1.2.0"}, "1.2.0", false},
		"custom name uses its prefix": {
			versioning.CustomName("foo"), v100, []string{"v3.0.0", "bar/v2.0.0", "foo/v1.5.0"}, "1.5.0", false,
		},
		"default ignores prefixed tags": {versioning.DefaultName(), v100, []string{"foo/v3.0.0", "v1.1.0"}, "1.1.0", false},
		"pre-release tag after stable": {
			versioning.DefaultName(), v100, []string{"v1.1.0-rc.1", tagV100}, "1.1.0-rc.1", false,
		},
		"no files and no tags": {versioning.DefaultName(), "", nil, "", true},
		"tags only":            {versioning.DefaultName(), "", []string{"v0.4.0"}, "0.4.0", false},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			var configs []versionedfile.Config
			var files []versionedfile.File
			if tc.fileVer != "" {
				configs = []versionedfile.Config{cfg(packageJSON, versionedfile.PackageJson, nil)}
				files = []versionedfile.File{fakeFile{path: packageJSON, version: tc.fileVer}}
			}
			scopes := []string{"a"}
			p, err := versioning.NewPackage(tc.name, configs, files, tc.tags, versionedfile.IgnoreMajorRules, scopes)
			if err != nil {
				t.Fatal(err)
			}
			got, ok := p.LatestVersion()
			if tc.wantNone {
				if ok {
					t.Fatalf("LatestVersion() = %v, want none", got)
				}
			} else if !ok || got.String() != tc.want {
				t.Errorf("LatestVersion() = %v, %v; want %s", got, ok, tc.want)
			}
			if p.GoVersioning != versionedfile.IgnoreMajorRules || !p.Name.Equal(tc.name) {
				t.Errorf("settings not kept: %+v", p)
			}
			scopes[0] = valChanged
			if p.Scopes[0] != "a" {
				t.Error("Scopes shares memory with the argument")
			}
		})
	}
}

func TestNewPackageError(t *testing.T) {
	t.Parallel()
	configs := []versionedfile.Config{
		cfg(packageJSON, versionedfile.PackageJson, nil),
		cfg("b/package.json", versionedfile.PackageJson, nil),
	}
	files := []versionedfile.File{
		fakeFile{path: packageJSON, version: v100}, fakeFile{path: "b/package.json", version: v200},
	}
	_, err := versioning.NewPackage(versioning.DefaultName(), configs, files, nil, versionedfile.Standard, nil)
	checkDiag(t, err, "Found inconsistent versions in package: package.json had 1.0.0 and b/package.json had 2.0.0",
		codeIncons, helpSame, nil)
}

func TestSetVersion(t *testing.T) {
	t.Parallel()
	t.Run("own version and dependency of one file", func(t *testing.T) {
		t.Parallel()
		configs := []versionedfile.Config{
			cfg(cargoToml, versionedfile.Cargo, nil),
			cfg(cargoToml, versionedfile.Cargo, strp(fooDep)),
		}
		files := []versionedfile.File{realCargo(t, cargoToml, cargoBody)}
		p, err := versioning.NewPackage(versioning.DefaultName(), configs, files, nil, versionedfile.Standard, nil)
		if err != nil {
			t.Fatal(err)
		}
		out, err := p.SetVersion(mustVersion(t, v200), versionedfile.Standard, files)
		if err != nil {
			t.Fatal(err)
		}
		writes := out[0].Write()
		if len(writes) != 1 {
			t.Fatalf("got %d actions, want 1", len(writes))
		}
		want := "[package]\nname = \"tester\"\nversion = \"2.0.0\"\n\n[dependencies]\nfoo = \"2.0.0\"\n"
		if writes[0].Content != want {
			t.Errorf("content = %q, want %q", writes[0].Content, want)
		}
		if got := writes[0].Diff; got != "version = 2.0.0, foo.version = 2.0.0" {
			t.Errorf("diff = %q", got)
		}
		if files[0].Write() != nil {
			t.Error("the input file was modified")
		}
	})

	t.Run("calls follow config order, unmatched files are untouched", func(t *testing.T) {
		t.Parallel()
		configs := []versionedfile.Config{
			cfg(packageJSON, versionedfile.PackageJson, strp(fooDep)), cfg(packageJSON, versionedfile.PackageJson, nil),
		}
		files := []versionedfile.File{fakeFile{path: "unrelated.json"}, fakeFile{path: packageJSON, version: v100}}
		p, err := versioning.NewPackage(versioning.DefaultName(), configs, files, nil, versionedfile.Standard, nil)
		if err != nil {
			t.Fatal(err)
		}
		out, err := p.SetVersion(mustVersion(t, v200), versionedfile.Standard, files)
		if err != nil {
			t.Fatal(err)
		}
		if got := out[0].(fakeFile).calls; len(got) != 0 {
			t.Errorf("unrelated file got calls %v", got)
		}
		if got := strings.Join(out[1].(fakeFile).calls, ","); got != "foo=2.0.0,2.0.0" {
			t.Errorf("calls = %q", got)
		}
	})

	t.Run("a file shared by two packages", func(t *testing.T) {
		t.Parallel()
		files := []versionedfile.File{fakeFile{path: packageJSON, version: v100}}
		one, err := versioning.NewPackage(versioning.DefaultName(),
			[]versionedfile.Config{cfg(packageJSON, versionedfile.PackageJson, nil)}, files, nil, versionedfile.Standard, nil)
		if err != nil {
			t.Fatal(err)
		}
		two, err := versioning.NewPackage(
			versioning.CustomName("two"),
			[]versionedfile.Config{
				cfg(packageJSON, versionedfile.PackageJson, strp(fooDep)),
			},
			files,
			nil,
			versionedfile.Standard,
			nil,
		)
		if err != nil {
			t.Fatal(err)
		}
		files, err = one.SetVersion(mustVersion(t, v200), versionedfile.Standard, files)
		if err != nil {
			t.Fatal(err)
		}
		files, err = two.SetVersion(mustVersion(t, "3.0.0"), versionedfile.Standard, files)
		if err != nil {
			t.Fatal(err)
		}
		if got := strings.Join(files[0].(fakeFile).calls, ","); got != "2.0.0,foo=3.0.0" {
			t.Errorf("calls = %q", got)
		}
	})

	t.Run("history is reset", func(t *testing.T) {
		t.Parallel()
		files := []versionedfile.File{fakeFile{path: packageJSON, version: v100}}
		p, err := versioning.NewPackage(versioning.DefaultName(),
			[]versionedfile.Config{cfg(packageJSON, versionedfile.PackageJson, nil)}, files,
			[]string{"v1.1.0-rc.1", tagV100}, versionedfile.Standard, nil)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := p.SetVersion(mustVersion(t, "0.5.0"), versionedfile.Standard, files); err != nil {
			t.Fatal(err)
		}
		got, ok := p.LatestVersion()
		if !ok || got.String() != "0.5.0" {
			t.Errorf("LatestVersion() = %v, %v; want 0.5.0", got, ok)
		}
		if s, _ := p.Versions.LastStable(); s.String() != "0.5.0" {
			t.Errorf("LastStable() = %v, want 0.5.0", s)
		}
	})

	t.Run("an error leaves the history alone", func(t *testing.T) {
		t.Parallel()
		boom := errors.New("boom")
		files := []versionedfile.File{fakeFile{path: packageJSON, version: v100, setErr: boom}}
		p, err := versioning.NewPackage(versioning.DefaultName(),
			[]versionedfile.Config{cfg(packageJSON, versionedfile.PackageJson, nil)}, files, nil, versionedfile.Standard, nil)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := p.SetVersion(mustVersion(t, v200), versionedfile.Standard, files); !errors.Is(err, boom) {
			t.Fatalf("err = %v, want boom", err)
		}
		if got, _ := p.LatestVersion(); got.String() != v100 {
			t.Errorf("LatestVersion() = %v, want %s", got, v100)
		}
	})
}

func TestTags(t *testing.T) {
	t.Parallel()
	tests := map[string]struct {
		name      versioning.PackageName
		tag       string
		want      string
		isRelease bool
	}{
		"default":                   {versioning.DefaultName(), tagV123, tagV123, true},
		"default rejects prefixed":  {versioning.DefaultName(), fooTagV123, tagV123, false},
		"custom":                    {versioning.CustomName("foo"), fooTagV123, fooTagV123, true},
		"custom rejects default":    {versioning.CustomName("foo"), tagV123, fooTagV123, false},
		"custom rejects other name": {versioning.CustomName("foo"), "bar/v1.2.3", fooTagV123, false},
		"not a version":             {versioning.DefaultName(), "vnext", tagV123, false},
		"named default is custom":   {versioning.CustomName("default"), "default/v1.2.3", "default/v1.2.3", true},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			p := &versioning.Package{Name: tc.name}
			if got := p.ReleaseTag(mustVersion(t, v123)); got != tc.want {
				t.Errorf("ReleaseTag() = %q, want %q", got, tc.want)
			}
			if got := p.IsReleaseTag(tc.tag); got != tc.isRelease {
				t.Errorf("IsReleaseTag(%q) = %v, want %v", tc.tag, got, tc.isRelease)
			}
		})
	}
}

func TestPackageName(t *testing.T) {
	t.Parallel()
	def, foo := versioning.DefaultName(), versioning.CustomName("foo")
	if def.Custom() != nil || def.String() != "default" {
		t.Errorf("default = %v, %q", def.Custom(), def)
	}
	if c := foo.Custom(); c == nil || *c != fooDep || foo.String() != fooDep {
		t.Errorf("custom = %v, %q", c, foo)
	}
	*foo.Custom() = valChanged
	if foo.String() != "foo" {
		t.Error("Custom() exposes the name")
	}
	if !def.Equal(versioning.PackageName{}) || def.Equal(foo) || foo.Equal(def) || !foo.Equal(versioning.CustomName("foo")) ||
		versioning.CustomName("default").Equal(def) {
		t.Error("Equal is wrong")
	}
}

func TestPackageClone(t *testing.T) {
	t.Parallel()
	dep := fooDep
	orig := &versioning.Package{
		Name: versioning.CustomName("foo"),
		Files: []versionedfile.Config{
			{Path: packageJSON, Format: versionedfile.PackageJson, Dependency: &dep, Regex: []string{"a"}},
		},
		Versions: semver.FromVersion(mustVersion(t, v100)),
		Scopes:   []string{"core"},
		Override: ptr(mustVersion(t, "3.0.0-rc.1")),
	}

	c := orig.Clone()
	*c.Files[0].Dependency = valChanged
	c.Files[0].Regex[0] = valChanged
	c.Files[0].Path = valChanged
	c.Scopes[0] = valChanged
	c.Versions.Update(mustVersion(t, v200))
	if c.Override == nil || c.Override.String() != "3.0.0-rc.1" {
		t.Errorf("clone override = %v, want 3.0.0-rc.1", c.Override)
	}
	c.TakeOverride()
	if orig.Override == nil {
		t.Error("taking the override from the clone took it from the original")
	}

	if *orig.Files[0].Dependency != fooDep || orig.Files[0].Regex[0] != "a" || orig.Files[0].Path != packageJSON {
		t.Errorf("files changed through the clone: %+v", orig.Files[0])
	}
	if orig.Scopes[0] != "core" {
		t.Errorf("scopes changed through the clone: %v", orig.Scopes)
	}
	if got, _ := orig.LatestVersion(); got.String() != v100 {
		t.Errorf("version changed through the clone: %s", got)
	}
	if got, _ := c.LatestVersion(); got.String() != v200 {
		t.Errorf("clone version = %s, want %s", got, v200)
	}
	if !c.Name.Equal(orig.Name) {
		t.Errorf("clone name = %s, want %s", c.Name, orig.Name)
	}
}

func ptr[T any](v T) *T { return &v }
