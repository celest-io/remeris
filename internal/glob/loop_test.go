package glob

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"testing/fstest"
	"time"
)

const (
	loopDeadline = 10 * time.Second
	pkgJSON      = "package.json"
	recursive    = "**"
	recursiveDir = recursive + "/"
	globAll      = recursiveDir + pkgJSON
	aliasName    = "alias"
	realDir      = "real"
	aPkg         = "a/" + pkgJSON
	realPkg      = realDir + "/" + pkgJSON
	dotDot       = ".."
)

type globResult struct {
	paths []string
	err   error
}

// runBounded runs Glob in a goroutine and fails the test if it does not finish.
func runBounded(t *testing.T, fsys fs.FS, pattern string, w *walker) globResult {
	t.Helper()
	ch := make(chan globResult, 1)
	go func() {
		paths, err := glob(fsys, pattern, w)
		ch <- globResult{paths, err}
	}()
	select {
	case r := <-ch:
		return r
	case <-time.After(loopDeadline):
		t.Fatalf("glob %q did not finish within %v", pattern, loopDeadline)
		return globResult{}
	}
}

// linkTree creates files (a name ending in / is a directory) and then the symbolic
// links (name to target) in a temporary directory.
func linkTree(t *testing.T, files []string, links map[string]string) fs.FS {
	t.Helper()
	root := t.TempDir()
	for _, name := range files {
		full := filepath.Join(root, filepath.FromSlash(name))
		if strings.HasSuffix(name, "/") {
			if err := os.MkdirAll(full, 0o750); err != nil {
				t.Fatal(err)
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(full), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte("{}"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	for name, target := range links {
		full := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(full), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(target, full); err != nil {
			t.Skipf("cannot create symbolic links: %v", err)
		}
	}
	return os.DirFS(root)
}

func TestGlobSymlinkLoops(t *testing.T) {
	t.Parallel()
	tests := map[string]struct {
		files   []string
		links   map[string]string
		pattern string
		want    []string
	}{
		"links to the root": {
			files:   []string{pkgJSON, aPkg},
			links:   map[string]string{"x": ".", "y": "."},
			pattern: globAll,
			want:    []string{aPkg, pkgJSON},
		},
		"many links to the root": {
			files:   []string{pkgJSON},
			links:   map[string]string{"a": ".", "b": ".", "c": ".", "d": ".", "e": ".", "f": ".", "g": ".", "h": "."},
			pattern: globAll,
			want:    []string{pkgJSON},
		},
		"mutual links": {
			files:   []string{pkgJSON, aPkg},
			links:   map[string]string{"a/b": dotDot, "c/d": "../a", "a/e": "../c"},
			pattern: globAll,
			want:    []string{aPkg, "c/d/" + pkgJSON, pkgJSON},
		},
		"link to a parent": {
			files:   []string{pkgJSON, "p/q/" + pkgJSON},
			links:   map[string]string{"p/q/up": "../../p", "p/up2": dotDot},
			pattern: globAll,
			want:    []string{"p/q/" + pkgJSON, pkgJSON},
		},
		"loop below a literal start": {
			files:   []string{"s/" + pkgJSON},
			links:   map[string]string{"s/x": ".", "s/y": "."},
			pattern: "s/" + globAll,
			want:    []string{"s/" + pkgJSON},
		},
		"link to a sibling is followed": {
			files:   []string{realPkg, "other/" + pkgJSON},
			links:   map[string]string{aliasName: realDir},
			pattern: globAll,
			want:    []string{"alias/" + pkgJSON, "other/" + pkgJSON, realPkg},
		},
		"link to a file and a sibling link to the same directory": {
			files:   []string{realPkg},
			links:   map[string]string{aliasName: realDir, "alias2": realDir, "f": realPkg},
			pattern: globAll,
			want:    []string{"alias/" + pkgJSON, "alias2/" + pkgJSON, realPkg},
		},
		"link to a directory listed by the pattern itself": {
			files:   []string{"d/"},
			links:   map[string]string{"d/self": "."},
			pattern: recursiveDir,
			want:    []string{"d", "d/self"},
		},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			fsys := linkTree(t, tc.files, tc.links)
			got := runBounded(t, fsys, tc.pattern, &walker{maxDirs: maxDirs})
			if got.err != nil {
				t.Fatalf("error: %v", got.err)
			}
			if !reflect.DeepEqual(got.paths, tc.want) {
				t.Errorf("got %q, want %q", got.paths, tc.want)
			}
		})
	}
}

// Without a loop the walk gives what the walk without the check gives.
func TestGlobSymlinkLoopFreeSameAsUnchecked(t *testing.T) {
	t.Parallel()
	fsys := linkTree(
		t,
		[]string{realPkg, "real/sub/" + pkgJSON, "other/" + pkgJSON, pkgJSON},
		map[string]string{
			aliasName:     "real",
			"other/inner": "../real/sub",
			"file":        realPkg,
			"dangling":    "nowhere",
			"z":           "other",
		},
	)
	for _, pattern := range []string{globAll, recursive, "**/*", "**/sub", recursiveDir, "*/**/" + pkgJSON, "**/inner/*",
		"**/sub/**/" + pkgJSON, "**/real/**/" + pkgJSON, "**/**/" + pkgJSON, "*/**/*/**/" + pkgJSON} {
		checked := runBounded(t, fsys, pattern, &walker{maxDirs: maxDirs})
		unchecked := runBounded(t, fsys, pattern, &walker{maxDirs: maxDirs, noLoop: true, noSeen: true})
		if checked.err != nil || unchecked.err != nil {
			t.Fatalf("%q: errors %v, %v", pattern, checked.err, unchecked.err)
		}
		if !reflect.DeepEqual(checked.paths, firstOccurrences(unchecked.paths)) {
			t.Errorf("%q: checked %q, unchecked %q", pattern, checked.paths, unchecked.paths)
		}
		if len(checked.paths) == 0 {
			t.Errorf("%q: no result", pattern)
		}
	}
}

// opaqueFS hides what identifies a directory: its file information is not the
// operating system's, so [os.SameFile] cannot compare it.
type opaqueFS struct{ fs.FS }

type opaqueInfo struct{ fs.FileInfo }

func (o opaqueFS) Stat(name string) (fs.FileInfo, error) {
	st, err := fs.Stat(o.FS, name)
	if err != nil {
		return nil, err
	}
	return opaqueInfo{st}, nil
}

func TestGlobSymlinkLoopsWithoutIdentity(t *testing.T) {
	t.Parallel()
	t.Run("one link stops at the hop limit or the operating system's", func(t *testing.T) {
		t.Parallel()
		fsys := opaqueFS{linkTree(t, []string{pkgJSON}, map[string]string{"x": "."})}
		got := runBounded(t, fsys, globAll, &walker{maxDirs: maxDirs})
		if got.err != nil {
			t.Fatal(got.err)
		}
		if n := len(got.paths); n < 2 || n > maxLinkHops+2 || got.paths[0] != pkgJSON {
			t.Errorf("got %d results, first %q", n, got.paths[0])
		}
	})
	t.Run("two links reach the directory limit", func(t *testing.T) {
		t.Parallel()
		fsys := opaqueFS{linkTree(t, []string{pkgJSON}, map[string]string{"x": ".", "y": "."})}
		got := runBounded(t, fsys, globAll, &walker{maxDirs: 3000})
		if !errors.Is(got.err, ErrTooManyDirectories) {
			t.Fatalf("error = %v, want ErrTooManyDirectories", got.err)
		}
		if got.paths != nil {
			t.Errorf("got partial matches %q with the error", got.paths)
		}
	})
}

func TestGlobTooManyDirectories(t *testing.T) {
	t.Parallel()
	fsys := linkTree(t, []string{"a/", "b/", "c/", "d/"}, nil)
	got := runBounded(t, fsys, "*/*", &walker{maxDirs: 3})
	if !errors.Is(got.err, ErrTooManyDirectories) {
		t.Errorf("error = %v, want ErrTooManyDirectories", got.err)
	}
	want := `glob: too many directories (more than 3) while expanding "*/*"; use a more specific pattern`
	if got.err == nil || got.err.Error() != want {
		t.Errorf("message = %v, want %q", got.err, want)
	}
	got = runBounded(t, fsys, "*/*", &walker{maxDirs: 5})
	if got.err != nil {
		t.Errorf("error = %v, want none", got.err)
	}
}

func TestGlobRejectsNUL(t *testing.T) {
	t.Parallel()
	for name, pattern := range map[string]string{"alone": "\x00", "inside": "a/b\x00c/*", "after a star": "*\x00"} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			paths, err := Glob(fstest.MapFS{}, pattern)
			if !errors.Is(err, errors.ErrUnsupported) || paths != nil {
				t.Errorf("got %q, %v; want an ErrUnsupported error", paths, err)
			}
		})
	}
}

