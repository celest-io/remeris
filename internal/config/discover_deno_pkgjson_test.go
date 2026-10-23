package config

import (
	"strings"
	"testing"
)

const (
	pjWorkspaceAB = `{"workspace":["a","b"]}`
	pjTargetDeno  = "deno"
	pjTargetPkg   = "pkg"
	pjTargetBoth  = "both"
	pjDeps        = "dependencies"
	pjDevDeps     = "devDependencies"
	pjEdgeB       = " b/deno.json@@s/a"
	pjFileA       = `"x":"file:../a"`
	pjWsStar      = `"@s/a":"workspace:*"`
	pjCaret       = `"@s/a":"^1.0.0"`
	pjScopedB     = "@s/b"
	pjBVersion    = `"name":"@s/b","version":"2.0.0"`
)

// pjFiles builds a workspace of a (the target, a deno.json, a package.json or both) and b (deno.json plus a
// package.json holding the dependency map).
func pjFiles(target, field, deps string) map[string]string {
	files := map[string]string{
		dtRoot:  pjWorkspaceAB,
		dtBDeno: `{` + pjBVersion + `}`,
		dtBPkg:  `{"` + field + `":{` + deps + `}}`,
	}
	if target != pjTargetPkg {
		files[dtADeno] = dtA
	}
	if target != pjTargetDeno {
		files[memberPkgJSON] = dtA
	}
	return files
}

func pjSummaryOf(t *testing.T, files map[string]string, member string) string {
	t.Helper()
	pkgs, err := denoWorkspace(rootOf(files))
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range pkgs {
		if p.Name.String() == member {
			return denoSummary(p)
		}
	}
	t.Fatalf("no package %s in %d packages", member, len(pkgs))
	return ""
}

