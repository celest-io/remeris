package config

import (
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/celest-io/remeris/internal/diag"
)

const (
	npmRootDoc  = `{"workspaces": ["packages/*"]}`
	npmNameA    = `{"name": "a"}`
	npmNameB    = `{"name": "b"}`
	npmNameC    = `{"name": "c"}`
	npmPkgA     = "packages/a/package.json"
	npmPkgB     = "packages/b/package.json"
	npmPkgC     = "packages/c/package.json"
	errNoNameA  = "Could not find a name in packages/a/package.json"
	npmSummaryA = "a packages/a/CHANGELOG.md a packages/a/package.json"
	npmSummaryB = "b packages/b/CHANGELOG.md b packages/b/package.json"
	npmSummaryC = "c packages/c/CHANGELOG.md c packages/c/package.json"
)

// summary prints a package as "name changelog scopes file[@dependency]...".
func summary(pkg PackageConfig) string {
	parts := []string{pkg.Name.String(), pkg.Changelog, strings.Join(pkg.Scopes, ",")}
	for _, f := range pkg.VersionedFiles {
		s := f.Path
		if f.Dependency != nil {
			s += "@" + *f.Dependency
		}
		parts = append(parts, s)
	}
	return strings.Join(parts, " ")
}

func TestNPMWorkspaces(t *testing.T) {
	cases := map[string]struct {
		files map[string]string
		want  []string
	}{
		"no package.json":          {files: map[string]string{npmPkgA: npmNameA}},
		"package.json is not JSON": {files: map[string]string{packageJSONName: "{", npmPkgA: npmNameA}},
		"no workspaces key":        {files: map[string]string{packageJSONName: emptyObject, npmPkgA: npmNameA}},
		"object form gives none": {
			files: map[string]string{packageJSONName: `{"workspaces": {"packages": ["packages/*"]}}`, npmPkgA: npmNameA},
		},
		"workspaces is a string": {
			files: map[string]string{packageJSONName: `{"workspaces": "packages/*"}`, npmPkgA: npmNameA},
		},
		"non-string entries are skipped": {
			files: map[string]string{
				packageJSONName: `{"workspaces": [1, null, {"a": 1}, ["x"], true, "packages/*"]}`,
				npmPkgA:         npmNameA,
			},
			want: []string{npmSummaryA},
		},
		"patterns in order, each one sorted": {
			files: map[string]string{
				packageJSONName:   `{"workspaces": ["z", "packages/*"]}`,
				"z/package.json":  `{"name": "z"}`,
				npmPkgB:           npmNameB,
				npmPkgA:           npmNameA,
				"packages/x.json": emptyObject,
			},
			want: []string{
				"z z/CHANGELOG.md z z/package.json",
				npmSummaryA,
				npmSummaryB,
			},
		},
		"a directory matched twice appears once": {
			files: map[string]string{
				packageJSONName: `{"workspaces": ["packages/b", "packages/*", "./packages/a", "packages/a/"]}`,
				npmPkgA:         npmNameA,
				npmPkgB:         npmNameB,
			},
			want: []string{npmSummaryB, npmSummaryA},
		},
		"a file match is skipped": {
			files: map[string]string{
				packageJSONName: npmRootDoc,
				"packages/file": "x",
				npmPkgA:         npmNameA,
			},
			want: []string{npmSummaryA},
		},
		"a directory without package.json is skipped": {
			files: map[string]string{packageJSONName: npmRootDoc, "packages/empty/README.md": "x", npmPkgA: npmNameA},
			want:  []string{npmSummaryA},
		},
		"a directory with an invalid package.json is skipped": {
			files: map[string]string{packageJSONName: npmRootDoc, "packages/bad/package.json": "{", npmPkgA: npmNameA},
			want:  []string{npmSummaryA},
		},
		"a negation pattern is literal": {
			files: map[string]string{packageJSONName: `{"workspaces": ["!packages/a", "packages/*"]}`, npmPkgA: npmNameA},
			want:  []string{npmSummaryA},
		},
		"a negation pattern alone matches nothing": {
			files: map[string]string{packageJSONName: `{"workspaces": ["!packages/a"]}`, npmPkgA: npmNameA},
		},
		"a pattern that matches nothing": {
			files: map[string]string{packageJSONName: `{"workspaces": ["nowhere/*"]}`, npmPkgA: npmNameA},
		},
		"root lock file": {
			files: map[string]string{packageJSONName: npmRootDoc, "package-lock.json": emptyObject, npmPkgA: npmNameA},
			want:  []string{"a packages/a/CHANGELOG.md a packages/a/package.json package-lock.json@a"},
		},
		"yarn and pnpm locks are ignored": {
			files: map[string]string{packageJSONName: npmRootDoc, "yarn.lock": "", "pnpm-lock.yaml": "", npmPkgA: npmNameA},
			want:  []string{npmSummaryA},
		},
		"a lock file in a workspace is ignored": {
			files: map[string]string{
				packageJSONName:                npmRootDoc,
				"packages/a/package-lock.json": emptyObject,
				npmPkgA:                        npmNameA,
			},
			want: []string{npmSummaryA},
		},
		"dependencies and devDependencies are edges, peer and optional are not": {
			files: map[string]string{
				packageJSONName: `{"workspaces": ["packages/*", "other/*"]}`,
				npmPkgA:         npmNameA,
				npmPkgB:         `{"name": "b", "dependencies": {"a": "1.0.0"}}`,
				npmPkgC:         `{"name": "c", "devDependencies": {"a": "1.0.0"}, "dependencies": {"b": "1"}}`,
				"other/d/package.json": `{"name": "d", "peerDependencies": {"a": "1", "b": "1"},
					"optionalDependencies": {"a": "1", "b": "1"}}`,
				"other/e/package.json": `{"name": "e", "dependencies": ["a"], "devDependencies": "a"}`,
			},
			want: []string{
				"a packages/a/CHANGELOG.md a packages/a/package.json packages/b/package.json@a packages/c/package.json@a",
				"b packages/b/CHANGELOG.md b packages/b/package.json packages/c/package.json@b",
				npmSummaryC,
				"d other/d/CHANGELOG.md d other/d/package.json",
				"e other/e/CHANGELOG.md e other/e/package.json",
			},
		},
		"a dependency with a null value is an edge": {
			files: map[string]string{
				packageJSONName: npmRootDoc,
				npmPkgA:         npmNameA,
				npmPkgB:         `{"name": "b", "dependencies": {"a": null}}`,
			},
			want: []string{
				"a packages/a/CHANGELOG.md a packages/a/package.json packages/b/package.json@a",
				npmSummaryB,
			},
		},
		"the lock file comes before the dependents": {
			files: map[string]string{
				packageJSONName:     npmRootDoc,
				"package-lock.json": emptyObject,
				npmPkgA:             npmNameA,
				npmPkgB:             `{"name": "b", "dependencies": {"a": "1"}}`,
			},
			want: []string{
				"a packages/a/CHANGELOG.md a packages/a/package.json package-lock.json@a packages/b/package.json@a",
				"b packages/b/CHANGELOG.md b packages/b/package.json package-lock.json@b",
			},
		},
		"the changelog is set when the file exists too": {
			files: map[string]string{packageJSONName: npmRootDoc, "packages/a/CHANGELOG.md": "# x", npmPkgA: npmNameA},
			want:  []string{npmSummaryA},
		},
		"the root as a workspace": {
			files: map[string]string{
				packageJSONName: `{"name": "root", "workspaces": ["."], "dependencies": {"a": "1"}}`,
				npmPkgA:         npmNameA,
			},
			want: []string{"root CHANGELOG.md root package.json"},
		},
		"the root and its members": {
			files: map[string]string{
				packageJSONName: `{"name": "root", "workspaces": [".", "packages/*"], "dependencies": {"a": "1"}}`,
				npmPkgA:         npmNameA,
			},
			want: []string{
				"root CHANGELOG.md root package.json",
				"a packages/a/CHANGELOG.md a packages/a/package.json package.json@a",
			},
		},
		"same name in two directories": {
			files: map[string]string{
				packageJSONName: npmRootDoc,
				npmPkgA:         npmNameA,
				npmPkgB:         `{"name": "a", "dependencies": {"a": "1"}}`,
			},
			want: []string{
				"a packages/a/CHANGELOG.md a packages/a/package.json packages/b/package.json@a",
				"a packages/b/CHANGELOG.md a packages/b/package.json",
			},
		},
		"deep pattern": {
			files: map[string]string{
				packageJSONName:             `{"workspaces": ["packages/**"]}`,
				npmPkgA:                     npmNameA,
				"packages/a/b/package.json": npmNameB,
			},
			want: []string{npmSummaryA, "b packages/a/b/CHANGELOG.md b packages/a/b/package.json"},
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			pkgs, err := npmWorkspaces(rootOf(tc.files))
			if err != nil {
				t.Fatal(err)
			}
			var got []string
			for _, p := range pkgs {
				got = append(got, summary(p))
			}
			if !slices.Equal(got, tc.want) {
				t.Errorf("packages:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(tc.want, "\n"))
			}
		})
	}
}

