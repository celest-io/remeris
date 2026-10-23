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
//   - empty components are dropped: `a//b` finds `a/b` and `*//b` finds `x/b` for every directory `x`. In the
//     crate an empty component after a wildcard component never matches, so `*//b` and `**//x` find nothing;
//   - `\` is not a separator on Windows;
//   - the crate drops file names that are not valid UTF-8; here they are matched
//     like any other (invalid bytes read as U+FFFD);
//   - an error while reading a directory does not stop the walk: Glob returns
//     every match it found, with the first such error as a [*ReadError]. The
//     crate yields the errors in line with the matches; Knope either fails on
//     the first one (release assets) or skips it (npm workspaces).
//
// Hostile trees: patterns, names and directory layouts come from untrusted
// repositories, so a walk below `**` is bounded. A symbolic link to a directory
// is followed, once per distinct real directory on the way (as the crate does
// for any layout without a loop), but a directory is never entered again while
// one of its ancestors in the walk is the same directory: `x -> .` is listed as
// a match when the pattern wants it, and its content is not walked. Directories
// are compared by [os.SameFile] when fsys is backed by the operating system
// (for example [os.DirFS]); for any other file system there is no identity, and
// a chain of more than 64 symbolic links is not entered. In both cases a walk
// that reads more than 1,000,000 directories stops with [ErrTooManyDirectories]
// and no matches. A pattern with a NUL byte, or longer than 4096 bytes, is an
// error.
//
// Matching a name against a pattern takes time proportional to the length of
// the pattern times the length of the name, whatever the number of stars. A
// walk expands each (path, component) pair once, so repeated `**` components
// cost a polynomial number of steps; a path that the first release listed more
// than once (`**/a/**/a` can reach `a/a/a` in several ways) is listed once, at
// the place of its first occurrence.
//
// Symbolic links are followed as in the crate, including links that leave the
// root of fsys: with [os.DirFS], `**` can match files outside the directory.
// Callers that must stay inside it have to check the result.
//
// Case-insensitive matching, require_literal_separator and
// require_literal_leading_dot of the crate's glob_with are not offered.
package glob

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
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

const (
	// maxDirs is the number of directories one Glob call may read.
	maxDirs = 1_000_000
	// maxLinkHops is the longest chain of symbolic links to directories entered
	// when directories have no identity.
	maxLinkHops = 64
	// maxPattern is the longest pattern, in bytes.
	maxPattern = 4096
)

// ErrTooManyDirectories is matched, with [errors.Is], by the error of a Glob
// call that read more than a million directories.
var ErrTooManyDirectories = errors.New("glob: too many directories")

type tooManyDirsError struct {
	pattern string
	limit   int
}

func (e *tooManyDirsError) Error() string {
	return fmt.Sprintf("glob: too many directories (more than %d) while expanding %q; use a more specific pattern",
		e.limit, e.pattern)
}

func (e *tooManyDirsError) Is(target error) bool { return target == ErrTooManyDirectories }

type seenKey struct {
	path string
	idx  int
}

// dirNode is a directory of the current walk below `**`, linked to its
// ancestors.
type dirNode struct {
	info   fs.FileInfo // nil if it could not be read
	parent *dirNode
	hops   int // symbolic links to directories on the way
}

// done marks an entry that already matched every component.
const done = -1

type entry struct {
	path string // slash-separated, "." for the root
	dir  bool
	idx  int // index of the component this entry still has to match, or done
	err  error
	node *dirNode // set for a directory reached below **
	loop bool     // a directory that is one of its own ancestors: not walked
}

type walker struct {
	fsys       fs.FS
	pats       []*pattern
	todo       []entry // a stack: the next entry to visit is the last one
	requireDir bool
	maxDirs    int
	dirs       int   // directories read so far
	abort      error // stops the walk
	noLoop     bool  // test hook: the walk of the first release, with no loop check
	seen       map[seenKey]struct{}
	noSeen     bool // test hook: expand an entry as often as it is reached
}

