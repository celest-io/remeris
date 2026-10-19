package versioning_test

import (
	"errors"
	"slices"
	"testing"

	"github.com/celest-io/remeris/pkg/versioning"
	"github.com/celest-io/remeris/pkg/versioning/action"
	"github.com/celest-io/remeris/pkg/versioning/releasenotes"
	"github.com/celest-io/remeris/pkg/versioning/semver"
	"github.com/celest-io/remeris/pkg/versioning/versionedfile"
)

const (
	relV130     = "1.3.0"
	relRC1Tag   = "v1.1.0-rc.1"
	relRC1      = "1.1.0-rc.1"
	relV110     = "1.1.0"
	relMarker   = "release"
	subModPath  = "sub/go.mod"
	subTagV100  = "sub/v1.0.0"
	goModPath   = "go.mod"
	changelogMD = "CHANGELOG.md"
	notesV123   = "## 1.2.3 - 2026-01-01\n\n### Features\n\n- Thing\n"
	changelogV3 = "# Changelog\n\n" + notesV123 + "\n## 1.0.0 - 2025-01-01\n\n### Features\n\n- Old\n"
)

func preparedPackage(t *testing.T, name versioning.PackageName, tags []string, current string, cl string) *versioning.Package {
	t.Helper()
	p := &versioning.Package{Name: name, Versions: semver.FromTags(name.Custom(), tags)}
	if current != "" {
		p.Versions.Update(mustVersion(t, current))
	}
	if cl != "" {
		c := releasenotes.NewChangelog(changelogMD, cl)
		p.ReleaseNotes.Changelog = &c
	}
	return p
}

func TestFindPreparedRelease(t *testing.T) {
	t.Parallel()
	foo := versioning.CustomName("foo")
	tests := map[string]struct {
		name      versioning.PackageName
		tags      []string
		current   string
		changelog string
		want      bool
		wantLast  string
		wantTitle string
		wantNotes string
		fromCL    bool
	}{
		"no version": {name: versioning.DefaultName()},
		"no tags and no changelog": {
			name: versioning.DefaultName(), current: v123, want: true, wantTitle: v123,
		},
		"version equals the latest tag": {
			name: versioning.DefaultName(), tags: []string{tagV123, tagV100}, current: v123, changelog: changelogV3,
		},
		"newer than the tag, from the changelog": {
			name: versioning.DefaultName(), tags: []string{tagV100}, current: v123, changelog: changelogV3,
			want: true, wantLast: v100, wantTitle: "1.2.3 - 2026-01-01", wantNotes: "\n## Features\n\n- Thing\n", fromCL: true,
		},
		"changelog lacks the version": {
			name: versioning.DefaultName(), tags: []string{tagV100}, current: relV130, changelog: changelogV3,
			want: true, wantLast: v100, wantTitle: relV130,
		},
		"named package from the changelog": {
			name: foo, tags: []string{"bar/v9.0.0", "v8.0.0", fooTagV123, "foo/v1.0.0"}, current: "1.4.0",
			changelog: "# Changelog\n\n## 1.4.0 - 2026-02-02\n\n### Fixes\n\n- Fix\n",
			want:      true, wantLast: v123, wantTitle: "1.4.0 - 2026-02-02", wantNotes: "\n## Fixes\n\n- Fix", fromCL: true,
		},
		"named package ignores the tags of others": {
			name: foo, tags: []string{"v1.2.3", "bar/v1.2.3"}, current: v123, want: true, wantTitle: v123,
		},
		"named package tag equals the version": {
			name: foo, tags: []string{"v5.0.0", fooTagV123}, current: v123,
		},
		"pre-release newer than the tag": {
			name: versioning.DefaultName(), tags: []string{relRC1Tag, tagV100}, current: "1.1.0-rc.2",
			want: true, wantLast: relRC1, wantTitle: "1.1.0-rc.2",
		},
		"pre-release equals the tag": {
			name: versioning.DefaultName(), tags: []string{relRC1Tag, tagV100}, current: relRC1,
		},
		"stable after a pre-release tag": {
			name: versioning.DefaultName(), tags: []string{relRC1Tag, tagV100}, current: relV110,
			want: true, wantLast: relRC1, wantTitle: relV110,
		},
		// The history ignores build metadata when ranking, so the tag with build metadata stays the current version.
		"build metadata of the tag": {
			name: versioning.DefaultName(), tags: []string{"v1.0.0+b1"}, current: v100,
		},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			p := preparedPackage(t, tc.name, tc.tags, tc.current, tc.changelog)
			got, ok := p.FindPreparedRelease(tc.tags)
			if ok != tc.want {
				t.Fatalf("found = %v, want %v", ok, tc.want)
			}
			if !ok {
				return
			}
			if got.Release.Title != tc.wantTitle || got.Release.Notes != tc.wantNotes {
				t.Errorf("release = %q / %q, want %q / %q", got.Release.Title, got.Release.Notes, tc.wantTitle, tc.wantNotes)
			}
			if got.Release.Version.String() != tc.current {
				t.Errorf("version = %s, want %s", got.Release.Version, tc.current)
			}
			gotName, wantName := got.Release.PackageName, tc.name.Custom()
			if (gotName == nil) != (wantName == nil) || (gotName != nil && *gotName != *wantName) {
				t.Errorf("package name = %v, want %v", gotName, wantName)
			}
			if got.FromChangelog != tc.fromCL {
				t.Errorf("FromChangelog = %v, want %v", got.FromChangelog, tc.fromCL)
			}
			switch {
			case tc.wantLast == "" && got.LastTag != nil:
				t.Errorf("LastTag = %s, want nil", got.LastTag)
			case tc.wantLast != "" && (got.LastTag == nil || got.LastTag.String() != tc.wantLast):
				t.Errorf("LastTag = %v, want %s", got.LastTag, tc.wantLast)
			}
		})
	}
}

