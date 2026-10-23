package config

import (
	"errors"
	"slices"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/celest-io/remeris/internal/diag"
)

const (
	dtRoot      = "deno.json"
	dtRootC     = "deno.jsonc"
	dtLock      = "deno.lock"
	dtWorkspace = `{"workspace":["a","b"]}`
	dtA         = `{"name":"@s/a","version":"1.0.0"}`
	dtB         = `{"name":"@s/b","version":"2.0.0"}`
	dtBImportsA = `{"name":"@s/b","version":"2.0.0","imports":{"a":"jsr:@s/a@^1.0.0"}}`
	dtADeno     = "a/deno.json"
	dtBDeno     = "b/deno.json"
	dtRootA     = "@s/a CHANGELOG.md @s/a: deno.json"
	dtLockA     = dtRootA + " deno.lock@@s/a"
	dtMemA      = "@s/a a/CHANGELOG.md @s/a: a/deno.json"
	dtEdgeA     = dtMemA + " b/deno.json@@s/a"
	dtPkgsA     = "@s/a pkgs/a/CHANGELOG.md @s/a: pkgs/a/deno.json"
	dtMemB      = "@s/b b/CHANGELOG.md @s/b: b/deno.json"
	dtWsA       = `{"workspace":["a"]}`
	dtJSRA      = "jsr @s/a"
	dtNPMPad    = "npm left-pad"

	dtBadWorkspace = `Failed to parse "workspace" configuration.`
	dtBPkg         = "b/package.json"
)

// summary renders a package as `name changelog scopes: file[@dependency] ...`.
func summary(p PackageConfig) string {
	var files []string
	for _, f := range p.VersionedFiles {
		s := f.Path
		if f.Dependency != nil {
			s += "@" + *f.Dependency
		}
		files = append(files, s)
	}
	return p.Name.String() + " " + p.Changelog + " " + strings.Join(p.Scopes, ",") + ": " + strings.Join(files, " ")
}

