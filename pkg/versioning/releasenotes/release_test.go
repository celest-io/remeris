package releasenotes_test

import (
	"testing"

	"github.com/celest-io/remeris/pkg/versioning/changes"
	"github.com/celest-io/remeris/pkg/versioning/releasenotes"
	"github.com/celest-io/remeris/pkg/versioning/semver"
)

const (
	sushi       = "Sushi"
	sushiTpl    = "* $summary by $commit_author_name ($commit_hash)"
	featuresHdr = "## Features\n\n"
	complexFeat = "a complex feature"
	someDetails = "some details"
	simpleFeat  = "a simple feature"
	bulletTpl   = "* $summary"
	wantTitle   = "1.3.0 (2024-05-06)"
	testDate    = "2024-05-06"
)

func str(s string) *string { return &s }

func intp(n int) *int { return &n }

func change(t changes.ChangeType, summary string, details *string, git *changes.GitInfo) changes.Change {
	return changes.Change{Type: t, Summary: summary, Details: details, GitInfo: git}
}

func gitInfo(author, hash string, pr *int, login *string) *changes.GitInfo {
	return &changes.GitInfo{AuthorName: author, Hash: hash, PRNumber: pr, PRAuthorLogin: login}
}

func tpls(s ...string) []releasenotes.ChangeTemplate {
	out := make([]releasenotes.ChangeTemplate, len(s))
	for i, t := range s {
		out[i] = releasenotes.ChangeTemplate(t)
	}
	return out
}

