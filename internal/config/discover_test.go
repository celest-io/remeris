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
		"no deno.jsonc":   {files: map[string]string{"deno.jsonc": "{}"}},
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
		"broken cargo": {files: map[string]string{cargoTomlName: "[[["}, wantFiles: []string{cargoTomlName}},
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
		"cargo": {map[string]string{cargoTomlName: cargoMembers}, "Cargo"},
		"npm":   {map[string]string{packageJSONName: `{"workspaces":["a"]}`}, "npm"},
		"deno":  {map[string]string{"deno.json": `{"workspace":["./a"]}`}, "Deno"},
		"deno jsonc with comments": {
			map[string]string{"deno.jsonc": "{\n// members\n\"workspace\": [\"./a\", /* x */],\n}\n"}, "Deno",
		},
		"cargo wins over npm": {
			map[string]string{cargoTomlName: cargoMembers, packageJSONName: `{"workspaces":["a"]}`}, "Cargo",
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
	if got := workspaceKind(rootOf(map[string]string{goModName: ""})); got != noWorkspace {
		t.Errorf("workspaceKind = %q", got)
	}
}

func TestLoadFSDiscoversPackages(t *testing.T) {
	root := rootOf(map[string]string{cargoTomlName: "[package]\n", defaultChangelog: ""})
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
	root := rootOf(map[string]string{cargoTomlName: cargoMembers})
	cfg, err := LoadFS(filepath.Join(t.TempDir(), "knope.toml"), root)
	if err != nil {
		t.Fatalf("loading must succeed so that --help works: %v", err)
	}
	if cfg.DiscoveryError() == nil || cfg.PackageCount() != 0 {
		t.Errorf("DiscoveryError = %v, count = %d", cfg.DiscoveryError(), cfg.PackageCount())
	}
}
