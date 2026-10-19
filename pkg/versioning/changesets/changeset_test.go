package changesets_test

import (
	"errors"
	"testing"
	"testing/fstest"

	"github.com/celest-io/remeris/pkg/versioning/changesets"
)

const (
	fileA         = "a.md"
	fileBad       = "bad.md"
	summaryUp     = "UP"
	firstPackage  = "first_package"
	secondPackage = "second_package"
)

func file(content string) *fstest.MapFile { return &fstest.MapFile{Data: []byte(content)} }

func TestFromFSLoadChangeset(t *testing.T) {
	t.Parallel()
	fsys := fstest.MapFS{
		"first_change.md":  file("---\nfirst_package: minor\nsecond_package: patch\n---\n\n### This is a summary\n"),
		"Second Change.md": file("---\nsecond_package: major\n---\n\n### Another summary\n"),
	}
	got, err := changesets.FromFS(fsys)
	noError(t, err)
	want := changesets.ChangeSet{Releases: []changesets.Release{
		{Package: secondPackage, Changes: []changesets.PackageRelease{
			{ID: "Second Change", Type: major(), Summary: "### Another summary"},
			{ID: "first_change", Type: patch(), Summary: summaryText},
		}},
		{Package: firstPackage, Changes: []changesets.PackageRelease{
			{ID: "first_change", Type: minor(), Summary: summaryText},
		}},
	}}
	equal(t, want, got)
	for _, r := range got.Releases {
		ct, ok := r.ChangeType()
		equal(t, true, ok)
		if r.Package == firstPackage {
			equal(t, minor(), ct)
		} else {
			equal(t, major(), ct)
		}
	}
}

func TestFromFSFiltering(t *testing.T) {
	t.Parallel()
	valid := "---\npkg: patch\n---\n\nS\n"
	tests := map[string]struct {
		fsys fstest.MapFS
		ids  []changesets.UniqueID
	}{
		"empty directory": {fsys: fstest.MapFS{}},
		"subdirectories ignored": {
			fsys: fstest.MapFS{fileA: file(valid), "sub/b.md": file(valid), "sub/deeper/c.md": file(valid)},
			ids:  []changesets.UniqueID{"a"},
		},
		"directory named like a change": {
			fsys: fstest.MapFS{"dir.md/x.md": file(valid), fileA: file(valid)},
			ids:  []changesets.UniqueID{"a"},
		},
		"other extensions ignored": {
			fsys: fstest.MapFS{
				fileA:         file(valid),
				"README.txt":  file("nope"),
				"config.json": file("{"),
				"b.markdown":  file(valid),
				"c.MD":        file(valid),
			},
			ids: []changesets.UniqueID{"a"},
		},
		"dot md is not a change": {
			fsys: fstest.MapFS{".md": file("nope"), fileA: file(valid)},
			ids:  []changesets.UniqueID{"a"},
		},
		"dots in the name": {
			fsys: fstest.MapFS{"a.b.md": file(valid)},
			ids:  []changesets.UniqueID{"a.b"},
		},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			got, err := changesets.FromFS(tc.fsys)
			noError(t, err)
			var ids []changesets.UniqueID
			for _, r := range got.Releases {
				for _, c := range r.Changes {
					ids = append(ids, c.ID)
				}
			}
			equal(t, tc.ids, ids)
		})
	}
}

func TestFromFSSortsUppercaseFirst(t *testing.T) {
	t.Parallel()
	content := "---\npkg: patch\n---\n\nS\n"
	got, err := changesets.FromFS(
		fstest.MapFS{"b.md": file(content), fileA: file(content), "B.md": file(content), "_z.md": file(content)},
	)
	noError(t, err)
	var ids []changesets.UniqueID
	for _, c := range got.Releases[0].Changes {
		ids = append(ids, c.ID)
	}
	equal(t, []changesets.UniqueID{"B", "_z", "a", "b"}, ids)
}

