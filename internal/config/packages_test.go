package config

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"testing"
	"testing/fstest"

	"github.com/celest-io/remeris/internal/diag"
	"github.com/celest-io/remeris/pkg/versioning"
	"github.com/celest-io/remeris/pkg/versioning/versionedfile"
)

const (
	cargoToml     = "Cargo.toml"
	packageHeader = "[package]\n"
	versionFile   = "VERSION"
)

// loadIn loads content with a file system that holds only the named files.
func loadIn(t *testing.T, content string, files ...string) (*Config, error) {
	t.Helper()
	root := fstest.MapFS{}
	for _, f := range files {
		root[f] = &fstest.MapFile{}
	}
	path := filepath.Join(t.TempDir(), "knope.toml")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return LoadFS(path, root)
}

func rendered(err error) string {
	var buf bytes.Buffer
	diag.Render(&buf, err)
	return buf.String()
}

func TestPackageValidationErrors(t *testing.T) {
	t.Parallel()
	cases := map[string]struct {
		toml  string
		files []string
		want  string
	}{
		"both package and packages": {
			toml: "[package]\n\n[packages.knope]\n",
			want: "Error: You cannot define both `packages` and `package`\n" +
				"  help: Add the `package` as a key under `packages` instead.\n",
		},
		"package and an empty packages": {
			toml: "[package]\n[packages]\n",
			want: "Error: The `packages` key is defined but does not contain any packages\n" +
				"  help: Add at least one package to the `packages` key.\n",
		},
		"empty packages alone": {
			toml: "[packages]\n",
			want: "Error: The `packages` key is defined but does not contain any packages\n" +
				"  help: Add at least one package to the `packages` key.\n",
		},
		"unknown file": {
			toml:  "[package]\nversioned_files = [\"setup.py\"]\n",
			files: []string{"setup.py"},
			want: "Error: Problem with versioned file\n" +
				"  ╰─▶ Unknown file: setup.py\n" +
				"      help: remeris identifies the type of file based on its name. Supported file names: ",
		},
		"missing file": {
			toml: "[package]\nversioned_files = [\"pyproject.toml\", \"" + cargoToml + "\"]\n",
			want: "Error: File pyproject.toml does not exist\n  help: Make sure the file exists and is accessible.\n",
		},
		"the first bad file wins, unknown before missing": {
			toml:  "[package]\nversioned_files = [\"" + cargoToml + "\", \"x.py\", \"missing/package.json\"]\n",
			files: []string{cargoToml},
			want:  "Error: Problem with versioned file\n  ╰─▶ Unknown file: x.py\n",
		},
		"missing file in a later package": {
			toml:  "[packages.a]\nversioned_files = [\"" + cargoToml + "\"]\n[packages.b]\nversioned_files = [\"b/package.json\"]\n",
			files: []string{cargoToml},
			want:  "Error: File b/package.json does not exist\n",
		},
		"dependency and regex": {
			toml: "[package]\nversioned_files = [{ path = \"" + cargoToml + "\", dependency = \"x\", regex = \"v\" }]\n",
			want: "Error: Problem with versioned file\n  ╰─▶ Cannot specify both 'dependency' and 'regex' for the same file: " +
				cargoToml + "\n",
		},
		"gitea with assets": {
			toml: "[gitea]\nowner = \"o\"\nrepo = \"r\"\nhost = \"h\"\n[package]\nassets = \"dist/*\"\n",
			want: "Error: Asset uploads for Gitea are not supported\n" +
				"  help: Remove the `[[package.assets]]` key from your config.\n",
		},
		"gitea with empty assets list": {
			toml: "[gitea]\nowner = \"o\"\n[package]\nassets = []\n",
			want: "Error: Asset uploads for Gitea are not supported\n",
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			_, err := loadIn(t, tc.toml, tc.files...)
			if err == nil {
				t.Fatal("Load() succeeded")
			}
			if got := rendered(err); len(got) < len(tc.want) || got[:len(tc.want)] != tc.want {
				t.Errorf("rendered error:\n%q\nwant prefix\n%q", got, tc.want)
			}
		})
	}
}

