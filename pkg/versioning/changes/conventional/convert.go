package conventional

import (
	"strings"

	"github.com/celest-io/remeris/pkg/versioning/changes"
)

// CommitMessage is a commit as the conversion needs it: Knope's Commit.
type CommitMessage struct {
	Message string
	// Info is nil when the commit information is unknown.
	Info *changes.GitInfo
}

// Changes converts commits (oldest first) to changes (F10.2), like Knope's changes_from_commit_messages. Messages
// that do not parse are skipped. scopes nil means no scope filter; non-nil empty drops every scoped commit.
// sections may be nil, then no custom footer is listed. The function is pure.
func Changes(commits []CommitMessage, scopes []string, sections changes.FooterLookup) []changes.Change {
	var out []changes.Change
	for _, c := range commits {
		out = append(out, commitChanges(c, scopes, sections)...)
	}
	return out
}

func commitChanges(in CommitMessage, scopes []string, sections changes.FooterLookup) []changes.Change {
	commit, err := Parse(strings.TrimSpace(in.Message))
	if err != nil {
		return nil
	}
	if commit.HasScope && scopes != nil && !changes.ContainsASCIIFold(scopes, commit.Scope) {
		return nil
	}

	summary := formatSummary(commit)
	var out []changes.Change
	hasBreakingFooter := false
	for _, f := range commit.Footers {
		var typ changes.ChangeType
		switch {
		case f.IsBreaking():
			hasBreakingFooter = true
			typ = changes.ChangeType{Kind: changes.Breaking}
		case sections != nil && sections.ContainsFooter(f.Token):
			typ = changes.FooterType(f.Token)
		default:
			continue
		}
		out = append(out, changes.Change{
			Type:    typ,
			Summary: f.Value,
			GitInfo: in.Info,
			Origin: changes.Origin{
				Kind:        changes.ConventionalCommit,
				Description: summary + "\n\tContaining footer " + f.Token + f.Separator + " " + f.Value,
			},
		})
	}

	var kind changes.Kind
	switch {
	case commit.Breaking && !hasBreakingFooter:
		kind = changes.Breaking
	case EqualFold(commit.Type, "feat"):
		kind = changes.Feature
	case EqualFold(commit.Type, "fix"):
		kind = changes.Fix
	default:
		return out // Only the footers, if any, are changes.
	}
	return append(out, changes.Change{
		Type:    changes.ChangeType{Kind: kind},
		Summary: commit.Description,
		GitInfo: in.Info,
		Origin:  changes.Origin{Kind: changes.ConventionalCommit, Description: summary},
	})
}

// formatSummary is "{type}{(scope)}{!}: {description}". The "!" is only added when the commit is breaking and no
// footer is (a breaking footer is the reason then).
func formatSummary(c Commit) string {
	var b strings.Builder
	b.WriteString(c.Type)
	if c.HasScope {
		b.WriteString("(" + c.Scope + ")")
	}
	if c.Breaking && !hasBreakingFooter(c) {
		b.WriteString("!")
	}
	b.WriteString(": " + c.Description)
	return b.String()
}

func hasBreakingFooter(c Commit) bool {
	for _, f := range c.Footers {
		if f.IsBreaking() {
			return true
		}
	}
	return false
}
