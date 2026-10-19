package glob

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"testing/fstest"
)

const (
	aaa           = "aaa"
	aaaApple      = "aaa/apple"
	aaaGreenApple = "aaa/green_apple"
	aaaKazusa     = "aaa/kazusa"
	aaaTomatoTxt  = "aaa/tomato/tomato.txt"
	specialsBang  = "bbb/specials/!"
	distB         = "dist/b.txt"
	distSubC      = "dist/sub/c.txt"
	emptyDir      = "empty"
	packagesC     = "packages/c.txt"
	rAnotherA     = "r/another/a.md"
	rCurrentDir   = "r/current_dir.md"
	rThreeC       = "r/three/c.md"
)

const (
	aaaTomotoTxt = "aaa/tomato/tomoto.txt"
	specialsStar = "bbb/specials/*"
	rOneA        = "r/one/a.md"
	rSpelunking  = "r/one/another/deep/spelunking.md"
	rTwoB        = "r/two/b.md"
)

const (
	specialsQuestion = "bbb/specials/?"
	specialsOpen     = "bbb/specials/["
	specialsClose    = "bbb/specials/]"
	rOneAnotherA     = "r/one/another/a.md"
)

// mkTree builds the tree of tests/glob-std.rs in the Rust glob crate. A name
// ending in / is a directory.
func mkTree(t *testing.T) fs.FS {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the fixture uses symbolic links and file names such as * and ?")
	}
	root := t.TempDir()
	for _, name := range []string{
		"aaa/", "aaa/apple/", "aaa/orange/", "aaa/tomato/", aaaTomatoTxt, aaaTomotoTxt,
		"bbb/", "bbb/specials/", specialsBang, specialsStar, specialsQuestion, specialsOpen, specialsClose,
		"ccc/", "xyz/", "xyz/x", "xyz/y", "xyz/z",
		"r/", rCurrentDir, "r/one/", rOneA, "r/one/another/", rOneAnotherA,
		"r/one/another/deep/", rSpelunking, "r/another/", rAnotherA,
		"r/two/", rTwoB, "r/three/", rThreeC,
		"dirsym/",
	} {
		full := filepath.Join(root, filepath.FromSlash(name))
		var err error
		if strings.HasSuffix(name, "/") {
			err = os.Mkdir(full, 0o750)
		} else {
			err = os.WriteFile(full, nil, 0o600)
		}
		if err != nil {
			t.Fatal(err)
		}
	}
	links := map[string]string{
		aaaGreenApple: "apple",                  // a valid symlink
		aaaKazusa:     "setsuna",                // a broken symlink
		"dirsym/link": filepath.Join(root, "r"), // a symlink to a directory
	}
	for link, target := range links {
		if err := os.Symlink(target, filepath.Join(root, filepath.FromSlash(link))); err != nil {
			t.Skipf("cannot create symbolic links: %v", err)
		}
	}
	return os.DirFS(root)
}

