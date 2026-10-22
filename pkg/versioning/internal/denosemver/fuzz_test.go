package denosemver_test

import (
	"testing"

	"github.com/celest-io/remeris/pkg/versioning/internal/denosemver"
)

// fuzzSeeds returns the requirement texts of testdata/fuzz_seeds.txt and the empty string.
func fuzzSeeds(f *testing.F) []string {
	seeds := []string{""}
	for _, r := range tsvRows(f, "fuzz_seeds.txt") {
		seeds = append(seeds, r.fields[0])
	}
	return seeds
}

// printedStable reports whether printing is stable once a requirement has been printed: parsing the normalised
// text and printing it again gives the same text. An unsatisfiable range such as "1 - 0" needs one extra round
// (">=1.0.0 <1.0.0" reparses as ">1.0.0 <1.0.0", like in Rust), so the check starts from the second print.
func printedStable(parse func(string) (string, error), text string) (bool, string, string, string) {
	p1, err := parse(text)
	if err != nil {
		return false, "", "", err.Error()
	}
	p2, err := parse(p1)
	if err != nil {
		return false, p1, "", err.Error()
	}
	p3, err := parse(p2)
	if err != nil {
		return false, p2, "", err.Error()
	}
	return p2 == p3, p2, p3, ""
}

// FuzzVersionReq checks that no parser panics and that the normalised form is a fixed point.
func FuzzVersionReq(f *testing.F) {
	for _, s := range fuzzSeeds(f) {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, text string) {
		_, _ = denosemver.ParseVersionReqSpecifier(text)
		_, _ = denosemver.ParseVersionReqNormalized(text)
		if _, err := denosemver.ParseVersionReqLoose(text); err != nil {
			return
		}
		normalize := func(s string) (string, error) {
			r, err := denosemver.ParseVersionReqNormalized(s)
			if err != nil {
				return "", err
			}
			return r.Normalized(), nil
		}
		first, err := denosemver.ParseVersionReqLoose(text)
		if err != nil {
			return
		}
		ok, p2, p3, msg := printedStable(normalize, first.Normalized())
		if !ok {
			t.Fatalf("the normalised form of %q (%q) is not a fixed point: %q, %q %s", text, first.Normalized(), p2, p3, msg)
		}
	})
}

// FuzzPackageReq checks the package parsers for panics and the stability of the normalised form.
func FuzzPackageReq(f *testing.F) {
	for _, s := range fuzzSeeds(f) {
		f.Add(s)
		f.Add("pkg@" + s)
		f.Add("@scope/pkg@" + s)
		f.Add("jsr:@scope/pkg@" + s)
		f.Add("npm:pkg@" + s)
	}
	f.Fuzz(func(t *testing.T, text string) {
		_, _ = denosemver.ParsePackageReq(text)
		_, _ = denosemver.ParsePackageReqNormalized(text)
		_, _ = denosemver.ParsePackageNv(text)
		_, _ = denosemver.ParseJsrDepPackageReq(text)
		_, _ = denosemver.ParseStandard(text)
		req, err := denosemver.ParsePackageReqLoose(text)
		if err != nil {
			return
		}
		normalize := func(s string) (string, error) {
			r, err := denosemver.ParsePackageReqNormalized(s)
			if err != nil {
				return "", err
			}
			return r.Normalized(), nil
		}
		ok, p2, p3, msg := printedStable(normalize, req.Normalized())
		if !ok {
			t.Fatalf("the normalised form of %q (%q) is not a fixed point: %q, %q %s", text, req.Normalized(), p2, p3, msg)
		}
		if req.Compare(req) != 0 || !req.Equal(req) {
			t.Fatalf("%q does not compare equal to itself", text)
		}
	})
}
