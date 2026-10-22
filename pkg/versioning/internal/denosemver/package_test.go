package denosemver_test

import (
	"errors"
	"testing"

	"github.com/celest-io/remeris/pkg/versioning/internal/denosemver"
)

// Ported from deno_semver 0.9.1 (MIT licence, Copyright 2018-2025 the Deno authors): the tests of src/package.rs
// (serialize_deserialize_*, sorting_package_reqs, missing_at_symbol) and src/jsr.rs (jsr_dep_package_req_display,
// test_jsr_dep_pkg_req_from_str, test_jsr_dep_pkg_req_normalized, serialize_deserialize_tag_package_req_with_v).
// The fixtures are in testdata/package_req.tsv, package_req_loose.txt, package_req_compare.tsv, package_nv_compare.tsv
// and jsr_dep.tsv.

func TestPackageReq(t *testing.T) {
	t.Parallel()
	parsers := map[string]func(string) (denosemver.PackageReq, error){
		grammarStrict: denosemver.ParsePackageReq,
		grammarLoose:  denosemver.ParsePackageReqLoose,
	}
	for _, r := range tsvRows(t, "package_req.tsv") {
		t.Run(r.name, func(t *testing.T) {
			t.Parallel()
			req, err := parsers[r.field(t, 0)](r.field(t, 1))
			if err != nil {
				t.Fatal(err)
			}
			if got, want := req.Name, r.field(t, 2); got != want {
				t.Errorf("Name = %q, want %q", got, want)
			}
			if got, want := req.String(), r.field(t, 3); got != want {
				t.Errorf("String() = %q, want %q", got, want)
			}
			if got, want := req.Normalized(), r.field(t, 4); got != want {
				t.Errorf("Normalized() = %q, want %q", got, want)
			}
			back, err := denosemver.ParsePackageReqNormalized(req.Normalized())
			if err != nil {
				t.Fatalf("ParsePackageReqNormalized(%q): %v", req.Normalized(), err)
			}
			if !back.Equal(req) {
				t.Errorf("the normalised form %q does not parse back to the same requirement", req.Normalized())
			}
		})
	}
}

func TestPackageReqLooseRoundTrip(t *testing.T) {
	t.Parallel()
	// serialize_deserialize_loose_package_req: each loose requirement prints to a normalised form that parses
	// back to an equal one.
	for _, r := range tsvRows(t, "package_req_loose.txt") {
		t.Run(r.name, func(t *testing.T) {
			t.Parallel()
			req, err := denosemver.ParsePackageReqLoose(r.name)
			if err != nil {
				t.Fatal(err)
			}
			back, err := denosemver.ParsePackageReqNormalized(req.Normalized())
			if err != nil {
				t.Fatalf("ParsePackageReqNormalized(%q): %v", req.Normalized(), err)
			}
			if !back.Equal(req) {
				t.Errorf("%q printed as %q parses to a different requirement", r.name, req.Normalized())
			}
		})
	}
}

func TestPackageReqCompare(t *testing.T) {
	t.Parallel()
	for _, r := range tsvRows(t, "package_req_compare.tsv") {
		t.Run(r.name, func(t *testing.T) {
			t.Parallel()
			a, err := denosemver.ParsePackageReqLoose(r.field(t, 0))
			if err != nil {
				t.Fatal(err)
			}
			b, err := denosemver.ParsePackageReqLoose(r.field(t, 1))
			if err != nil {
				t.Fatal(err)
			}
			want := sign(t, r.field(t, 2))
			if got := a.Compare(b); got != want {
				t.Errorf("Compare(a, b) = %d, want %d", got, want)
			}
			if got := b.Compare(a); got != -want {
				t.Errorf("Compare(b, a) = %d, want %d", got, -want)
			}
		})
	}
}

func TestPackageReqErrors(t *testing.T) {
	t.Parallel()
	tests := map[string]struct {
		parse  func(string) (denosemver.PackageReq, error)
		source error
	}{
		"scope/name":   {denosemver.ParsePackageReq, denosemver.ErrMissingAtSymbol},
		"":             {denosemver.ParsePackageReq, denosemver.ErrNoPackageName},
		"/":            {denosemver.ParsePackageReq, denosemver.ErrMissingAtSymbol}, // the slash decides, not the empty name
		"@scope":       {denosemver.ParsePackageReq, denosemver.ErrInvalidPackageName},
		"@scope/":      {denosemver.ParsePackageReq, denosemver.ErrInvalidPackageName},
		"a@1/mod.ts":   {denosemver.ParsePackageReq, denosemver.ErrMissingAtSymbol},
		"scope/name@1": {denosemver.ParsePackageReqLoose, denosemver.ErrMissingAtSymbol},
	}
	for in, tt := range tests {
		t.Run(in, func(t *testing.T) {
			t.Parallel()
			_, err := tt.parse(in)
			if err == nil {
				t.Fatal("expected an error")
			}
			if want := "Invalid package requirement '" + in + "'"; err.Error() != want {
				t.Errorf("Error() = %q, want %q", err.Error(), want)
			}
			var pe *denosemver.PackageReqParseError
			if !errors.As(err, &pe) || pe.Text != in {
				t.Errorf("not a *PackageReqParseError for %q: %#v", in, err)
			}
			if !errors.Is(err, tt.source) {
				t.Errorf("errors.Is(%v, %v) is false", err, tt.source)
			}
		})
	}
}

