// Package variables implements templates and the variables substituted into them (spec F4.4).
package variables

import (
	"strings"

	"github.com/celest-io/remeris/internal/diag"
	"github.com/celest-io/remeris/internal/workflow"
	"github.com/celest-io/remeris/pkg/versioning"
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
// Version needs exactly one package and is that package's latest version (spec F4.4). A nil scope means the
// single-package rules; a scope is the hook for per-package variables (Phase 2). ChangelogEntry applies the same
// package rules, then fails until the changelog lands (milestone M5). There is no selected issue yet.
func Resolve(state workflow.State, v Variable, _ *PackageScope) (string, error) {
	switch v {
	case Version:
		pkg, err := onlyPackage(state)
		if err != nil {
			return "", err
		}
		latest, ok := pkg.LatestVersion()
		if !ok {
			return "", diag.New("Could not determine version of package").WithCode("remeris::variables::no_version")
		}
		return latest.String(), nil
	case ChangelogEntry:
		if _, err := onlyPackage(state); err != nil {
			return "", err
		}
		return "", diag.New("The ChangelogEntry variable is not implemented yet (milestone M5)").
			WithCode("remeris::not_implemented")
	case IssueBranch:
		return "", diag.New("No issue selected").
			WithCode("remeris::no_issue_selected").
			WithHelp("You must select an issue with a Select*Issue step before using the IssueBranch variable.")
	}
	return "", diag.Newf("Unknown variable %q", string(v))
}

// onlyPackage returns the single package of the state, or the error Knope gives for none or several.
func onlyPackage(state workflow.State) (*versioning.Package, error) {
	switch len(state.Packages) {
	case 0:
		return nil, diag.New("No packages to operate on").
			WithCode("remeris::no_defined_packages").
			WithHelp("There must be at least one package for remeris to work with, " +
				"no supported package files were found in this directory.")
	case 1:
		return state.Packages[0], nil
	}
	return nil, diag.New("Too many packages defined").
		WithCode("remeris::variables::too_many_packages").
		WithHelp("The Version and Changelog variables can only be used with a single [package].")
}