func TestDenoWorkspace(t *testing.T) {
	cases := map[string]struct {
		files map[string]string
		want  []string
	}{
		"lone root with name and version": {
			files: map[string]string{dtRoot: dtA},
			want:  []string{dtRootA},
		},
		"lone root deno.jsonc with comments": {
			files: map[string]string{dtRootC: "{\n// c\n\"name\":\"@s/a\",\"version\":\"1.0.0\",\n}"},
			want:  []string{"@s/a CHANGELOG.md @s/a: deno.jsonc"},
		},
		"no root config": {files: map[string]string{dtADeno: dtA, packageJSON: dtA}},
		"root package.json only": {
			files: map[string]string{packageJSON: dtA, "x": ""},
		},
		"root without name and version": {files: map[string]string{dtRoot: `{"name":"@s/a"}`}},
		"root without name, members with": {
			files: map[string]string{dtRoot: dtWorkspace, dtADeno: dtA, dtBDeno: dtB},
			want: []string{
				dtMemA,
				dtMemB,
			},
		},
		"root first, then members": {
			files: map[string]string{dtRoot: `{"name":"@s/r","version":"3.0.0","workspace":["a"]}`, dtADeno: dtA},
			want: []string{
				"@s/r CHANGELOG.md @s/r: deno.json",
				dtMemA,
			},
		},
		"object form": {
			files: map[string]string{dtRoot: `{"workspace":{"members":["./a"]}}`, dtADeno: dtA},
			want:  []string{dtMemA},
		},
		"members sorted by URL, not declared order": {
			files: map[string]string{
				dtRoot: `{"workspace":["a","a-b"]}`, dtADeno: dtA, "a-b/deno.json": `{"name":"@s/ab","version":"1.0.0"}`,
			},
			want: []string{
				"@s/ab a-b/CHANGELOG.md @s/ab: a-b/deno.json",
				dtMemA,
			},
		},
		"reversed declared order": {
			files: map[string]string{dtRoot: `{"workspace":["b","a"]}`, dtADeno: dtA, dtBDeno: dtB},
			want: []string{
				dtMemA,
				dtMemB,
			},
		},
		"glob with exclusion": {
			files: map[string]string{
				dtRoot: `{"workspace":["pkgs/*","!pkgs/b"]}`, "pkgs/a/deno.json": dtA, "pkgs/b/deno.json": dtB,
				"pkgs/c/README.md": "", "pkgs/node_modules/deno.json": dtB,
			},
			want: []string{dtPkgsA},
		},
		"glob with a negated glob": {
			files: map[string]string{
				dtRoot: `{"workspace":["pkgs/*","!pkgs/b*"]}`, "pkgs/a/deno.json": dtA, "pkgs/b1/deno.json": dtB,
			},
			want: []string{dtPkgsA},
		},
		"glob finds package.json folders and ignores brackets": {
			files: map[string]string{
				dtRoot: `{"workspace":["p[1]/*"]}`, "p[1]/a/package.json": dtA, "p1/x/package.json": dtB,
			},
			want: []string{"@s/a p[1]/a/CHANGELOG.md @s/a: p[1]/a/package.json"},
		},
		"deno.json wins over deno.jsonc": {
			files: map[string]string{dtRoot: dtWsA, dtADeno: dtA, "a/deno.jsonc": dtB},
			want:  []string{dtMemA},
		},
		"deno.jsonc is used alone": {
			files: map[string]string{dtRoot: dtWsA, "a/deno.jsonc": dtA},
			want:  []string{"@s/a a/CHANGELOG.md @s/a: a/deno.jsonc"},
		},
		"deno.json without a version falls back to package.json": {
			files: map[string]string{
				dtRoot: dtWsA, dtADeno: `{"name":"@s/x"}`, memberPkgJSON: dtA,
			},
			want: []string{"@s/a a/CHANGELOG.md @s/a: a/package.json"},
		},
		"member package.json fallback": {
			files: map[string]string{dtRoot: `{"workspace":["b"]}`, dtBPkg: dtB},
			want:  []string{"@s/b b/CHANGELOG.md @s/b: b/package.json"},
		},
		"member without name or version is skipped": {
			files: map[string]string{dtRoot: dtWorkspace, dtADeno: emptyObject, dtBDeno: dtB},
			want:  []string{dtMemB},
		},
		"lockfile default": {
			files: map[string]string{dtRoot: dtA, dtLock: emptyObject},
			want:  []string{dtLockA},
		},
		"lockfile default missing": {
			files: map[string]string{dtRoot: dtA},
			want:  []string{dtRootA},
		},
		"lockfile false": {
			files: map[string]string{dtRoot: `{"name":"@s/a","version":"1.0.0","lock":false}`, dtLock: emptyObject},
			want:  []string{dtRootA},
		},
		"lockfile true": {
			files: map[string]string{dtRoot: `{"name":"@s/a","version":"1.0.0","lock":true}`, dtLock: emptyObject},
			want:  []string{dtLockA},
		},
		"lockfile string": {
			files: map[string]string{
				dtRoot:          `{"name":"@s/a","version":"1.0.0","lock":"./sub/deno.lock"}`,
				"sub/deno.lock": emptyObject,
			},
			want: []string{"@s/a CHANGELOG.md @s/a: deno.json sub/deno.lock@@s/a"},
		},
		"lockfile object path": {
			files: map[string]string{
				dtRoot:        `{"name":"@s/a","version":"1.0.0","lock":{"path":"x/deno.lock","frozen":true}}`,
				"x/deno.lock": emptyObject,
			},
			want: []string{"@s/a CHANGELOG.md @s/a: deno.json x/deno.lock@@s/a"},
		},
		"lockfile object without path": {
			files: map[string]string{dtRoot: `{"name":"@s/a","version":"1.0.0","lock":{"frozen":true}}`, dtLock: emptyObject},
			want:  []string{dtLockA},
		},
		"lockfile named in the root is used for every member": {
			files: map[string]string{dtRoot: dtWorkspace, dtADeno: dtA, dtBDeno: dtB, dtLock: emptyObject},
			want: []string{
				"@s/a a/CHANGELOG.md @s/a: a/deno.json deno.lock@@s/a",
				"@s/b b/CHANGELOG.md @s/b: b/deno.json deno.lock@@s/b",
			},
		},
		"jsr import edge": {
			files: map[string]string{dtRoot: dtWorkspace, dtADeno: dtA, dtBDeno: dtBImportsA},
			want: []string{
				dtEdgeA,
				dtMemB,
			},
		},
		"jsr import edge in a scope and in compilerOptions": {
			files: map[string]string{
				dtRoot: dtWorkspace, dtADeno: dtA,
				dtBDeno: `{"name":"@s/b","version":"2.0.0","scopes":{"/x/":{"a":"jsr:/@s/a/sub.ts"}},` +
					`"compilerOptions":{"jsxImportSource":"jsr:@s/a@1"}}`,
			},
			want: []string{
				dtEdgeA,
				dtMemB,
			},
		},
		"jsr import with another name": {
			files: map[string]string{
				dtRoot: dtWorkspace, dtADeno: dtA,
				dtBDeno: `{"name":"@s/b","version":"2.0.0","imports":{"a":"jsr:@s/other@^1.0.0","c":"jsr:@s/aa"}}`,
			},
			want: []string{
				dtMemA,
				dtMemB,
			},
		},
		"npm import needs a package.json target": {
			files: map[string]string{
				dtRoot: dtWorkspace, memberPkgJSON: dtA,
				dtBDeno: `{"name":"@s/b","version":"2.0.0","imports":{"a":"npm:@s/a@1","b":"npm:@s/b"}}`,
			},
			want: []string{
				"@s/a a/CHANGELOG.md @s/a: a/package.json b/deno.json@@s/a",
				dtMemB,
			},
		},
		"npm import of a deno.json only target": {
			files: map[string]string{
				dtRoot: dtWorkspace, dtADeno: dtA,
				dtBDeno: `{"name":"@s/b","version":"2.0.0","imports":{"a":"npm:@s/a"}}`,
			},
			want: []string{
				dtMemA,
				dtMemB,
			},
		},
		"import edge points at the config of the dependent": {
			files: map[string]string{
				dtRoot: dtWorkspace, dtADeno: dtA,
				dtBDeno: `{"name":"@s/b","imports":{"a":"jsr:@s/a"}}`, dtBPkg: dtB,
			},
			want: []string{
				"@s/a a/CHANGELOG.md @s/a: a/deno.json b/package.json@@s/a",
				"@s/b b/CHANGELOG.md @s/b: b/package.json",
			},
		},
		"odd JSON shapes are ignored": {
			files: map[string]string{
				dtRoot: dtWorkspace, dtADeno: dtA,
				dtBDeno: `{"name":"@s/b","version":"2.0.0","imports":[1],"scopes":{"x":1},` +
					`"compilerOptions":{"types":[1,"jsr:@s/a"],"jsxImportSource":3}}`,
			},
			want: []string{
				dtEdgeA,
				dtMemB,
			},
		},
		"workspace null": {
			files: map[string]string{dtRoot: `{"name":"@s/a","version":"1.0.0","workspace":null}`},
			want:  []string{dtRootA},
		},
		"same member twice with the same text": {
			files: map[string]string{dtRoot: `{"workspace":["a","a"]}`, dtADeno: dtA},
			want:  []string{dtMemA},
		},
		"two globs reach the same member": {
			files: map[string]string{dtRoot: `{"workspace":["*","a*"]}`, dtADeno: dtA},
			want:  []string{dtMemA},
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			pkgs, err := denoWorkspace(rootOf(tc.files))
			if err != nil {
				t.Fatal(err)
			}
			var got []string
			for _, p := range pkgs {
				got = append(got, summary(p))
			}
			if !slices.Equal(got, tc.want) {
				t.Errorf("packages =\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(tc.want, "\n"))
			}
		})
	}
}

