package versioning_test

import (
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/celest-io/remeris/pkg/versioning"
	"github.com/celest-io/remeris/pkg/versioning/action"
	"github.com/celest-io/remeris/pkg/versioning/changes"
	"github.com/celest-io/remeris/pkg/versioning/releasenotes"
	"github.com/celest-io/remeris/pkg/versioning/versionedfile"
)

const (
	applyDate     = "2024-01-02"
	changelogPath = "CHANGELOG.md"
	changelogHead = "# Changelog\n"
	removeA       = ".changeset/a.md"
	removeB       = ".changeset/b.md"
	notesFeature  = "## Features\n\n- new"
	v130          = "1.3.0"
	title130      = "1.3.0 (" + applyDate + ")"
)

func featureChange(summary string) changes.Change {
	return changes.Change{Type: changes.ChangeType{Kind: changes.Feature}, Summary: summary,
		Origin: changes.Origin{Kind: changes.ConventionalCommit, Description: summary}}
}

func fileChange(id string, t changes.ChangeType, summary string) changes.Change {
	return changes.Change{Type: t, Summary: summary, Origin: changes.Origin{Kind: changes.ChangeFile, ID: id}}
}

func newApplyPackage(t *testing.T, name versioning.PackageName, notes versioning.ReleaseNotes) (
	*versioning.Package, []versionedfile.File,
) {
	t.Helper()
	files := []versionedfile.File{realCargo(t, cargoToml, cargoBody)}
	p, err := versioning.NewPackage(name, []versionedfile.Config{cfg(cargoToml, versionedfile.Cargo, nil)}, files, nil,
		versionedfile.Standard, nil)
	if err != nil {
		t.Fatal(err)
	}
	p.ReleaseNotes = notes
	return p, files
}

func kinds(actions []action.Action) []action.Kind {
	out := make([]action.Kind, len(actions))
	for i, a := range actions {
		out[i] = a.Kind
	}
	return out
}

