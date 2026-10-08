// Package releasenotes holds the release-note configuration of a package: the changelog sections and the
// conversion from the configuration's extra sections. It mirrors knope-versioning's release_notes::config and
// knope-config's changelog_section. The package is pure.
package releasenotes

import "github.com/celest-io/remeris/pkg/versioning/changes"

// Section is one changelog section: a name and the change types it lists, in order.
type Section struct {
	Name    string
	Sources []changes.ChangeType
}

// Sections is the ordered list of changelog sections of a package. Rendering (F12.2) iterates it in order and
// selects the changes whose type is in a section's Sources.
type Sections []Section

// Names of the default sections and the footer of the default "Notes" section.
const (
	breakingChangesName = "Breaking Changes"
	featuresName        = "Features"
	fixesName           = "Fixes"
	notesName           = "Notes"
	notesFooter         = "Changelog-Note"
)

// DefaultSections returns the default sections (F12.1), in order: Breaking Changes, Features, Fixes and Notes
// (the footer "Changelog-Note"). The result is a fresh value the caller may modify.
func DefaultSections() Sections {
	return Sections{
		{Name: breakingChangesName, Sources: []changes.ChangeType{{Kind: changes.Breaking}}},
		{Name: featuresName, Sources: []changes.ChangeType{{Kind: changes.Feature}}},
		{Name: fixesName, Sources: []changes.ChangeType{{Kind: changes.Fix}}},
		{Name: notesName, Sources: []changes.ChangeType{changes.FooterType(notesFooter)}},
	}
}

// ChangelogSectionConfig is one entry of a package's extra_changelog_sections in knope.toml (F2.2). The
// configuration PR decodes it.
type ChangelogSectionConfig struct {
	Name string
	// Footers are commit footer tokens listed in the section.
	Footers []string
	// Types are change file types listed in the section; "major", "minor" and "patch" mean the built-in types.
	Types []string
}

// typeFromName is Knope's changesets::ChangeType::from(String): exactly "major", "minor" and "patch" (lower case)
// are the built-ins, anything else is a custom change type.
func typeFromName(name string) changes.ChangeType {
	switch name {
	case "major":
		return changes.ChangeType{Kind: changes.Breaking}
	case "minor":
		return changes.ChangeType{Kind: changes.Feature}
	case "patch":
		return changes.ChangeType{Kind: changes.Fix}
	default:
		return changes.CustomChangeType(name)
	}
}

// ConvertSections builds the sections of a package from its extra sections (F12.1), like Knope's
// convert_to_versioning. For each extra section, in order, every default whose source the section lists is
// removed (the source moves to the extra section), then every default with the same name is merged: its source is
// appended to the section's and the default is removed. The result is the remaining defaults, in their original
// order, followed by the extra sections: custom sections always come after the defaults. Source comparison is
// exact (case-sensitive), as in Knope.
func ConvertSections(extra []ChangelogSectionConfig) Sections {
	type entry struct {
		name   string
		source changes.ChangeType
	}
	var defaults []entry
	for _, s := range DefaultSections() {
		defaults = append(defaults, entry{s.Name, s.Sources[0]})
	}
	custom := make(Sections, 0, len(extra))
	for _, cfg := range extra {
		sources := make([]changes.ChangeType, 0, len(cfg.Footers)+len(cfg.Types))
		for _, f := range cfg.Footers {
			sources = append(sources, changes.FooterType(f))
		}
		for _, t := range cfg.Types {
			sources = append(sources, typeFromName(t))
		}
		var kept []entry
		for _, d := range defaults {
			if !containsType(sources, d.source) {
				kept = append(kept, d)
			}
		}
		defaults = kept
		for i := 0; i < len(defaults); {
			if defaults[i].name == cfg.Name {
				sources = append(sources, defaults[i].source)
				defaults = append(defaults[:i], defaults[i+1:]...)
				continue
			}
			i++
		}
		custom = append(custom, Section{Name: cfg.Name, Sources: sources})
	}
	out := make(Sections, 0, len(defaults)+len(custom))
	for _, d := range defaults {
		out = append(out, Section{Name: d.name, Sources: []changes.ChangeType{d.source}})
	}
	return append(out, custom...)
}

func containsType(list []changes.ChangeType, t changes.ChangeType) bool {
	for _, item := range list {
		if item == t {
			return true
		}
	}
	return false
}

// ContainsFooter reports whether any section lists the commit footer token as a custom footer source. The
// comparison ignores ASCII case, like Knope's eq_ignore_ascii_case. It implements [changes.FooterLookup].
func (s Sections) ContainsFooter(token string) bool {
	for _, sec := range s {
		for _, src := range sec.Sources {
			if src.Kind == changes.Custom && src.Custom.Kind == changes.CustomFooter &&
				changes.EqualASCIIFold(src.Custom.Name, token) {
				return true
			}
		}
	}
	return false
}

// ChangeTypes returns the sources of every section, flattened in section order. Duplicates are kept.
func (s Sections) ChangeTypes() []changes.ChangeType {
	var out []changes.ChangeType
	for _, sec := range s {
		out = append(out, sec.Sources...)
	}
	return out
}

// ChangesetTypes returns, in section order, the change file types CreateChangeFile offers (F11.4 step 3): Breaking
// is "major", Feature "minor", Fix "patch" and a custom type its name. Footer sources are left out. Duplicates are
// kept, like Knope's flat_map.
func (s Sections) ChangesetTypes() []string {
	var out []string
	for _, t := range s.ChangeTypes() {
		switch {
		case t.Kind == changes.Breaking:
			out = append(out, "major")
		case t.Kind == changes.Feature:
			out = append(out, "minor")
		case t.Kind == changes.Fix:
			out = append(out, "patch")
		case t.Custom.Kind == changes.CustomType:
			out = append(out, t.Custom.Name)
		}
	}
	return out
}

var _ changes.FooterLookup = Sections(nil)
