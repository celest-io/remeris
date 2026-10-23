package config

import (
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/celest-io/remeris/internal/diag"
	"github.com/celest-io/remeris/pkg/versioning/versionedfile"
)

const (
	firstManifest  = "first/Cargo.toml"
	secondManifest = "second/Cargo.toml"
	firstName      = "first-package"
	secondName     = "second-package"
	workspaceFirst = "[workspace]\nmembers = [\"first\"]\n"
	workspaceBoth  = "[workspace]\nmembers = [\"first\", \"second\"]\n"
	depSection     = "[dependencies]\n"
	badTOML        = "[[["
)

func manifest(name, extra string) string {
	return "[package]\nname = \"" + name + "\"\nversion = \"1.0.0\"\n" + extra
}

// describe prints a package as name, scopes and its files, a dependency entry as path#dependency.
func describe(pkg PackageConfig) (name string, scopes, files []string) {
	for _, f := range pkg.VersionedFiles {
		entry := f.Path
		if f.Dependency != nil {
			entry += "#" + *f.Dependency
		}
		files = append(files, entry)
	}
	return pkg.Name.String(), pkg.Scopes, files
}

func TestCargoWorkspaceRoot(t *testing.T) {
	cases := map[string]struct {
		files    map[string]string
		wantCode string
	}{
		"missing Cargo.toml":        {files: map[string]string{goModName: ""}},
		"empty Cargo.toml":          {files: map[string]string{cargoTomlName: ""}},
		"package without workspace": {files: map[string]string{cargoTomlName: packageHeader}},
		"workspace without members": {files: map[string]string{cargoTomlName: "[workspace]\n"}},
		"empty members":             {files: map[string]string{cargoTomlName: "[workspace]\nmembers = []\n"}},
		"inline workspace table": {
			files: map[string]string{
				cargoTomlName: "workspace = { members = [\"first\"] }\n",
				firstManifest: manifest(firstName, ""),
			},
		},
		"members as array of tables": {
			files: map[string]string{cargoTomlName: "[[workspace.members]]\nname = \"x\"\n"},
		},
		"members is not an array": {files: map[string]string{cargoTomlName: "[workspace]\nmembers = \"first\"\n"}},
		"does not parse":          {files: map[string]string{cargoTomlName: badTOML}, wantCode: codeCargoTOML},
		"unterminated string":     {files: map[string]string{cargoTomlName: "a = \"x"}, wantCode: codeCargoTOML},
		"non-string member": {
			files: map[string]string{
				cargoTomlName: "[workspace]\nmembers = [\"first\", 3]\n",
				firstManifest: manifest(firstName, ""),
			},
			wantCode: codeCargoMembers,
		},
		"missing member file": {
			files:    map[string]string{cargoTomlName: workspaceFirst},
			wantCode: codeCargoRead,
		},
		"glob member": {
			files: map[string]string{
				cargoTomlName:         "[workspace]\nmembers = [\"crates/*\"]\n",
				"crates/a/Cargo.toml": manifest("a", ""),
			},
			wantCode: codeCargoRead,
		},
		"member does not parse": {
			files:    map[string]string{cargoTomlName: workspaceFirst, firstManifest: badTOML},
			wantCode: codeCargoTOML,
		},
		"member without a name": {
			files:    map[string]string{cargoTomlName: workspaceFirst, firstManifest: packageHeader},
			wantCode: codeCargoNoPackageName,
		},
		"member name is not a string": {
			files:    map[string]string{cargoTomlName: workspaceFirst, firstManifest: "[package]\nname = 3\n"},
			wantCode: codeCargoNoPackageName,
		},
		"member without a package": {
			files:    map[string]string{cargoTomlName: workspaceFirst, firstManifest: "[workspace]\n"},
			wantCode: codeCargoNoPackageName,
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			pkgs, err := cargoWorkspace(rootOf(tc.files))
			if pkgs != nil {
				t.Errorf("packages = %+v, want none", pkgs)
			}
			if tc.wantCode == "" {
				if err != nil {
					t.Fatalf("error = %v, want none", err)
				}
				return
			}
			var d *diag.Error
			if !errors.As(err, &d) || d.Code() != tc.wantCode {
				t.Fatalf("error = %v, want code %s", err, tc.wantCode)
			}
		})
	}
}