func TestApplyChanges(t *testing.T) {
	t.Parallel()
	label := "rc"
	forced := mustVersion(t, "5.0.0")
	changelog := releasenotes.NewChangelog(changelogPath, changelogHead)
	major, minor, patch := changes.ChangeType{Kind: changes.Breaking}, changes.ChangeType{Kind: changes.Feature},
		changes.ChangeType{Kind: changes.Fix}
	tests := map[string]struct {
		notes       versioning.ReleaseNotes
		name        versioning.PackageName
		changes     []changes.Change
		cfg         versioning.ChangeConfig
		wantVersion string // empty: nothing happens
		wantKinds   []action.Kind
		wantPaths   []string // paths of the RemoveFile actions, in order
		wantNotes   string
		wantTitle   string
		wantPkg     *string
	}{
		"no changes and no override": {},
		"one feature, no changelog": {
			changes:     []changes.Change{featureChange("new")},
			wantVersion: v130,
			wantKinds:   []action.Kind{action.KindCreateRelease},
			wantNotes:   notesFeature,
			wantTitle:   title130,
		},
		"strongest rule wins, change files are removed in order": {
			changes: []changes.Change{
				fileChange("b", patch, "fixed"), fileChange("a", major, "broke"), featureChange("new"),
			},
			wantVersion: v200,
			wantKinds: []action.Kind{
				action.KindRemoveFile, action.KindRemoveFile, action.KindCreateRelease,
			},
			wantPaths: []string{removeB, removeA},
			wantNotes: "## Breaking Changes\n\n- broke\n\n## Features\n\n- new\n\n## Fixes\n\n- fixed",
			wantTitle: "2.0.0 (" + applyDate + ")",
		},
		"override with no changes still releases": {
			cfg:         versioning.ChangeConfig{Force: &forced},
			wantVersion: "5.0.0",
			wantKinds:   []action.Kind{action.KindCreateRelease},
			wantTitle:   "5.0.0 (" + applyDate + ")",
		},
		"override ignores the rule and the label": {
			changes:     []changes.Change{featureChange("new")},
			cfg:         versioning.ChangeConfig{Force: &forced, PrereleaseLabel: &label},
			wantVersion: "5.0.0",
			wantKinds:   []action.Kind{action.KindCreateRelease},
			wantNotes:   notesFeature,
			wantTitle:   "5.0.0 (" + applyDate + ")",
		},
		"pre-release keeps the change files": {
			changes:     []changes.Change{fileChange("a", minor, "new"), featureChange("also")},
			cfg:         versioning.ChangeConfig{PrereleaseLabel: &label},
			wantVersion: "1.3.0-rc.0",
			wantKinds:   []action.Kind{action.KindCreateRelease},
			wantNotes:   "## Features\n\n- new\n- also",
			wantTitle:   "1.3.0-rc.0 (" + applyDate + ")",
		},
		"changelog is written between the removals and the release": {
			notes:       versioning.ReleaseNotes{Changelog: &changelog},
			changes:     []changes.Change{fileChange("a", patch, "fixed")},
			wantVersion: "1.2.4",
			wantKinds:   []action.Kind{action.KindRemoveFile, action.KindWriteToFile, action.KindCreateRelease},
			wantPaths:   []string{removeA},
			wantNotes:   "## Fixes\n\n- fixed",
			wantTitle:   "1.2.4 (" + applyDate + ")",
		},
		"custom sections and templates": {
			notes: versioning.ReleaseNotes{
				Sections: releasenotes.ConvertSections([]releasenotes.ChangelogSectionConfig{
					{Name: "Docs", Types: []string{"docs"}},
				}),
				ChangeTemplates: []releasenotes.ChangeTemplate{"* $summary!"},
			},
			changes:     []changes.Change{fileChange("a", changes.CustomChangeType("docs"), "readme"), featureChange("new")},
			wantVersion: v130,
			wantKinds:   []action.Kind{action.KindRemoveFile, action.KindCreateRelease},
			wantPaths:   []string{removeA},
			wantNotes:   "## Features\n\n* new!\n\n## Docs\n\n* readme!",
			wantTitle:   title130,
		},
		"custom package name": {
			name:        versioning.CustomName("foo"),
			changes:     []changes.Change{featureChange("new")},
			wantVersion: v130,
			wantKinds:   []action.Kind{action.KindCreateRelease},
			wantNotes:   notesFeature,
			wantTitle:   title130,
			wantPkg:     strp("foo"),
		},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			p, files := newApplyPackage(t, tc.name, tc.notes)
			got, actions, err := p.ApplyChanges(tc.changes, files, tc.cfg, applyDate)
			if err != nil {
				t.Fatal(err)
			}
			if tc.wantVersion == "" {
				if len(actions) != 0 || got[0].Write() != nil {
					t.Errorf("something changed: %v", actions)
				}
				return
			}
			if !slices.Equal(kinds(actions), tc.wantKinds) {
				t.Fatalf("kinds = %v, want %v", kinds(actions), tc.wantKinds)
			}
			var paths []string
			for _, a := range actions {
				if a.Kind == action.KindRemoveFile {
					paths = append(paths, a.Path)
				}
			}
			if !slices.Equal(paths, tc.wantPaths) {
				t.Errorf("removed = %v, want %v", paths, tc.wantPaths)
			}
			if w := got[0].Write(); len(w) != 1 || !strings.Contains(w[0].Content, `version = "`+tc.wantVersion+`"`) {
				t.Errorf("file not set to %s: %v", tc.wantVersion, w)
			}
			if latest, _ := p.LatestVersion(); latest.String() != tc.wantVersion {
				t.Errorf("latest = %s, want %s", latest, tc.wantVersion)
			}
			last := actions[len(actions)-1].Release
			if last.Notes != tc.wantNotes || last.Title != tc.wantTitle || last.Version.String() != tc.wantVersion {
				t.Errorf("release = %+v", *last)
			}
			if (last.PackageName == nil) != (tc.wantPkg == nil) || (tc.wantPkg != nil && *last.PackageName != *tc.wantPkg) {
				t.Errorf("package name = %v, want %v", last.PackageName, tc.wantPkg)
			}
		})
	}
}

