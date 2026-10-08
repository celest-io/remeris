package semver_test

import (
	"errors"
	"testing"

	"github.com/celest-io/remeris/internal/diag"
	"github.com/celest-io/remeris/pkg/versioning/semver"
)

const (
	v123     = "1.2.3"
	v100     = "1.0.0"
	v12      = "1.2"
	v100a    = "1.0.0-a"
	v123a    = "1.2.3+a"
	detParts = "Version must have exactly 3 parts"
	detDigit = "invalid digit found in string"
	detEmpty = "cannot parse integer from empty string"
)

// validVersions and invalidVersions are the conformance lists from the semver.org test suites.
var validVersions = []string{
	"0.0.4", v123, "10.20.30", "1.1.2-prerelease+meta", "1.1.2+meta", "1.1.2+meta-valid",
	"1.0.0-alpha", "1.0.0-beta", "1.0.0-alpha.beta", "1.0.0-alpha.beta.1", "1.0.0-alpha.1", "1.0.0-alpha0.valid",
	"1.0.0-alpha.0valid", "1.0.0-alpha-a.b-c-somethinglong+build.1-aef.1-its-okay", "1.0.0-rc.1+build.1",
	"2.0.0-rc.1+build.123", "1.2.3-beta", "10.2.3-DEV-SNAPSHOT", "1.2.3-SNAPSHOT-123", v100, "2.0.0",
	"1.1.7", "2.0.0+build.1848", "2.0.1-alpha.1227", "1.0.0-alpha+beta", "1.2.3----RC-SNAPSHOT.12.9.1--.12+788",
	"1.2.3----R-S.12.9.1--.12+meta", "1.2.3----RC-SNAPSHOT.12.9.1--.12", "1.0.0+0.build.1-rc.10000aaa-kk-0.1",
	"18446744073709551615.18446744073709551615.18446744073709551615", "1.0.0-0A.is.legal",
	"1.1.2+meta-valid-with-001", "1.0.0+001", "1.0.0-alpha+00.01",
}

var invalidVersions = []string{
	"1", v12, "1.2.3-0123", "1.2.3-0123.0123", "1.1.2+.123", "+invalid", "-invalid", "-invalid+invalid",
	"-invalid.01", "alpha", "alpha.beta", "alpha.beta.1", "alpha.1", "alpha+beta", "alpha_beta", "alpha.",
	"alpha..", "beta", "1.0.0-alpha_beta", "-alpha.", "1.0.0-alpha..", "1.0.0-alpha..1", "1.0.0-alpha...1",
	"1.0.0-alpha....1", "1.0.0-alpha.....1", "1.0.0-alpha......1", "1.0.0-alpha.......1", "01.1.1", "1.01.1",
	"1.1.01", v12, "1.2.3.DEV", "1.2-SNAPSHOT", "1.2.31.2.3----RC-SNAPSHOT.12.09.1--..12+788", "1.2-RC-SNAPSHOT",
	"-1.0.3-gamma+b7718", "+justmeta", "9.8.7+meta+meta", "9.8.7-whatever+meta+meta",
	"99999999999999999999999.999999999999999999.99999999999999999",
	"", "v1.2.3", " 1.2.3", "1.2.3 ", "1.2.3-", "1.2.3+", "1.2.3-a..b", "1.2.3+a..b", "1.2.3+a_b", "1.2.3-a+b+c",
	"1.2.-3", "+1.2.3", "1.2.3-rc.01", "18446744073709551616.0.0", "1.2.3-18446744073709551616", "1.2.3-é",
}

func TestParseConformance(t *testing.T) {
	t.Parallel()
	for _, s := range validVersions {
		t.Run("valid "+s, func(t *testing.T) {
			t.Parallel()
			v, err := semver.Parse(s)
			if err != nil {
				t.Fatalf("Parse(%q) error: %v", s, err)
			}
			if got := v.String(); got != s {
				t.Errorf("String() = %q, want %q", got, s)
			}
		})
	}
	for _, s := range invalidVersions {
		t.Run("invalid "+s, func(t *testing.T) {
			t.Parallel()
			if v, err := semver.Parse(s); err == nil {
				t.Errorf("Parse(%q) = %v, want an error", s, v)
			}
		})
	}
}

