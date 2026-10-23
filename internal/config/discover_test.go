package config

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"testing/fstest"

	"github.com/celest-io/remeris/internal/diag"
	"github.com/celest-io/remeris/pkg/versioning"
)

const (
	cargoTomlName   = "Cargo.toml"
	cargoLockName   = "Cargo.lock"
	goModName       = "go.mod"
	packageJSONName = "package.json"
	cargoMembers    = "[workspace]\nmembers = [\"a\"]\n"
	noWorkspace     = ""
	denoJSONName    = "deno.json"
	denoJSONCName   = "deno.jsonc"
	denoMemberJSON  = "a/deno.json"
	kindNPM         = "npm"
	kindDeno        = "Deno"
	emptyObject     = "{}"
	denoWorkspaceA  = `{"workspace":["./a"]}`
	nameOnlyA       = `{"name":"a"}`
	memberPkgJSON   = "a/package.json"
	workspacesA     = `{"workspaces":["a"]}`
	nameVersionA    = `{"name":"a","version":"1.0.0"}`
)

func rootOf(files map[string]string) fstest.MapFS {
	root := fstest.MapFS{}
	for name, content := range files {
		root[name] = &fstest.MapFile{Data: []byte(content)}
	}
	return root
}

func TestDiscoverPackages(t *testing.T) {
	cases := map[string]struct {
		files         map[string]string
		wantFiles     []string
		wantChangelog string
	}{
		"nothing":         {files: map[string]string{"README.md": ""}},
		"empty directory": {files: nil},
		"cargo and lock": {
			files:     map[string]string{cargoTomlName: "", cargoLockName: ""},
			wantFiles: []string{cargoTomlName, cargoLockName},
		},
		"table order": {
			files:     map[string]string{"pyproject.toml": "", goModName: "", cargoTomlName: ""},
			wantFiles: []string{cargoTomlName, goModName, "pyproject.toml"},
		},
		"changelog": {
			files:         map[string]string{goModName: "", defaultChangelog: ""},
			wantFiles:     []string{goModName},
			wantChangelog: defaultChangelog,
		},
		"changelog alone": {files: map[string]string{defaultChangelog: ""}},
		"no deno.jsonc":   {files: map[string]string{denoJSONCName: emptyObject}},
		"empty cargo members": {
			files:     map[string]string{cargoTomlName: "[workspace]\nmembers = []\n"},
			wantFiles: []string{cargoTomlName},
		},
		"cargo package": {
			files:     map[string]string{cargoTomlName: "[package]\nname = \"a\"\n[workspace]\n"},
			wantFiles: []string{cargoTomlName},
		},
		"npm object form": {
			files:     map[string]string{packageJSONName: `{"workspaces":{"packages":["a"]}}`},
			wantFiles: []string{packageJSONName},
		},
		"cargo workspace.package only": {
			files:     map[string]string{cargoTomlName: "[workspace.package]\nversion = \"1.0.0\"\n"},
			wantFiles: []string{cargoTomlName},
		},
		"npm empty workspaces": {
			files:     map[string]string{packageJSONName: `{"workspaces":[]}`},
			wantFiles: []string{packageJSONName},
		},
		"npm patterns match nothing": {
			files:     map[string]string{packageJSONName: `{"workspaces":["packages/*"]}`, "packages/a/README.md": ""},
			wantFiles: []string{packageJSONName},
		},
		"npm non-string entries": {
			files:     map[string]string{packageJSONName: `{"workspaces":[1,null]}`},
			wantFiles: []string{packageJSONName},
		},
		"deno empty workspace": {
			files:     map[string]string{denoJSONName: `{"workspace":[]}`},
			wantFiles: []string{denoJSONName},
		},
		"deno members without name and version": {
			files: map[string]string{
				denoJSONName: denoWorkspaceA, denoMemberJSON: nameOnlyA, memberPkgJSON: nameOnlyA,
			},
			wantFiles: []string{denoJSONName},
		},
		"deno object without members": {
			files:     map[string]string{denoJSONName: `{"workspace":{}}`},
			wantFiles: []string{denoJSONName},
		},
		"deno root without name or version": {
			files:     map[string]string{denoJSONName: `{"imports":{}}`},
			wantFiles: []string{denoJSONName},
		},
		"unparseable deno.json": {
			files:     map[string]string{denoJSONName: `{"name": "x", "version": `},
			wantFiles: []string{denoJSONName},
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			pkgs, err := discoverPackages(rootOf(tc.files))
			if err != nil {
				t.Fatal(err)
			}
			if tc.wantFiles == nil {
				if len(pkgs) != 0 {
					t.Fatalf("packages = %+v, want none", pkgs)
				}
				return
			}
			if len(pkgs) != 1 || pkgs[0].Name != versioning.DefaultName() {
				t.Fatalf("packages = %+v, want the default package", pkgs)
			}
			var got []string
			for _, f := range pkgs[0].VersionedFiles {
				got = append(got, f.Path)
			}
			if !slices.Equal(got, tc.wantFiles) || pkgs[0].Changelog != tc.wantChangelog {
				t.Errorf("files = %v changelog = %q, want %v %q", got, pkgs[0].Changelog, tc.wantFiles, tc.wantChangelog)
			}
		})
	}
}

