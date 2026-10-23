package config

import (
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/celest-io/remeris/internal/diag"
)

const notInside = "must be a path inside the workspace"

// TestCargoMembersOnDisk runs the detector on the production root, where `..` and absolute paths would escape.
func TestCargoMembersOnDisk(t *testing.T) {
	cases := map[string]struct {
		member    string
		wantFiles []string
		wantErr   string
	}{
		"plain":           {member: "first", wantFiles: []string{firstManifest}},
		"dot slash":       {member: "./first", wantFiles: []string{firstManifest}},
		"trailing slash":  {member: "first/", wantFiles: []string{firstManifest}},
		"inner dot":       {member: "./first/.", wantFiles: []string{firstManifest}},
		"double slash":    {member: "first//", wantFiles: []string{firstManifest}},
		"the root itself": {member: ".", wantFiles: []string{cargoTomlFile}},
		"parent":          {member: "../first", wantErr: notInside},
		"inner parent":    {member: "first/../first", wantErr: notInside},
		"only parent":     {member: "..", wantErr: notInside},
		"absolute":        {member: "/etc", wantErr: notInside},
		"empty":           {member: "", wantErr: notInside},
		"glob":            {member: "fi*", wantErr: "Error reading from fi*/Cargo.toml: "},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Chdir(t.TempDir())
			writeTemp(t, cargoTomlName, "[package]\nname = \"root\"\n[workspace]\nmembers = [\""+tc.member+"\"]\n")
			writeTemp(t, firstManifest, manifest(firstName, ""))
			pkgs, err := cargoWorkspace(osRoot{})
			if tc.wantErr != "" {
				var d *diag.Error
				if !errors.As(err, &d) || !strings.Contains(d.Error(), tc.wantErr) {
					t.Fatalf("error = %v, want %q", err, tc.wantErr)
				}
				return
			}
			if err != nil || len(pkgs) != 1 {
				t.Fatalf("packages = %+v, %v", pkgs, err)
			}
			if _, _, files := describe(pkgs[0]); !slices.Equal(files, tc.wantFiles) {
				t.Errorf("files = %v, want %v", files, tc.wantFiles)
			}
		})
	}
}

func TestCargoManifestTooLarge(t *testing.T) {
	big := strings.Repeat("#", maxManifestSize+1)
	cases := map[string]map[string]string{
		"root":   {cargoTomlName: workspaceFirst + big},
		"member": {cargoTomlName: workspaceFirst, firstManifest: manifest(firstName, big)},
	}
	for name, files := range cases {
		t.Run(name, func(t *testing.T) {
			t.Chdir(t.TempDir())
			for path, content := range files {
				writeTemp(t, path, content)
			}
			_, err := cargoWorkspace(osRoot{})
			var d *diag.Error
			if !errors.As(err, &d) || d.Code() != codeCargoTooLarge || !strings.HasSuffix(d.Error(), " is larger than 8 MiB") {
				t.Fatalf("error = %v, want %s", err, codeCargoTooLarge)
			}
			if strings.Contains(d.Error(), "##") {
				t.Error("the error carries file content")
			}
		})
	}
}

func TestCargoManifestKinds(t *testing.T) {
	t.Chdir(t.TempDir())
	writeTemp(t, cargoTomlName, workspaceFirst)
	mkdirTemp(t, firstManifest) // a directory where the manifest should be
	_, err := cargoWorkspace(osRoot{})
	var d *diag.Error
	if !errors.As(err, &d) || d.Code() != codeCargoRead || !strings.Contains(d.Error(), "not a regular file") {
		t.Fatalf("error = %v", err)
	}
}

func TestCargoDuplicateNames(t *testing.T) {
	pkgs, err := cargoWorkspace(rootOf(map[string]string{
		cargoTomlName: workspaceBoth,
		firstManifest: manifest("dup", ""), secondManifest: manifest("dup", ""),
	}))
	if err != nil || len(pkgs) != 2 {
		t.Fatalf("packages = %+v, %v", pkgs, err)
	}
	for _, pkg := range pkgs {
		if _, _, files := describe(pkg); !slices.Equal(files, []string{firstManifest, secondManifest}) {
			t.Errorf("files = %v, want both manifests", files)
		}
	}
}

func TestCargoWorkspaceKeyPlacement(t *testing.T) {
	const crlfWorkspace = "[workspace]\r\nmembers = [\"first\"]\r\n\r\n[dependencies]\r\nworkspace = { path = \"w\" }\r\n"
	cases := map[string]struct {
		cargo string
		want  int // packages
	}{
		"crate named workspace after the table": {
			cargo: workspaceFirst + "\n[dependencies]\nworkspace = { path = \"w\" }\n", want: 1,
		},
		"metadata key named workspace": {
			cargo: "[package.metadata.x]\nworkspace = { a = 1 }\n" + workspaceFirst, want: 1,
		},
		"quoted top-level inline key":     {cargo: "\"workspace\" = { members = [\"first\"] }\n", want: 0},
		"quoted key inside another table": {cargo: workspaceFirst + "[x]\n\"workspace\" = {}\n", want: 1},
		"CRLF with a later workspace key": {cargo: crlfWorkspace, want: 1},
		"CRLF inline":                     {cargo: "workspace = { members = [\"first\"] }\r\n", want: 0},
		"inline after a dotted key":       {cargo: "a.b = 1\nworkspace = { members = [\"first\"] }\n", want: 0},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			pkgs, err := cargoWorkspace(
				rootOf(map[string]string{cargoTomlName: tc.cargo, firstManifest: manifest(firstName, "")}),
			)
			if err != nil || len(pkgs) != tc.want {
				t.Fatalf("packages = %d, %v; want %d", len(pkgs), err, tc.want)
			}
		})
	}
}

func TestCargoThreeMembersOrder(t *testing.T) {
	const mid = "mid"
	pkgs, err := cargoWorkspace(rootOf(map[string]string{
		cargoTomlName:    "[workspace]\nmembers = [\"a\", \"mid\", \"c\"]\n[workspace.dependencies]\nmid = \"1\"\n",
		cargoLockName:    "version = 3\n",
		"a/Cargo.toml":   manifest("a", depSection+"mid = \"1\"\n"),
		"mid/Cargo.toml": manifest(mid, ""),
		"c/Cargo.toml":   manifest("c", "[dev-dependencies]\nmid = \"1\"\n"),
	}))
	if err != nil || len(pkgs) != 3 {
		t.Fatalf("packages = %d, %v", len(pkgs), err)
	}
	_, _, files := describe(pkgs[1])
	want := []string{"a/Cargo.toml#mid", "mid/Cargo.toml", "c/Cargo.toml#mid", "Cargo.toml#mid", "Cargo.lock"}
	if !slices.Equal(files, want) {
		t.Errorf("files = %v, want %v", files, want)
	}
}