func TestParse(t *testing.T) {
	t.Parallel()
	tests := map[string]struct {
		input string
		want  semver.Version
	}{
		"stable": {v123, semver.Version{Major: 1, Minor: 2, Patch: 3}},
		"zero":   {"0.0.0", semver.Version{}},
		"label.N": {"1.2.3-rc.4", semver.Version{Major: 1, Minor: 2, Patch: 3,
			Pre: []semver.PreID{{Str: "rc"}, {Str: "4", Num: 4, IsNum: true}}}},
		// Spec F1.1 "Differences from Knope": Knope rejected all four of these; full SemVer accepts them.
		"no dot in pre": {"1.2.3-rc4", semver.Version{Major: 1, Minor: 2, Patch: 3, Pre: []semver.PreID{{Str: "rc4"}}}},
		"build":         {"1.2.3+build.7", semver.Version{Major: 1, Minor: 2, Patch: 3, Build: []string{"build", "7"}}},
		"three ids": {"1.2.3-alpha.beta.1", semver.Version{Major: 1, Minor: 2, Patch: 3,
			Pre: []semver.PreID{{Str: "alpha"}, {Str: "beta"}, {Str: "1", Num: 1, IsNum: true}}}},
		"two numbers": {"1.2.3-rc.4.5", semver.Version{Major: 1, Minor: 2, Patch: 3,
			Pre: []semver.PreID{{Str: "rc"}, {Str: "4", Num: 4, IsNum: true}, {Str: "5", Num: 5, IsNum: true}}}},
		"build leading zeros": {"1.2.3+001", semver.Version{Major: 1, Minor: 2, Patch: 3, Build: []string{"001"}}},
		"hyphen id":           {"1.2.3--", semver.Version{Major: 1, Minor: 2, Patch: 3, Pre: []semver.PreID{{Str: "-"}}}},
		"pre and build": {
			"1.0.0-a.1+b",
			semver.Version{Major: 1, Pre: []semver.PreID{{Str: "a"}, {Str: "1", Num: 1, IsNum: true}},
				Build: []string{"b"}},
		},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			got, err := semver.Parse(tc.input)
			if err != nil {
				t.Fatalf("Parse(%q) error: %v", tc.input, err)
			}
			if !got.Equal(tc.want) {
				t.Errorf("Parse(%q) = %#v, want %#v", tc.input, got, tc.want)
			}
			if got.String() != tc.input {
				t.Errorf("String() = %q, want %q", got.String(), tc.input)
			}
		})
	}
}

func TestParseErrors(t *testing.T) {
	t.Parallel()
	const helpText = "The version must be a valid Semantic Version"
	tests := map[string]struct {
		input  string
		detail string
	}{
		"two parts":  {v12, detParts},
		"four":       {"1.2.3.4", detParts},
		"empty":      {"", detParts},
		"letters":    {"a.b.c", detDigit},
		"v prefix":   {"v1.2.3", detDigit},
		"space":      {" 1.2.3", detDigit},
		"empty part": {"1..3", detEmpty},
		"overflow":   {"18446744073709551616.0.0", "number too large to fit in target type"},
		// Spec F1.1 "Differences from Knope": Knope read 01.2.3 as 1.2.3 and accepted rc.01.
		"leading zero core": {"01.2.3", "numbers must not have leading zeros"},
		"leading zero pre":  {"1.2.3-rc.01", "numbers must not have leading zeros"},
		"empty pre id":      {"1.2.3-rc..1", detEmpty},
		"empty build id":    {"1.2.3+", detEmpty},
		"bad build char":    {"1.2.3+a_b", detDigit},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			_, err := semver.Parse(tc.input)
			var pe *semver.ParseError
			if !errors.As(err, &pe) {
				t.Fatalf("Parse(%q) error = %v, want *ParseError", tc.input, err)
			}
			if want := "Found invalid semantic version " + tc.detail; err.Error() != want {
				t.Errorf("message = %q, want %q", err.Error(), want)
			}
			var d diag.Diagnostic
			if !errors.As(err, &d) || d.Help() != helpText {
				t.Errorf("help = %v, want %q", d, helpText)
			}
		})
	}
}

func TestMustParsePanics(t *testing.T) {
	t.Parallel()
	defer func() {
		if recover() == nil {
			t.Error("MustParse did not panic")
		}
	}()
	semver.MustParse("nope")
}