// Ported from tests/glob-std.rs in the Rust glob crate.
func TestGlob(t *testing.T) {
	t.Parallel()
	fsys := mkTree(t)
	all := []string{"r/another", "r/one", "r/one/another", "r/one/another/deep", "r/three", "r/two"}
	tests := map[string]struct {
		pattern string
		want    []string
	}{
		"recursive through a symlink": {"dirsym/**/*.md", []string{
			"dirsym/link/another/a.md", "dirsym/link/current_dir.md", "dirsym/link/one/a.md",
			"dirsym/link/one/another/a.md", "dirsym/link/one/another/deep/spelunking.md",
			"dirsym/link/three/c.md", "dirsym/link/two/b.md",
		}},
		"all recursive entities": {"r/**", all},
		"consecutive recursive":  {"r/**/**", all},
		"recursive then star": {"r/**/*", []string{
			"r/another", rAnotherA, rCurrentDir, "r/one", rOneA, "r/one/another",
			rOneAnotherA, "r/one/another/deep", rSpelunking, "r/three",
			rThreeC, "r/two", rTwoB,
		}},
		"recursive then wildcard": {"r/**/*.md", []string{
			rAnotherA, rCurrentDir, rOneA, rOneAnotherA,
			rSpelunking, rThreeC, rTwoB,
		}},
		"recursive then literal":      {"r/one/**/a.md", []string{rOneA, rOneAnotherA}},
		"two recursive then literal":  {"r/one/**/**/a.md", []string{rOneA, rOneAnotherA}},
		"recursive then two literals": {"r/**/another/a.md", []string{rAnotherA, rOneAnotherA}},
		"empty pattern":               {"", nil},
		"dot":                         {".", []string{"."}},
		"literal":                     {aaa, []string{aaa}},
		"literal with trailing slash": {"aaa/", []string{aaa}},
		"missing a":                   {"a", nil},
		"missing aa":                  {"aa", nil},
		"missing aaaa":                {"aaaa", nil},
		"nested literal":              {aaaApple, []string{aaaApple}},
		"below a file or missing":     {"aaa/apple/nope", nil},
		"three chars directories":     {"???/", []string{aaa, "bbb", "ccc", "xyz"}},
		"question in a name":          {"aaa/tomato/tom?to.txt", []string{aaaTomatoTxt, aaaTomotoTxt}},
		"question":                    {"xyz/?", []string{"xyz/x", "xyz/y", "xyz/z"}},
		"star suffix":                 {"a*", []string{aaa}},
		"star around":                 {"*a*", []string{aaa}},
		"star middle":                 {"a*a", []string{aaa}},
		"star after":                  {"aaa*", []string{aaa}},
		"star before":                 {"*aaa", []string{aaa}},
		"star both":                   {"*aaa*", []string{aaa}},
		"many stars":                  {"*a*a*a*", []string{aaa}},
		"star then slash":             {"aaa*/", []string{aaa}},
		"directory content": {
			"aaa/*",
			[]string{aaaApple, aaaGreenApple, aaaKazusa, "aaa/orange", "aaa/tomato"},
		},
		"directory content with a": {
			"aaa/*a*",
			[]string{aaaApple, aaaGreenApple, aaaKazusa, "aaa/orange", "aaa/tomato"},
		},
		"two levels": {"*/*/*.txt", []string{aaaTomatoTxt, aaaTomotoTxt}},
		"sets in names": {
			"*/*/t[aob]m?to[.]t[!y]t",
			[]string{aaaTomatoTxt, aaaTomotoTxt},
		},
		"leading dot slash":           {"./aaa", []string{aaa}},
		"file with trailing slash":    {"aaa/tomato/tomato.txt/", nil},
		"broken symlink":              {aaaKazusa, []string{aaaKazusa}},
		"set":                         {"aa[a]", []string{aaa}},
		"set of three":                {"aa[abc]", []string{aaa}},
		"set in the middle":           {"a[bca]a", []string{aaa}},
		"set misses":                  {"aa[b]", nil},
		"set misses three":            {"aa[xyz]", nil},
		"closing bracket set misses":  {"aa[]]", nil},
		"negated set":                 {"aa[!b]", []string{aaa}},
		"negated set of three":        {"aa[!bcd]", []string{aaa}},
		"negated set in the middle":   {"a[!bcd]a", []string{aaa}},
		"negated set misses":          {"aa[!a]", nil},
		"negated set of three misses": {"aa[!abc]", nil},
		"opening bracket":             {"bbb/specials/[[]", []string{specialsOpen}},
		"bang":                        {specialsBang, []string{specialsBang}},
		"closing bracket":             {"bbb/specials/[]]", []string{specialsClose}},
		"star in brackets":            {"bbb/specials/[*]", []string{specialsStar}},
		"question in brackets":        {"bbb/specials/[?]", []string{specialsQuestion}},
		"star matches the specials too": {
			specialsStar,
			[]string{specialsBang, specialsStar, specialsQuestion, specialsOpen, specialsClose},
		},
		"dot segments are dropped":  {"./aaa/./apple", []string{aaaApple}},
		"backslash is a plain char": {`aaa\*`, nil},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			got, err := Glob(fsys, tc.pattern)
			if err != nil {
				t.Fatalf("Glob(%q): %v", tc.pattern, err)
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("Glob(%q) = %q, want %q", tc.pattern, got, tc.want)
			}
		})
	}
}

