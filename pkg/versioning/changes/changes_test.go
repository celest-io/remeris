package changes_test

import (
	"testing"

	"github.com/celest-io/remeris/pkg/versioning/changes"
	"github.com/celest-io/remeris/pkg/versioning/semver"
)

func TestChangeTypeRule(t *testing.T) {
	t.Parallel()
	tests := map[string]struct {
		typ  changes.ChangeType
		want semver.StableRule
	}{
		"breaking":      {changes.ChangeType{Kind: changes.Breaking}, semver.StableMajor},
		"feature":       {changes.ChangeType{Kind: changes.Feature}, semver.StableMinor},
		"fix":           {changes.ChangeType{Kind: changes.Fix}, semver.StablePatch},
		"custom footer": {changes.FooterType("Changelog-Note"), semver.StablePatch},
		"custom type":   {changes.CustomChangeType("docs"), semver.StablePatch},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if got := tt.typ.Rule(); got != tt.want {
				t.Errorf("Rule() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestRuleString(t *testing.T) {
	t.Parallel()
	got := changes.ChangeType{Kind: changes.Breaking}.Rule().String()
	if got != "MAJOR" {
		t.Errorf("got %q", got)
	}
}

func TestOriginString(t *testing.T) {
	t.Parallel()
	tests := map[string]struct {
		origin changes.Origin
		want   string
	}{
		"commit": {
			changes.Origin{Kind: changes.ConventionalCommit, Description: "fix: a bug"},
			"commit fix: a bug",
		},
		"change file": {changes.Origin{Kind: changes.ChangeFile, ID: "my_change"}, "changeset my_change.md"},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if got := tt.origin.String(); got != tt.want {
				t.Errorf("String() = %q, want %q", got, tt.want)
			}
		})
	}
}

const scopeText = "scope"

func TestEqualASCIIFold(t *testing.T) {
	t.Parallel()
	tests := map[string]struct {
		a, b string
		want bool
	}{
		"same":             {scopeText, scopeText, true},
		"case":             {"Scope", "sCOPE", true},
		"different":        {scopeText, "other", false},
		"length":           {scopeText, "scopes", false},
		"kelvin sign":      {"K", "k", false},
		"non ascii exact":  {"é", "é", true},
		"non ascii folded": {"É", "é", false},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if got := changes.EqualASCIIFold(tt.a, tt.b); got != tt.want {
				t.Errorf("EqualASCIIFold(%q, %q) = %v, want %v", tt.a, tt.b, got, tt.want)
			}
		})
	}
}

func TestHasDetails(t *testing.T) {
	t.Parallel()
	details := "more"
	if (changes.Change{}).HasDetails() || !(changes.Change{Details: &details}).HasDetails() {
		t.Error("HasDetails is wrong")
	}
}
