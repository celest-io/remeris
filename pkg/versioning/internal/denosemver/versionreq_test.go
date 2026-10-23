package denosemver_test

import (
	"errors"
	"strconv"
	"testing"

	"github.com/celest-io/remeris/pkg/versioning/internal/denosemver"
)

// Ported from deno_semver 0.9.1 (MIT licence, Copyright 2018-2025 the Deno authors): the tests of src/range.rs
// (range_set_or_tag_display), src/npm.rs (range_parse, range_satisfies, range_not_satisfies, the caret and tilde
// with a primitive kind, zero_version_with_pre_release_matches, is_valid_npm_tag), src/specifier.rs (version_req_*,
// parses_tag) and src/lib.rs (version_req_eq). The fixtures are in testdata/normalized.tsv, range_parse.tsv and
// range_matches.tsv.

// latestTag is the npm tag that the lockfile fixtures use most.
const latestTag = "latest"

func mustLoose(t *testing.T, text string) denosemver.VersionReq {
	t.Helper()
	r, err := denosemver.ParseVersionReqLoose(text)
	if err != nil {
		t.Fatalf("ParseVersionReqLoose(%q): %v", text, err)
	}
	return r
}

func mustSpecifier(t *testing.T, text string) denosemver.VersionReq {
	t.Helper()
	r, err := denosemver.ParseVersionReqSpecifier(text)
	if err != nil {
		t.Fatalf("ParseVersionReqSpecifier(%q): %v", text, err)
	}
	return r
}

func TestNormalizedPrinting(t *testing.T) {
	t.Parallel()
	for _, r := range tsvRows(t, "normalized.tsv") {
		t.Run(r.name, func(t *testing.T) {
			t.Parallel()
			in, want := r.field(t, 0), r.field(t, 1)
			lossy := len(r.fields) > 2 && r.fields[2] == "lossy"
			req := mustLoose(t, in)
			got := req.Normalized()
			if got != want {
				t.Fatalf("Normalized(%q) = %q, want %q", in, got, want)
			}
			// the printed form parses back, with the npm and the normalised grammar, to the same requirement
			norm, err := denosemver.ParseVersionReqNormalized(got)
			if err != nil {
				t.Fatalf("ParseVersionReqNormalized(%q): %v", got, err)
			}
			if norm.Normalized() != got {
				t.Errorf("the normalised print is not stable: %q then %q", got, norm.Normalized())
			}
			if !lossy && (!mustLoose(t, got).Equal(req) || !norm.Equal(req)) {
				t.Errorf("%q does not parse back to the same requirement as %q", got, in)
			}
		})
	}
}

func TestRangeParse(t *testing.T) {
	t.Parallel()
	for _, r := range tsvRows(t, "range_parse.tsv") {
		t.Run(r.name, func(t *testing.T) {
			t.Parallel()
			in, want := r.field(t, 0), r.field(t, 1)
			got, wantReq := mustLoose(t, in), mustLoose(t, want)
			if !got.Equal(wantReq) {
				t.Errorf("%q parses to %q, want the range of %q (%q)", in, got.Normalized(), want, wantReq.Normalized())
			}
		})
	}
}

func TestRangeMatches(t *testing.T) {
	t.Parallel()
	parsers := map[string]func(*testing.T, string) denosemver.VersionReq{
		grammarLoose:     mustLoose,
		grammarSpecifier: mustSpecifier,
	}
	for _, r := range tsvRows(t, "range_matches.tsv") {
		t.Run(r.name, func(t *testing.T) {
			t.Parallel()
			parse, ok := parsers[r.field(t, 0)]
			if !ok {
				t.Fatalf("unknown grammar %q", r.fields[0])
			}
			req, version := parse(t, r.field(t, 1)), mustVersion(t, r.field(t, 2))
			want, err := strconv.ParseBool(r.field(t, 3))
			if err != nil {
				t.Fatal(err)
			}
			if got := req.Matches(version); got != want {
				t.Errorf("Matches = %v, want %v", got, want)
			}
		})
	}
}