func TestNPMWorkspacesErrors(t *testing.T) {
	cases := map[string]struct {
		files    map[string]string
		wantCode string
		wantMsg  string
	}{
		"pattern that does not compile": {
			files:    map[string]string{packageJSONName: `{"workspaces": ["packages/[a"]}`},
			wantCode: codeNPMGlob,
			wantMsg:  "Could not process workspaces glob pattern \"packages/[a\" in package.json: Pattern syntax error near position",
		},
		"a bad pattern after a good one": {
			files:    map[string]string{packageJSONName: `{"workspaces": ["packages/*", "a***"]}`, npmPkgA: npmNameA},
			wantCode: codeNPMGlob,
			wantMsg:  "Could not process workspaces glob pattern \"a***\" in package.json: ",
		},
		"the glob error wins over a missing name": {
			files:    map[string]string{packageJSONName: `{"workspaces": ["packages/*", "["]}`, npmPkgA: emptyObject},
			wantCode: codeNPMGlob,
			wantMsg:  "Could not process workspaces glob pattern \"[\" in package.json: ",
		},
		"parent directory component": {
			files:    map[string]string{packageJSONName: `{"workspaces": ["../a"]}`},
			wantCode: codeNPMGlob,
			wantMsg:  "Could not process workspaces glob pattern \"../a\" in package.json: ",
		},
		"parent directory component in the middle": {
			files:    map[string]string{packageJSONName: `{"workspaces": ["packages/../*"]}`, npmPkgA: npmNameA},
			wantCode: codeNPMGlob,
			wantMsg:  "Could not process workspaces glob pattern \"packages/../*\" in package.json: ",
		},
		"absolute pattern": {
			files:    map[string]string{packageJSONName: `{"workspaces": ["/etc/*"]}`},
			wantCode: codeNPMGlob,
			wantMsg:  "Could not process workspaces glob pattern \"/etc/*\" in package.json: ",
		},
		"missing name": {
			files:    map[string]string{packageJSONName: npmRootDoc, npmPkgA: emptyObject},
			wantCode: codeNPMNoName,
			wantMsg:  errNoNameA,
		},
		"name that is not a string": {
			files:    map[string]string{packageJSONName: npmRootDoc, npmPkgA: `{"name": 1}`},
			wantCode: codeNPMNoName,
			wantMsg:  errNoNameA,
		},
		"a manifest that is not an object has no name": {
			files:    map[string]string{packageJSONName: npmRootDoc, npmPkgA: `null`},
			wantCode: codeNPMNoName,
			wantMsg:  errNoNameA,
		},
		"the first nameless workspace is reported": {
			files:    map[string]string{packageJSONName: npmRootDoc, npmPkgA: npmNameA, npmPkgB: emptyObject, npmPkgC: `[]`},
			wantCode: codeNPMNoName,
			wantMsg:  "Could not find a name in packages/b/package.json",
		},
		"the root as a workspace without a name": {
			files:    map[string]string{packageJSONName: `{"workspaces": ["."]}`},
			wantCode: codeNPMNoName,
			wantMsg:  "Could not find a name in package.json",
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			pkgs, err := npmWorkspaces(rootOf(tc.files))
			var d *diag.Error
			if !errors.As(err, &d) {
				t.Fatalf("error = %v, want a diagnostic", err)
			}
			if pkgs != nil || d.Code() != tc.wantCode || !strings.HasPrefix(d.Error(), tc.wantMsg) {
				t.Errorf("packages = %+v, error = %q (%s), want %q (%s)", pkgs, d.Error(), d.Code(), tc.wantMsg, tc.wantCode)
			}
		})
	}
}