func TestGlobMapFS(t *testing.T) {
	t.Parallel()
	fsys := fstest.MapFS{
		"dist/app.tar.gz":         {},
		"dist/.hidden":            {},
		distB:                     {},
		distSubC:                  {},
		"packages/b/package.json": {},
		"packages/a/package.json": {},
		packagesC:                 {},
		"x":                       {},
		emptyDir:                  {Mode: fs.ModeDir},
	}
	tests := map[string]struct {
		pattern string
		want    []string
	}{
		"assets":                   {"dist/*", []string{"dist/.hidden", "dist/app.tar.gz", distB, "dist/sub"}},
		"extension":                {"dist/*.txt", []string{distB}},
		"star never crosses slash": {"*.txt", nil},
		"workspaces":               {"packages/*", []string{"packages/a", "packages/b", packagesC}},
		"workspaces dirs only":     {"packages/*/", []string{"packages/a", "packages/b"}},
		"missing base directory":   {"nope/*", nil},
		"missing recursive base":   {"nope/**/*.txt", nil},
		"empty directory":          {"empty/*", nil},
		"empty directory itself":   {emptyDir, []string{emptyDir}},
		"depth first order":        {"**/*.txt", []string{distB, distSubC, packagesC}},
		"recursive order":          {"**/package.json", []string{"packages/a/package.json", "packages/b/package.json"}},
		"files before dirs":        {"dist/**/*.txt", []string{distB, distSubC}},
		"star on a file":           {"x/*", nil},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			got, err := Glob(fsys, tc.pattern)
			if err != nil {
				t.Fatalf("Glob(%q): %v", tc.pattern, err)
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("Glob(%q) = %q, want %q", tc.pattern, got, tc.want)
			}
		})
	}
}

func TestGlobPatternErrors(t *testing.T) {
	t.Parallel()
	tests := map[string]struct {
		pattern string
		pos     int
	}{
		"double star then letter": {"a/**b", 4},
		"unclosed bracket":        {"abc[def", 3},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			got, err := Glob(fstest.MapFS{"a": {}}, tc.pattern)
			var perr *PatternError
			if !errors.As(err, &perr) || perr.Pos != tc.pos || got != nil {
				t.Errorf("Glob(%q) = %q, %v; want a *PatternError at %d", tc.pattern, got, err, tc.pos)
			}
		})
	}
}

func TestGlobUnsupported(t *testing.T) {
	t.Parallel()
	for _, pattern := range []string{"/", "/*", "//", "../x", "a/../b", ".."} {
		if got, err := Glob(fstest.MapFS{}, pattern); !errors.Is(err, errors.ErrUnsupported) || got != nil {
			t.Errorf("Glob(%q) = %q, %v; want ErrUnsupported", pattern, got, err)
		}
	}
}

// unreadable fails to list the directories whose name is in bad.
type unreadable struct {
	fs.FS
	bad string
}

func (u unreadable) ReadDir(name string) ([]fs.DirEntry, error) {
	if name == u.bad {
		return nil, fs.ErrPermission
	}
	return fs.ReadDir(u.FS, name)
}

// Replaces test_iteration_errors, which needs a directory the user cannot read.
func TestGlobReadError(t *testing.T) {
	t.Parallel()
	fsys := unreadable{FS: fstest.MapFS{"a/x": {}, "b/y": {}, "c/z": {}}, bad: "b"}
	got, err := Glob(fsys, "*/*")
	var rerr *ReadError
	if !errors.As(err, &rerr) || rerr.Path != "b" || !errors.Is(err, fs.ErrPermission) {
		t.Fatalf("error = %v, want a *ReadError for b", err)
	}
	if want := []string{"a/x", "c/z"}; !reflect.DeepEqual(got, want) {
		t.Errorf("matches = %q, want %q", got, want)
	}
	if !strings.Contains(err.Error(), "attempting to read `b` resulted in an error") {
		t.Errorf("message = %q", err)
	}
}
