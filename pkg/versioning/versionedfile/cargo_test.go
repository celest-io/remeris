package versionedfile_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/celest-io/remeris/internal/diag"
	"github.com/celest-io/remeris/pkg/versioning/versionedfile"
)

const (
	cargoPath     = "beep/Cargo.toml"
	cargoTester   = "[package]\nname = \"tester\"\nversion = \"1.2.3-rc.0\"\n"
	knopeVersions = "knope-versioning"
	v020          = "0.2.0"
	v110          = "1.1.0"
	cargoWS       = "[workspace.package]\nversion = \"1.2.3\"\n"
	cargoDepsHdr  = "\n[dependencies]\n"
	codeMissing   = "remeris::cargo::missing_property"
	v130          = "1.3.0"
	v123          = "1.2.3"
	diffV130      = "version = 1.3.0"
	diffKnope020  = "knope-versioning.version = 0.2.0"
)

func newCargo(t *testing.T, content string) versionedfile.File {
	t.Helper()
	f, err := versionedfile.New(versionedfile.Config{Path: cargoPath, Format: versionedfile.Cargo}, content, nil)
	if err != nil {
		t.Fatal(err)
	}
	return f
}

type cargoStep struct {
	version string
	dep     *string
}

func applySteps(t *testing.T, f versionedfile.File, steps []cargoStep) versionedfile.File {
	t.Helper()
	for _, s := range steps {
		var err error
		if f, err = f.SetVersion(mustVersion(t, s.version), s.dep, versionedfile.Standard); err != nil {
			t.Fatal(err)
		}
	}
	return f
}

func TestCargoVersion(t *testing.T) {
	t.Parallel()
	tests := map[string]struct {
		content string
		want    string
	}{
		"package":                      {cargoTester, "1.2.3-rc.0"},
		"workspace package":            {cargoWS, v123},
		"package wins":                 {cargoWS + "[package]\nname = \"a\"\nversion = \"4.5.6\"\n", "4.5.6"},
		"inherited from the workspace": {cargoWS + "[package]\nname = \"a\"\nversion.workspace = true\n", v123},
		"non-string package version":   {"[package]\nversion = 3\n" + cargoWS, v123},
		"comment after the value":      {"[package]\nversion = \"0.1.0\" # keep\n", "0.1.0"},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			got, err := newCargo(t, tc.content).Version()
			if err != nil {
				t.Fatal(err)
			}
			if got.String() != tc.want {
				t.Errorf("got %s, want %s", got, tc.want)
			}
		})
	}
}

func TestCargoErrors(t *testing.T) {
	t.Parallel()
	missing := cargoPath + " was missing required property `package.version` or `workspace.package.version`"
	tests := map[string]struct {
		content string
		build   bool // the error comes from New
		want    string
		code    string
	}{
		"missing":            {"[package]\nname = \"a\"\n", false, missing, codeMissing},
		"empty file":         {"", false, missing, codeMissing},
		"inherited only":     {"[package]\nversion.workspace = true\n", false, missing, codeMissing},
		"non-string version": {"[package]\nversion = 1\n", false, missing, codeMissing},
		"invalid toml":       {"[package\nname = ", true, "Invalid TOML in " + cargoPath, "remeris::cargo::toml"},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			var err error
			if tc.build {
				_, err = versionedfile.New(versionedfile.Config{Path: cargoPath, Format: versionedfile.Cargo}, tc.content, nil)
			} else {
				_, err = newCargo(t, tc.content).Version()
			}
			var d diag.Diagnostic
			if !errors.As(err, &d) {
				t.Fatalf("want a diagnostic, got %v", err)
			}
			if err.Error() != tc.want || d.Code() != tc.code {
				t.Errorf("got %q (%s), want %q (%s)", err.Error(), d.Code(), tc.want, tc.code)
			}
			if tc.build && errors.Unwrap(err) == nil {
				t.Error("the parse error should be the cause")
			}
		})
	}
}

