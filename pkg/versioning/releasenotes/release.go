package releasenotes

import (
	"slices"
	"strings"

	"github.com/celest-io/remeris/pkg/versioning/action"
	"github.com/celest-io/remeris/pkg/versioning/changes"
	"github.com/celest-io/remeris/pkg/versioning/semver"
)

// Notes renders the release notes of the changes at header level 1 (F12.2): "## Section" for each section that
// has changes, in the order of sections, and "### summary" for a change with details. Within a section, changes
// without details come first and the order is otherwise kept. A change is listed in every section whose sources
// contain its type (exact comparison). Each change is rendered by the first change template that applies, or by
// the default "- summary" or "### summary\n\ndetails". The result is empty when no section has a change.
func Notes(list []changes.Change, sections Sections, templates []ChangeTemplate) string {
	var sb strings.Builder
	for _, sec := range sections {
		var selected []changes.Change
		for _, c := range list {
			if slices.Contains(sec.Sources, c.Type) {
				selected = append(selected, c)
			}
		}
		if len(selected) == 0 {
			continue
		}
		slices.SortStableFunc(selected, compareDetails)
		if sb.Len() > 0 {
			sb.WriteString("\n\n")
		}
		sb.WriteString("## " + sec.Name + "\n\n")
		for i, c := range selected {
			sb.WriteString(renderChange(c, templates))
			if i+1 < len(selected) {
				if selected[i+1].HasDetails() {
					sb.WriteString("\n\n")
				} else {
					sb.WriteString("\n")
				}
			}
		}
	}
	return sb.String()
}

// compareDetails is Knope's Ord for Change: a change without details sorts before one with details.
func compareDetails(a, b changes.Change) int {
	switch {
	case !a.HasDetails() && b.HasDetails():
		return -1
	case a.HasDetails() && !b.HasDetails():
		return 1
	default:
		return 0
	}
}

func renderChange(c changes.Change, templates []ChangeTemplate) string {
	for _, t := range templates {
		if s, ok := t.Render(c); ok {
			return s
		}
	}
	if c.Details != nil {
		return "### " + c.Summary + "\n\n" + *c.Details
	}
	return "- " + c.Summary
}

// Title returns the title of a release without a header prefix: "{version} ({date})". date is "YYYY-MM-DD" (UTC
// today); the caller supplies it so that the package stays pure.
func Title(v semver.Version, date string) string {
	return v.String() + " (" + date + ")"
}

// CreateRelease builds the release of version v (F12.2): its title ([Title]) and its notes ([Notes]). pkg is the
// custom package name, nil for the default package. Writing the changelog is done elsewhere.
func CreateRelease(v semver.Version, date string, list []changes.Change, sections Sections, templates []ChangeTemplate,
	pkg *string) action.Release {
	return action.Release{
		Title:       Title(v, date),
		Version:     v,
		Notes:       Notes(list, sections, templates),
		PackageName: pkg,
	}
}
