package versioning_test

import (
	"bytes"
	"context"
	"log/slog"
	"slices"
	"strings"
	"testing"

	"github.com/celest-io/remeris/pkg/versioning"
	"github.com/celest-io/remeris/pkg/versioning/changes"
	"github.com/celest-io/remeris/pkg/versioning/trace"
	"github.com/celest-io/remeris/pkg/versioning/versionedfile"
)

const (
	lineCargo123   = "Cargo.toml has version 1.2.3"
	lineLooking    = "Looking for Git tags matching package name."
	lineNoTagsV    = "No tags found starting with v"
	lineNoTagsFoo  = "No tags found starting with foo/v"
	lineDetermine  = "Determining new version for foo"
	lineBumpMinor  = "Using MINOR rule to bump from 1.2.3 to 1.3.0"
	lineSearching  = "Searching for last package tag to determine if there's a release to release"
	lineNoChangelg = "Release has previously been prepared without a changelog, will create a release with no notes."
	lineImpliesNew = "commit new\n\timplies rule MINOR"
	lineImpliesFix = "changeset a.md\n\timplies rule PATCH"
	lineOverride   = "Using overridden version 5.0.0"
	lineLast100    = "Last tag is 1.0.0"
)

type lineHandler struct{ buf *bytes.Buffer }

func (lineHandler) Enabled(context.Context, slog.Level) bool { return true }
func (h lineHandler) Handle(_ context.Context, r slog.Record) error {
	h.buf.WriteString(r.Message + "\x00")
	return nil
}
func (h lineHandler) WithAttrs([]slog.Attr) slog.Handler { return h }
func (h lineHandler) WithGroup(string) slog.Handler      { return h }

// capture returns a Tracer and a function that lists the messages it received, in order.
func capture() (*trace.Tracer, func() []string) {
	var buf bytes.Buffer
	return trace.New(slog.New(lineHandler{&buf})), func() []string {
		if buf.Len() == 0 {
			return nil
		}
		return strings.Split(strings.TrimSuffix(buf.String(), "\x00"), "\x00")
	}
}

func checkLines(t *testing.T, got, want []string) {
	t.Helper()
	if !slices.Equal(got, want) {
		t.Errorf("debug lines = %q\nwant         %q", got, want)
	}
}

func TestNewPackageTrace(t *testing.T) {
	t.Parallel()
	foo := versioning.CustomName("foo")
	tests := map[string]struct {
		name    versioning.PackageName
		tags    []string
		configs []versionedfile.Config
		files   func(t *testing.T) []versionedfile.File
		want    []string
		wantErr bool
	}{
		"default package without tags": {
			name: versioning.DefaultName(),
			want: []string{lineCargo123, lineLooking, lineNoTagsV},
		},
		"default package with a tag": {
			name: versioning.DefaultName(), tags: []string{tagV123},
			want: []string{lineCargo123, lineLooking},
		},
		"named package without its tags": {
			name: foo, tags: []string{tagV123},
			want: []string{lineCargo123, lineLooking, lineNoTagsFoo},
		},
		"named package with a tag": {
			name: foo, tags: []string{fooTagV123},
			want: []string{lineCargo123, lineLooking},
		},
		"inconsistent versions print both files before the error": {
			name: versioning.DefaultName(),
			configs: []versionedfile.Config{
				cfg(cargoToml, versionedfile.Cargo, nil), cfg(packageJSON, versionedfile.PackageJson, nil),
			},
			files: func(t *testing.T) []versionedfile.File {
				return []versionedfile.File{realCargo(t, cargoToml, cargoBody), fakeFile{path: packageJSON, version: v200}}
			},
			want:    []string{lineCargo123, "package.json has version 2.0.0"},
			wantErr: true,
		},
		"dependency entries are not versions": {
			name: versioning.DefaultName(),
			configs: []versionedfile.Config{
				cfg(cargoToml, versionedfile.Cargo, nil), cfg(packageJSON, versionedfile.PackageJson, strp(fooDep)),
			},
			files: func(t *testing.T) []versionedfile.File {
				return []versionedfile.File{realCargo(t, cargoToml, cargoBody), fakeFile{path: packageJSON, version: v200}}
			},
			want: []string{lineCargo123, lineLooking, lineNoTagsV},
		},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			configs := tc.configs
			if configs == nil {
				configs = []versionedfile.Config{cfg(cargoToml, versionedfile.Cargo, nil)}
			}
			files := []versionedfile.File{realCargo(t, cargoToml, cargoBody)}
			if tc.files != nil {
				files = tc.files(t)
			}
			tr, lines := capture()
			_, err := versioning.NewPackage(tc.name, configs, files, tc.tags, versionedfile.Standard, nil, tr)
			if (err != nil) != tc.wantErr {
				t.Fatalf("error = %v, want error: %v", err, tc.wantErr)
			}
			checkLines(t, lines(), tc.want)
		})
	}
}