func TestCargoSetVersion(t *testing.T) {
	t.Parallel()
	dep := strp(knopeVersions)
	tests := map[string]struct {
		content string
		steps   []cargoStep
		want    string
		diff    string
	}{
		// Ported from cargo.rs set_package_version.
		"set package version": {
			content: "\n[package]\nname = \"tester\"\nversion = \"0.1.0-rc.0\"\n\n[dependencies]\nknope-versioning = \"0.1.0\"\n",
			steps:   []cargoStep{{"1.2.3-rc.4", nil}},
			want:    "\n[package]\nname = \"tester\"\nversion = \"1.2.3-rc.4\"\n\n[dependencies]\nknope-versioning = \"0.1.0\"\n",
			diff:    "version = 1.2.3-rc.4",
		},
		// Ported from cargo.rs dependencies (two calls, diffs joined).
		"dependencies": {
			content: cargoTester + cargoDepsHdr + "knope-versioning = \"0.1.0\"\nother = {path = \"../other\"}\n" +
				"complex-requirement = \"3.*\"\ncomplex-requirement-in-object = { version = \"1.2.*\" }\n\n" +
				"[dev-dependencies]\nknope-versioning = {path = \"../blah\", version = \"0.1.0\" }\n\n" +
				"[workspace.dependencies]\nknope-versioning = \"0.1.0\"\n",
			steps: []cargoStep{{v020, dep}, {v200, strp("complex-requirement-in-object")}},
			want: cargoTester + cargoDepsHdr + "knope-versioning = \"0.2.0\"\nother = {path = \"../other\"}\n" +
				"complex-requirement = \"3.*\"\ncomplex-requirement-in-object = { version = \"2.0.0\" }\n\n" +
				"[dev-dependencies]\nknope-versioning = {path = \"../blah\", version = \"0.2.0\" }\n\n" +
				"[workspace.dependencies]\nknope-versioning = \"0.2.0\"\n",
			diff: "knope-versioning.version = 0.2.0, complex-requirement-in-object.version = 2.0.0",
		},
		"workspace version": {
			content: "[workspace]\nmembers = [\"a\"]\n\n" + cargoWS,
			steps:   []cargoStep{{v130, nil}},
			want:    "[workspace]\nmembers = [\"a\"]\n\n[workspace.package]\nversion = \"1.3.0\"\n",
			diff:    diffV130,
		},
		"inherited version edits the workspace": {
			content: cargoWS + "\n[package]\nname = \"root\"\nversion.workspace = true\n",
			steps:   []cargoStep{{v130, nil}},
			want:    "[workspace.package]\nversion = \"1.3.0\"\n\n[package]\nname = \"root\"\nversion.workspace = true\n",
			diff:    diffV130,
		},
		"member inheriting without a workspace table": {
			content: "[package]\nname = \"first\"\nversion.workspace = true\n",
			steps:   []cargoStep{{v130, nil}},
			want:    "[package]\nname = \"first\"\nversion.workspace = true\n",
			diff:    diffV130,
		},
		"package version wins over the workspace": {
			content: "[workspace.package]\nversion = \"9.8.7\" # Should be ignored\n\n[package]\nversion = \"1.0.0\"\n",
			steps:   []cargoStep{{v110, nil}},
			want:    "[workspace.package]\nversion = \"9.8.7\" # Should be ignored\n\n[package]\nversion = \"1.1.0\"\n",
			diff:    "version = 1.1.0",
		},
		"table dependency inserts version": {
			content: cargoTester + "\n[dependencies.knope-versioning]\npath = \"../x\"\n\n[other]\na = 1\n",
			steps:   []cargoStep{{v020, dep}},
			want:    cargoTester + "\n[dependencies.knope-versioning]\npath = \"../x\"\nversion = \"0.2.0\"\n\n[other]\na = 1\n",
			diff:    diffKnope020,
		},
		"table dependency sets version": {
			content: cargoTester + "\n[dependencies.knope-versioning]\nversion = \"0.1.0\"\npath = \"../x\"\n",
			steps:   []cargoStep{{v020, dep}},
			want:    cargoTester + "\n[dependencies.knope-versioning]\nversion = \"0.2.0\"\npath = \"../x\"\n",
			diff:    diffKnope020,
		},
		"inline table inserts version": {
			content: cargoTester + cargoDepsHdr + "knope-versioning = { path = \"../x\" }\n",
			steps:   []cargoStep{{v020, dep}},
			want:    cargoTester + cargoDepsHdr + "knope-versioning = { path = \"../x\" , version = \"0.2.0\" }\n",
			diff:    diffKnope020,
		},
		"dev dependency only": {
			content: cargoTester + "\n[dev-dependencies]\nknope-versioning = \"0.1.0\"\n",
			steps:   []cargoStep{{v020, dep}},
			want:    cargoTester + "\n[dev-dependencies]\nknope-versioning = \"0.2.0\"\n",
			diff:    diffKnope020,
		},
		"workspace dependency only": {
			content: cargoTester + "\n[workspace.dependencies]\nknope-versioning = \"0.1.0\"\n",
			steps:   []cargoStep{{v020, dep}},
			want:    cargoTester + "\n[workspace.dependencies]\nknope-versioning = \"0.2.0\"\n",
			diff:    diffKnope020,
		},
		"build dependencies are not edited": {
			content: cargoTester + "\n[build-dependencies]\nknope-versioning = \"0.1.0\"\n\n[target.'cfg(unix)'.dependencies]\n" +
				"knope-versioning = \"0.1.0\"\n",
			steps: []cargoStep{{v020, dep}},
			want: cargoTester + "\n[build-dependencies]\nknope-versioning = \"0.1.0\"\n\n[target.'cfg(unix)'.dependencies]\n" +
				"knope-versioning = \"0.1.0\"\n",
			diff: diffKnope020,
		},
		"absent dependency still records the diff": {
			content: cargoTester + cargoDepsHdr + "other = \"1\"\n",
			steps:   []cargoStep{{v020, dep}},
			want:    cargoTester + cargoDepsHdr + "other = \"1\"\n",
			diff:    diffKnope020,
		},
		"own version and a dependency": {
			content: cargoTester + cargoDepsHdr + "knope-versioning = \"0.1.0\"\n",
			steps:   []cargoStep{{v200, nil}, {v020, dep}},
			want:    strings.Replace(cargoTester, "1.2.3-rc.0", v200, 1) + cargoDepsHdr + "knope-versioning = \"0.2.0\"\n",
			diff:    "version = 2.0.0, knope-versioning.version = 0.2.0",
		},
		"comments and formatting are kept": {
			content: "# top\n[package]\nname = 'tester' # name\nversion   =   '1.0.0' # keep me\n\n# deps\n[dependencies]\n" +
				"knope-versioning = \"0.1.0\" # trailing\n",
			steps: []cargoStep{{v110, nil}, {v020, dep}},
			want: "# top\n[package]\nname = 'tester' # name\nversion   =   \"1.1.0\" # keep me\n\n# deps\n[dependencies]\n" +
				"knope-versioning = \"0.2.0\" # trailing\n",
			diff: "version = 1.1.0, knope-versioning.version = 0.2.0",
		},
		"crlf": {
			content: "[package]\r\nname = \"a\"\r\nversion = \"1.0.0\"\r\n\r\n[dependencies.d]\r\npath = \"x\"\r\n",
			steps:   []cargoStep{{v110, strp("d")}},
			want:    "[package]\r\nname = \"a\"\r\nversion = \"1.0.0\"\r\n\r\n[dependencies.d]\r\npath = \"x\"\r\nversion = \"1.1.0\"\r\n",
			diff:    "d.version = 1.1.0",
		},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			acts := applySteps(t, newCargo(t, tc.content), tc.steps).Write()
			if len(acts) != 1 {
				t.Fatalf("got %d actions", len(acts))
			}
			if got := acts[0]; got.Path != cargoPath || got.Content != tc.want || got.Diff != tc.diff {
				t.Errorf("got path %q diff %q content:\n%s\nwant diff %q content:\n%s",
					got.Path, got.Diff, got.Content, tc.diff, tc.want)
			}
		})
	}
}

