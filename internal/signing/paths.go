package signing

import (
	"io/fs"
	"slices"
	"strings"
	"testing/fstest"

	"github.com/celest-io/remeris/internal/diag"
	"github.com/celest-io/remeris/internal/git"
	"github.com/celest-io/remeris/internal/glob"
)

// Filter returns the changes selected by include and exclude: with no include pattern every change, else those that
// match one; then those that match an exclude pattern are dropped. A pattern that names a directory selects what is
// under it. Patterns are internal/glob patterns (Rust's glob crate: * and ? stay inside a directory, ** crosses them),
// matched against the paths of the changes, which are relative to the root of the working tree.
func Filter(changes []git.Change, include, exclude []string) ([]git.Change, error) {
	tree := make(fstest.MapFS, len(changes))
	for _, ch := range changes {
		tree[ch.Path] = &fstest.MapFile{}
	}
	selected := func(patterns []string) (map[string]bool, error) {
		out := map[string]bool{}
		for _, pattern := range patterns {
			matches, err := expand(tree, pattern)
			if err != nil {
				return nil, diag.Newf("The pattern %q is not valid: %w", pattern, err).
					WithCode(codeOptions).
					WithHelp("Patterns are globs such as CHANGELOG.md, docs/** or mise*.lock.")
			}
			for _, m := range matches {
				if err := fs.WalkDir(tree, m, func(p string, d fs.DirEntry, err error) error {
					if err == nil && !d.IsDir() {
						out[p] = true
					}
					return err
				}); err != nil {
					return nil, err
				}
			}
		}
		return out, nil
	}

	in, err := selected(include)
	if err != nil {
		return nil, err
	}
	out, err := selected(exclude)
	if err != nil {
		return nil, err
	}
	var kept []git.Change
	for _, ch := range changes {
		if (len(include) == 0 || in[ch.Path]) && !out[ch.Path] {
			kept = append(kept, ch)
		}
	}
	slices.SortFunc(kept, func(a, b git.Change) int { return strings.Compare(a.Path, b.Path) })
	return kept, nil
}

// expand is glob.Glob, except that a pattern that ends in "/**" also selects the directory before it. The crate's "**"
// matches directories only, but "docs/**" is read as "everything under docs", which the directory selects.
func expand(tree fs.FS, pattern string) ([]string, error) {
	matches, err := glob.Glob(tree, pattern)
	if err != nil {
		return nil, err
	}
	if dir, ok := strings.CutSuffix(pattern, "/**"); ok && dir != "" {
		more, err := glob.Glob(tree, dir)
		if err != nil {
			return nil, err
		}
		matches = append(matches, more...)
	}
	return matches, nil
}
