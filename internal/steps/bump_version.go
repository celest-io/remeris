package steps

import (
	"context"

	"github.com/celest-io/remeris/internal/workflow"
)

// BumpVersionRule is the `rule` of a BumpVersion step.
type BumpVersionRule string

const (
	RuleMajor   BumpVersionRule = "Major"
	RuleMinor   BumpVersionRule = "Minor"
	RulePatch   BumpVersionRule = "Patch"
	RulePre     BumpVersionRule = "Pre"
	RuleRelease BumpVersionRule = "Release"
)

// BumpVersion bumps the version of the packages (milestone M3).
type BumpVersion struct {
	Rule BumpVersionRule `toml:"rule"`
	// Label is the pre-release label. It is set if and only if Rule is RulePre.
	Label string `toml:"label"`
}

func (*BumpVersion) Type() string { return "BumpVersion" }

// Run fails until the step is implemented, in a dry run too, so --validate reports it.
func (s *BumpVersion) Run(_ context.Context, rt workflow.RunType[workflow.State]) (workflow.RunType[workflow.State], error) {
	return rt, notImplemented(s, "M3")
}