func TestPackageErrorsHaveLabelsAndCodes(t *testing.T) {
	t.Parallel()
	const src = "[package]\n\n[packages.knope]\n"
	_, err := loadIn(t, src)
	var d diag.Diagnostic
	if !asDiag(err, &d) || d.Code() != codeConflictingPackages {
		t.Fatalf("error = %v", err)
	}
	if d.SourceCode() != src || len(d.Labels()) != 2 {
		t.Fatalf("source %q labels %v", d.SourceCode(), d.Labels())
	}
	if got := src[d.Labels()[0].Start:d.Labels()[0].End]; got[:9] != "[package]" ||
		d.Labels()[0].Text != "`package` defined here" {
		t.Errorf("label 0 = %q %q", got, d.Labels()[0].Text)
	}
	if got := src[d.Labels()[1].Start:]; got[:16] != "[packages.knope]" || d.Labels()[1].Text != "`packages` defined here" {
		t.Errorf("label 1 = %q %q", got, d.Labels()[1].Text)
	}

	const missing = "[packages.a]\nchangelog = \"C.md\"\nversioned_files = [\"x/package.json\"]\n"
	_, err = loadIn(t, missing)
	if !asDiag(err, &d) || d.Code() != codeMissingFile || len(d.Labels()) != 1 {
		t.Fatalf("error = %v", err)
	}
	if l := d.Labels()[0]; missing[l.Start:l.End] != `"x/package.json"` || l.Text != "Declared here" {
		t.Errorf("label = %q %q", missing[l.Start:l.End], l.Text)
	}

	const unknown = "[package]\nversioned_files = [\"a.py\"]\n"
	_, err = loadIn(t, unknown, "a.py")
	if !asDiag(err, &d) || len(d.Labels()) != 1 || unknown[d.Labels()[0].Start:d.Labels()[0].End] != `"a.py"` {
		t.Fatalf("error = %v labels %v", err, d.Labels())
	}
	if rel := d.Related(); len(rel) != 1 {
		t.Errorf("related = %v", rel)
	}
}

func asDiag(err error, d *diag.Diagnostic) bool { return errors.As(err, d) }

func TestVersionedFileForms(t *testing.T) {
	t.Parallel()
	dep := "serde"
	cases := map[string]struct {
		entry string
		want  versionedfile.Config
	}{
		"bare path": {`"Cargo.toml"`, versionedfile.Config{Path: cargoToml, Format: versionedfile.Cargo}},
		"path in a directory": {
			`"web/package.json"`, versionedfile.Config{Path: "web/package.json", Format: versionedfile.PackageJson},
		},
		"dependency": {
			`{ path = "Cargo.toml", dependency = "serde" }`,
			versionedfile.Config{Path: cargoToml, Format: versionedfile.Cargo, Dependency: &dep},
		},
		"regex string": {
			`{ path = "VERSION", regex = "v(?<version>\\d+)" }`,
			versionedfile.Config{Path: versionFile, Format: versionedfile.RegexFile, Regex: []string{`v(?<version>\d+)`}},
		},
		"regex list": {
			`{ path = "VERSION", regex = ["a(?<version>1)", "b(?<version>2)"] }`,
			versionedfile.Config{
				Path:   versionFile,
				Format: versionedfile.RegexFile,
				Regex:  []string{"a(?<version>1)", "b(?<version>2)"},
			},
		},
		"empty regex list is a present regex": {
			`{ path = "VERSION", regex = [] }`,
			versionedfile.Config{Path: versionFile, Format: versionedfile.RegexFile, Regex: []string{}},
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			cfg, err := loadIn(t, packageHeader+"versioned_files = ["+tc.entry+"]\n", tc.want.Path)
			if err != nil {
				t.Fatal(err)
			}
			got := cfg.PackageConfigs()
			if len(got) != 1 || len(got[0].VersionedFiles) != 1 || !reflect.DeepEqual(got[0].VersionedFiles[0], tc.want) {
				t.Errorf("PackageConfigs() = %+v, want file %+v", got, tc.want)
			}
		})
	}
}

