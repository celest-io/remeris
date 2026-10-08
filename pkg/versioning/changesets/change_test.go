package changesets_test

import (
	"errors"
	"reflect"
	"testing"

	"github.com/celest-io/remeris/pkg/versioning/changesets"
)

const (
	namePackage = "package"
	namePkg     = "pkg"
	msgMissing  = "missing front matter"
	msgInvalid  = "invalid front matter"
	pkgA        = "my_package"
	summaryText = "### This is a summary"
	frontOpen   = "---\n"
)

func patch() changesets.ChangeType { return changesets.ChangeType{Kind: changesets.Patch} }
func minor() changesets.ChangeType { return changesets.ChangeType{Kind: changesets.Minor} }
func major() changesets.ChangeType { return changesets.ChangeType{Kind: changesets.Major} }
func custom(s string) changesets.ChangeType {
	return changesets.ChangeType{Kind: changesets.Custom, Custom: s}
}

func TestNormalizeID(t *testing.T) {
	t.Parallel()
	tests := map[string]struct{ in, want string }{
		"special characters":  {"`[i carry your_heart with-me(i carry it in]`", "i_carry_your_heart_with_mei_carry_it_in"},
		"spec example":        {"`[i carry your heart with me(i carry it in]`", "i_carry_your_heart_with_mei_carry_it_in"},
		"capitalization":      {"This is a Title", "this_is_a_title"},
		"no duplicate unders": {"Something ______ else", "something_else"},
		"leading separators":  {"  -_ abc", "_abc"},
		"trailing separators": {"abc -_ ", "abc_"},
		"mixed separators":    {"a - _ b", "a_b"},
		"non ascii dropped":   {"café ünï", "caf_n"},
		"dropped keeps flag":  {"a (_) b", "a_b"},
		"digits":              {"Release 2.0", "release_20"},
		"empty string":        {"", ""},
		"only symbols":        {"!?.", ""},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			equal(t, changesets.UniqueID(tc.want), changesets.NormalizeID(tc.in))
		})
	}
}

func TestChangeType(t *testing.T) {
	t.Parallel()
	tests := map[string]struct {
		in   string
		want changesets.ChangeType
	}{
		"patch":       {"patch", patch()},
		"minor":       {"minor", minor()},
		"major":       {"major", major()},
		"uppercase":   {"Patch", custom("Patch")},
		"custom":      {"something custom", custom("something custom")},
		"empty value": {"", custom("")},
		"padded one":  {" minor", custom(" minor")},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			got := changesets.ParseChangeType(tc.in)
			equal(t, tc.want, got)
			equal(t, tc.in, got.String())
		})
	}
}

func TestChangeTypeCompare(t *testing.T) {
	t.Parallel()
	equal(t, -1, custom("x").Compare(patch()))
	equal(t, -1, patch().Compare(minor()))
	equal(t, -1, minor().Compare(major()))
	equal(t, 1, major().Compare(custom("x")))
	equal(t, 0, custom("x").Compare(custom("y")))
	equal(t, 0, minor().Compare(minor()))
}

func TestParseChange(t *testing.T) {
	t.Parallel()
	tests := map[string]struct {
		content    string
		versioning []changesets.PackageChange
		summary    string
	}{
		"spaces in package names": {
			content: "---\npackage name: patch\npackage name 2: minor\n---\nThis is a summary\n",
			versioning: []changesets.PackageChange{
				{Package: "package name", Type: patch()},
				{Package: "package name 2", Type: minor()},
			},
			summary: "This is a summary",
		},
		"spaces in change types": {
			content: "---\npackage: custom change type\npackage name 2: something custom\n---\nThis is a summary\n",
			versioning: []changesets.PackageChange{
				{Package: namePackage, Type: custom("custom change type")},
				{Package: "package name 2", Type: custom("something custom")},
			},
			summary: "This is a summary",
		},
		"empty summary without newline": {
			content:    "---\npackage: patch\n---",
			versioning: []changesets.PackageChange{{Package: namePackage, Type: patch()}},
		},
		"empty summary with newline": {
			content:    "---\npackage: patch\n---\n",
			versioning: []changesets.PackageChange{{Package: namePackage, Type: patch()}},
		},
		"split on first colon only": {
			content:    "---\na:b: minor: x\n---\n\nS\n",
			versioning: []changesets.PackageChange{{Package: "a", Type: custom("b: minor: x")}},
			summary:    "S",
		},
		"whitespace trimmed": {
			content:    "  ---  \n\t pkg \t:\t major \t\n \t--- \n\nS",
			versioning: []changesets.PackageChange{{Package: namePkg, Type: major()}},
			summary:    "S",
		},
		"crlf": {
			content:    "---\r\npkg: minor\r\n---\r\n\r\n# Title\r\n\r\nBody\r\n",
			versioning: []changesets.PackageChange{{Package: namePkg, Type: minor()}},
			summary:    "# Title\n\nBody",
		},
		"quotes are kept": {
			content:    "---\n\"pkg\": minor\n---\n\nS\n",
			versioning: []changesets.PackageChange{{Package: `"pkg"`, Type: minor()}},
			summary:    "S",
		},
		"leading blank lines skipped": {
			content:    "---\npkg: patch\n---\n\n \n\t\n\nFirst\n\nSecond\n\n",
			versioning: []changesets.PackageChange{{Package: namePkg, Type: patch()}},
			summary:    "First\n\nSecond\n",
		},
		"front matter order kept": {
			content: "---\nz: patch\na: major\nm: minor\n---\n\nS\n",
			versioning: []changesets.PackageChange{
				{Package: "z", Type: patch()}, {Package: "a", Type: major()}, {Package: "m", Type: minor()},
			},
			summary: "S",
		},
		"fence inside the summary": {
			content:    "---\npkg: patch\n---\n\nS\n---\nmore\n",
			versioning: []changesets.PackageChange{{Package: namePkg, Type: patch()}},
			summary:    "S\n---\nmore",
		},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			got, err := changesets.ParseChange("an_id", tc.content)
			noError(t, err)
			equal(t, changesets.Change{ID: "an_id", Versioning: tc.versioning, Summary: tc.summary}, got)
		})
	}
}

