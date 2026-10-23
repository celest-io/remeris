package config

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/celest-io/remeris/internal/diag"
)

const (
	fsRootWS   = `{"workspace":["a","b"]}`
	fsNameA    = `{"name":"@s/a","version":"1.0.0"}`
	fsNameFoo  = `{"name":"foo","version":"1.0.0"}`
	fsNameAt   = `{"name":"@/a","version":"1.0.0"}`
	fsImportB  = `{"name":"@s/b","version":"1.0.0","imports":{"x":"%s"}}`
	fsPkgsDeno = "pkgs/a/deno.json"
)

// TestDenoMembersOnDisk runs the detector on the production root (osRoot), where links and hidden directories exist.
func TestDenoMembersOnDisk(t *testing.T) {
	cases := map[string]struct {
		files map[string]string
		links map[string]string // link -> target
		want  []string
	}{
		"glob through a symlinked directory is dropped": {
			files: map[string]string{
				denoJSONFile: `{"workspace":["pkgs/*"]}`, fsPkgsDeno: fsNameA,
				"outside/deno.json": `{"name":"@s/out","version":"1.0.0"}`,
			},
			links: map[string]string{"pkgs/x": "../outside"},
			want:  []string{dtPkgsA},
		},
		"hidden directory is not matched by a star": {
			files: map[string]string{
				denoJSONFile: `{"workspace":["packages/*"]}`, "packages/a/deno.json": fsNameA,
				"packages/.template/deno.json": `{"name":"@s/t","version":"1.0.0"}`,
			},
			want: []string{"@s/a packages/a/CHANGELOG.md @s/a: packages/a/deno.json"},
		},
		"hidden directory is not matched by a double star": {
			files: map[string]string{
				denoJSONFile: `{"workspace":["packages/**"]}`, "packages/a/deno.json": fsNameA,
				"packages/.t/b/deno.json": `{"name":"@s/t","version":"1.0.0"}`,
			},
			want: []string{"@s/a packages/a/CHANGELOG.md @s/a: packages/a/deno.json"},
		},
		"hidden directory spelled with a dot is matched": {
			files: map[string]string{
				denoJSONFile: `{"workspace":[".hidden/*"]}`, ".hidden/a/deno.json": fsNameA,
			},
			want: []string{"@s/a .hidden/a/CHANGELOG.md @s/a: .hidden/a/deno.json"},
		},
		"hidden directory as a plain member": {
			files: map[string]string{denoJSONFile: `{"workspace":[".t"]}`, ".t/deno.json": fsNameA},
			want:  []string{"@s/a .t/CHANGELOG.md @s/a: .t/deno.json"},
		},
		"exclusion does not reach descendants": {
			files: map[string]string{
				denoJSONFile: `{"workspace":["pkgs/**","!pkgs/a"]}`,
				fsPkgsDeno:   fsNameA, "pkgs/a/sub/deno.json": `{"name":"@s/sub","version":"1.0.0"}`,
			},
			want: []string{"@s/sub pkgs/a/sub/CHANGELOG.md @s/sub: pkgs/a/sub/deno.json"},
		},
		"exclusion matches the directory itself only": {
			files: map[string]string{
				denoJSONFile: `{"workspace":["pkgs/*/*","!pkgs/*"]}`, "pkgs/a/b/deno.json": fsNameA,
			},
			want: []string{"@s/a pkgs/a/b/CHANGELOG.md @s/a: pkgs/a/b/deno.json"},
		},
		"exclusion by glob": {
			files: map[string]string{
				denoJSONFile: `{"workspace":["pkgs/*","!pkgs/b*"]}`, fsPkgsDeno: fsNameA,
				"pkgs/b1/deno.json": `{"name":"@s/b","version":"1.0.0"}`,
			},
			want: []string{dtPkgsA},
		},
		"jsr requirement without a scope": {
			files: map[string]string{
				denoJSONFile:  fsRootWS,
				dtADeno:       fsNameFoo,
				"b/deno.json": strings.Replace(fsImportB, "%s", "jsr:foo@1", 1),
			},
			want: []string{
				"foo a/CHANGELOG.md foo: a/deno.json b/deno.json@foo",
				"@s/b b/CHANGELOG.md @s/b: b/deno.json",
			},
		},
		"jsr requirement with an empty scope": {
			files: map[string]string{
				denoJSONFile:  fsRootWS,
				dtADeno:       fsNameAt,
				"b/deno.json": strings.Replace(fsImportB, "%s", "jsr:@/a@^1", 1),
			},
			want: []string{
				"@/a a/CHANGELOG.md @/a: a/deno.json b/deno.json@@/a",
				"@s/b b/CHANGELOG.md @s/b: b/deno.json",
			},
		},
		"null is the default configuration": {
			files: map[string]string{denoJSONFile: dtWsA, dtADeno: "null"},
		},
		"null root": {files: map[string]string{denoJSONFile: "null"}},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Chdir(t.TempDir())
			for file, content := range tc.files {
				writeTemp(t, file, content)
			}
			for link, target := range tc.links {
				if err := os.MkdirAll(filepath.Dir(link), 0o750); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(target, link); err != nil {
					t.Skipf("no symlinks: %v", err)
				}
			}
			pkgs, err := denoWorkspace(osRoot{})
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

func TestDenoErrorsOnDisk(t *testing.T) {
	cases := map[string]struct {
		files map[string]string
		want  string
	}{
		"version that is not a string": {
			files: map[string]string{denoJSONFile: `{"name":"a","version":1}`},
			want:  `Could not parse "deno.json"`,
		},
		"name that is not a string": {
			files: map[string]string{denoJSONFile: `{"workspace":["a"]}`, memberPkgJSON: `{"name":[],"version":"1"}`},
			want:  `Could not parse "a/package.json"`,
		},
		"lock path that is not a string": {
			files: map[string]string{denoJSONFile: `{"name":"a","version":"1.0.0","lock":{"path":5}}`},
			want:  `Could not parse "deno.json"`,
		},
		"jsonc nested too deep": {
			files: map[string]string{denoJSONCFile: strings.Repeat("[", 4<<20)},
			want:  `Could not parse "deno.jsonc": nesting is deeper than 10000 levels`,
		},
		"json nested too deep": {
			files: map[string]string{denoJSONFile: `{"workspace":["a"]}`, "a/package.json": strings.Repeat("[", 4<<20)},
			want:  `Could not parse "a/package.json": nesting is deeper than 10000 levels`,
		},
		"lock that is a directory": {
			files: map[string]string{denoJSONFile: fsNameA, "deno.lock/x": ""},
			want:  `"deno.lock" is not a regular file`,
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Chdir(t.TempDir())
			for file, content := range tc.files {
				writeTemp(t, file, content)
			}
			_, err := denoWorkspace(osRoot{})
			var d *diag.Error
			if !errors.As(err, &d) || !strings.Contains(d.Error(), tc.want) {
				t.Errorf("error = %v, want %q", err, tc.want)
			}
		})
	}
}

func TestJSONDepthExceeded(t *testing.T) {
	cases := map[string]struct {
		in    string
		limit int
		want  bool
	}{
		"flat":                      {`{"a":[1]}`, 2, false},
		"too deep":                  {`[[[`, 2, true},
		"brackets in strings":       {`["[[[[", "\"[[[["]`, 1, false},
		"brackets in line comment":  {"[ // [[[[\n]", 1, false},
		"brackets in block comment": {"[ /* [[[[ */ ]", 1, false},
		"closing resets":            {`[][][]`, 1, false},
		"unterminated string":       {`["[[[[`, 5, false},
		"unterminated comment":      {`[ /* [[[[`, 5, false},
		"lone slash":                {`[/`, 5, false},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			if got := jsonDepthExceeded([]byte(tc.in), tc.limit); got != tc.want {
				t.Errorf("jsonDepthExceeded(%q, %d) = %v, want %v", tc.in, tc.limit, got, tc.want)
			}
		})
	}
}