func TestRawTextKept(t *testing.T) {
	t.Parallel()
	tests := map[string]struct {
		parse func(string) (denosemver.VersionReq, error)
		in    string
		raw   string
	}{
		"specifier keeps the input": {denosemver.ParseVersionReqSpecifier, "^1.0.0", "^1.0.0"},
		"loose trims the input":     {denosemver.ParseVersionReqLoose, "  ~1.0.0 ", "~1.0.0"},
		"normalised keeps it":       {denosemver.ParseVersionReqNormalized, "1 || 2", "1 || 2"},
		"tag":                       {denosemver.ParseVersionReqSpecifier, latestTag, latestTag},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			r, err := tt.parse(tt.in)
			if err != nil {
				t.Fatal(err)
			}
			if r.VersionText() != tt.raw || r.String() != tt.raw {
				t.Errorf("VersionText() = %q, String() = %q, want %q", r.VersionText(), r.String(), tt.raw)
			}
		})
	}
	if got := denosemver.WildcardVersionReq().VersionText(); got != "*" {
		t.Errorf("wildcard VersionText() = %q", got)
	}
}

func TestTags(t *testing.T) {
	t.Parallel()
	parsers := map[string]func(string) (denosemver.VersionReq, error){
		"specifier": denosemver.ParseVersionReqSpecifier,
		"loose":     denosemver.ParseVersionReqLoose,
		"normal":    denosemver.ParseVersionReqNormalized,
	}
	version := mustVersion(t, "1.0.0")
	for _, in := range []string{latestTag, "next", "latest-v1", "beta_2", "a~b"} {
		for name, parse := range parsers {
			r, err := parse(in)
			if err != nil {
				t.Errorf("%s(%q): %v", name, in, err)
				continue
			}
			if tag, ok := r.Tag(); !ok || tag != in {
				t.Errorf("%s(%q).Tag() = %q, %v", name, in, tag, ok)
			}
			if r.Normalized() != in {
				t.Errorf("%s(%q).Normalized() = %q", name, in, r.Normalized())
			}
			if r.Matches(version) {
				t.Errorf("%s(%q) matches a version, a tag matches nothing", name, in)
			}
		}
	}
	if _, ok := mustSpecifier(t, "1.x").Tag(); ok {
		t.Error("1.x is not a tag")
	}
	// The strict grammar has no leading "v", so "v1.0" is a tag there; the npm grammar reads it as a version.
	if tag, ok := mustSpecifier(t, "v1.0").Tag(); !ok || tag != "v1.0" {
		t.Errorf("specifier v1.0: tag %q, %v", tag, ok)
	}
	if _, ok := mustLoose(t, "v1.0").Tag(); ok {
		t.Error("loose v1.0 should be a range")
	}
	// Digits that overflow u64 are not an error for a specifier: the text is a valid tag.
	if tag, ok := mustSpecifier(t, "99999999999999999999").Tag(); !ok || tag != "99999999999999999999" {
		t.Errorf("overflowing digits: tag %q, %v", tag, ok)
	}
}

func TestVersionReqEqual(t *testing.T) {
	t.Parallel()
	tests := map[string]struct {
		a, b string
		want bool
	}{
		"1 and 1.x":      {"1", "1.x", true},
		"1 and 2":        {"1", "2", false},
		"two tags":       {latestTag, "next", false},
		"same tag":       {latestTag, latestTag, true},
		"tag and range":  {latestTag, "1", false},
		"caret and bare": {"^1", "1", true},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if got := mustSpecifier(t, tt.a).Equal(mustSpecifier(t, tt.b)); got != tt.want {
				t.Errorf("Equal(%q, %q) = %v, want %v", tt.a, tt.b, got, tt.want)
			}
		})
	}
}