func TestApplyChangesTrace(t *testing.T) {
	t.Parallel()
	foo := versioning.CustomName("foo")
	label := "rc"
	forced := mustVersion(t, "5.0.0")
	minor := changes.ChangeType{Kind: changes.Feature}
	tests := map[string]struct {
		name    versioning.PackageName
		changes []changes.Change
		cfg     versioning.ChangeConfig
		want    []string
	}{
		"no changes and no override": {name: foo},
		"default package prints no heading": {
			name:    versioning.DefaultName(),
			changes: []changes.Change{featureChange("new")},
			want:    []string{lineImpliesNew, lineBumpMinor},
		},
		"named package prints the heading": {
			name:    foo,
			changes: []changes.Change{featureChange("new")},
			want:    []string{lineDetermine, lineImpliesNew, lineBumpMinor},
		},
		"commits come before change files, in list order": {
			name:    versioning.DefaultName(),
			changes: []changes.Change{featureChange("new"), fileChange("a", changes.ChangeType{Kind: changes.Fix}, "x")},
			want:    []string{lineImpliesNew, lineImpliesFix, lineBumpMinor},
		},
		"override prints no implies lines": {
			name:    foo,
			changes: []changes.Change{featureChange("new")},
			cfg:     versioning.ChangeConfig{Force: &forced},
			want:    []string{lineDetermine, lineOverride},
		},
		"override without changes": {
			name: versioning.DefaultName(),
			cfg:  versioning.ChangeConfig{Force: &forced},
			want: []string{lineOverride},
		},
		"pre-release label": {
			name:    versioning.DefaultName(),
			changes: []changes.Change{fileChange("a", minor, "new")},
			cfg:     versioning.ChangeConfig{PrereleaseLabel: &label},
			want: []string{
				"changeset a.md\n\timplies rule MINOR",
				"Pre-release label rc selected. Determining next stable version...",
				lineBumpMinor,
				"No existing pre-release version found; creating rc.0",
			},
		},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			p, files := newApplyPackage(t, tc.name, versioning.ReleaseNotes{})
			tr, lines := capture()
			tc.cfg.Trace = tr
			if _, _, err := p.ApplyChanges(tc.changes, files, tc.cfg, applyDate); err != nil {
				t.Fatal(err)
			}
			checkLines(t, lines(), tc.want)
		})
	}
}

func TestFindPreparedReleaseTrace(t *testing.T) {
	t.Parallel()
	foo := versioning.CustomName("foo")
	tests := map[string]struct {
		name      versioning.PackageName
		tags      []string
		current   string
		changelog string
		want      []string
	}{
		"no version prints nothing": {name: versioning.DefaultName()},
		"no tags": {
			name: versioning.DefaultName(), current: v123,
			want: []string{lineSearching, lineNoTagsV, lineNoChangelg},
		},
		"older tag, no changelog": {
			name: versioning.DefaultName(), tags: []string{tagV100}, current: v123,
			want: []string{lineSearching, lineLast100, lineNoChangelg},
		},
		"older tag, entry in the changelog": {
			name: versioning.DefaultName(), tags: []string{tagV100}, current: v123, changelog: changelogV3,
			want: []string{lineSearching, lineLast100},
		},
		"already tagged": {
			name: versioning.DefaultName(), tags: []string{tagV123}, current: v123,
			want: []string{lineSearching, "Last tag is 1.2.3"},
		},
		"named package without its tags": {
			name: foo, tags: []string{tagV123}, current: v123,
			want: []string{lineSearching, lineNoTagsFoo, lineNoChangelg},
		},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			p := preparedPackage(t, tc.name, tc.tags, tc.current, tc.changelog)
			tr, lines := capture()
			p.FindPreparedRelease(tc.tags, tr)
			checkLines(t, lines(), tc.want)
		})
	}
}