func TestDenoWorkspaceErrors(t *testing.T) {
	cases := map[string]struct {
		files map[string]string
		want  string
		code  string
	}{
		"member that does not parse": {
			files: map[string]string{dtRoot: dtWsA, dtADeno: `{"name": `},
			want:  "Failed to discover Deno workspace: Could not parse \"a/deno.json\"",
		},
		"member package.json that does not parse": {
			files: map[string]string{dtRoot: dtWsA, memberPkgJSON: `[1]`},
			want:  "Failed to discover Deno workspace: Could not parse \"a/package.json\"",
		},
		"root that does not parse": {
			files: map[string]string{dtRoot: `{"name": `},
			want:  "Failed to discover Deno workspace: Could not parse \"deno.json\"",
		},
		"root that is not an object": {
			files: map[string]string{dtRootC: `[]`},
			want:  "Failed to discover Deno workspace: Could not parse \"deno.jsonc\"",
		},
		"missing member directory": {
			files: map[string]string{dtRoot: `{"workspace":["nope"]}`},
			want:  "Failed to discover Deno workspace: Could not find config file for workspace member in \"nope/\".",
		},
		"member directory without a config": {
			files: map[string]string{dtRoot: dtWsA, "a/README.md": ""},
			want:  "Failed to discover Deno workspace: Could not find config file for workspace member in \"a/\".",
		},
		"member names the file": {
			files: map[string]string{dtRoot: `{"workspace":["a/deno.json"]}`, dtADeno: dtA},
			want:  "Ensure you specify the directory and not the configuration file",
		},
		"member outside the root": {
			files: map[string]string{dtRoot: `{"workspace":["../x"]}`},
			want:  "Failed to discover Deno workspace: Workspace member must be nested in a directory under the workspace.",
		},
		"absolute member": {
			files: map[string]string{dtRoot: `{"workspace":["/x"]}`},
			want:  "Workspace member must be nested in a directory under the workspace.",
		},
		"member that is the root": {
			files: map[string]string{dtRoot: `{"workspace":["."]}`},
			want:  "Remove the reference to the current config file",
		},
		"glob that reaches the root": {
			files: map[string]string{dtRoot: `{"workspace":["**"]}`},
			want:  "Remove the reference to the current config file",
		},
		"member twice as path and glob": {
			files: map[string]string{dtRoot: `{"workspace":["a","*"]}`, dtADeno: dtA},
			want:  "Failed to discover Deno workspace: Cannot specify a workspace member twice (\"a\").",
		},
		"member twice as two spellings": {
			files: map[string]string{dtRoot: `{"workspace":["a","./a"]}`, dtADeno: dtA},
			want:  "Cannot specify a workspace member twice (\"./a\").",
		},
		"duplicate package names": {
			files: map[string]string{dtRoot: dtWorkspace, dtADeno: dtA, dtBDeno: dtA},
			want: `Failed to discover Deno workspace: The "@s/a" package ("b/deno.json") cannot have the same name as ` +
				`the package at "a/deno.json".`,
		},
		"duplicate names without a version": {
			files: map[string]string{dtRoot: dtWorkspace, dtADeno: `{"name":"x"}`, dtBDeno: `{"name":"x"}`},
			want:  "cannot have the same name",
		},
		"duplicate with the root": {
			files: map[string]string{dtRoot: `{"name":"@s/a","workspace":["a"]}`, dtADeno: dtA},
			want:  `The "@s/a" package ("a/deno.json") cannot have the same name as the package at "deno.json".`,
		},
		"invalid workspace type": {
			files: map[string]string{dtRoot: `{"workspace":"a"}`},
			want:  `Failed to discover Deno workspace: Failed to parse "workspace" configuration.`,
		},
		"object form without members": {
			files: map[string]string{dtRoot: `{"workspace":{}}`},
			want:  dtBadWorkspace,
		},
		"object form with another key": {
			files: map[string]string{dtRoot: `{"workspace":{"members":[],"x":1}}`},
			want:  dtBadWorkspace,
		},
		"member that is a number": {
			files: map[string]string{dtRoot: `{"workspace":["a",1]}`},
			want:  dtBadWorkspace,
		},
		"pattern that does not compile": {
			files: map[string]string{dtRoot: `{"workspace":["a/***"]}`},
			want:  `Failed converting Deno workspace member "a/***" to pattern`,
		},
		"lock outside the root": {
			files: map[string]string{dtRoot: `{"name":"@s/a","version":"1.0.0","lock":"../deno.lock"}`},
			want:  `Failed to resolve deno.lock path: "../deno.lock" is outside the project directory`,
			code:  codeDenoLockfilePath,
		},
		"absolute lock": {
			files: map[string]string{dtRoot: `{"name":"@s/a","version":"1.0.0","lock":{"path":"/tmp/deno.lock"}}`},
			want:  `Failed to resolve deno.lock path: "/tmp/deno.lock" is outside the project directory`,
			code:  codeDenoLockfilePath,
		},
		"lock of the wrong type": {
			files: map[string]string{dtRoot: `{"name":"@s/a","version":"1.0.0","lock":3}`},
			want:  "Failed to resolve deno.lock path",
			code:  codeDenoLockfilePath,
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := denoWorkspace(rootOf(tc.files))
			var d *diag.Error
			if !errors.As(err, &d) {
				t.Fatalf("error = %v, want a diagnostic", err)
			}
			wantCode := tc.code
			if wantCode == "" {
				wantCode = codeDenoDiscover
			}
			if !strings.Contains(d.Error(), tc.want) || d.Code() != wantCode {
				t.Errorf("error = %q (%s), want %q (%s)", d.Error(), d.Code(), tc.want, wantCode)
			}
			if strings.Contains(d.Error(), `"name"`) {
				t.Errorf("error quotes the file: %q", d.Error())
			}
		})
	}
}