func firstOccurrences(in []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, p := range in {
		if !seen[p] {
			seen[p] = true
			out = append(out, p)
		}
	}
	return out
}

func TestGlobRepeatedRecursiveWildcards(t *testing.T) {
	t.Parallel()
	dir := strings.Repeat("a/", 22) + pkgJSON
	fsys := linkTree(t, []string{dir}, nil)
	tests := map[string]struct {
		pattern string
		want    int
	}{
		"eight a then x": {strings.Repeat("**/a/", 8) + "x", 0},
		"eight a then a": {strings.Repeat("**/a/", 8) + "a", 1},
		"then package":   {strings.Repeat("**/a/", 8) + "**/" + pkgJSON, 1},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			got := runBounded(t, fsys, tc.pattern, &walker{maxDirs: maxDirs})
			if got.err != nil {
				t.Fatal(got.err)
			}
			if len(got.paths) < tc.want || (tc.want == 0 && len(got.paths) != 0) {
				t.Errorf("got %d results, want %d: %q", len(got.paths), tc.want, got.paths)
			}
			if len(got.paths) != len(firstOccurrences(got.paths)) {
				t.Errorf("duplicates in %q", got.paths)
			}
		})
	}
}

func TestGlobRejectsLongPattern(t *testing.T) {
	t.Parallel()
	ok := strings.Repeat("a", maxPattern)
	if _, err := Glob(fstest.MapFS{}, ok); err != nil {
		t.Errorf("pattern of %d bytes: %v", len(ok), err)
	}
	paths, err := Glob(fstest.MapFS{}, ok+"a")
	if !errors.Is(err, errors.ErrUnsupported) || paths != nil || !strings.Contains(err.Error(), "pattern too long") {
		t.Errorf("got %q, %v; want a pattern too long error", paths, err)
	}
}
