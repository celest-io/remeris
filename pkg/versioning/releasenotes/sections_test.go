package releasenotes_test

import (
	"reflect"
	"testing"

	"github.com/celest-io/remeris/pkg/versioning/changes"
	"github.com/celest-io/remeris/pkg/versioning/releasenotes"
)

const (
	nameFeatures    = "Features"
	nameNotes       = "Notes"
	nameDocs        = "Docs"
	nameExtras      = "Extras"
	typeDocs        = "docs"
	footerNote      = "Changelog-Note"
	footerDoc       = "Doc"
	footerOther     = "Other"
	footerNoteLower = "changelog-note"
	typeMajor       = "major"
	typeMinor       = "minor"
	typePatch       = "patch"
)

var (
	breaking = changes.ChangeType{Kind: changes.Breaking}
	feature  = changes.ChangeType{Kind: changes.Feature}
	fix      = changes.ChangeType{Kind: changes.Fix}
)

type extraList = []releasenotes.ChangelogSectionConfig

func sec(name string, sources ...changes.ChangeType) releasenotes.Section {
	return releasenotes.Section{Name: name, Sources: sources}
}

func defaultsExcept(skip ...string) releasenotes.Sections {
	var out releasenotes.Sections
	for _, s := range releasenotes.DefaultSections() {
		drop := false
		for _, n := range skip {
			drop = drop || n == s.Name
		}
		if !drop {
			out = append(out, s)
		}
	}
	return out
}

func withDefaults(extra ...releasenotes.Section) releasenotes.Sections {
	return append(releasenotes.DefaultSections(), extra...)
}

func TestDefaultSections(t *testing.T) {
	t.Parallel()
	want := releasenotes.Sections{
		sec("Breaking Changes", breaking),
		sec(nameFeatures, feature),
		sec("Fixes", fix),
		sec(nameNotes, changes.FooterType(footerNote)),
	}
	if got := releasenotes.DefaultSections(); !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	// A fresh value each time.
	a := releasenotes.DefaultSections()
	a[0].Name = "x"
	if releasenotes.DefaultSections()[0].Name != "Breaking Changes" {
		t.Fatal("DefaultSections shares state")
	}
}

func TestConvertSections(t *testing.T) {
	t.Parallel()
	tests := map[string]struct {
		extra extraList
		want  releasenotes.Sections
	}{
		"no extras gives the defaults": {want: releasenotes.DefaultSections()},
		"custom section goes after the defaults": {
			extra: extraList{{Name: nameDocs, Types: []string{typeDocs}}},
			want:  withDefaults(sec(nameDocs, changes.CustomChangeType(typeDocs))),
		},
		"footers come before types": {
			extra: extraList{{Name: nameDocs, Types: []string{typeDocs}, Footers: []string{footerDoc, footerOther}}},
			want: withDefaults(sec(nameDocs,
				changes.FooterType(footerDoc), changes.FooterType(footerOther), changes.CustomChangeType(typeDocs))),
		},
		"overriding the default footer moves it": {
			extra: extraList{{Name: nameExtras, Footers: []string{footerNote}}},
			want:  append(defaultsExcept(nameNotes), sec(nameExtras, changes.FooterType(footerNote))),
		},
		"types major maps to breaking and moves it": {
			extra: extraList{{Name: "Big", Types: []string{typeMajor}}},
			want:  append(defaultsExcept("Breaking Changes"), sec("Big", breaking)),
		},
		"types minor and patch map to the built-ins": {
			extra: extraList{{Name: "Small", Types: []string{typeMinor, typePatch}}},
			want:  append(defaultsExcept(nameFeatures, "Fixes"), sec("Small", feature, fix)),
		},
		"type names are case sensitive": {
			extra: extraList{{Name: "Big", Types: []string{"Major"}}},
			want:  withDefaults(sec("Big", changes.CustomChangeType("Major"))),
		},
		"footer source comparison is case sensitive": {
			extra: extraList{{Name: nameExtras, Footers: []string{footerNoteLower}}},
			want:  withDefaults(sec(nameExtras, changes.FooterType(footerNoteLower))),
		},
		"a footer named major stays a footer": {
			extra: extraList{{Name: nameExtras, Footers: []string{typeMajor}}},
			want:  withDefaults(sec(nameExtras, changes.FooterType(typeMajor))),
		},
		"same name as a default absorbs its source": {
			extra: extraList{{Name: nameFeatures, Types: []string{typeDocs}}},
			want:  append(defaultsExcept(nameFeatures), sec(nameFeatures, changes.CustomChangeType(typeDocs), feature)),
		},
		"override and merge together": {
			extra: extraList{{Name: nameNotes, Footers: []string{footerOther}, Types: []string{typeMinor}}},
			want: append(defaultsExcept(nameFeatures, nameNotes),
				sec(nameNotes, changes.FooterType(footerOther), feature, changes.FooterType(footerNote))),
		},
		"merging a source that is also overridden does not duplicate it": {
			extra: extraList{{Name: nameFeatures, Types: []string{typeMinor}}},
			want:  append(defaultsExcept(nameFeatures), sec(nameFeatures, feature)),
		},
		"a later section lists a source already taken": {
			extra: extraList{{Name: "A", Types: []string{typeMinor}}, {Name: "B", Types: []string{typeMinor}}},
			want:  append(defaultsExcept(nameFeatures), sec("A", feature), sec("B", feature)),
		},
		"duplicate section names are kept as separate sections": {
			extra: extraList{{Name: nameDocs, Types: []string{typeDocs}}, {Name: nameDocs, Types: []string{"more"}}},
			want: withDefaults(
				sec(nameDocs, changes.CustomChangeType(typeDocs)), sec(nameDocs, changes.CustomChangeType("more"))),
		},
		"duplicate sources inside a section are kept": {
			extra: extraList{{Name: nameDocs, Types: []string{typeDocs, typeDocs}}},
			want:  withDefaults(sec(nameDocs, changes.CustomChangeType(typeDocs), changes.CustomChangeType(typeDocs))),
		},
		"empty section is kept": {
			extra: extraList{{Name: "Empty"}},
			want:  withDefaults(releasenotes.Section{Name: "Empty", Sources: []changes.ChangeType{}}),
		},
		"all defaults overridden leaves only the custom section": {
			extra: extraList{{Name: "All", Types: []string{typeMajor, typeMinor, typePatch}, Footers: []string{footerNote}}},
			want:  releasenotes.Sections{sec("All", changes.FooterType(footerNote), breaking, feature, fix)},
		},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			got := releasenotes.ConvertSections(tc.extra)
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("got  %v\nwant %v", got, tc.want)
			}
		})
	}
}