func TestCargoFixtures(t *testing.T) {
	t.Parallel()
	// Knope's prepare_release/cargo and tauri_conf_json fixtures (in, out), reduced.
	const tauriHead = "[package]\nname = \"knope-test\"\nversion = \"%s\"\ndescription = \"A Tauri App\"\n\n" +
		"# See more keys\n\n[lib]\n# comment\nname = \"knope_test_lib\"\n\n[build-dependencies]\ntauri-build = { version = \"2\" }\n"
	tauri := func(v string) string { return strings.Replace(tauriHead, "%s", v, 1) }
	tests := map[string]struct {
		in, out string
		steps   []cargoStep
	}{
		"dependencies": {
			in: "[package]\nname = \"test-package\"\nversion = \"1.0.0\"\n\n[dependencies]\ndependency-name = \"1.0.0\"\n\n" +
				"[dev-dependencies]\ndev-dependency-name = \"1.0.0\"\n",
			out: "[package]\nname = \"test-package\"\nversion = \"1.1.0\"\n\n[dependencies]\ndependency-name = \"1.1.0\"\n\n" +
				"[dev-dependencies]\ndev-dependency-name = \"1.1.0\"\n",
			steps: []cargoStep{{v110, nil}, {v110, strp("dependency-name")}, {v110, strp("dev-dependency-name")}},
		},
		"workspace root": {
			in: "[workspace]\nmembers = [\"first\", \"second\"]\n\n[workspace.package]\nversion = \"9.8.7\" # Should be ignored\n\n" +
				"[workspace.dependencies]\nfirst-package = { path = \"first\", version = \"1.0.0\" }\nsecond-package = \"0.1.0\"\n",
			out: "[workspace]\nmembers = [\"first\", \"second\"]\n\n[workspace.package]\nversion = \"9.8.7\" # Should be ignored\n\n" +
				"[workspace.dependencies]\nfirst-package = { path = \"first\", version = \"1.1.0\" }\nsecond-package = \"0.2.0\"\n",
			steps: []cargoStep{{v110, strp("first-package")}, {v020, strp("second-package")}},
		},
		"workspace member": {
			in: "[package]\nname = \"first-package\"\nversion = \"1.0.0\"\n\n[dependencies]\nsecond-package = \"0.1.0\"\n\n" +
				"[dev-dependencies]\nsecond-package = { path = \"../second\", version = \"0.1.0\" }\n",
			out: "[package]\nname = \"first-package\"\nversion = \"1.1.0\"\n\n[dependencies]\nsecond-package = \"0.2.0\"\n\n" +
				"[dev-dependencies]\nsecond-package = { path = \"../second\", version = \"0.2.0\" }\n",
			steps: []cargoStep{{v110, nil}, {v020, strp("second-package")}},
		},
		"shared version": {
			in:    "[workspace]\nmembers = [\"first\"]\n\n" + cargoWS + "\n[package]\nname = \"root\"\nversion.workspace = true\n",
			out:   "[workspace]\nmembers = [\"first\"]\n\n[workspace.package]\nversion = \"1.3.0\"\n\n[package]\nname = \"root\"\nversion.workspace = true\n",
			steps: []cargoStep{{v130, nil}},
		},
		"tauri": {in: tauri("0.1.0"), out: tauri(v020), steps: []cargoStep{{v020, nil}}},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			acts := applySteps(t, newCargo(t, tc.in), tc.steps).Write()
			if len(acts) != 1 || acts[0].Content != tc.out {
				t.Errorf("got %v, want content:\n%s", acts, tc.out)
			}
		})
	}
}