func TestPackageReqVersionErrors(t *testing.T) {
	t.Parallel()
	tests := map[string]struct {
		parse   func(string) (denosemver.PackageReq, error)
		parseOf string
		message string
		snippet string
	}{
		"package@":            {denosemver.ParsePackageReq, errSpecifier, "Empty version constraint.", ""},
		"@scope/foo@1/mod.ts": {denosemver.ParsePackageReq, errSpecifier, "Unexpected character '/'", "/mod.ts"},
		"a@>=1.2":             {denosemver.ParsePackageReq, errSpecifier, unexpectedCharacter, ">=1.2"},
		"a@$%":                {denosemver.ParsePackageReqLoose, errLoose, unexpectedCharacter, "$%"},
		"b@$%":                {denosemver.ParsePackageReqNormalized, errNormalized, unexpectedCharacter, "$%"},
	}
	for in, tt := range tests {
		t.Run(in, func(t *testing.T) {
			t.Parallel()
			_, err := tt.parse(in)
			if err == nil {
				t.Fatal("expected an error")
			}
			var pe *denosemver.ParseError
			if !errors.As(err, &pe) || pe.Error() != tt.parseOf {
				t.Fatalf("source = %v, want %q", errors.Unwrap(err), tt.parseOf)
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

func TestPackageNvParse(t *testing.T) {
	t.Parallel()
	tests := map[string]struct {
		name    string
		version string
	}{
		"nanoid@3.3.4":                 {"nanoid", "3.3.4"},
		"@scope/first@1.0.0":           {"@scope/first", "1.0.0"},
		"@std/path@1.0.0-rc.1+build.5": {"@std/path", "1.0.0-rc.1+build.5"},
		"left-pad@v1.3.0":              {"left-pad", "1.3.0"},
		"@a/b@01.002.0003":             {"@a/b", "1.2.3"},
	}
	for in, tt := range tests {
		t.Run(in, func(t *testing.T) {
			t.Parallel()
			nv, err := denosemver.ParsePackageNv(in)
			if err != nil {
				t.Fatal(err)
			}
			if nv.Name != tt.name || nv.Version.String() != tt.version {
				t.Errorf("got %q @ %q, want %q @ %q", nv.Name, nv.Version, tt.name, tt.version)
			}
			if want := tt.name + "@" + tt.version; nv.String() != want {
				t.Errorf("String() = %q, want %q", nv.String(), want)
			}
		})
	}
}

func TestPackageNvParseErrors(t *testing.T) {
	t.Parallel()
	const invalidNpm = "Invalid npm version"
	tests := map[string]string{
		"noversion":    unexpectedCharacter + "\n  noversion\n  ~",
		"a@x":          invalidNpm + "\n  x\n  ~",
		"a@":           unexpectedCharacter + "\n  a@\n  ~",
		"a@1.0":        invalidNpm + "\n  1.0\n  ~",
		"a@1.0.0_b@2":  unexpectedCharacter + "\n  _b@2\n  ~",
		"a@1.0.0/path": unexpectedCharacter + "\n  /path\n  ~",
		"":             unexpectedCharacter + "\n  \n  ~",
		"@scope/name":  unexpectedCharacter + "\n  @scope/name\n  ~",
	}
	for in, message := range tests {
		t.Run(in, func(t *testing.T) {
			t.Parallel()
			_, err := denosemver.ParsePackageNv(in)
			if err == nil {
				t.Fatal("expected an error")
			}
			if want := "Invalid package name and version '" + in + "'. " + message; err.Error() != want {
				t.Errorf("Error() = %q, want %q", err.Error(), want)
			}
			var pe *denosemver.PackageNvParseError
			if !errors.As(err, &pe) || pe.Text != in || pe.Message != message {
				t.Errorf("not the expected *PackageNvParseError for %q: %#v", in, err)
			}
		})
	}
}

func TestPackageNvCompare(t *testing.T) {
	t.Parallel()
	for _, r := range tsvRows(t, "package_nv_compare.tsv") {
		t.Run(r.name, func(t *testing.T) {
			t.Parallel()
			a, err := denosemver.ParsePackageNv(r.field(t, 0))
			if err != nil {
				t.Fatal(err)
			}
			b, err := denosemver.ParsePackageNv(r.field(t, 1))
			if err != nil {
				t.Fatal(err)
			}
			want := sign(t, r.field(t, 2))
			if got := a.Compare(b); got != want {
				t.Errorf("Compare(a, b) = %d, want %d", got, want)
			}
			if got := b.Compare(a); got != -want {
				t.Errorf("Compare(b, a) = %d, want %d", got, -want)
			}
		})
	}
}

func TestJsrDepPackageReq(t *testing.T) {
	t.Parallel()
	parsers := map[string]func(string) (denosemver.JsrDepPackageReq, error){
		grammarStrict:     denosemver.ParseJsrDepPackageReq,
		grammarLoose:      denosemver.ParseJsrDepPackageReqLoose,
		grammarNormalized: denosemver.ParseJsrDepPackageReqNormalized,
	}
	kinds := map[string]denosemver.PackageKind{"jsr": denosemver.Jsr, "npm": denosemver.Npm}
	for _, r := range tsvRows(t, "jsr_dep.tsv") {
		t.Run(r.name, func(t *testing.T) {
			t.Parallel()
			in := r.field(t, 1)
			dep, err := parsers[r.field(t, 0)](in)
			if err != nil {
				t.Fatal(err)
			}
			if want := kinds[r.field(t, 2)]; dep.Kind != want {
				t.Errorf("Kind = %v, want %v", dep.Kind, want)
			}
			if got, want := dep.Req.Name, r.field(t, 3); got != want {
				t.Errorf("Name = %q, want %q", got, want)
			}
			// Knope matches the raw text of the requirement against the resolved versions
			if got, want := dep.Req.VersionReq.VersionText(), r.field(t, 4); got != want {
				t.Errorf("VersionText() = %q, want %q", got, want)
			}
			if got := dep.String(); got != in {
				t.Errorf("String() = %q, want %q", got, in)
			}
			if got, want := dep.Normalized(), r.field(t, 5); got != want {
				t.Errorf("Normalized() = %q, want %q", got, want)
			}
			back, err := denosemver.ParseJsrDepPackageReqNormalized(dep.Normalized())
			if err != nil {
				t.Fatal(err)
			}
			if !back.Equal(dep) {
				t.Errorf("%q does not parse back to the same requirement", dep.Normalized())
			}
		})
	}
}

func TestJsrDepPackageReqErrors(t *testing.T) {
	t.Parallel()
	tests := map[string]struct {
		parse  func(string) (denosemver.JsrDepPackageReq, error)
		scheme string
	}{
		"other:a@^1.0": {denosemver.ParseJsrDepPackageReq, "other"},
		"a@1":          {denosemver.ParseJsrDepPackageReq, "a@1"},
		"https://x/y":  {denosemver.ParseJsrDepPackageReqLoose, "https"},
		"":             {denosemver.ParseJsrDepPackageReqNormalized, ""},
	}
	for in, tt := range tests {
		t.Run(in, func(t *testing.T) {
			t.Parallel()
			_, err := tt.parse(in)
			var se *denosemver.NotExpectedSchemeError
			if !errors.As(err, &se) {
				t.Fatalf("error = %v, want *NotExpectedSchemeError", err)
			}
			want := "Unexpected JSR dependency scheme '" + tt.scheme + "'. Expected 'npm:' or 'jsr:'"
			if se.Scheme != tt.scheme || err.Error() != want {
				t.Errorf("scheme %q, text %q; want %q, %q", se.Scheme, err.Error(), tt.scheme, want)
			}
		})
	}
	_, err := denosemver.ParseJsrDepPackageReq("jsr:scope/name")
	var pe *denosemver.PackageReqParseError
	if !errors.As(err, &pe) || !errors.Is(err, denosemver.ErrMissingAtSymbol) ||
		err.Error() != "Invalid package requirement 'scope/name'" {
		t.Errorf("jsr:scope/name error = %v, want the missing @ error", err)
	}
}

func TestJsrDepPackageReqCompare(t *testing.T) {
	t.Parallel()
	tests := map[string]struct {
		a, b string
		want int
	}{
		"jsr before npm":  {"jsr:z@1", "npm:a@^1", -1},
		"same kind, name": {"jsr:a@1", "jsr:b@1", -1},
		"same kind, req":  {"npm:a@2", "npm:a@1", -1},
		"wildcard last":   {"npm:a", "npm:a@1", 1},
		"equal":           {"jsr:@std/path@^1", "jsr:@std/path@1", 0},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			a, err := denosemver.ParseJsrDepPackageReqLoose(tt.a)
			if err != nil {
				t.Fatal(err)
			}
			b, err := denosemver.ParseJsrDepPackageReqLoose(tt.b)
			if err != nil {
				t.Fatal(err)
			}
			if got := a.Compare(b); got != tt.want {
				t.Errorf("Compare(%q, %q) = %d, want %d", tt.a, tt.b, got, tt.want)
			}
			if got := b.Compare(a); got != -tt.want {
				t.Errorf("Compare(%q, %q) = %d, want %d", tt.b, tt.a, got, -tt.want)
			}
		})
	}
}

func TestPackageKind(t *testing.T) {
	t.Parallel()
	if got := denosemver.Jsr.SchemeWithColon(); got != "jsr:" {
		t.Errorf("Jsr = %q", got)
	}
	if got := denosemver.Npm.SchemeWithColon(); got != "npm:" {
		t.Errorf("Npm = %q", got)
	}
}
