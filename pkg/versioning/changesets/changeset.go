package changesets

import (
	"io/fs"
	"slices"
	"strings"
)

// LoadError is returned by [FromFS] when a change file cannot be read or parsed. Its message is the cause's,
// like the crate's loading error (Knope reports it as "changesets::could_not_read_changeset"); File names the
// offending file.
type LoadError struct {
	// File is the name of the file in the directory.
	File string
	Err  error
}

// Error returns the message of the cause.
func (e *LoadError) Error() string { return e.Err.Error() }

// Unwrap returns the cause.
func (e *LoadError) Unwrap() error { return e.Err }

// PackageRelease is a [Change] as it applies to a single package (the crate's PackageChange).
type PackageRelease struct {
	ID      UniqueID
	Type    ChangeType
	Summary string
}

// Release is the combination of the changes that apply to one package.
type Release struct {
	Package string
	Changes []PackageRelease
}

// ChangeType is the overall change type of the release: the highest of its changes, where the last of equal
// ones wins (as Rust's Iterator::max does). It reports false when there is no change.
func (r Release) ChangeType() (ChangeType, bool) {
	var best ChangeType
	found := false
	for _, c := range r.Changes {
		if !found || c.Type.Compare(best) >= 0 {
			best, found = c.Type, true
		}
	}
	return best, found
}

// ChangeSet is a set of changes grouped into one [Release] per package.
type ChangeSet struct {
	Releases []Release
}

// FromChanges groups the changes by package, in the order each package is first seen, and sorts the changes of
// each package by id (plain byte comparison, so uppercase sorts before lowercase). Equal ids keep their order.
func FromChanges(changes []Change) ChangeSet {
	var releases []Release
	index := map[string]int{}
	for _, change := range changes {
		for _, v := range change.Versioning {
			i, ok := index[v.Package]
			if !ok {
				i = len(releases)
				index[v.Package] = i
				releases = append(releases, Release{Package: v.Package})
			}
			pr := PackageRelease{ID: change.ID, Type: v.Type, Summary: change.Summary}
			releases[i].Changes = append(releases[i].Changes, pr)
		}
	}
	for i := range releases {
		slices.SortStableFunc(
			releases[i].Changes,
			func(a, b PackageRelease) int { return strings.Compare(string(a.ID), string(b.ID)) },
		)
	}
	return ChangeSet{Releases: releases}
}

// FromFS loads the change files of a directory: the regular files ending in ".md" directly in the root of fsys
// (the id is the name without ".md"). Files are read in name order, so the result is deterministic. A file that
// cannot be read or parsed fails the whole load with a [*LoadError].
func FromFS(fsys fs.FS) (ChangeSet, error) {
	entries, err := fs.ReadDir(fsys, ".")
	if err != nil {
		return ChangeSet{}, err
	}
	var changes []Change
	for _, entry := range entries {
		name := entry.Name()
		id, ok := strings.CutSuffix(name, fileSuffix)
		if !ok || id == "" || !isFile(fsys, entry) {
			continue
		}
		data, err := fs.ReadFile(fsys, name)
		if err != nil {
			return ChangeSet{}, &LoadError{File: name, Err: err}
		}
		change, err := ParseChange(UniqueID(id), string(data))
		if err != nil {
			return ChangeSet{}, &LoadError{File: name, Err: err}
		}
		changes = append(changes, change)
	}
	return FromChanges(changes), nil
}

// isFile reports whether the entry is a regular file, following symbolic links.
func isFile(fsys fs.FS, entry fs.DirEntry) bool {
	if entry.Type()&fs.ModeSymlink != 0 {
		info, err := fs.Stat(fsys, entry.Name())
		return err == nil && info.Mode().IsRegular()
	}
	return entry.Type().IsRegular()
}