func TestFromFSErrors(t *testing.T) {
	t.Parallel()
	tests := map[string]struct {
		fsys fstest.MapFS
		file string
		is   error
	}{
		"missing opening": {
			fstest.MapFS{"ok.md": file("---\np: patch\n---\n"), fileBad: file("hello")},
			fileBad,
			changesets.ErrMissingFrontMatter,
		},
		"bad line":         {fstest.MapFS{fileBad: file("---\nnope\n---\n")}, fileBad, changesets.ErrInvalidFrontMatter},
		"empty versioning": {fstest.MapFS{fileBad: file("---\n---\n")}, fileBad, changesets.ErrEmptyVersioning},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			_, err := changesets.FromFS(tc.fsys)
			mustErr(t, err)
			isErr(t, err, tc.is)
			var loadErr *changesets.LoadError
			if !errors.As(err, &loadErr) {
				t.Fatalf("not a LoadError: %v", err)
			}
			equal(t, tc.file, loadErr.File)
			equal(t, loadErr.Err.Error(), err.Error())
		})
	}
}

func TestFromFSInvalidUTF8(t *testing.T) {
	t.Parallel()
	fsys := fstest.MapFS{fileBad: file("---\nfoo: patch\n---\n\n\xff\n")}
	_, err := changesets.FromFS(fsys)
	mustErr(t, err)
	var loadErr *changesets.LoadError
	if !errors.As(err, &loadErr) {
		t.Fatalf("not a LoadError: %v", err)
	}
	equal(t, fileBad, loadErr.File)
	equal(t, "stream did not contain valid UTF-8", err.Error())
}

func TestFromChanges(t *testing.T) {
	t.Parallel()
	changes := []changesets.Change{
		{
			ID:         "b",
			Versioning: []changesets.PackageChange{{Package: "y", Type: patch()}, {Package: "x", Type: minor()}},
			Summary:    "B",
		},
		{ID: "a", Versioning: []changesets.PackageChange{{Package: "x", Type: custom("c")}}, Summary: "A"},
		{
			ID:         "A",
			Versioning: []changesets.PackageChange{{Package: "z", Type: major()}, {Package: "y", Type: custom("d")}},
			Summary:    summaryUp,
		},
	}
	got := changesets.FromChanges(changes)
	want := changesets.ChangeSet{Releases: []changesets.Release{
		{Package: "y", Changes: []changesets.PackageRelease{
			{ID: "A", Type: custom("d"), Summary: summaryUp},
			{ID: "b", Type: patch(), Summary: "B"},
		}},
		{Package: "x", Changes: []changesets.PackageRelease{
			{ID: "a", Type: custom("c"), Summary: "A"},
			{ID: "b", Type: minor(), Summary: "B"},
		}},
		{Package: "z", Changes: []changesets.PackageRelease{{ID: "A", Type: major(), Summary: summaryUp}}},
	}}
	equal(t, want, got)
	equal(t, 0, len(changesets.FromChanges(nil).Releases))
}

func TestReleaseChangeType(t *testing.T) {
	t.Parallel()
	tests := map[string]struct {
		types []changesets.ChangeType
		want  changesets.ChangeType
		ok    bool
	}{
		"none":                {nil, changesets.ChangeType{}, false},
		"highest wins":        {[]changesets.ChangeType{patch(), major(), minor()}, major(), true},
		"custom is lowest":    {[]changesets.ChangeType{custom("x"), patch()}, patch(), true},
		"last of equals wins": {[]changesets.ChangeType{custom("x"), custom("y")}, custom("y"), true},
		"single":              {[]changesets.ChangeType{minor()}, minor(), true},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			r := changesets.Release{}
			for _, ct := range tc.types {
				r.Changes = append(r.Changes, changesets.PackageRelease{Type: ct})
			}
			got, ok := r.ChangeType()
			equal(t, tc.ok, ok)
			equal(t, tc.want, got)
		})
	}
}
