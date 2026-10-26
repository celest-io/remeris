package semver

import "github.com/celest-io/remeris/pkg/versioning/trace"

// StableRule is a bump rule that only applies to stable versions. The values are ordered, so the larger one
// wins when several changes imply different rules.
type StableRule int

// The stable rules, from the weakest to the strongest. The zero value is StablePatch, Knope's default.
const (
	StablePatch StableRule = iota
	StableMinor
	StableMajor
)

// String returns the upper-case name Knope prints in verbose logs: MAJOR, MINOR or PATCH.
func (r StableRule) String() string {
	switch r {
	case StableMajor:
		return "MAJOR"
	case StableMinor:
		return "MINOR"
	default:
		return "PATCH"
	}
}

// MaxStableRule returns the strongest of the rules, or StablePatch when there are none.
func MaxStableRule(rules ...StableRule) StableRule {
	highest := StablePatch
	for _, r := range rules {
		highest = max(highest, r)
	}
	return highest
}

// RuleKind tells which variant a Rule is.
type RuleKind int

// The rule kinds. RuleStable is the zero value.
const (
	// RuleStable bumps a stable version using Rule.Stable.
	RuleStable RuleKind = iota
	// RulePre creates or bumps a pre-release with Rule.Label, using Rule.Stable to pick the next stable version.
	RulePre
	// RuleRelease turns the latest pre-release into its stable version.
	RuleRelease
)

// Rule is a version bump rule. Build one with StableBump, PreBump or ReleaseBump.
type Rule struct {
	Kind RuleKind
	// Stable is the rule for RuleStable, and the base rule for RulePre.
	Stable StableRule
	// Label is the pre-release label, for RulePre only.
	Label string
}

// StableBump returns the rule that bumps to the next stable version.
func StableBump(r StableRule) Rule { return Rule{Kind: RuleStable, Stable: r} }

// PreBump returns the rule that bumps to a pre-release with the label, based on the stable rule r.
func PreBump(label string, r StableRule) Rule { return Rule{Kind: RulePre, Stable: r, Label: label} }

// ReleaseBump returns the rule that promotes a pre-release to its stable version.
func ReleaseBump() Rule { return Rule{Kind: RuleRelease} }

// BumpStable returns the next stable core for the rule. While major is 0, Knope shifts the rules down one
// level: Major bumps minor, and Minor and Patch bump patch.
func BumpStable(c Core, r StableRule) Core { return bumpStable(c, r, nil) }

// bumpStable is BumpStable with Knope's debug lines for the tracer t, which may be nil.
func bumpStable(c Core, r StableRule, t *trace.Tracer) Core {
	effective := r
	if c.Major == 0 && r != StablePatch {
		effective--
	}
	var next Core
	switch effective {
	case StableMajor:
		next = Core{Major: c.Major + 1}
	case StableMinor:
		next = Core{Major: c.Major, Minor: c.Minor + 1}
	default:
		next = Core{Major: c.Major, Minor: c.Minor, Patch: c.Patch + 1}
	}
	if !t.Enabled() {
		return next
	}
	span := " from " + c.String() + " to " + next.String()
	switch {
	case effective == r:
		t.Debug("Using " + r.String() + " rule to bump" + span)
	case r == StableMajor:
		t.Debug("Rule is MAJOR, but major component is 0. Bumping minor component" + span)
	default:
		t.Debug("Rule is MINOR, but major component is 0. Bumping patch component" + span)
	}
	return next
}