func TestApplyChangesChangelogChain(t *testing.T) {
	t.Parallel()
	changelog := releasenotes.NewChangelog(changelogPath, changelogHead)
	p, files := newApplyPackage(t, versioning.DefaultName(), versioning.ReleaseNotes{Changelog: &changelog})
	_, actions, err := p.ApplyChanges([]changes.Change{featureChange("one")}, files, versioning.ChangeConfig{}, applyDate)
	if err != nil {
		t.Fatal(err)
	}
	want := "# Changelog\n## 1.3.0 (2024-01-02)\n\n### Features\n\n- one\n"
	if got := actions[0].Content; actions[0].Path != changelogPath || got == "" {
		t.Fatalf("first changelog = %q (%s)", got, actions[0].Path)
	}
	if changelog.Content != changelogHead {
		t.Error("the caller's changelog was modified")
	}
	first := actions[0].Content
	if !strings.Contains(first, "## 1.3.0 (2024-01-02)\n\n### Features\n\n- one") {
		t.Errorf("first changelog = %q, want it to look like %q", first, want)
	}
	// The second release in the same run sees the first one.
	_, actions, err = p.ApplyChanges([]changes.Change{featureChange("two")}, files, versioning.ChangeConfig{}, applyDate)
	if err != nil {
		t.Fatal(err)
	}
	second := actions[0].Content
	i2, i1 := strings.Index(second, "## 1.4.0"), strings.Index(second, "## 1.3.0")
	if i2 < 0 || i1 < 0 || i2 > i1 {
		t.Errorf("second changelog = %q, want 1.4.0 above 1.3.0", second)
	}
}

func TestApplyChangesSharedChangeFile(t *testing.T) {
	t.Parallel()
	shared := fileChange("a", changes.ChangeType{Kind: changes.Feature}, "both")
	var all []action.Action
	for _, n := range []versioning.PackageName{versioning.DefaultName(), versioning.CustomName("foo")} {
		p, files := newApplyPackage(t, n, versioning.ReleaseNotes{})
		_, actions, err := p.ApplyChanges([]changes.Change{shared}, files, versioning.ChangeConfig{}, applyDate)
		if err != nil {
			t.Fatal(err)
		}
		if len(actions) != 2 || actions[0].Path != removeA {
			t.Fatalf("actions = %+v", actions)
		}
		all = append(all, actions...)
	}
	// Two RemoveFile and two different releases: only the removal is a duplicate.
	if got := action.Dedupe(all); len(got) != 3 {
		t.Errorf("deduped to %d actions, want 3", len(got))
	}
}

func TestApplyChangesNoTagAction(t *testing.T) {
	t.Parallel()
	p, files := newApplyPackage(t, versioning.DefaultName(), versioning.ReleaseNotes{})
	_, actions, err := p.ApplyChanges([]changes.Change{featureChange("x")}, files, versioning.ChangeConfig{}, applyDate)
	if err != nil {
		t.Fatal(err)
	}
	for _, a := range actions {
		if a.Kind == action.KindAddTag {
			t.Error("ApplyChanges must not add tags: they come from the file writes")
		}
	}
}

func TestApplyChangesErrors(t *testing.T) {
	t.Parallel()
	t.Run("set version error leaves the package unchanged", func(t *testing.T) {
		t.Parallel()
		boom := errors.New("boom")
		files := []versionedfile.File{fakeFile{path: packageJSON, version: v123, setErr: boom}}
		p, err := versioning.NewPackage(versioning.DefaultName(),
			[]versionedfile.Config{cfg(packageJSON, versionedfile.PackageJson, nil)}, files, nil, versionedfile.Standard, nil)
		if err != nil {
			t.Fatal(err)
		}
		changelog := releasenotes.NewChangelog(changelogPath, changelogHead)
		p.ReleaseNotes.Changelog = &changelog
		_, _, err = p.ApplyChanges([]changes.Change{featureChange("x")}, files, versioning.ChangeConfig{}, applyDate)
		if !errors.Is(err, boom) {
			t.Fatalf("err = %v", err)
		}
		if latest, _ := p.LatestVersion(); latest.String() != v123 {
			t.Errorf("latest = %s", latest)
		}
		if p.ReleaseNotes.Changelog != &changelog || changelog.Content != changelogHead {
			t.Error("the changelog was touched")
		}
	})
	t.Run("invalid label", func(t *testing.T) {
		t.Parallel()
		bad := "a..b"
		p, files := newApplyPackage(t, versioning.DefaultName(), versioning.ReleaseNotes{})
		_, _, err := p.ApplyChanges([]changes.Change{featureChange("x")}, files,
			versioning.ChangeConfig{PrereleaseLabel: &bad}, applyDate)
		if err == nil {
			t.Fatal("want an error")
		}
	})
}