func TestDiscoverWorkspaces(t *testing.T) {
	cases := map[string]struct {
		files map[string]string
		want  string
	}{
		"deno": {
			map[string]string{
				denoJSONName:   denoWorkspaceA,
				denoMemberJSON: nameVersionA,
			},
			kindDeno,
		},
		"deno jsonc with comments": {
			map[string]string{
				denoJSONCName: "{\n// members\n\"workspace\": [\"./a\", /* x */],\n}\n",
				memberPkgJSON: nameVersionA,
			}, kindDeno,
		},
		"deno member with deno.json": {
			map[string]string{
				denoJSONName:   denoWorkspaceA,
				denoMemberJSON: nameVersionA,
			},
			kindDeno,
		},
		"deno member with package.json": {
			map[string]string{
				denoJSONName:  `{"workspace":["a"]}`,
				memberPkgJSON: nameVersionA,
			},
			kindDeno,
		},
		"deno member by glob": {
			map[string]string{
				denoJSONName:        `{"workspace":["pkgs/*"]}`,
				"pkgs/a/deno.jsonc": nameVersionA,
			},
			kindDeno,
		},
		"deno object form": {
			map[string]string{
				denoJSONName: `{"workspace":{"members":["./a"]}}`, denoMemberJSON: nameVersionA,
			}, kindDeno,
		},
		"deno root with name and version": {map[string]string{denoJSONName: nameVersionA}, kindDeno},
		"deno.jsonc root with name and version": {
			map[string]string{denoJSONCName: "{\n// x\n\"name\":\"a\",\"version\":\"1.0.0\",\n}"}, kindDeno,
		},
		"deno root with a named package.json": {
			map[string]string{denoJSONName: `{}`, packageJSONName: nameVersionA}, kindDeno,
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := discoverPackages(rootOf(tc.files))
			var d *diag.Error
			if !errors.As(err, &d) {
				t.Fatalf("error = %v, want a diagnostic", err)
			}
			want := "Package discovery for " + tc.want + " workspaces is not implemented yet (milestone M10)"
			if d.Error() != want || d.Code() != codeDiscoveryNotImplemented {
				t.Errorf("error = %q (%s), want %q", d.Error(), d.Code(), want)
			}
		})
	}
}