func TestPrecedenceChain(t *testing.T) {
	t.Parallel()
	chain := []string{
		"1.0.0-alpha", "1.0.0-alpha.1", "1.0.0-alpha.beta", "1.0.0-beta", "1.0.0-beta.2", "1.0.0-beta.11",
		"1.0.0-rc.1", v100, "1.0.1-0", "1.1.0", "2.0.0",
	}
	for i, as := range chain {
		for j, bs := range chain {
			t.Run(as+" vs "+bs, func(t *testing.T) {
				t.Parallel()
				want := 0
				if i < j {
					want = -1
				} else if i > j {
					want = 1
				}
				if got := semver.Compare(semver.MustParse(as), semver.MustParse(bs)); got != want {
					t.Errorf("Compare(%s, %s) = %d, want %d", as, bs, got, want)
				}
			})
		}
	}
}

func TestComparePreIDs(t *testing.T) {
	t.Parallel()
	tests := map[string]struct {
		a, b string
		want int
	}{
		"numeric below alphanumeric":  {"1.0.0-1", v100a, -1},
		"numeric compared as numbers": {"1.0.0-2", "1.0.0-10", -1},
		"ASCII order":                 {"1.0.0-B", v100a, -1},
		"longer is higher":            {"1.0.0-a.1", v100a, 1},
		"digits in alnum are text":    {"1.0.0-rc10", "1.0.0-rc9", -1},
		"hyphen alnum":                {"1.0.0-1-1", "1.0.0-2", 1},
		"u64 max":                     {"1.0.0-18446744073709551615", "1.0.0-18446744073709551614", 1},
		"core wins over pre":          {"1.0.1-alpha", v100, 1},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			a, b := semver.MustParse(tc.a), semver.MustParse(tc.b)
			if got := semver.Compare(a, b); got != tc.want {
				t.Errorf("Compare(%s, %s) = %d, want %d", tc.a, tc.b, got, tc.want)
			}
			if got := semver.Compare(b, a); got != -tc.want {
				t.Errorf("Compare(%s, %s) = %d, want %d", tc.b, tc.a, got, -tc.want)
			}
		})
	}
}

func TestBuildMetadata(t *testing.T) {
	t.Parallel()
	tests := map[string]struct {
		a, b      string
		wantEqual bool
		wantCmp   int
	}{
		"same":                {v123a, v123a, true, 0},
		"different build":     {v123a, "1.2.3+b", false, 0},
		"build vs none":       {v123, "1.2.3+b1", false, 0},
		"pre with build":      {"1.2.3-rc.1+a", "1.2.3-rc.1+b", false, 0},
		"different pre":       {"1.2.3-rc.1", "1.2.3-rc.2", false, -1},
		"build is not prefix": {"1.2.3+a.b", v123a, false, 0},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			a, b := semver.MustParse(tc.a), semver.MustParse(tc.b)
			if got := a.Equal(b); got != tc.wantEqual {
				t.Errorf("Equal = %v, want %v", got, tc.wantEqual)
			}
			// Compare ignores build metadata: it is 0 exactly when the cores and pre-releases match.
			if got := semver.Compare(a, b); got != tc.wantCmp {
				t.Errorf("Compare = %d, want %d", got, tc.wantCmp)
			}
		})
	}
}

func TestIsPrerelease(t *testing.T) {
	t.Parallel()
	tests := map[string]struct {
		input string
		want  bool
	}{
		"stable":       {v123, false},
		"stable build": {"1.2.3+b", false},
		"pre":          {"1.2.3-rc.1", true},
		"pre word":     {"1.2.3-beta", true},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if got := semver.MustParse(tc.input).IsPrerelease(); got != tc.want {
				t.Errorf("IsPrerelease() = %v, want %v", got, tc.want)
			}
		})
	}
}

func FuzzParseRoundTrip(f *testing.F) {
	for _, s := range validVersions {
		f.Add(s)
	}
	for _, s := range invalidVersions {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		v, err := semver.Parse(s)
		if err != nil {
			return
		}
		str := v.String()
		again, err := semver.Parse(str)
		if err != nil {
			t.Fatalf("Parse(%q) from String() of %q failed: %v", str, s, err)
		}
		if !again.Equal(v) {
			t.Errorf("round trip of %q: got %#v, want %#v", s, again, v)
		}
		if again.String() != str {
			t.Errorf("String() unstable: %q then %q", str, again.String())
		}
		if str != s {
			t.Errorf("canonical form %q differs from accepted input %q", str, s)
		}
	})
}