func TestLastTagOf(t *testing.T) {
	t.Parallel()
	p := &versioning.Package{Name: versioning.CustomName("foo")}
	if got := p.LastTagOf([]string{"v1.0.0", "bar/v2.0.0"}); got != nil {
		t.Errorf("LastTagOf = %s, want nil", got)
	}
	if got := p.LastTagOf([]string{"v1.0.0", fooTagV123}); got == nil || got.String() != v123 {
		t.Errorf("LastTagOf = %v, want %s", got, v123)
	}
}

func goModFile(t *testing.T, path, module string, tags []string) versionedfile.File {
	t.Helper()
	f, err := versionedfile.New(versionedfile.Config{Path: path, Format: versionedfile.GoMod},
		"module "+module+"\n\ngo 1.19\n", tags)
	if err != nil {
		t.Fatal(err)
	}
	return f
}

func TestPreparedReleaseActions(t *testing.T) {
	t.Parallel()
	const mod = "github.com/o/r"
	tests := map[string]struct {
		files   func(t *testing.T) []versionedfile.File
		cfgs    []versionedfile.Config
		tags    []string
		current string
		gv      versionedfile.GoVersioning
		setErr  bool
		want    []string // relMarker or the tag of each action
	}{
		"nothing prepared": {
			files: func(*testing.T) []versionedfile.File { return nil },
			tags:  []string{tagV123}, current: v123,
		},
		"no files": {
			files: func(*testing.T) []versionedfile.File { return nil },
			tags:  []string{tagV100}, current: v123, want: []string{relMarker},
		},
		"root go.mod": {
			files: func(t *testing.T) []versionedfile.File {
				return []versionedfile.File{goModFile(t, goModPath, mod, []string{tagV100})}
			},
			cfgs: []versionedfile.Config{cfg(goModPath, versionedfile.GoMod, nil)},
			tags: []string{tagV100}, current: v123, want: []string{relMarker, tagV123},
		},
		"v2 module": {
			files: func(t *testing.T) []versionedfile.File {
				return []versionedfile.File{goModFile(t, goModPath, mod+"/v2", []string{"v1.9.0", "v2.0.0"})}
			},
			cfgs: []versionedfile.Config{cfg(goModPath, versionedfile.GoMod, nil)},
			tags: []string{"v2.0.0"}, current: "2.1.0", want: []string{relMarker, "v2.1.0"},
		},
		"subdirectory module": {
			files: func(t *testing.T) []versionedfile.File {
				return []versionedfile.File{goModFile(t, subModPath, mod+"/sub", []string{subTagV100})}
			},
			cfgs: []versionedfile.Config{cfg(subModPath, versionedfile.GoMod, nil)},
			tags: []string{subTagV100}, current: v123, want: []string{relMarker, "sub/v1.2.3"},
		},
		"two go.mod files give one tag each": {
			files: func(t *testing.T) []versionedfile.File {
				return []versionedfile.File{
					goModFile(t, goModPath, mod, []string{tagV100}),
					goModFile(t, subModPath, mod+"/sub", []string{subTagV100}),
				}
			},
			cfgs: []versionedfile.Config{cfg(goModPath, versionedfile.GoMod, nil), cfg(subModPath, versionedfile.GoMod, nil)},
			tags: []string{tagV100, subTagV100}, current: v123, want: []string{relMarker, tagV123, "sub/v1.2.3"},
		},
		"go.mod of another package is untouched": {
			files: func(t *testing.T) []versionedfile.File {
				return []versionedfile.File{goModFile(t, "other/go.mod", mod+"/other", []string{"other/v1.0.0"})}
			},
			cfgs: []versionedfile.Config{cfg(goModPath, versionedfile.GoMod, nil)},
			tags: []string{tagV100}, current: v123, want: []string{relMarker},
		},
		"non-go file gives no tag": {
			files: func(*testing.T) []versionedfile.File {
				return []versionedfile.File{fakeFile{path: packageJSON, version: v100}}
			},
			cfgs: []versionedfile.Config{cfg(packageJSON, versionedfile.PackageJson, nil)},
			tags: []string{tagV100}, current: v123, want: []string{relMarker},
		},
		"set version error gives only the release": {
			files: func(*testing.T) []versionedfile.File {
				return []versionedfile.File{fakeFile{path: packageJSON, version: v100, setErr: errors.New("boom")}}
			},
			cfgs: []versionedfile.Config{cfg(packageJSON, versionedfile.PackageJson, nil)},
			tags: []string{tagV100}, current: v123, setErr: true, want: []string{relMarker},
		},
		"go.mod major bump without a major suffix is an error": {
			files: func(t *testing.T) []versionedfile.File {
				return []versionedfile.File{goModFile(t, goModPath, mod, []string{tagV100})}
			},
			cfgs: []versionedfile.Config{cfg(goModPath, versionedfile.GoMod, nil)},
			tags: []string{tagV100}, current: v200, want: []string{relMarker},
		},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			p := preparedPackage(t, versioning.DefaultName(), tc.tags, tc.current, "")
			p.Files, p.GoVersioning = tc.cfgs, tc.gv
			var got []action.Action
			if pr, ok := p.FindPreparedRelease(tc.tags); ok {
				got = p.PreparedReleaseActions(pr.Release, tc.files(t))
			}
			var kinds []string
			for _, a := range got {
				switch a.Kind {
				case action.KindCreateRelease:
					kinds = append(kinds, relMarker)
				case action.KindAddTag:
					kinds = append(kinds, a.Tag)
				default:
					t.Fatalf("unexpected action %v", a)
				}
			}
			if !slices.Equal(kinds, tc.want) {
				t.Errorf("actions = %v, want %v", kinds, tc.want)
			}
			if len(got) > 0 && got[0].Release.Title != tc.current {
				t.Errorf("title = %q, want %q", got[0].Release.Title, tc.current)
			}
		})
	}
}
