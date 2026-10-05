// Package variables implements templates and the variables substituted into them (spec F4.4).
package variables

import (
	"strings"

	"github.com/celest-io/remeris/internal/diag"
	"github.com/celest-io/remeris/internal/workflow"
)

// Variable names a value remeris can compute. Exactly these three exist.
type Variable string

const (
	Version        Variable = "Version"
	ChangelogEntry Variable = "ChangelogEntry"
	IssueBranch    Variable = "IssueBranch"
)

// Valid reports whether v is one of the known variables.
func (v Variable) Valid() bool {
	switch v {
	case Version, ChangelogEntry, IssueBranch:
		return true
	}
	return false
}

// Entry binds a key found in a template to the variable that replaces it.
type Entry struct {
	Key      string
	Variable Variable
}

// defaults apply to templates that declare no `variables` key at all.
var defaults = []Entry{
	{Key: "$version", Variable: Version},
	{Key: "$changelog", Variable: ChangelogEntry},
}

// Template is text with keys to replace. Variables is kept in declaration order, which is the
// replacement order.
//
// A nil Variables means the key was absent, and the default variables apply. An empty, non-nil
// Variables (`variables = {}`) turns substitution off.
type Template struct {
	Text      string  `toml:"template"`
	Variables []Entry `toml:"variables"`
}

// Replace returns the template text with every key replaced by its variable's value.
//
// Entries are applied in declared order. An entry whose key is not in the current text is skipped,
// so an unused variable never fails, even when it could not be resolved. Later replacements see
// the output of earlier ones.
func (t Template) Replace(state workflow.State) (string, error) {
	entries := t.Variables
	if entries == nil {
		entries = defaults
	}
	text := t.Text
	for _, e := range entries {
		if !strings.Contains(text, e.Key) {
			continue
		}
		value, err := Resolve(state, e.Variable, nil)
		if err != nil {
			return "", err
		}
		text = strings.ReplaceAll(text, e.Key, value)
	}
	return text, nil
}

// PackageScope selects the package a variable is resolved for. It is the hook for per-package
// variables (Phase 2, E4); until then Resolve is always called with a nil scope, which means the
// single-package rules.
type PackageScope struct{}

// Resolve computes the value of v.
//
// Milestone M0 has no packages and no selected issue, so every variable fails with the error it
// would give in that situation.
func Resolve(_ workflow.State, v Variable, _ *PackageScope) (string, error) {
	switch v {
	case Version, ChangelogEntry:
		return "", diag.New("No packages to operate on").
			WithCode("remeris::no_defined_packages").
			WithHelp("There must be at least one package for remeris to work with, " +
				"no supported package files were found in this directory.")
	case IssueBranch:
		return "", diag.New("No issue selected").
			WithCode("remeris::no_issue_selected").
			WithHelp("You must select an issue with a Select*Issue step before using the IssueBranch variable.")
	}
	return "", diag.Newf("Unknown variable %q", string(v))
}