func TestPackageConfigs(t *testing.T) {
	t.Parallel()
	t.Run("default package", func(t *testing.T) {
		t.Parallel()
		cfg, err := loadIn(t, `
[package]
versioned_files = ["Cargo.toml"]
changelog = "CHANGELOG.md"
scopes = []
ignore_go_major_versioning = true
extra_changelog_sections = [{ name = "Docs", footers = ["Docs"], types = ["docs"] }]
assets = [{ path = "dist/*.zip", name = "bundle" }]
`, cargoToml)
		if err != nil {
			t.Fatal(err)
		}
		pkgs := cfg.PackageConfigs()
		if len(pkgs) != 1 {
			t.Fatalf("PackageConfigs() = %+v", pkgs)
		}
		p := pkgs[0]
		if !p.Name.Equal(versioning.DefaultName()) || p.Changelog != "CHANGELOG.md" || !p.IgnoreGoMajorVersioning {
			t.Errorf("package = %+v", p)
		}
		if p.Scopes == nil || len(p.Scopes) != 0 {
			t.Errorf("`scopes = []` should be empty and non-nil, got %#v", p.Scopes)
		}
		if want := []ExtraSection{{Name: "Docs", Footers: []string{"Docs"}, Types: []string{"docs"}}}; !reflect.DeepEqual(
			p.ExtraChangelogSections, want) {
			t.Errorf("sections = %+v", p.ExtraChangelogSections)
		}
		if want := []Asset{{Path: "dist/*.zip", Name: "bundle"}}; !reflect.DeepEqual(p.Assets, want) {
			t.Errorf("assets = %+v", p.Assets)
		}
	})

	t.Run("absent scopes and assets are nil", func(t *testing.T) {
		t.Parallel()
		cfg, err := loadIn(t, packageHeader)
		if err != nil {
			t.Fatal(err)
		}
		p := cfg.PackageConfigs()[0]
		if p.Scopes != nil || p.Assets != nil || p.Changelog != "" || len(p.VersionedFiles) != 0 {
			t.Errorf("package = %+v", p)
		}
	})

	t.Run("named packages keep declaration order", func(t *testing.T) {
		t.Parallel()
		cfg, err := loadIn(t, `
[packages.zebra]
versioned_files = ["z/Cargo.toml"]
[packages."foo.bar"]
scopes = ["a"]
[packages.apple]
`, "z/Cargo.toml")
		if err != nil {
			t.Fatal(err)
		}
		var names []string
		for _, p := range cfg.PackageConfigs() {
			names = append(names, p.Name.String())
			if p.Name.Custom() == nil {
				t.Errorf("%s should be a custom name", p.Name)
			}
		}
		if want := []string{"zebra", "foo.bar", "apple"}; !slices.Equal(names, want) {
			t.Errorf("order = %q, want %q", names, want)
		}
		if got := cfg.PackageConfigs()[1].Scopes; !slices.Equal(got, []string{"a"}) {
			t.Errorf("scopes = %q", got)
		}
	})

	t.Run("no packages", func(t *testing.T) {
		t.Parallel()
		cfg, err := loadIn(t, "[[workflows]]\nname = \"w\"\nsteps = []\n")
		if err != nil {
			t.Fatal(err)
		}
		if len(cfg.PackageConfigs()) != 0 {
			t.Errorf("PackageConfigs() = %+v", cfg.PackageConfigs())
		}
	})

	t.Run("gitea without assets is fine", func(t *testing.T) {
		t.Parallel()
		if _, err := loadIn(t, "[gitea]\nowner = \"o\"\n[package]\n"); err != nil {
			t.Fatal(err)
		}
	})
}

func TestLoadChecksTheWorkingDirectory(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	if err := os.WriteFile(cargoToml, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile("knope.toml", []byte("[package]\nversioned_files = [\"Cargo.toml\"]\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load("knope.toml"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile("knope.toml", []byte("[package]\nversioned_files = [\"package.json\"]\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load("knope.toml"); err == nil || err.Error() != "File package.json does not exist" {
		t.Errorf("Load() error = %v", err)
	}
}