func TestCargoWorkspaceErrorTexts(t *testing.T) {
	cases := map[string]struct {
		files map[string]string
		want  string
	}{
		"non-string member": {
			files: map[string]string{cargoTomlName: "[workspace]\nmembers = [1]\n"},
			want:  "The Cargo workspace members array should contain only strings",
		},
		"missing member file": {
			files: map[string]string{cargoTomlName: workspaceFirst},
			want:  "Error reading from first/Cargo.toml: ",
		},
		"member does not parse": {
			files: map[string]string{cargoTomlName: workspaceFirst, firstManifest: badTOML},
			want:  "Could not parse TOML in first/Cargo.toml: ",
		},
		"missing name": {
			files: map[string]string{cargoTomlName: workspaceFirst, firstManifest: packageHeader},
			want:  "Could not find a package.name in first/Cargo.toml",
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := cargoWorkspace(rootOf(tc.files))
			if err == nil || !strings.HasPrefix(err.Error(), tc.want) {
				t.Errorf("error = %v, want prefix %q", err, tc.want)
			}
		})
	}
}

func TestCargoWorkspacePackages(t *testing.T) {
	cases := map[string]struct {
		files map[string]string
		want  map[string][]string // package name to files, in member order
	}{
		"no dependencies": {
			files: map[string]string{
				cargoTomlName: workspaceBoth, firstManifest: manifest(firstName, ""), secondManifest: manifest(secondName, ""),
			},
			want: map[string][]string{firstName: {firstManifest}, secondName: {secondManifest}},
		},
		"own file is not first": {
			files: map[string]string{
				cargoTomlName: workspaceBoth,
				firstManifest: manifest(
					firstName,
					depSection+"second-package = \"1\"\n",
				),
				secondManifest: manifest(secondName, ""),
			},
			want: map[string][]string{
				firstName:  {firstManifest},
				secondName: {firstManifest + "#" + secondName, secondManifest},
			},
		},
		"dev and workspace dependencies": {
			files: map[string]string{
				cargoTomlName: workspaceBoth,
				firstManifest: manifest(firstName, "[dev-dependencies]\nsecond-package = \"1\"\n"),
				secondManifest: manifest(secondName,
					"[workspace.dependencies]\nfirst-package = \"1\"\n"),
			},
			want: map[string][]string{
				firstName:  {firstManifest, secondManifest + "#" + firstName},
				secondName: {firstManifest + "#" + secondName, secondManifest},
			},
		},
		"root dependency and lock": {
			files: map[string]string{
				cargoTomlName: workspaceBoth + "[workspace.dependencies]\nsecond-package = { path = \"second\" }\n",
				cargoLockName: "version = 3\n",
				firstManifest: manifest(firstName, ""), secondManifest: manifest(secondName, ""),
			},
			want: map[string][]string{
				firstName:  {firstManifest, cargoLockName},
				secondName: {secondManifest, cargoTomlName + "#" + secondName, cargoLockName},
			},
		},
		"root dev-dependencies": {
			files: map[string]string{
				cargoTomlName: workspaceFirst + "[dev-dependencies]\nfirst-package = \"1\"\n",
				firstManifest: manifest(firstName, ""),
			},
			want: map[string][]string{firstName: {firstManifest, cargoTomlName + "#" + firstName}},
		},
		"build, target and renamed dependencies are not edges": {
			files: map[string]string{
				cargoTomlName: workspaceBoth + "[build-dependencies]\nfirst-package = \"1\"\n" +
					"[target.'cfg(unix)'.dependencies]\nsecond-package = \"1\"\n",
				firstManifest: manifest(
					firstName,
					"[build-dependencies]\nsecond-package = \"1\"\n[dependencies]\nrenamed = { package = \"second-package\" }\n",
				),
				secondManifest: manifest(secondName, "[target.'cfg(unix)'.dependencies]\nfirst-package = \"1\"\n"),
			},
			want: map[string][]string{firstName: {firstManifest}, secondName: {secondManifest}},
		},
		"exclude and root package are ignored": {
			files: map[string]string{
				cargoTomlName: "[package]\nname = \"root\"\nversion = \"9.0.0\"\n[workspace]\nmembers = [\"first\"]\n" +
					"exclude = [\"second\"]\n[workspace.package]\nversion = \"7.0.0\"\n",
				firstManifest: manifest(firstName, ""),
			},
			want: map[string][]string{firstName: {firstManifest}},
		},
		"irrelevant keys of other types": {
			files: map[string]string{
				cargoTomlName: workspaceFirst + "[dependencies]\nfirst-package = 1\n[dev-dependencies]\nx = [1]\n",
				firstManifest: manifest(firstName, "[dependencies]\nx = 1\n[workspace]\ndependencies = 3\n"),
			},
			want: map[string][]string{firstName: {firstManifest, cargoTomlName + "#" + firstName}},
		},
		"dotted workspace key": {
			files: map[string]string{
				cargoTomlName: "workspace.members = [\"first\"]\n", firstManifest: manifest(firstName, ""),
			},
			want: map[string][]string{firstName: {firstManifest}},
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			pkgs, err := cargoWorkspace(rootOf(tc.files))
			if err != nil {
				t.Fatal(err)
			}
			if len(pkgs) != len(tc.want) {
				t.Fatalf("got %d packages, want %d", len(pkgs), len(tc.want))
			}
			for _, pkg := range pkgs {
				pkgName, scopes, files := describe(pkg)
				want, ok := tc.want[pkgName]
				if !ok || !slices.Equal(files, want) {
					t.Errorf("%s files = %v, want %v", pkgName, files, want)
				}
				if !slices.Equal(scopes, []string{pkgName}) || pkg.Changelog != "" {
					t.Errorf("%s scopes = %v changelog = %q", pkgName, scopes, pkg.Changelog)
				}
			}
		})
	}
}