func TestParseChangeErrors(t *testing.T) {
	t.Parallel()
	tests := map[string]struct {
		content string
		is      error
		message string
	}{
		"empty content":       {"", changesets.ErrMissingFrontMatter, msgMissing},
		"missing opening":     {"pkg: patch\n---\n", changesets.ErrMissingFrontMatter, msgMissing},
		"blank first line":    {"\n---\npkg: patch\n---\n", changesets.ErrMissingFrontMatter, msgMissing},
		"missing closing":     {"---\npkg: patch\n", changesets.ErrInvalidFrontMatter, msgInvalid},
		"line without colon":  {"---\npkg patch\n---\n", changesets.ErrInvalidFrontMatter, msgInvalid},
		"blank line in front": {"---\npkg: patch\n\n---\n", changesets.ErrInvalidFrontMatter, msgInvalid},
		"only opening":        {"---", changesets.ErrInvalidFrontMatter, ""},
		"duplicate names": {
			"---\npkg: patch\npkg: minor\n---\n\nS\n",
			changesets.ErrInvalidFrontMatter,
			msgInvalid,
		},
		"empty front matter": {
			"---\n---\n\nS\n",
			changesets.ErrEmptyVersioning,
			"invalid front matter: Versioning needs to contain at least one item.",
		},
		"empty front matter 2": {
			"---\n---",
			changesets.ErrInvalidFrontMatter,
			"invalid front matter: Versioning needs to contain at least one item.",
		},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			_, err := changesets.ParseChange("x", tc.content)
			mustErr(t, err)
			isErr(t, err, tc.is)
			if tc.message != "" {
				equal(t, tc.message, err.Error())
			}
		})
	}
}

func TestChangeString(t *testing.T) {
	t.Parallel()
	tests := map[string]struct {
		change changesets.Change
		want   string
	}{
		"basic programmatic": {
			change: changesets.Change{
				ID:         "basic_programmatic",
				Versioning: []changesets.PackageChange{{Package: pkgA, Type: minor()}},
				Summary:    summaryText,
			},
			want: "---\nmy_package: minor\n---\n\n### This is a summary\n",
		},
		"multiple packages keep given order": {
			change: changesets.Change{
				ID: "multiple_packages",
				Versioning: []changesets.PackageChange{
					{
						Package: pkgA,
						Type:    minor(),
					},
					{Package: "my_other_package", Type: major()},
					{Package: "c", Type: custom("x y")},
				},
				Summary: summaryText,
			},
			want: "---\nmy_package: minor\nmy_other_package: major\nc: x y\n---\n\n### This is a summary\n",
		},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			equal(t, tc.want, tc.change.String())
			equal(t, string(tc.change.ID)+".md", tc.change.FileName())
			got, err := changesets.ParseChange(tc.change.ID, tc.change.String())
			noError(t, err)
			equal(t, tc.change, got)
		})
	}
}

func TestLoadChange(t *testing.T) {
	t.Parallel()
	got, err := changesets.ParseChange("a_change", frontOpen+"my_package: minor\n---\n\n### This is a summary\n")
	noError(t, err)
	equal(t, changesets.UniqueID("a_change"), got.ID)
	equal(t, summaryText, got.Summary)
	equal(t, []changesets.PackageChange{{Package: pkgA, Type: minor()}}, got.Versioning)
}

func equal(t *testing.T, want, got any) {
	t.Helper()
	if !reflect.DeepEqual(want, got) {
		t.Errorf("want %#v, got %#v", want, got)
	}
}

func noError(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func mustErr(t *testing.T, err error) {
	t.Helper()
	if err == nil {
		t.Fatal("expected an error")
	}
}

func isErr(t *testing.T, err, target error) {
	t.Helper()
	if !errors.Is(err, target) {
		t.Errorf("error %v is not %v", err, target)
	}
}
