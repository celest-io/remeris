// Package glob expands Unix shell style patterns against an [fs.FS], with the
// semantics of the glob function of Rust's glob crate (the one Knope uses) and
// its default options: case sensitive, `*` and `?` also match a leading dot.
//
// Pattern syntax, per path component (components are separated by `/`):
//
//	?        any single character
//	*        any sequence of characters (never crosses a `/`)
//	[abc]    any character of the set; [a-z] is a range; [!abc] negates it
//	**       a whole component only: the current directory and all its subdirectories
//
// There is no backslash escape: `\` is an ordinary character. Escape a
// metacharacter with brackets: `[*]`, `[?]`, `[[]`, `[]]`. A `]` right after `[`
// or `[!` belongs to the set. `**` mixed with other characters in one component
// and runs of three or more `*` are errors, as is an unclosed `[`.
//
// Results are slash-separated paths relative to the root of the file system,
// in the crate's order: each directory's entries are visited sorted by name,
// depth first, and a directory is listed before its content. A directory
// matches like a file does (but a pattern ending in `/` matches directories
// only, and the `/` is dropped). A symbolic link is followed to decide whether
// it is a directory. A pattern that matches nothing, or whose base directory
// does not exist, gives no results and no error.
//
// Differences from the crate, all because the input is an [fs.FS] and not the
// process working directory:
//
//   - the pattern is relative: a leading `/` is an error;
//   - `..` components are an error (an fs.FS cannot leave its root);
//   - `.` components are dropped: `./a/./b` finds `a/b`, `.` finds `.`;
//   - `.*` does not match the special entries `.` and `..`;
//   - `\` is not a separator on Windows;
//   - the crate drops file names that are not valid UTF-8; here they are matched
//     like any other (invalid bytes read as U+FFFD);
//   - an error while reading a directory does not stop the walk: Glob returns
//     every match it found, with the first such error as a [*ReadError]. The
//     crate yields the errors in line with the matches; Knope either fails on
//     the first one (release assets) or skips it (npm workspaces).
//
// Case-insensitive matching, require_literal_separator and
// require_literal_leading_dot of the crate's glob_with are not offered.
package glob

import (
	"errors"
	"fmt"
	"io/fs"
	"path"
	"slices"
	"strings"
)

// ReadError reports a directory that could not be read, so its content could
// not be matched.
type ReadError struct {
	Path string
	Err  error
}

func (e *ReadError) Error() string {
	return fmt.Sprintf("attempting to read `%s` resulted in an error: %v", e.Path, e.Err)
}

func (e *ReadError) Unwrap() error { return e.Err }

// done marks an entry that already matched every component.
const done = -1

type entry struct {
	path string // slash-separated, "." for the root
	dir  bool
	idx  int // index of the component this entry still has to match, or done
	err  error
}

type walker struct {
	fsys       fs.FS
	pats       []*pattern
	todo       []entry // a stack: the next entry to visit is the last one
	requireDir bool
}

// Glob returns the paths of fsys that match pattern, in the order described in
// the package documentation. A *[PatternError] is returned for an invalid
// pattern, with no results.
func Glob(fsys fs.FS, pattern string) ([]string, error) {
	if _, err := compile(pattern); err != nil {
		return nil, err
	}
	if pattern == "" {
		return nil, nil
	}
	w := &walker{fsys: fsys, requireDir: strings.HasSuffix(pattern, "/")}
	if err := w.parse(pattern); err != nil {
		return nil, err
	}
	if len(w.pats) == 0 {
		return []string{"."}, nil
	}
	return w.run()
}

func (w *walker) parse(pattern string) error {
	if strings.HasPrefix(pattern, "/") {
		return fmt.Errorf("glob: absolute pattern %q: %w", pattern, errors.ErrUnsupported)
	}
	for part := range strings.SplitSeq(pattern, "/") {
		switch part {
		case "", ".":
		case "..":
			return fmt.Errorf("glob: `..` in pattern %q: %w", pattern, errors.ErrUnsupported)
		default:
			p, err := compile(part)
			if err != nil {
				return err
			}
			w.pats = append(w.pats, p)
		}
	}
	return nil
}

// run is the crate's Paths iterator, drained.
func (w *walker) run() ([]string, error) {
	var out []string
	var firstErr error
	w.fill(0, entry{path: ".", dir: true})
	for len(w.todo) > 0 {
		e := w.todo[len(w.todo)-1]
		w.todo = w.todo[:len(w.todo)-1]
		switch {
		case e.err != nil:
			if firstErr == nil {
				firstErr = e.err
			}
		case w.step(e):
			out = append(out, e.path)
		}
	}
	return out, firstErr
}

// step advances one entry. It reports whether the entry is a result and
// queues the entries below it.
func (w *walker) step(e entry) bool {
	last := len(w.pats) - 1
	idx := e.idx
	if idx == done {
		return !w.requireDir || e.dir
	}
	if w.pats[idx].recursive {
		next := idx
		for next < last && w.pats[next+1].recursive {
			next++
		}
		switch {
		case e.dir:
			w.fill(next, e)
			if next == last {
				return true // the pattern ends in **: the directory itself is a result
			}
		case next == last:
			return false
		}
		idx = next + 1
	}
	if !w.pats[idx].matches(path.Base(e.path)) {
		return false
	}
	if idx == last {
		return !w.requireDir || e.dir
	}
	w.fill(idx+1, e)
	return false
}

// fill queues what lies below dir that may match pats[idx], like the crate's
// fill_todo: a component without metacharacters is looked up directly, any
// other one lists the directory.
func (w *walker) fill(idx int, dir entry) {
	pat := w.pats[idx]
	if !pat.meta {
		w.add(idx, join(dir.path, pat.src))
		return
	}
	if !dir.dir {
		return
	}
	entries, err := fs.ReadDir(w.fsys, dir.path)
	if err != nil {
		w.todo = append(w.todo, entry{err: &ReadError{Path: dir.path, Err: err}})
		return
	}
	slices.SortFunc(entries, func(a, b fs.DirEntry) int { return strings.Compare(a.Name(), b.Name()) })
	for _, de := range slices.Backward(entries) {
		child := entry{path: join(dir.path, de.Name()), dir: de.IsDir(), idx: idx}
		if de.Type()&fs.ModeSymlink != 0 {
			child.dir = w.isDir(child.path)
		}
		w.todo = append(w.todo, child)
	}
}

// add queues a literal component, or goes on with the next one, if it exists.
// A dangling symbolic link exists.
func (w *walker) add(idx int, p string) {
	if !fs.ValidPath(p) {
		return
	}
	if _, err := fs.Lstat(w.fsys, p); err != nil {
		return
	}
	e := entry{path: p, dir: w.isDir(p)}
	if idx+1 == len(w.pats) {
		e.idx = done
		w.todo = append(w.todo, e)
		return
	}
	w.fill(idx+1, e)
}

// isDir follows symbolic links.
func (w *walker) isDir(p string) bool {
	st, err := fs.Stat(w.fsys, p)
	return err == nil && st.IsDir()
}

func join(dir, name string) string {
	if dir == "." {
		return name
	}
	return dir + "/" + name
}