func TestDenoWorkspaceLimits(t *testing.T) {
	big := fstest.MapFS{dtRoot: &fstest.MapFile{Data: []byte(`{"pad":"` + strings.Repeat("x", maxManifestSize) + `"}`)}}
	if _, err := denoWorkspace(big); err == nil || !strings.Contains(err.Error(), "larger than 8 MiB") {
		t.Errorf("error = %v, want the size limit", err)
	}
	dir := fstest.MapFS{dtRoot: &fstest.MapFile{Mode: 0o20000000 | 0o755}}
	if _, err := denoWorkspace(dir); err == nil {
		t.Error("a directory named deno.json must be an error, not a panic")
	}
}

func TestDenoWorkspaceOtherDetectorsFiles(t *testing.T) {
	// A broken root package.json belongs to the npm detector: without a deno.json it is not read here.
	pkgs, err := denoWorkspace(rootOf(map[string]string{"package.json": "{"}))
	if err != nil || pkgs != nil {
		t.Errorf("= %v, %v; want nothing", pkgs, err)
	}
}

func TestDenoFallsBackToDefaultPackage(t *testing.T) {
	// A root deno.json with no usable folder finds no Deno packages: the default package takes over.
	pkgs, err := discoverPackages(rootOf(map[string]string{dtRoot: dtWsA, dtADeno: emptyObject}))
	if err != nil || len(pkgs) != 1 || summary(pkgs[0]) != "default  : deno.json" {
		t.Errorf("packages = %v, %v", pkgs, err)
	}
	// A root with a name and a version is one Deno package, and the default package is not added.
	pkgs, err = discoverPackages(rootOf(map[string]string{dtRoot: dtA, "package.json": dtB}))
	if err != nil || len(pkgs) != 1 || summary(pkgs[0]) != dtRootA {
		t.Errorf("packages = %v, %v", pkgs, err)
	}
}