func TestCargoWriteWithoutEdit(t *testing.T) {
	t.Parallel()
	if acts := newCargo(t, cargoTester).Write(); acts != nil {
		t.Errorf("got %v, want nil", acts)
	}
}

func TestCargoImmutable(t *testing.T) {
	t.Parallel()
	orig := newCargo(t, cargoTester)
	edited := applySteps(t, orig, []cargoStep{{"3.0.0", nil}})
	if orig.Write() != nil {
		t.Error("the receiver gained an edit")
	}
	if v, _ := orig.Version(); v.String() != "1.2.3-rc.0" {
		t.Errorf("the receiver's version changed to %s", v)
	}
	if v, _ := edited.Version(); v.String() != "3.0.0" {
		t.Errorf("got %s, want 3.0.0", v)
	}
	again := applySteps(t, edited, []cargoStep{{"4.0.0", strp("x")}})
	if got := edited.Write()[0].Diff; got != "version = 3.0.0" {
		t.Errorf("the second call changed the first value: %q", got)
	}
	if got := again.Write()[0].Diff; got != "version = 3.0.0, x.version = 4.0.0" {
		t.Errorf("got %q", got)
	}
}

func TestCargoDottedDependency(t *testing.T) {
	t.Parallel()
	content := cargoTester + cargoDepsHdr + "d.path = \"../d\"\n"
	_, err := newCargo(t, content).SetVersion(mustVersion(t, v020), strp("d"), versionedfile.Standard)
	var d diag.Diagnostic
	if !errors.As(err, &d) {
		t.Fatalf("want a diagnostic, got %v", err)
	}
	if want := "Cannot add a version to dependency d in " + cargoPath; err.Error() != want ||
		d.Code() != "remeris::cargo::dependency_not_insertable" {
		t.Errorf("got %q (%s)", err.Error(), d.Code())
	}
	// A dotted dependency that already has a version can be edited.
	f := applySteps(t, newCargo(t, cargoTester+cargoDepsHdr+"d.version = \"1\"\n"), []cargoStep{{v020, strp("d")}})
	if got := f.Write()[0].Content; got != cargoTester+cargoDepsHdr+"d.version = \"0.2.0\"\n" {
		t.Errorf("got %q", got)
	}
}

