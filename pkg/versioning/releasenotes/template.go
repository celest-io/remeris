package releasenotes

import (
	"strconv"
	"strings"

	"github.com/celest-io/remeris/pkg/versioning/changes"
)

// The variables of a change template (F12.3).
const (
	varCommitAuthorName = "$commit_author_name"
	varCommitHash       = "$commit_hash"
	varPRAuthorLogin    = "$pr_author_login"
	varPRNumber         = "$pr_number"
	varDetails          = "$details"
	varSummary          = "$summary"
)

// ChangeTemplate is one entry of [release_notes].change_templates (F12.3): text with the variables $summary,
// $details, $commit_author_name, $commit_hash, $pr_number and $pr_author_login. It mirrors Knope's ChangeTemplate.
// A template replaces the whole default rendering of a change, bullet included.
type ChangeTemplate string

// Render fills the template with the change. It reports false when the template does not apply, that is when it
// contains a variable the change cannot fill: $commit_author_name or $commit_hash without GitInfo,
// $pr_author_login or $pr_number without that PR field, or $details without details.
//
// The substitution is plain text replacement, in Knope's order: author and hash, PR login, PR number, details,
// then summary. Text inserted by an earlier step is not protected from later ones, as in Knope. Unknown variables
// such as "$other" are left as written.
func (t ChangeTemplate) Render(c changes.Change) (string, bool) {
	out := string(t)
	if strings.Contains(out, varCommitAuthorName) || strings.Contains(out, varCommitHash) {
		if c.GitInfo == nil {
			return "", false
		}
		out = strings.ReplaceAll(out, varCommitAuthorName, c.GitInfo.AuthorName)
		out = strings.ReplaceAll(out, varCommitHash, c.GitInfo.Hash)
	}
	if strings.Contains(out, varPRAuthorLogin) {
		if c.GitInfo == nil || c.GitInfo.PRAuthorLogin == nil {
			return "", false
		}
		out = strings.ReplaceAll(out, varPRAuthorLogin, *c.GitInfo.PRAuthorLogin)
	}
	if strings.Contains(out, varPRNumber) {
		if c.GitInfo == nil || c.GitInfo.PRNumber == nil {
			return "", false
		}
		out = strings.ReplaceAll(out, varPRNumber, strconv.Itoa(*c.GitInfo.PRNumber))
	}
	if strings.Contains(out, varDetails) {
		if c.Details == nil {
			return "", false
		}
		out = strings.ReplaceAll(out, varDetails, *c.Details)
	}
	return strings.ReplaceAll(out, varSummary, c.Summary), true
}

// forgeVariable returns the first of $pr_author_login and $pr_number (in that order) the template contains, or "".
func (t ChangeTemplate) forgeVariable() string {
	for _, v := range [...]string{varPRAuthorLogin, varPRNumber} {
		if strings.Contains(string(t), v) {
			return v
		}
	}
	return ""
}

// ForgeVariable returns the first variable that needs forge data (F13): for the first template, in order, that
// contains $pr_author_login or $pr_number, the former if both are present. It returns "" when no template needs
// forge data.
func ForgeVariable(templates []ChangeTemplate) string {
	for _, t := range templates {
		if v := t.forgeVariable(); v != "" {
			return v
		}
	}
	return ""
}