func TestContainsFooter(t *testing.T) {
	t.Parallel()
	moved := releasenotes.ConvertSections(extraList{{Name: "X", Footers: []string{footerNote}}})
	custom := releasenotes.ConvertSections(extraList{{Name: "X", Footers: []string{footerDoc}}})
	overridden := releasenotes.ConvertSections(extraList{{Name: "X", Types: []string{"a"}}})
	tests := map[string]struct {
		sections releasenotes.Sections
		token    string
		want     bool
	}{
		"default note footer":       {releasenotes.DefaultSections(), footerNote, true},
		"ASCII case is ignored":     {releasenotes.DefaultSections(), "changelog-NOTE", true},
		"unknown footer":            {releasenotes.DefaultSections(), footerOther, false},
		"prefix is not a match":     {releasenotes.DefaultSections(), "Changelog", false},
		"Kelvin sign is not folded": {releasenotes.Sections{sec("S", changes.FooterType("k"))}, "K", false},
		"type with the same name": {
			releasenotes.Sections{sec("S", changes.CustomChangeType(footerDoc))},
			footerDoc,
			false,
		},
		"built-in types are not footers": {releasenotes.Sections{sec("S", breaking, feature, fix)}, typeMajor, false},
		"default kept by unrelated type": {overridden, footerNoteLower, true},
		"custom footer":                  {custom, "DOC", true},
		"nil sections":                   {nil, footerNote, false},
		"footer moved to an extra":       {moved, footerNoteLower, true},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if got := tc.sections.ContainsFooter(tc.token); got != tc.want {
				t.Fatalf("ContainsFooter(%q) = %v, want %v", tc.token, got, tc.want)
			}
		})
	}
}

func TestChangeTypes(t *testing.T) {
	t.Parallel()
	tests := map[string]struct {
		sections releasenotes.Sections
		wantAll  []changes.ChangeType
		wantFile []string
	}{
		"defaults leave the footer out of the change file types": {
			sections: releasenotes.DefaultSections(),
			wantAll:  []changes.ChangeType{breaking, feature, fix, changes.FooterType(footerNote)},
			wantFile: []string{typeMajor, typeMinor, typePatch},
		},
		"custom sections follow the defaults in order": {
			sections: releasenotes.ConvertSections(extraList{
				{Name: nameDocs, Footers: []string{footerDoc}, Types: []string{typeDocs, typeMajor}},
			}),
			wantAll: []changes.ChangeType{feature, fix, changes.FooterType(footerNote),
				changes.FooterType(footerDoc), changes.CustomChangeType(typeDocs), breaking},
			wantFile: []string{typeMinor, typePatch, typeDocs, typeMajor},
		},
		"duplicates are kept": {
			sections: releasenotes.Sections{sec("A", fix), sec("B", fix)},
			wantAll:  []changes.ChangeType{fix, fix},
			wantFile: []string{typePatch, typePatch},
		},
		"nil": {},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if got := tc.sections.ChangeTypes(); !reflect.DeepEqual(got, tc.wantAll) {
				t.Errorf("ChangeTypes = %v, want %v", got, tc.wantAll)
			}
			if got := tc.sections.ChangesetTypes(); !reflect.DeepEqual(got, tc.wantFile) {
				t.Errorf("ChangesetTypes = %v, want %v", got, tc.wantFile)
			}
		})
	}
}