func TestPrimitiveKindWithoutWhitespaceIsInvalid(t *testing.T) {
	t.Parallel()
	// node-semver rejected a primitive kind glued to a caret or a tilde, and so does the Rust crate.
	for _, in := range []string{">=^1.2.3", ">^1.2.3", "<^1.2.3", "<=^1.2.3", ">=~1.2.3", ">~1.2.3", "<~1.2.3", "<=~1.2.3"} {
		if _, err := denosemver.ParseVersionReqLoose(in); err == nil {
			t.Errorf("ParseVersionReqLoose(%q) should fail", in)
		}
	}
}

func TestSpecifierErrors(t *testing.T) {
	t.Parallel()
	const emptyConstraint = "Empty version constraint."
	tests := map[string]struct {
		message string
		snippet string
	}{
		"":        {emptyConstraint, ""},
		" ":       {emptyConstraint, " "},
		">=1":     {unexpectedCharacter, ">=1"},
		"1 || 3":  {unexpectedCharacter, " || 3"},
		"01.2.3":  {unexpectedCharacter, "1.2.3"},
		"1.2.3 x": {unexpectedCharacter, " x"},
		"99999999999999999999 ": {
			"Error parsing '99999999999999999999' to u64.\n\nnumber too large to fit in target type",
			" ",
		},
	}
	for in, tt := range tests {
		t.Run(in, func(t *testing.T) {
			t.Parallel()
			_, err := denosemver.ParseVersionReqSpecifier(in)
			if err == nil {
				t.Fatalf("ParseVersionReqSpecifier(%q) succeeded", in)
			}
			if got := err.Error(); got != errSpecifier {
				t.Errorf("Error() = %q", got)
			}
			var f *denosemver.Failure
			if !errors.As(err, &f) {
				t.Fatalf("no *Failure in %v", err)
			}
			if f.Message != tt.message || f.CodeSnippet != tt.snippet {
				t.Errorf("failure = %q / %q, want %q / %q", f.Message, f.CodeSnippet, tt.message, tt.snippet)
			}
		})
	}
}

func TestLooseAndNormalizedErrors(t *testing.T) {
	t.Parallel()
	const garbage = "$#$%^"
	tests := map[string]struct {
		parse func(string) (denosemver.VersionReq, error)
		in    string
		want  string
	}{
		"loose, garbage":        {denosemver.ParseVersionReqLoose, garbage, errLoose},
		"normalised, garbage":   {denosemver.ParseVersionReqNormalized, garbage, errNormalized},
		"normalised, bad range": {denosemver.ParseVersionReqNormalized, "1 || $%^ || 2", errNormalized},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			_, err := tt.parse(tt.in)
			if err == nil || err.Error() != tt.want {
				t.Errorf("parse(%q) error = %v, want %q", tt.in, err, tt.want)
			}
		})
	}
	// the npm grammar drops an invalid range among several instead of failing
	r := mustLoose(t, ">= 2.1.2 < 3.0.0 || 5.x || ignored-invalid-range || $#$%^#$^#$^%@#$%SDF|||")
	for v, want := range map[string]bool{"2.1.1": false, "2.1.2": true, "2.9.9": true, "3.0.0": false, "5.1.0": true, "6.1.0": false} {
		if got := r.Matches(mustVersion(t, v)); got != want {
			t.Errorf("matches %q = %v, want %v", v, got, want)
		}
	}
}

func TestLooseAndRange(t *testing.T) {
	t.Parallel()
	for _, in := range []string{">= 1.2 && <= 2.0.0", ">= 1.2, <= 2.0.0"} {
		r := mustLoose(t, in)
		for v, want := range map[string]bool{"1.1.9": false, "1.2.2": true, "1.2.0": true, "1.9.9": true, "2.1.1": false, "2.0.1": false} {
			if got := r.Matches(mustVersion(t, v)); got != want {
				t.Errorf("%q matches %q = %v, want %v", in, v, got, want)
			}
		}
	}
}