func TestDenoPackageJSONEdges(t *testing.T) {
	cases := map[string]struct {
		target string
		field  string
		deps   string
		want   bool
	}{
		"file to the directory":        {pjTargetDeno, pjDeps, pjFileA, true},
		"file to the directory, slash": {pjTargetDeno, pjDeps, `"x":"file:../a/"`, true},
		"file to the directory of a package.json only member": {
			pjTargetPkg, pjDeps, pjFileA, true,
		},
		"file to package.json of a package.json config": {
			pjTargetPkg, pjDeps, `"x":"file:../a/package.json"`, true,
		},
		"file to deno.json of a deno.json config": {
			pjTargetDeno, pjDeps, `"x":"file:../a/deno.json"`, true,
		},
		"file to package.json when the config is deno.json": {
			pjTargetBoth, pjDeps, `"x":"file:../a/package.json"`, false,
		},
		"file to deno.jsonc is not deno.json": {
			pjTargetDeno, pjDeps, `"x":"file:../a/deno.jsonc"`, false,
		},
		"file with dot segments":       {pjTargetDeno, pjDeps, `"x":"file:./sub/../../a"`, true},
		"file with empty segments":     {pjTargetDeno, pjDeps, `"x":"file:..//a/./"`, true},
		"file to another directory":    {pjTargetDeno, pjDeps, `"x":"file:../c"`, false},
		"file to the dependent itself": {pjTargetDeno, pjDeps, `"x":"file:."`, false},
		"file to a child of the target": {
			pjTargetDeno, pjDeps, `"x":"file:../a/src"`, false,
		},
		"absolute file path": {pjTargetDeno, pjDeps, `"x":"file:/a"`, false},
		"file path outside the root": {
			pjTargetDeno, pjDeps, `"x":"file:../../a"`, false,
		},
		"file path far outside the root": {
			pjTargetDeno, pjDeps, `"x":"file:../../../../../a"`, false,
		},
		"file in devDependencies":          {pjTargetDeno, pjDevDeps, pjFileA, true},
		"workspace star":                   {pjTargetDeno, pjDeps, pjWsStar, true},
		"workspace caret":                  {pjTargetDeno, pjDeps, `"@s/a":"workspace:^"`, true},
		"workspace tilde":                  {pjTargetPkg, pjDeps, `"@s/a":"workspace:~"`, true},
		"workspace version":                {pjTargetDeno, pjDeps, `"@s/a":"workspace:^1.2.0"`, true},
		"workspace with another alias":     {pjTargetDeno, pjDeps, `"other":"workspace:*"`, false},
		"workspace aliasing the target":    {pjTargetDeno, pjDeps, `"other":"workspace:@s/a@*"`, false},
		"workspace name at star":           {pjTargetDeno, pjDeps, `"@s/a":"workspace:b@*"`, false},
		"workspace empty requirement":      {pjTargetDeno, pjDeps, `"@s/a":"workspace:"`, true},
		"workspace in devDependencies":     {pjTargetDeno, pjDevDeps, pjWsStar, true},
		"workspace alias without version":  {pjTargetDeno, pjDeps, `"@s/a":"workspace:bad!"`, false},
		"jsr alias of an npm package":      {pjTargetDeno, pjDeps, `"x":"npm:@jsr/s__a@1"`, true},
		"jsr alias without version":        {pjTargetBoth, pjDeps, `"x":"npm:@jsr/s__a"`, true},
		"jsr alias of a package.json only": {pjTargetPkg, pjDeps, `"x":"npm:@jsr/s__a@1"`, false},
		"jsr alias with another package":   {pjTargetDeno, pjDeps, `"x":"npm:@jsr/s__other@1"`, false},
		"jsr alias as a key":               {pjTargetDeno, pjDeps, `"@jsr/s__a":"^1.0.0"`, true},
		"jsr alias as a key, package.json": {pjTargetPkg, pjDeps, `"@jsr/s__a":"^1.0.0"`, false},
		"jsr scheme":                       {pjTargetDeno, pjDeps, `"x":"jsr:@s/a@^1"`, true},
		"jsr scheme without version":       {pjTargetDeno, pjDeps, `"x":"jsr:@s/a"`, true},
		"jsr scheme version only":          {pjTargetDeno, pjDeps, `"@s/a":"jsr:^1.0.0"`, true},
		"jsr scheme without a scope":       {pjTargetDeno, pjDeps, `"x":"jsr:a@1"`, false},
		"jsr scheme of a key without scope": {
			pjTargetDeno, pjDeps, `"a":"jsr:^1"`, false,
		},
		"plain name, target package.json": {pjTargetPkg, pjDeps, pjCaret, true},
		"plain name, target with both":    {pjTargetBoth, pjDeps, `"@s/a":"1.x"`, true},
		"plain name, no target package.json": {
			pjTargetDeno, pjDeps, pjCaret, false,
		},
		"plain name of another package": {pjTargetPkg, pjDeps, `"@s/aa":"^1.0.0"`, false},
		"plain name tag":                {pjTargetPkg, pjDeps, `"@s/a":"latest"`, true},
		"plain name empty requirement":  {pjTargetPkg, pjDeps, `"@s/a":""`, true},
		"plain name number":             {pjTargetPkg, pjDeps, `"@s/a":1`, true},
		"plain name with alternatives":  {pjTargetPkg, pjDeps, `"@s/a":"^1 || bad!"`, true},
		"plain name range":              {pjTargetPkg, pjDeps, `"@s/a":">=1.0.0 <2.0.0"`, true},
		"plain name hyphen range":       {pjTargetPkg, pjDeps, `"@s/a":"1.0.0 - 2.0.0"`, true},
		"plain name in devDependencies": {pjTargetPkg, pjDevDeps, pjCaret, true},
		"aliased npm dependency":        {pjTargetPkg, pjDeps, `"x":"npm:@s/a@1"`, true},
		"aliased npm without version":   {pjTargetPkg, pjDeps, `"x":"npm:@s/a"`, true},
		"aliased npm, target deno only": {pjTargetDeno, pjDeps, `"x":"npm:@s/a@1"`, false},
		"aliased npm key is the target": {pjTargetPkg, pjDeps, `"@s/a":"npm:other@1"`, false},
		"npm alias with a bad version":  {pjTargetPkg, pjDeps, `"x":"npm:@s/a@not valid!"`, false},
		"unscoped npm alias":            {pjTargetPkg, pjDeps, `"x":"npm:left-pad@1"`, false},
		"peerDependencies are ignored":  {pjTargetPkg, "peerDependencies", pjCaret, false},
		"optionalDependencies ignored":  {pjTargetPkg, "optionalDependencies", pjCaret, false},
		"peer workspace is ignored":     {pjTargetDeno, "peerDependencies", pjWsStar, false},
		"git url does not parse":        {pjTargetPkg, pjDeps, `"@s/a":"git+https://example.com/a.git"`, false},
		"github scheme does not parse":  {pjTargetPkg, pjDeps, `"@s/a":"github:o/r"`, false},
		"http scheme does not parse":    {pjTargetPkg, pjDeps, `"@s/a":"https://example.com/a.tgz"`, false},
		"invalid requirement skipped":   {pjTargetPkg, pjDeps, `"@s/a":"not a range!"`, false},
		"null value is skipped":         {pjTargetPkg, pjDeps, `"@s/a":null`, false},
		"array value is skipped":        {pjTargetPkg, pjDeps, `"@s/a":["^1"]`, false},
		"object value is skipped":       {pjTargetPkg, pjDeps, `"@s/a":{}`, false},
		"bad entry before a good one":   {pjTargetPkg, pjDeps, `"a":"git:x","@s/a":"^1"`, true},
		"dependencies is not an object": {pjTargetPkg, pjDeps, ``, false},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			files := pjFiles(tc.target, tc.field, tc.deps)
			if name == "dependencies is not an object" {
				files[dtBPkg] = `{"dependencies":["@s/a"]}`
			}
			got := strings.Contains(pjSummaryOf(t, files, "@s/a"), pjEdgeB)
			if got != tc.want {
				t.Errorf("edge = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestDenoPackageJSONEdgeShapes(t *testing.T) {
	const (
		depOnA   = `{"dependencies":{"@s/a":"workspace:*"}}`
		memberC  = `{"name":"@s/c","version":"3.0.0"}`
		memberCP = "c/package.json"
		memberCD = "c/deno.json"
		wsABC    = `{"workspace":["c","b","a"]}`
	)
	cases := map[string]struct {
		files map[string]string
		want  string
	}{
		"self reference": {
			files: map[string]string{
				dtRoot: dtWsA, dtADeno: dtA, memberPkgJSON: depOnA,
			},
			want: dtMemA,
		},
		"dependents are listed in directory order": {
			files: map[string]string{
				dtRoot: wsABC, dtADeno: dtA, dtBDeno: dtB, dtBPkg: depOnA, memberCD: memberC, memberCP: depOnA,
			},
			want: dtEdgeA + " c/deno.json@@s/a",
		},
		"dependent that is a package.json only member": {
			files: map[string]string{
				dtRoot: pjWorkspaceAB, dtADeno: dtA,
				dtBPkg: `{` + pjBVersion + `,"dependencies":{"@s/a":"workspace:*"}}`,
			},
			want: dtMemA + " b/package.json@@s/a",
		},
		"dependent with a deno.json lists the deno.json": {
			files: map[string]string{
				dtRoot: pjWorkspaceAB, dtADeno: dtA, dtBDeno: dtB, dtBPkg: depOnA,
			},
			want: dtEdgeA,
		},
		"the same dependent through imports and package.json is listed once": {
			files: map[string]string{
				dtRoot: pjWorkspaceAB, dtADeno: dtA, dtBPkg: depOnA,
				dtBDeno: `{` + pjBVersion + `,"imports":{"a":"jsr:@s/a"}}`,
			},
			want: dtEdgeA,
		},
		"dependent package.json without a deno.json or version is skipped": {
			files: map[string]string{
				dtRoot:   `{"workspace":["a","c"]}`,
				dtADeno:  dtA,
				memberCP: `{"name":"@s/c","dependencies":{"@s/a":"workspace:*"}}`,
			},
			want: dtMemA,
		},
		"dependent on a root package": {
			files: map[string]string{
				dtRoot:  `{"name":"@s/r","version":"1.0.0","workspace":["b"]}`,
				dtBDeno: dtB,
				dtBPkg:  `{"dependencies":{"@s/r":"workspace:*"}}`,
			},
			want: "@s/r CHANGELOG.md @s/r: deno.json b/deno.json@@s/r",
		},
		"file to the root directory": {
			files: map[string]string{
				dtRoot:  `{"name":"@s/r","version":"1.0.0","workspace":["b"]}`,
				dtBDeno: dtB,
				dtBPkg:  `{"dependencies":{"r":"file:.."}}`,
			},
			want: "@s/r CHANGELOG.md @s/r: deno.json b/deno.json@@s/r",
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			member := "@s/a"
			if name == "dependent on a root package" || name == "file to the root directory" {
				member = "@s/r"
			}
			got := pjSummaryOf(t, tc.files, member)
			if got != tc.want {
				t.Errorf("summary =\n%s\nwant\n%s", got, tc.want)
			}
		})
	}
}

func TestDenoPackageJSONDependentNeedsValidManifest(t *testing.T) {
	files := pjFiles(pjTargetDeno, pjDeps, pjWsStar)
	files[dtBPkg] = `{"dependencies":`
	_, err := denoWorkspace(rootOf(files))
	if err == nil || !strings.Contains(err.Error(), `Could not parse "b/package.json"`) {
		t.Errorf("err = %v, want a parse failure of b/package.json", err)
	}
}

func TestLogicalJoin(t *testing.T) {
	cases := map[string]struct {
		dir, path string
		want      string
		ok        bool
	}{
		"sibling":              {"b", "../a", "a", true},
		"nested":               {"x/b", "../../a/c", "a/c", true},
		"to the root":          {"b", parentDirNames, rootDir, true},
		"from the root":        {rootDir, "a/./b", "a/b", true},
		"dot only":             {"b", ".", "b", true},
		"empty path":           {"b", "", "b", true},
		"outside":              {"b", "../..", "", false},
		"outside from root":    {rootDir, parentDirNames, "", false},
		"absolute path":        {"b", "/a", "", false},
		"back in after a pop":  {"b", "../b/c", "b/c", true},
		"empty segment pieces": {"b", "..//a", "a", true},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			got, ok := logicalJoin(tc.dir, tc.path)
			if got != tc.want || ok != tc.ok {
				t.Errorf("logicalJoin(%q, %q) = %q, %v; want %q, %v", tc.dir, tc.path, got, ok, tc.want, tc.ok)
			}
		})
	}
}

