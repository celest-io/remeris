package semver_test

import (
	"errors"
	"testing"

	"github.com/celest-io/remeris/pkg/versioning/semver"
)

const (
	v023       = "0.2.3"
	v000       = "0.0.0"
	vRc1       = "1.2.3-rc.1"
	lRc        = "rc"
	lRc4       = "rc4"
	lAlphaBeta = "alpha.beta"
	lBeta      = "beta"
	vBeta      = "1.0.0-beta"
	vAlphaBeta = "1.0.0-alpha.beta"
)

func TestBumpStable(t *testing.T) {
	t.Parallel()
	tests := map[string]struct {
		from string
		rule semver.StableRule
		want string
	}{
		"major from 4.1.1":       {"4.1.1", semver.StableMajor, "5.0.0"},
		"minor bump":             {v123, semver.StableMinor, "1.3.0"},
		"patch bump":             {v123, semver.StablePatch, "1.2.4"},
		"major zero":             {v023, semver.StableMajor, "0.3.0"},
		"minor zero":             {v023, semver.StableMinor, "0.2.4"},
		"patch zero":             {v023, semver.StablePatch, "0.2.4"},
		"zero zero zero major":   {v000, semver.StableMajor, "0.1.0"},
		"zero zero zero minor":   {v000, semver.StableMinor, "0.0.1"},
		"major resets lower":     {"3.9.9", semver.StableMajor, "4.0.0"},
		"minor resets patch":     {"3.9.9", semver.StableMinor, "3.10.0"},
		"pre-release is ignored": {vRc1, semver.StablePatch, "1.2.4"},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			got := semver.BumpStable(semver.MustParse(tt.from).Core(), tt.rule)
			if got.String() != tt.want {
				t.Errorf("BumpStable(%s, %v) = %s, want %s", tt.from, tt.rule, got, tt.want)
			}
		})
	}
}

func TestMaxStableRule(t *testing.T) {
	t.Parallel()
	tests := map[string]struct {
		rules []semver.StableRule
		want  semver.StableRule
	}{
		"no rules":    {nil, semver.StablePatch},
		"one":         {[]semver.StableRule{semver.StableMinor}, semver.StableMinor},
		"patch minor": {[]semver.StableRule{semver.StablePatch, semver.StableMinor}, semver.StableMinor},
		"major first": {[]semver.StableRule{semver.StableMajor, semver.StablePatch, semver.StableMinor}, semver.StableMajor},
		"all patches": {[]semver.StableRule{semver.StablePatch, semver.StablePatch}, semver.StablePatch},
		"two minors":  {[]semver.StableRule{semver.StableMinor, semver.StableMinor}, semver.StableMinor},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if got := semver.MaxStableRule(tt.rules...); got != tt.want {
				t.Errorf("got %v, want %v", got, tt.want)
			}
		})
	}
}

func TestStableRuleOrderAndString(t *testing.T) {
	t.Parallel()
	if semver.StablePatch >= semver.StableMinor || semver.StableMinor >= semver.StableMajor {
		t.Error("want Patch < Minor < Major")
	}
	var zero semver.StableRule
	if zero != semver.StablePatch {
		t.Error("zero value must be Patch")
	}
	tests := map[string]struct {
		rule semver.StableRule
		want string
	}{
		"major":      {semver.StableMajor, "MAJOR"},
		"minor name": {semver.StableMinor, "MINOR"},
		"patch name": {semver.StablePatch, "PATCH"},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if got := tt.rule.String(); got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestRuleConstructors(t *testing.T) {
	t.Parallel()
	tests := map[string]struct {
		got  semver.Rule
		want semver.Rule
	}{
		"stable rule": {
			semver.StableBump(semver.StableMinor),
			semver.Rule{Kind: semver.RuleStable, Stable: semver.StableMinor},
		},
		"pre":     {semver.PreBump(lRc, semver.StablePatch), semver.Rule{Kind: semver.RulePre, Label: lRc}},
		"release": {semver.ReleaseBump(), semver.Rule{Kind: semver.RuleRelease}},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if tt.got != tt.want {
				t.Errorf("got %+v, want %+v", tt.got, tt.want)
			}
		})
	}
}