// TestCargoWorkspaceKnopeGenerate reproduces tests/generate/cargo_workspace/out/knope.toml.
func TestCargoWorkspaceKnopeGenerate(t *testing.T) {
	pkgs, err := cargoWorkspace(rootOf(map[string]string{
		cargoTomlName: workspaceBoth,
		firstManifest: manifest(firstName, ""), secondManifest: manifest(secondName, depSection+"unrelated = \"1\"\n"),
	}))
	if err != nil {
		t.Fatal(err)
	}
	dep := secondName
	// first-package depends on nothing; second-package is versioned in first/Cargo.toml only if first depends on it.
	want := []PackageConfig{
		{Name: pkgs[0].Name, VersionedFiles: []versionedfile.Config{{Path: firstManifest, Format: versionedfile.Cargo}}},
		{Name: pkgs[1].Name, VersionedFiles: []versionedfile.Config{{Path: secondManifest, Format: versionedfile.Cargo}}},
	}
	want[0].Scopes, want[1].Scopes = []string{firstName}, []string{secondName}
	if !equalPackages(pkgs, want) {
		t.Fatalf("packages = %+v, want %+v", pkgs, want)
	}

	// With first depending on second, the second package lists [{first, dependency}, second] as Knope's fixture.
	pkgs, err = cargoWorkspace(rootOf(map[string]string{
		cargoTomlName: workspaceBoth,
		firstManifest: manifest(firstName, depSection+"second-package = \"1\"\n"), secondManifest: manifest(secondName, ""),
	}))
	if err != nil {
		t.Fatal(err)
	}
	want[1].VersionedFiles = []versionedfile.Config{
		{Path: firstManifest, Format: versionedfile.Cargo, Dependency: &dep},
		{Path: secondManifest, Format: versionedfile.Cargo},
	}
	if !equalPackages(pkgs, want) {
		t.Fatalf("packages = %+v, want %+v", pkgs, want)
	}
}

func equalPackages(got, want []PackageConfig) bool {
	return slices.EqualFunc(got, want, func(a, b PackageConfig) bool {
		return a.Name.Equal(b.Name) && slices.Equal(a.Scopes, b.Scopes) && a.Changelog == b.Changelog &&
			slices.EqualFunc(a.VersionedFiles, b.VersionedFiles, func(x, y versionedfile.Config) bool {
				return x.Path == y.Path && x.Format == y.Format && (x.Dependency == nil) == (y.Dependency == nil) &&
					(x.Dependency == nil || *x.Dependency == *y.Dependency)
			})
	})
}

func TestCargoParseErrorStopsDiscovery(t *testing.T) {
	_, err := discoverPackages(rootOf(map[string]string{cargoTomlName: badTOML, goModName: ""}))
	var d *diag.Error
	if !errors.As(err, &d) || d.Code() != codeCargoTOML || errors.Unwrap(d) == nil {
		t.Fatalf("error = %v, want the TOML error with its cause", err)
	}
	if !strings.HasPrefix(d.Error(), "Could not parse TOML in Cargo.toml: ") {
		t.Errorf("message = %q", d.Error())
	}
}

func TestCargoMemberErrorStopsDiscovery(t *testing.T) {
	files := map[string]string{cargoTomlName: cargoMembers, packageJSONName: workspacesA}
	_, err := discoverPackages(rootOf(files))
	var d *diag.Error
	if !errors.As(err, &d) || d.Code() != codeCargoRead {
		t.Fatalf("error = %v, want %s before the npm detector runs", err, codeCargoRead)
	}
}

func TestCargoWorkspaceFoundBeatsDefaultPackage(t *testing.T) {
	pkgs, err := discoverPackages(rootOf(map[string]string{
		cargoTomlName: workspaceFirst, firstManifest: manifest(firstName, ""), goModName: "",
	}))
	if err != nil || len(pkgs) != 1 || pkgs[0].Name.String() != firstName {
		t.Fatalf("packages = %+v, %v", pkgs, err)
	}
}