func TestNPMWorkspacesManifestTooLarge(t *testing.T) {
	big := `{"name": "a", "x": "` + strings.Repeat("a", maxManifestSize) + `"}`
	cases := map[string]map[string]string{
		"member": {packageJSONName: npmRootDoc, npmPkgA: big, npmPkgB: npmNameB},
		"root":   {packageJSONName: big, npmPkgA: npmNameA},
	}
	for name, files := range cases {
		t.Run(name, func(t *testing.T) {
			pkgs, err := npmWorkspaces(rootOf(files))
			var d *diag.Error
			if !errors.As(err, &d) || d.Code() != codeNPMTooLarge || pkgs != nil {
				t.Fatalf("packages = %+v, error = %v; want the too large error", pkgs, err)
			}
			if strings.Contains(d.Error(), "aaaa") {
				t.Errorf("message %q carries file content", d.Error())
			}
		})
	}
}

func TestPatternAllowed(t *testing.T) {
	cases := map[string]struct {
		pattern string
		windows bool
		want    bool
	}{
		"plain":                {pattern: "packages/*", want: true},
		"plain on windows":     {pattern: "packages/*", windows: true, want: true},
		"dot slash":            {pattern: "./packages/a", want: true},
		"dots in a name":       {pattern: "a..b/*", want: true},
		"empty pattern":        {pattern: "", want: true},
		"parent":               {pattern: "../a"},
		"inner parent":         {pattern: "a/../b"},
		"only parent":          {pattern: ".."},
		"absolute":             {pattern: "/etc/*"},
		"nul byte":             {pattern: "a\x00b"},
		"backslash on unix":    {pattern: `a\b`, want: true},
		"backslash parent":     {pattern: `..\sibling`, windows: true},
		"backslash on windows": {pattern: `a\b`, windows: true},
		"drive":                {pattern: "C:/Users/*", windows: true},
		"drive alone":          {pattern: "C:", windows: true},
		"drive relative":       {pattern: "C:a", windows: true},
		"unc":                  {pattern: `\\host\share\x`, windows: true},
		"unc with slashes":     {pattern: "//host/share/x", windows: true},
		"alternate stream":     {pattern: "a/b:c", windows: true},
		"colon on unix":        {pattern: "a:b", want: true},
		"device name":          {pattern: "packages/NUL", windows: true},
		"device with ext":      {pattern: "packages/nul.txt", windows: true},
		"com port":             {pattern: "com1/*", windows: true},
		"device on unix":       {pattern: "packages/NUL", want: true},
		"not a device":         {pattern: "console/*", windows: true, want: true},
		"com10 is no device":   {pattern: "com10", windows: true, want: true},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			if got := patternAllowed(tc.pattern, tc.windows); got != tc.want {
				t.Errorf("patternAllowed(%q, %v) = %v, want %v", tc.pattern, tc.windows, got, tc.want)
			}
		})
	}
}

func TestNPMPatternErrorIsQuotedOnce(t *testing.T) {
	_, err := npmWorkspaces(rootOf(map[string]string{packageJSONName: `{"workspaces": ["../a\u001b[31m"]}`}))
	var d *diag.Error
	if !errors.As(err, &d) {
		t.Fatalf("error = %v", err)
	}
	if strings.Contains(d.Error(), "\x1b") || strings.Count(d.Error(), "../a") != 1 {
		t.Errorf("message = %q, want the pattern quoted, once", d.Error())
	}
}

func TestDiscoverUsesNPMWorkspaces(t *testing.T) {
	pkgs, err := discoverPackages(rootOf(map[string]string{packageJSONName: npmRootDoc, npmPkgA: npmNameA}))
	if err != nil || len(pkgs) != 1 || summary(pkgs[0]) != npmSummaryA {
		t.Errorf("packages = %+v, error = %v; want a", pkgs, err)
	}
}