func TestCargoPackageName(t *testing.T) {
	t.Parallel()
	tests := map[string]struct {
		content string
		want    string
		ok      bool
		wantErr bool
	}{
		"name":        {cargoTester, "tester", true, false},
		"no name":     {"[package]\nversion = \"1.0.0\"\n", "", false, false},
		"no package":  {cargoWS, "", false, false},
		"not string":  {"[package]\nname = 3\n", "", false, false},
		"invalid":     {"[package", "", false, true},
		"literal str": {"[package]\nname = 'a-b'\n", "a-b", true, false},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			got, ok, err := versionedfile.CargoPackageName(tc.content)
			if (err != nil) != tc.wantErr || got != tc.want || ok != tc.ok {
				t.Errorf("got %q, %v, %v", got, ok, err)
			}
		})
	}
}

func TestCargoContainsDependency(t *testing.T) {
	t.Parallel()
	tests := map[string]struct {
		content string
		want    bool
		wantErr bool
	}{
		"basic":        {cargoTester + cargoDepsHdr + "knope-versioning = \"0.1.0\"\n", true, false},
		"inline table": {cargoTester + cargoDepsHdr + "knope-versioning = { version = \"0.1.0\" }\n", true, false},
		"table": {
			cargoTester + "\n[dependencies.knope-versioning]\npath = \"../x\"\nversion = \"0.1.0\"\n",
			true,
			false,
		},
		"dev":              {cargoTester + "\n[dev-dependencies]\nknope-versioning = \"0.1.0\"\n", true, false},
		"workspace":        {cargoTester + "\n[workspace.dependencies]\nknope-versioning = \"0.1.0\"\n", true, false},
		"build only":       {cargoTester + "\n[build-dependencies]\nknope-versioning = \"0.1.0\"\n", false, false},
		"other dependency": {cargoTester + cargoDepsHdr + "other = \"0.1.0\"\n", false, false},
		"none":             {cargoTester, false, false},
		"invalid":          {"[dependencies", false, true},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			got, err := versionedfile.CargoContainsDependency(tc.content, knopeVersions)
			if (err != nil) != tc.wantErr || got != tc.want {
				t.Errorf("got %v, %v", got, err)
			}
		})
	}
}