func TestUnsafeDenoPath(t *testing.T) {
	cases := map[string]struct {
		path    string
		windows bool
		want    bool
	}{
		"relative":            {"x/y", false, false},
		"relative on windows": {"x/y", true, false},
		"empty path":          {"", false, true},
		"rooted":              {"/x", false, true},
		"backslash on linux":  {`x\y`, false, false},
		"backslash":           {`x\y`, true, true},
		"parent backslash":    {`..\x`, true, true},
		"drive backslash":     {`C:\x`, true, true},
		"drive slash":         {"C:/x", true, true},
		"lower case drive":    {"c:x", true, true},
		"unc":                 {`\\srv\share`, true, true},
		"unc slashes":         {"//srv/share", false, true},
		"colon in a name":     {"ab:c", true, false},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			if got := unsafeDenoPath(tc.path, tc.windows); got != tc.want {
				t.Errorf("unsafeDenoPath(%q, %v) = %v, want %v", tc.path, tc.windows, got, tc.want)
			}
		})
	}
}

func TestResolveDenoMemberWindows(t *testing.T) {
	for _, raw := range []string{`..\x`, `C:\x`, "C:/x", `\\srv\share`, `p\q`} {
		if _, err := resolveDenoMember(raw, true); err == nil {
			t.Errorf("resolveDenoMember(%q, windows) succeeded", raw)
		}
	}
	if dir, err := resolveDenoMember("./p/q/", true); err != nil || dir != "p/q" {
		t.Errorf("resolveDenoMember = %q, %v", dir, err)
	}
}
