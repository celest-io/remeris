package steps

import (
	"context"

	"github.com/celest-io/remeris/internal/workflow"
	"github.com/celest-io/remeris/pkg/versioning"
	"github.com/celest-io/remeris/pkg/versioning/action"
	"github.com/celest-io/remeris/pkg/versioning/semver"
	"github.com/celest-io/remeris/pkg/versioning/versionedfile"
)

// BumpVersionRule is the `rule` of a BumpVersion step.
type BumpVersionRule string

// The rules a BumpVersion step accepts.
const (
	RuleMajor   BumpVersionRule = "Major"
	RuleMinor   BumpVersionRule = "Minor"
	RulePatch   BumpVersionRule = "Patch"
	RulePre     BumpVersionRule = "Pre"
	RuleRelease BumpVersionRule = "Release"
)

// BumpVersion bumps the version of every package and writes the versioned files (spec F8). It stages nothing
// and writes no changelog.
type BumpVersion struct {
	Rule BumpVersionRule `toml:"rule"`
	// Label is the pre-release label. It is set if and only if Rule is RulePre.
	Label string `toml:"label"`
}

// Type returns "BumpVersion".
func (*BumpVersion) Type() string { return "BumpVersion" }

// NeedsPackages is true: the step works on the packages.
func (*BumpVersion) NeedsPackages() bool { return true }

// SemverRule maps the TOML rule to the versioning core's rule. Pre is based on a patch bump.
func (s *BumpVersion) SemverRule() semver.Rule {
	switch s.Rule {
	case RuleMajor:
		return semver.StableBump(semver.StableMajor)
	case RuleMinor:
		return semver.StableBump(semver.StableMinor)
	case RulePre:
		return semver.PreBump(s.Label, semver.StablePatch)
	case RuleRelease:
		return semver.ReleaseBump()
	default:
		return semver.StableBump(semver.StablePatch)
	}
}

// Run sets the new version in every package, then executes the writes of the versioned files. Files are drained
// from the state afterwards, as in Knope, so a later step does not write them again. With no package it does
// nothing.
func (s *BumpVersion) Run(_ context.Context, rt workflow.RunType[workflow.State]) (workflow.RunType[workflow.State], error) {
	state := rt.State
	rule := s.SemverRule()
	for _, pkg := range state.Packages {
		v, gv, err := nextVersion(pkg, rule)
		if err != nil {
			return rt, err
		}
		if state.AllVersionedFiles, err = pkg.SetVersion(v, gv, state.AllVersionedFiles); err != nil {
			return rt, err
		}
	}

	var actions []action.Action
	for _, f := range state.AllVersionedFiles {
		actions = append(actions, f.Write()...)
	}
	state.AllVersionedFiles = nil

	// The AddTag and CreateRelease actions that come back are dropped, as Knope's semver.rs does: BumpVersion
	// never tags or releases (spec F8), and keeping them would change what a later Release step does.
	if _, err := ExecuteActions(rt.DryRun, actions); err != nil {
		return rt, err
	}
	rt.State = state
	return rt, nil
}

// nextVersion is the version a package gets and how go.mod files treat it. An override is used as given and
// bumps the major version in go.mod files (spec F8).
func nextVersion(pkg *versioning.Package, rule semver.Rule) (semver.Version, versionedfile.GoVersioning, error) {
	if pkg.Override != nil {
		return *pkg.Override, versionedfile.BumpMajor, nil
	}
	v, err := pkg.Versions.CalculateNewVersion(rule)
	return v, pkg.GoVersioning, err
}