func TestParseDenoRequirement(t *testing.T) {
	cases := map[string]struct {
		spec string
		want string // "jsr name", "npm name", or "" for none
	}{
		"jsr":                 {"jsr:@s/a", dtJSRA},
		"jsr with req":        {"jsr:@s/a@^1.2.3", dtJSRA},
		"jsr with subpath":    {"jsr:@s/a@1/mod.ts", dtJSRA},
		"jsr with slash":      {"jsr:/@s/a", dtJSRA},
		"jsr unscoped":        {"jsr:foo@1", "jsr foo"},
		"jsr no name":         {"jsr:@s/", ""},
		"jsr empty scope":     {"jsr:@/a", "jsr @/a"},
		"npm":                 {"npm:left-pad", dtNPMPad},
		"npm with req":        {"npm:left-pad@1", dtNPMPad},
		"npm scoped":          {"npm:@s/a@1/sub", "npm @s/a"},
		"npm with subpath":    {"npm:left-pad/x", dtNPMPad},
		"npm empty":           {"npm:", ""},
		"npm scope only":      {"npm:@s", ""},
		"https":               {"https://deno.land/x/a", ""},
		"relative":            {"./a.ts", ""},
		"empty spec":          {"", ""},
		"jsr prefix, no name": {"jsr:", ""},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			req, ok := parseDenoRequirement(tc.spec)
			got := ""
			if ok {
				got = "npm " + req.name
				if req.jsr {
					got = "jsr " + req.name
				}
			}
			if got != tc.want {
				t.Errorf("parseDenoRequirement(%q) = %q, want %q", tc.spec, got, tc.want)
			}
		})
	}
}