// Glob returns the paths of fsys that match pattern, in the order described in
// the package documentation. A *[PatternError] is returned for an invalid
// pattern, with no results. A walk that reads too many directories returns an
// error matching [ErrTooManyDirectories] and no results.
//
// When the error is a *[ReadError] the matches are complete except for the
// unreadable directories. For any other non-nil error a caller MUST NOT use the
// matches: there are none.
func Glob(fsys fs.FS, pattern string) ([]string, error) {
	return glob(fsys, pattern, &walker{maxDirs: maxDirs})
}

func glob(fsys fs.FS, pattern string, w *walker) ([]string, error) {
	if len(pattern) > maxPattern {
		return nil, fmt.Errorf(
			"glob: pattern too long (%d bytes, the limit is %d): %w",
			len(pattern),
			maxPattern,
			errors.ErrUnsupported,
		)
	}
	if strings.IndexByte(pattern, 0) >= 0 {
		return nil, fmt.Errorf("glob: NUL byte in pattern %q: %w", pattern, errors.ErrUnsupported)
	}
	if _, err := compile(pattern); err != nil {
		return nil, err
	}
	if pattern == "" {
		return nil, nil
	}
	w.fsys, w.requireDir = fsys, strings.HasSuffix(pattern, "/")
	if err := w.parse(pattern); err != nil {
		return nil, err
	}
	if len(w.pats) == 0 {
		return []string{"."}, nil
	}
	out, err := w.run()
	if errors.Is(err, ErrTooManyDirectories) {
		return nil, &tooManyDirsError{pattern: pattern, limit: w.maxDirs}
	}
	return out, err
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
	for len(w.todo) > 0 && w.abort == nil {
		e := w.todo[len(w.todo)-1]
		w.todo = w.todo[:len(w.todo)-1]
		if e.err == nil && !w.noSeen && w.visited(e) {
			continue
		}
		switch {
		case e.err != nil:
			if firstErr == nil {
				firstErr = e.err
			}
		case w.step(e):
			out = append(out, e.path)
		}
	}
	if w.abort != nil {
		return out, w.abort
	}
	return out, firstErr
}

// visited reports whether e, a path with the component it still has to match,
// was already expanded, and notes it.
func (w *walker) visited(e entry) bool {
	if w.seen == nil {
		w.seen = make(map[seenKey]struct{})
	}
	k := seenKey{e.path, e.idx}
	if _, ok := w.seen[k]; ok {
		return true
	}
	w.seen[k] = struct{}{}
	return false
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
			if !e.loop {
				w.fill(next, e)
			}
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
	if w.dirs++; w.dirs > w.maxDirs {
		w.abort = ErrTooManyDirectories
		return
	}
	if pat.recursive && dir.node == nil {
		dir.node = w.enter(&dir, nil, false)
	}
	entries, err := fs.ReadDir(w.fsys, dir.path)
	if err != nil {
		w.todo = append(w.todo, entry{err: &ReadError{Path: dir.path, Err: err}})
		return
	}
	slices.SortFunc(entries, func(a, b fs.DirEntry) int { return strings.Compare(a.Name(), b.Name()) })
	for _, de := range slices.Backward(entries) {
		child := entry{path: join(dir.path, de.Name()), dir: de.IsDir(), idx: idx}
		link := de.Type()&fs.ModeSymlink != 0
		if link {
			child.dir = w.isDir(child.path)
		}
		if child.dir && pat.recursive {
			child.node = w.enter(&child, dir.node, link)
		}
		w.todo = append(w.todo, child)
	}
}

// enter links the directory e below parent, which is nil for the first one of a
// chain. A directory that is one of its ancestors, or that is too many links
// away from the first one when nothing can tell directories apart, gets loop
// set and no node.
func (w *walker) enter(e *entry, parent *dirNode, link bool) *dirNode {
	n := &dirNode{parent: parent}
	if parent != nil && link {
		n.hops = parent.hops + 1
	} else if parent != nil {
		n.hops = parent.hops
	}
	if st, err := fs.Stat(w.fsys, e.path); err == nil {
		n.info = st
	}
	if w.noLoop {
		return n
	}
	identity := n.info != nil && os.SameFile(n.info, n.info) // false unless the file system is os-backed
	for a := parent; identity && a != nil; a = a.parent {
		if a.info != nil && os.SameFile(a.info, n.info) {
			e.loop = true
			return nil
		}
	}
	if !identity && n.hops > maxLinkHops {
		e.loop = true
		return nil
	}
	return n
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
