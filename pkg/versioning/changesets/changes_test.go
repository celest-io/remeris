package changesets_test

import (
	"reflect"
	"testing"

	"github.com/celest-io/remeris/pkg/versioning/changes"
	"github.com/celest-io/remeris/pkg/versioning/changesets"
)

const (
	idA    = "a"
	idB    = "b"
	idUp   = "Z"
	titleT = "# T"
	bodyD  = "# T\n\nd"
)

var (
	fixType     = changes.ChangeType{Kind: changes.Fix}
	featureType = changes.ChangeType{Kind: changes.Feature}
)

func strPtr(s string) *string { return &s }

func csMinor() changesets.ChangeType { return changesets.ChangeType{Kind: changesets.Minor} }
func csPatch() changesets.ChangeType { return changesets.ChangeType{Kind: changesets.Patch} }
func csMajor() changesets.ChangeType { return changesets.ChangeType{Kind: changesets.Major} }

func TestToChange(t *testing.T) {
	t.Parallel()
	tests := map[string]struct {
		summary     string
		typ         changesets.ChangeType
		wantSummary string
		wantDetails *string
		wantType    changes.ChangeType
	}{
		// Ported from Knope's simple_changeset and complex_changeset.
		"knope simple": {"# a feature\n\n\n\n", csMinor(), "a feature", nil, featureType},
		"knope complex": {
			"# a feature\n\nwith details\n\n- first\n- second", csMinor(), "a feature",
			strPtr("with details\n\n- first\n- second"), featureType,
		},
		"title and paragraphs": {
			"# Title\n\nFirst.\n\nSecond.", csMajor(), "Title", strPtr("First.\n\nSecond."),
			changes.ChangeType{Kind: changes.Breaking},
		},
		"several hashes":      {"## Title\n\nbody", csPatch(), "Title", strPtr("body"), fixType},
		"hashes and spaces":   {"#  # x", csPatch(), "x", nil, fixType},
		"no heading":          {"plain\nmore", csPatch(), "plain", strPtr("more"), fixType},
		"leading blanks":      {"\n\n  \n" + bodyD, csPatch(), "T", strPtr("d"), fixType},
		"only a heading":      {"#", csPatch(), "", nil, fixType},
		"empty":               {"", csPatch(), "", nil, fixType},
		"trailing whitespace": {"# T  \n\nd  \n\n  ", csPatch(), "T  ", strPtr("d"), fixType},
		"tab is kept":         {"#\tT", csPatch(), "\tT", nil, fixType},
		"whitespace line":     {"# T\n\n \nx", csPatch(), "T", strPtr(" \nx"), fixType},
		"crlf":                {"# T\r\n\r\nd\r\ne", csPatch(), "T", strPtr("d\ne"), fixType},
		"custom": {
			titleT, changesets.ChangeType{Kind: changesets.Custom, Custom: "docs"}, "T", nil, changes.CustomChangeType("docs"),
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			got := changesets.ToChange(changesets.PackageRelease{ID: "1234", Type: tt.typ, Summary: tt.summary}, nil)
			if got.Summary != tt.wantSummary {
				t.Errorf("summary = %q, want %q", got.Summary, tt.wantSummary)
			}
			if !reflect.DeepEqual(got.Details, tt.wantDetails) {
				t.Errorf("details = %v, want %v", got.Details, tt.wantDetails)
			}
			if got.Type != tt.wantType {
				t.Errorf("type = %v, want %v", got.Type, tt.wantType)
			}
			if want := (changes.Origin{Kind: changes.ChangeFile, ID: "1234"}); got.Origin != want {
				t.Errorf("origin = %v, want %v", got.Origin, want)
			}
			if got.Origin.String() != "changeset 1234.md" {
				t.Errorf("origin string = %q", got.Origin.String())
			}
			if got.GitInfo != nil {
				t.Errorf("git info = %v, want nil", got.GitInfo)
			}
		})
	}
}

func TestPackageChanges(t *testing.T) {
	t.Parallel()
	set := changesets.FromChanges([]changesets.Change{
		{ID: idB, Versioning: []changesets.PackageChange{{Package: idA, Type: csMinor()}}, Summary: "# B"},
		{ID: idA, Versioning: []changesets.PackageChange{
			{Package: idA, Type: csPatch()}, {Package: changesets.DefaultPackageName, Type: csMajor()},
		}, Summary: "# A"},
		{ID: idUp, Versioning: []changesets.PackageChange{
			{Package: idA, Type: changesets.ChangeType{Kind: changesets.Custom, Custom: "docs"}},
		}, Summary: "# Z"},
	})
	git := &changes.GitInfo{Hash: "abcdef0", AuthorName: "Bob"}
	sorted := []string{idUp, idA, idB}

	tests := map[string]struct {
		name      string
		gitInfo   map[string]*changes.GitInfo
		wantOrder []string
		wantGit   map[string]*changes.GitInfo
	}{
		"sorted, uppercase first": {name: idA, wantOrder: sorted},
		"git info by path": {
			name: idA, gitInfo: map[string]*changes.GitInfo{".changeset/a.md": git},
			wantOrder: sorted, wantGit: map[string]*changes.GitInfo{idA: git},
		},
		"git info for other file": {
			name: idA, gitInfo: map[string]*changes.GitInfo{"a.md": git}, wantOrder: sorted,
		},
		"default package": {name: changesets.DefaultPackageName, wantOrder: []string{idA}},
		"not present":     {name: "missing"},
		"case matters":    {name: "A"},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			var order []string
			for _, c := range changesets.PackageChanges(set, tt.name, tt.gitInfo) {
				order = append(order, c.Origin.ID)
				if c.GitInfo != tt.wantGit[c.Origin.ID] {
					t.Errorf("git info of %s = %v, want %v", c.Origin.ID, c.GitInfo, tt.wantGit[c.Origin.ID])
				}
			}
			if !reflect.DeepEqual(order, tt.wantOrder) {
				t.Errorf("order = %v, want %v", order, tt.wantOrder)
			}
		})
	}
}

func TestPath(t *testing.T) {
	t.Parallel()
	if got := changesets.Path("my_change"); got != ".changeset/my_change.md" {
		t.Errorf("Path = %q", got)
	}
}