func TestCore(t *testing.T) {
	t.Parallel()
	v := semver.MustParse("1.2.3-rc.4+b7")
	c := v.Core()
	if c != (semver.Core{Major: 1, Minor: 2, Patch: 3}) || c.String() != v123 {
		t.Errorf("Core = %+v (%s)", c, c)
	}
	if got := c.Version(); !got.Equal(semver.MustParse(v123)) {
		t.Errorf("Version() = %s", got)
	}
	tests := map[string]struct {
		a, b semver.Core
		want int
	}{
		"equal":      {semver.Core{Major: 1, Minor: 2, Patch: 3}, semver.Core{Major: 1, Minor: 2, Patch: 3}, 0},
		"major":      {semver.Core{Major: 2}, semver.Core{Major: 1, Minor: 9, Patch: 9}, 1},
		"minor core": {semver.Core{Major: 1, Minor: 2}, semver.Core{Major: 1, Minor: 3}, -1},
		"patch core": {semver.Core{Major: 1, Minor: 2, Patch: 4}, semver.Core{Major: 1, Minor: 2, Patch: 3}, 1},
		"numeric 10": {semver.Core{Minor: 10}, semver.Core{Minor: 9}, 1},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if got := semver.CompareCore(tt.a, tt.b); got != tt.want {
				t.Errorf("got %d, want %d", got, tt.want)
			}
		})
	}
}

func TestPreLabel(t *testing.T) {
	t.Parallel()
	tests := map[string]struct {
		version    string
		label      string
		counter    uint64
		hasCounter bool
	}{
		"rc.4":               {"1.0.0-rc.4", lRc, 4, true},
		"alpha.beta.2":       {"1.0.0-alpha.beta.2", lAlphaBeta, 2, true},
		"rc4 label":          {"1.0.0-rc4", lRc4, 0, false},
		"single word":        {vBeta, lBeta, 0, false},
		"single 1":           {"1.0.0-1", "", 1, true},
		"single 0":           {"1.0.0-0", "", 0, true},
		"rc.4.5":             {"1.0.0-rc.4.5", "rc.4", 5, true},
		"rc.0":               {"1.0.0-rc.0", lRc, 0, true},
		"two ids no counter": {vAlphaBeta, lAlphaBeta, 0, false},
		"build ignored":      {"1.0.0-rc.1+b", lRc, 1, true},
		"not a pre-release":  {"1.0.0", "", 0, false},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			label, counter, has := semver.MustParse(tt.version).PreLabel()
			if label != tt.label || counter != tt.counter || has != tt.hasCounter {
				t.Errorf("got (%q, %d, %v), want (%q, %d, %v)", label, counter, has, tt.label, tt.counter, tt.hasCounter)
			}
		})
	}
}

func TestNewPre(t *testing.T) {
	t.Parallel()
	core := semver.Core{Major: 1, Minor: 2, Patch: 3}
	tests := map[string]struct {
		label   string
		counter uint64
		want    string
	}{
		"simple":     {lRc, 0, "1.2.3-rc.0"},
		"counter":    {lRc, 12, "1.2.3-rc.12"},
		"dotted":     {lAlphaBeta, 2, "1.2.3-alpha.beta.2"},
		"no label":   {"", 1, "1.2.3-1"},
		"hyphen":     {"my-label", 3, "1.2.3-my-label.3"},
		"numeric id": {"1", 2, "1.2.3-1.2"},
		"digits mix": {lRc4, 1, "1.2.3-rc4.1"},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			v, err := semver.NewPre(core, tt.label, tt.counter)
			if err != nil {
				t.Fatal(err)
			}
			if v.String() != tt.want {
				t.Errorf("got %s, want %s", v, tt.want)
			}
			if !v.Equal(semver.MustParse(tt.want)) {
				t.Errorf("%s differs from the parsed version", v)
			}
			label, counter, has := v.PreLabel()
			if label != tt.label || counter != tt.counter || !has {
				t.Errorf("round trip got (%q, %d, %v)", label, counter, has)
			}
		})
	}
}

func TestNewPreInvalid(t *testing.T) {
	t.Parallel()
	tests := map[string]string{
		"space":        "rc 1",
		"leading dot":  ".rc",
		"trailing dot": "rc.",
		"double dot":   "a..b",
		"leading zero": "01",
		"plus":         "rc+1",
		"unicode":      "rç",
	}
	for name, label := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			_, err := semver.NewPre(semver.Core{Major: 1}, label, 1)
			var pe *semver.ParseError
			if !errors.As(err, &pe) {
				t.Fatalf("want *ParseError, got %v", err)
			}
		})
	}
}
