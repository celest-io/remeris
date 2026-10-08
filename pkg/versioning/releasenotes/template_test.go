package releasenotes_test

import (
	"testing"

	"github.com/celest-io/remeris/pkg/versioning/changes"
	"github.com/celest-io/remeris/pkg/versioning/releasenotes"
)

const (
	atLogin     = "@$pr_author_login"
	hashPR      = "#$pr_number"
	varDetailsT = "$details"
	varLoginT   = "$pr_author_login"
)

func TestChangeTemplateRender(t *testing.T) {
	t.Parallel()
	full := change(feature, "sum", str("det"), gitInfo("Ann", "abc1234", intp(7), str("ann")))
	noGit := change(feature, "sum", str("det"), nil)
	noDetails := change(feature, "sum", nil, gitInfo("Ann", "abc1234", intp(7), str("ann")))
	gitOnly := change(feature, "sum", nil, gitInfo("Ann", "abc1234", nil, nil))
	tests := map[string]struct {
		template string
		change   changes.Change
		want     string
		ok       bool
	}{
		"summary only":                {"* $summary", noGit, "* sum", true},
		"summary replaced everywhere": {"$summary $summary", noGit, "sum sum", true},
		"no variable":                 {"plain", noGit, "plain", true},
		"unknown variable kept":       {"$summary $other", noGit, "sum $other", true},
		"all variables": {
			"$summary $details $commit_author_name $commit_hash $pr_number $pr_author_login",
			full,
			"sum det Ann abc1234 7 ann",
			true,
		},
		"author with git":                     {"$commit_author_name", gitOnly, "Ann", true},
		"author without git":                  {"$commit_author_name", noGit, "", false},
		"hash with git":                       {"$commit_hash", gitOnly, "abc1234", true},
		"hash without git":                    {"$commit_hash", noGit, "", false},
		"login present":                       {atLogin, full, "@ann", true},
		"login absent with git":               {atLogin, gitOnly, "", false},
		"login absent without git":            {atLogin, noGit, "", false},
		"pr number present":                   {hashPR, full, "#7", true},
		"pr number absent with git":           {hashPR, gitOnly, "", false},
		"pr number without git":               {hashPR, noGit, "", false},
		"details present":                     {varDetailsT, noGit, "det", true},
		"details absent":                      {varDetailsT, noDetails, "", false},
		"one failing variable fails":          {"$summary $details", noDetails, "", false},
		"details containing summary variable": {varDetailsT, change(feature, "S", str("x $summary"), nil), "x S", true},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			got, ok := releasenotes.ChangeTemplate(tt.template).Render(tt.change)
			if got != tt.want || ok != tt.ok {
				t.Errorf("Render() = %q, %v; want %q, %v", got, ok, tt.want, tt.ok)
			}
		})
	}
}

func TestForgeVariable(t *testing.T) {
	t.Parallel()
	tests := map[string]struct {
		templates []string
		want      string
	}{
		"none":                  {nil, ""},
		"local only":            {[]string{"* $summary by $commit_author_name"}, ""},
		"pr number":             {[]string{"* $summary in #$pr_number"}, "$pr_number"},
		"login":                 {[]string{"* $summary by @$pr_author_login"}, varLoginT},
		"login wins in one":     {[]string{"#$pr_number @$pr_author_login"}, varLoginT},
		"first template wins":   {[]string{hashPR, "@$pr_author_login"}, "$pr_number"},
		"later template needed": {[]string{"* $summary", "@$pr_author_login"}, varLoginT},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if got := releasenotes.ForgeVariable(tpls(tt.templates...)); got != tt.want {
				t.Errorf("ForgeVariable() = %q, want %q", got, tt.want)
			}
		})
	}
}