func TestNotes(t *testing.T) {
	t.Parallel()
	complexChange := change(feature, complexFeat, str(someDetails), nil)
	simple := change(feature, simpleFeat, nil, nil)
	super := change(feature, "a super simple feature", nil, nil)
	sushiGit := gitInfo(sushi, "1234", nil, nil)
	docsType := changes.CustomChangeType("docs")
	tests := map[string]struct {
		changes   []changes.Change
		sections  releasenotes.Sections
		templates []releasenotes.ChangeTemplate
		want      string
	}{
		"simple changes before complex": {
			changes:  []changes.Change{complexChange, simple, super},
			sections: releasenotes.DefaultSections(),
			want: featuresHdr + "- a simple feature\n- a super simple feature\n\n" +
				"### a complex feature\n\nsome details",
		},
		"no changes": {sections: releasenotes.DefaultSections(), want: ""},
		"no change in any section": {
			changes:  []changes.Change{change(docsType, "docs", nil, nil)},
			sections: releasenotes.DefaultSections(),
			want:     "",
		},
		"sections in section order, separated by a blank line": {
			changes: []changes.Change{
				change(fix, "fixed", nil, nil), change(feature, "added", nil, nil), change(breaking, "broke", nil, nil),
			},
			sections: releasenotes.DefaultSections(),
			want:     "## Breaking Changes\n\n- broke\n\n" + featuresHdr + "- added\n\n## Fixes\n\n- fixed",
		},
		"two complex changes are separated by a blank line": {
			changes: []changes.Change{
				change(fix, "one", str("first"), nil), change(fix, "two", str("second"), nil),
			},
			sections: releasenotes.DefaultSections(),
			want:     "## Fixes\n\n### one\n\nfirst\n\n### two\n\nsecond",
		},
		"stable order of equal changes": {
			changes: []changes.Change{
				change(fix, "c", nil, nil), change(fix, "a", str("d1"), nil), change(fix, "b", nil, nil),
				change(fix, "z", str("d2"), nil),
			},
			sections: releasenotes.DefaultSections(),
			want:     "## Fixes\n\n- c\n- b\n\n### a\n\nd1\n\n### z\n\nd2",
		},
		"custom section and footer section": {
			changes: []changes.Change{
				change(docsType, "write docs", nil, nil),
				change(changes.FooterType("Changelog-Note"), "a note", nil, nil),
			},
			sections: releasenotes.ConvertSections([]releasenotes.ChangelogSectionConfig{
				{Name: "Documentation", Types: []string{"docs"}},
			}),
			want: "## Notes\n\n- a note\n\n## Documentation\n\n- write docs",
		},
		"type comparison is exact": {
			changes: []changes.Change{
				change(changes.CustomChangeType("Docs"), "upper", nil, nil),
				change(changes.FooterType("docs"), "footer", nil, nil),
			},
			sections: releasenotes.Sections{{Name: "Documentation", Sources: []changes.ChangeType{docsType}}},
			want:     "",
		},
		"a change in two sections is listed twice": {
			changes: []changes.Change{change(fix, "twice", nil, nil)},
			sections: releasenotes.Sections{
				{Name: "A", Sources: []changes.ChangeType{fix}}, {Name: "B", Sources: []changes.ChangeType{fix}},
			},
			want: "## A\n\n- twice\n\n## B\n\n- twice",
		},
		"custom templates": {
			changes: []changes.Change{
				complexChange, simple, change(feature, "a super simple feature", nil, sushiGit),
			},
			sections: releasenotes.DefaultSections(),
			templates: tpls(
				sushiTpl,
				"###### $summary!!! $notAVariable\n\n$details",
				bulletTpl,
			),
			want: featuresHdr + "* a simple feature\n* a super simple feature by Sushi (1234)\n\n" +
				"###### a complex feature!!! $notAVariable\n\nsome details",
		},
		"fall back to the built-in rendering": {
			changes: []changes.Change{
				complexChange, simple, change(feature, "a super simple feature", nil, sushiGit),
			},
			sections:  releasenotes.DefaultSections(),
			templates: tpls("* $summary by $commit_author_name"),
			want: featuresHdr + "- a simple feature\n* a super simple feature by Sushi\n\n" +
				"### a complex feature\n\nsome details",
		},
		"change files with commit info use commit templates": {
			changes: []changes.Change{
				change(feature, "a committed feature with details", str("some implementation details"),
					gitInfo("Alice", "abc123", nil, nil)),
				change(feature, "a committed simple feature", nil, gitInfo("Bob", "def456", nil, nil)),
				change(feature, "an uncommitted feature with details", str("some more details"), nil),
				change(feature, "an uncommitted simple feature", nil, nil),
				change(feature, "conventional commit feature", nil, gitInfo("Charlie", "ghi789", nil, nil)),
			},
			sections: releasenotes.DefaultSections(),
			templates: tpls(
				"* $summary by $commit_author_name ($commit_hash)\n\n$details",
				sushiTpl,
				"### $summary\n\n$details",
				bulletTpl,
			),
			want: featuresHdr + "* a committed simple feature by Bob (def456)\n* an uncommitted simple feature\n" +
				"* conventional commit feature by Charlie (ghi789)\n\n" +
				"* a committed feature with details by Alice (abc123)\n\nsome implementation details\n\n" +
				"### an uncommitted feature with details\n\nsome more details",
		},
		"github style templates with pr and login": {
			changes: []changes.Change{
				change(feature, "add dark mode", nil, gitInfo("Dale Seo", "abc1234", intp(42), str("DaleSeo"))),
				change(feature, "improve logging", nil, gitInfo("Alice", "def5678", nil, str("alice"))),
				change(feature, "uncommitted feature", nil, nil),
				change(fix, "fix crash", nil, gitInfo("Bob", "ghi9012", nil, nil)),
			},
			sections: releasenotes.DefaultSections(),
			templates: tpls(
				"* $summary by @$pr_author_login in #$pr_number",
				"* $summary by @$pr_author_login",
				bulletTpl,
			),
			want: featuresHdr + "* add dark mode by @DaleSeo in #42\n* improve logging by @alice\n* uncommitted feature\n\n" +
				"## Fixes\n\n* fix crash",
		},
		"knope all_custom fixture": {
			changes: []changes.Change{
				change(
					breaking,
					"A breaking change with the intentionally wrong header",
					str("Some details about the breaking change"),
					gitInfo("Alice", "abc1234", nil, nil),
				),
				change(feature, "A simple feature", nil, nil),
				change(feature, "Feature from commit", nil, gitInfo(sushi, "def5678", nil, nil)),
			},
			sections: releasenotes.DefaultSections(),
			templates: tpls(
				"## $summary by $commit_author_name ($commit_hash)\n\n\n$details",
				sushiTpl,
				"## $summary\n\n\n$details",
				bulletTpl,
			),
			want: "## Breaking Changes\n\n## A breaking change with the intentionally wrong header by Alice (abc1234)\n\n\n" +
				"Some details about the breaking change\n\n" +
				featuresHdr + "* A simple feature\n* Feature from commit by Sushi (def5678)",
		},
		"knope with_fallback fixture": {
			changes: []changes.Change{
				change(breaking, "A breaking change", str("Some details"), nil),
				change(feature, "A simple feature", nil, nil),
				change(feature, "Feature from commit", nil, gitInfo(sushi, "def5678", nil, nil)),
			},
			sections:  releasenotes.DefaultSections(),
			templates: tpls(sushiTpl),
			want: "## Breaking Changes\n\n### A breaking change\n\nSome details\n\n" +
				featuresHdr + "- A simple feature\n* Feature from commit by Sushi (def5678)",
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if got := releasenotes.Notes(tt.changes, tt.sections, tt.templates); got != tt.want {
				t.Errorf("Notes() =\n%q\nwant\n%q", got, tt.want)
			}
		})
	}
}

func TestCreateRelease(t *testing.T) {
	t.Parallel()
	v := semver.MustParse("1.3.0")
	list := []changes.Change{change(feature, "added", nil, nil)}
	tests := map[string]struct {
		pkg  *string
		want string
	}{
		"default package": {pkg: nil, want: wantTitle},
		"named package":   {pkg: str("lib"), want: wantTitle},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			r := releasenotes.CreateRelease(v, testDate, list, releasenotes.DefaultSections(), nil, tt.pkg)
			if r.Title != tt.want || r.Notes != featuresHdr+"- added" || !r.Version.Equal(v) {
				t.Errorf("CreateRelease() = %+v", r)
			}
			if (r.PackageName == nil) != (tt.pkg == nil) || (tt.pkg != nil && *r.PackageName != *tt.pkg) {
				t.Errorf("PackageName = %v, want %v", r.PackageName, tt.pkg)
			}
		})
	}
	t.Run("no changes still makes a release with empty notes", func(t *testing.T) {
		t.Parallel()
		r := releasenotes.CreateRelease(v, testDate, nil, releasenotes.DefaultSections(), nil, nil)
		if r.Notes != "" || r.Title != wantTitle {
			t.Errorf("CreateRelease() = %+v", r)
		}
	})
}