func TestLoadFSDiscoversPackages(t *testing.T) {
	root := rootOf(map[string]string{cargoTomlName: packageHeader, defaultChangelog: ""})
	dir := t.TempDir()

	cfg, err := LoadFS(filepath.Join(dir, "knope.toml"), root)
	if err != nil || cfg.Source != SourceDefault || cfg.PackageCount() != 1 || cfg.HasForge() {
		t.Fatalf("default config = %+v, %v", cfg, err)
	}
	var names []string
	for _, w := range cfg.Workflows {
		names = append(names, w.Name)
	}
	if want := []string{wfRelease, wfDocumentChange, "get-version"}; !slices.Equal(names, want) {
		t.Errorf("workflows = %v, want %v", names, want)
	}

	// A file without packages (Knope's hybrid config) gets them too, even with its own workflows.
	path := filepath.Join(dir, "knope.toml")
	if err := os.WriteFile(path, []byte("[[workflows]]\nname = \"a\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err = LoadFS(path, root)
	if err != nil || cfg.Source != SourceFile || cfg.PackageCount() != 1 {
		t.Fatalf("file config = %+v, %v", cfg, err)
	}

	// Declared packages are never replaced.
	if err := os.WriteFile(path, []byte("[package]\nversioned_files = [\"Cargo.toml\"]\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err = LoadFS(path, rootOf(map[string]string{cargoTomlName: "", goModName: ""}))
	if err != nil || len(cfg.PackageConfigs()[0].VersionedFiles) != 1 {
		t.Fatalf("declared package = %+v, %v", cfg.PackageConfigs(), err)
	}
}

func TestLoadFSDefersWorkspaceError(t *testing.T) {
	root := rootOf(map[string]string{packageJSONName: workspacesA, memberPkgJSON: emptyObject})
	cfg, err := LoadFS(filepath.Join(t.TempDir(), "knope.toml"), root)
	if err != nil {
		t.Fatalf("loading must succeed so that --help works: %v", err)
	}
	if cfg.DiscoveryError() == nil || cfg.PackageCount() != 0 {
		t.Errorf("DiscoveryError = %v, count = %d", cfg.DiscoveryError(), cfg.PackageCount())
	}
}

func TestReadJSONC(t *testing.T) {
	cases := map[string]struct {
		in   string
		want int // number of keys, or -1 for nil
	}{
		"comment markers in strings": {`{"a":"// no /* no */"}`, 1},
		"line comment":               {"{\"a\":1} // x\n", 1},
		"block comment":              {"{/* x */\"a\":1}", 1},
		"trailing comma array":       {`{"a":[1,2,]}`, 1},
		"trailing comma object":      {`{"a":1,}`, 1},
		"comment before }":           {"{\"a\":1, /* x */ }", 1},
		"bom":                        {"\xef\xbb\xbf{}", 0},
		"unterminated block comment": {"{\"a\":1 /* x", -1},
		"not an object":              {`[1]`, -1},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			got := readJSONC(rootOf(map[string]string{denoJSONCName: tc.in}), denoJSONCName)
			if (tc.want < 0) != (got == nil) || len(got) != max(tc.want, 0) {
				t.Errorf("readJSONC(%q) = %v, want %d keys", tc.in, got, tc.want)
			}
		})
	}
}

func TestSourceValues(t *testing.T) {
	cases := map[string]struct {
		toml string
		want Source
	}{
		"no file":                {"", SourceDefault},
		"file with workflows":    {"[[workflows]]\nname = \"a\"\n", SourceFile},
		"file without workflows": {"[changes]\n", SourceHybrid},
		"packages, no workflows": {"[package]\nversioned_files = [\"" + cargoTomlName + "\"]\n", SourceHybrid},
	}
	root := rootOf(map[string]string{cargoTomlName: packageHeader})
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "knope.toml")
			if tc.toml != "" {
				if err := os.WriteFile(path, []byte(tc.toml), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			cfg, err := LoadFS(path, root)
			if err != nil || cfg.Source != tc.want {
				t.Errorf("Source = %v, %v; want %v", cfg.Source, err, tc.want)
			}
		})
	}
}

func TestEmptyPackagesBeatsDiscovery(t *testing.T) {
	root := rootOf(map[string]string{cargoTomlName: cargoMembers})
	path := filepath.Join(t.TempDir(), "knope.toml")
	if err := os.WriteFile(path, []byte("[packages]\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := LoadFS(path, root)
	var d *diag.Error
	if !errors.As(err, &d) || d.Code() != codeEmptyPackages {
		t.Errorf("error = %v, want %s", err, codeEmptyPackages)
	}
}