func TestValidNPMVersionReq(t *testing.T) {
	valid := []string{
		"", " ", "*", "x", "1", "1.2", "1.2.3", "1.x", "1.2.x", "^1.2.3", "~1.2.3", "~>1.2", "^ 1.2", ">=1.2.3", ">= 1.2.3",
		"<2", "<=2.0.0", "=1.0.0", ">1 <3", ">=1,<3", ">=1 && <3", "1.2.3 - 2.0.0", "v1.2.3", "1.2.3-beta.1", "1.2.3+build",
		"latest", "next", "beta-1", "some_tag", "^1 || ^2", "^1 || bad!", "  ^1  ", "1.2.3-rc.1+sha.5",
	}
	invalid := []string{
		"bad!", "a b", "b@*", "^", ">=", "@s/a", "not a range!", "^^1",
	}
	for _, text := range valid {
		if !validNPMVersionReq(text) {
			t.Errorf("validNPMVersionReq(%q) = false, want true", text)
		}
	}
	for _, text := range invalid {
		if validNPMVersionReq(text) {
			t.Errorf("validNPMVersionReq(%q) = true, want false", text)
		}
	}
}

func TestParsePackageJSONDep(t *testing.T) {
	cases := map[string]struct {
		key, value string
		want       pkgJSONDep
		ok         bool
	}{
		"plain requirement": {"k", "^1", pkgJSONDep{pkgDepReq, "k"}, true},
		"file":              {"k", "file:../x", pkgJSONDep{pkgDepFile, "../x"}, true},
		"workspace":         {"k", "workspace:*", pkgJSONDep{kind: pkgDepWorkspace}, true},
		"npm unscoped":      {"k", "npm:b@1", pkgJSONDep{pkgDepReq, "b"}, true},
		"npm scoped":        {"k", "npm:@s/b@1", pkgJSONDep{pkgDepReq, pjScopedB}, true},
		"npm scoped bare":   {"k", "npm:@s/b", pkgJSONDep{pkgDepReq, pjScopedB}, true},
		"npm path":          {"k", "npm:b/sub@1", pkgJSONDep{pkgDepReq, "b/sub"}, true},
		"jsr scoped":        {"k", "jsr:@s/b@1", pkgJSONDep{pkgDepReq, "@jsr/s__b"}, true},
		"jsr key scoped":    {"@s/b", "jsr:^1", pkgJSONDep{pkgDepReq, "@jsr/s__b"}, true},
		"jsr unscoped":      {"k", "jsr:b@1", pkgJSONDep{}, false},
		"unknown scheme":    {"k", "git:x", pkgJSONDep{}, false},
		"bad workspace":     {"k", "workspace:b@*", pkgJSONDep{}, false},
		"bad plain":         {"k", "no good!", pkgJSONDep{}, false},
		"bad npm version":   {"k", "npm:b@no good!", pkgJSONDep{}, false},
		"jsr bad version":   {"k", "jsr:@s/b@no good!", pkgJSONDep{}, false},
		"jsr only the at":   {"k", "jsr:@", pkgJSONDep{}, false},
		"jsr scope no pkg":  {"k", "jsr:@s", pkgJSONDep{}, false},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			got, ok := parsePackageJSONDep(tc.key, tc.value)
			if ok != tc.ok || got != tc.want {
				t.Errorf("parsePackageJSONDep(%q, %q) = %+v, %v; want %+v, %v", tc.key, tc.value, got, ok, tc.want, tc.ok)
			}
		})
	}
}
