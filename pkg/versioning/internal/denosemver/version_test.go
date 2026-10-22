package denosemver_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/celest-io/remeris/pkg/versioning/internal/denosemver"
)

// Ported from deno_semver 0.9.1 (MIT licence, Copyright 2018-2025 the Deno authors): the version tests of
// src/lib.rs and src/npm.rs. The fixtures are in testdata/version_compare.tsv and testdata/version_string.tsv.

const (
	// unexpectedCharacter is the message monch gives when the input does not match, or is not consumed entirely.
	unexpectedCharacter = "Unexpected character."
	// The texts of the errors of the three requirement grammars.
	errSpecifier  = "Invalid specifier version requirement"
	errLoose      = "Invalid version requirement"
	errNormalized = "Invalid normalized version requirement"
)

func mustVersion(t *testing.T, text string) denosemver.Version {
	t.Helper()
	v, err := denosemver.ParseNpm(text)
	if err != nil {
		t.Fatalf("ParseNpm(%q): %v", text, err)
	}
	return v
}

func TestVersionString(t *testing.T) {
	t.Parallel()
	for _, r := range tsvRows(t, "version_string.tsv") {
		t.Run(r.name, func(t *testing.T) {
			t.Parallel()
			in, want := r.field(t, 0), r.field(t, 1)
			for name, parse := range map[string]func(string) (denosemver.Version, error){
				"standard": denosemver.ParseStandard,
				"npm":      denosemver.ParseNpm,
			} {
				v, err := parse(in)
				if err != nil {
					t.Fatalf("%s: %v", name, err)
				}
				if got := v.String(); got != want {
					t.Errorf("%s: String() = %q, want %q", name, got, want)
				}
			}
		})
	}
}

func TestVersionCompare(t *testing.T) {
	t.Parallel()
	for _, r := range tsvRows(t, "version_compare.tsv") {
		t.Run(r.name, func(t *testing.T) {
			t.Parallel()
			a, b := mustVersion(t, r.field(t, 0)), mustVersion(t, r.field(t, 1))
			want := sign(t, r.field(t, 2))
			if got := a.Compare(b); got != want {
				t.Errorf("Compare(a, b) = %d, want %d", got, want)
			}
			if got := b.Compare(a); got != -want {
				t.Errorf("Compare(b, a) = %d, want %d", got, -want)
			}
			if got := a.Compare(a); got != 0 {
				t.Errorf("Compare(a, a) = %d, want 0", got)
			}
		})
	}
}

func TestVersionEqualIncludesBuild(t *testing.T) {
	t.Parallel()
	a, b := mustVersion(t, "1.2.3+one"), mustVersion(t, "1.2.3+two")
	if a.Compare(b) != 0 {
		t.Error("the build metadata must not count for precedence")
	}
	if a.Equal(b) {
		t.Error("Equal must compare the build metadata, like the derived PartialEq of Rust")
	}
	if !a.Equal(mustVersion(t, "v 1.2.3+one")) {
		t.Error("the same version must be equal")
	}
}

func TestVersionParseErrors(t *testing.T) {
	t.Parallel()
	const overflow = "Error parsing '99999999999999999999' to u64.\n\nnumber too large to fit in target type"
	tests := map[string]struct {
		message string
		snippet string
	}{
		"1.2":                      {message: unexpectedCharacter, snippet: "1.2"},
		"":                         {message: unexpectedCharacter, snippet: ""},
		"a.b.c":                    {message: unexpectedCharacter, snippet: "a.b.c"},
		"1.2.3 4":                  {message: unexpectedCharacter, snippet: " 4"},
		"1.2.3-":                   {message: unexpectedCharacter, snippet: "-"},
		"1.2.3-a b":                {message: unexpectedCharacter, snippet: " b"},
		"99999999999999999999.0.0": {message: overflow, snippet: ".0.0"},
	}
	for in, tt := range tests {
		t.Run(in, func(t *testing.T) {
			t.Parallel()
			_, err := denosemver.ParseStandard(in)
			if err == nil {
				t.Fatalf("ParseStandard(%q) succeeded", in)
			}
			if got := err.Error(); got != "Invalid version" {
				t.Errorf("Error() = %q, want %q", got, "Invalid version")
			}
			var f *denosemver.Failure
			if !errors.As(err, &f) {
				t.Fatalf("no *Failure in %v", err)
			}
			if f.Message != tt.message || f.CodeSnippet != tt.snippet {
				t.Errorf("failure = %q / %q, want %q / %q", f.Message, f.CodeSnippet, tt.message, tt.snippet)
			}
			_, err = denosemver.ParseNpm(in)
			if err == nil || err.Error() != "Invalid npm version" {
				t.Errorf("ParseNpm error = %v, want Invalid npm version", err)
			}
		})
	}
}

func TestFailureDisplay(t *testing.T) {
	t.Parallel()
	_, err := denosemver.ParseStandard("1.2")
	var f *denosemver.Failure
	if !errors.As(err, &f) {
		t.Fatal("no failure")
	}
	if got, want := f.Error(), unexpectedCharacter+"\n  1.2\n  ~"; got != want {
		t.Errorf("Failure.Error() = %q, want %q", got, want)
	}
}

func TestFailureSnippetIsCut(t *testing.T) {
	t.Parallel()
	_, err := denosemver.ParseStandard("1.2.3 " + strings.Repeat("é", 70))
	var f *denosemver.Failure
	if !errors.As(err, &f) {
		t.Fatal("no failure")
	}
	if n := len([]rune(f.CodeSnippet)); n != 60 {
		t.Errorf("snippet has %d characters, want 60", n)
	}
}
