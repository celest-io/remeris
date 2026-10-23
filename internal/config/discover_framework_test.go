package config

import (
	"errors"
	"io/fs"
	"slices"
	"testing"

	"github.com/celest-io/remeris/pkg/versioning"
)

// fakeDetector returns a detector that records its name in calls and gives pkgs and err.
func fakeDetector(calls *[]string, name string, pkgs []PackageConfig, err error) detector {
	return func(fs.StatFS) ([]PackageConfig, error) {
		*calls = append(*calls, name)
		return pkgs, err
	}
}

func names(pkgs []PackageConfig) []string {
	var out []string
	for _, p := range pkgs {
		out = append(out, p.Name.String())
	}
	return out
}

func TestDiscoverWithOrderAndConcatenation(t *testing.T) {
	var calls []string
	list := []detector{
		fakeDetector(
			&calls,
			"a",
			[]PackageConfig{{Name: versioning.CustomName("a1")}, {Name: versioning.CustomName("a2")}},
			nil,
		),
		fakeDetector(&calls, "b", nil, nil),
		fakeDetector(&calls, "c", []PackageConfig{{Name: versioning.CustomName("c1")}}, nil),
	}
	// A default file exists, but found packages win over the default package.
	pkgs, err := discoverWith(rootOf(map[string]string{goModName: ""}), list)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"a1", "a2", "c1"}; !slices.Equal(names(pkgs), want) {
		t.Errorf("packages = %v, want %v", names(pkgs), want)
	}
	if want := []string{"a", "b", "c"}; !slices.Equal(calls, want) {
		t.Errorf("calls = %v, want %v", calls, want)
	}
}

func TestDiscoverWithFallsBackToDefaultPackage(t *testing.T) {
	var calls []string
	list := []detector{fakeDetector(&calls, "a", nil, nil)}

	pkgs, err := discoverWith(rootOf(map[string]string{goModName: "", defaultChangelog: ""}), list)
	if err != nil || len(pkgs) != 1 || pkgs[0].Changelog != defaultChangelog || len(pkgs[0].VersionedFiles) != 1 {
		t.Errorf("packages = %+v, %v; want the default package", pkgs, err)
	}

	pkgs, err = discoverWith(rootOf(nil), list)
	if err != nil || pkgs != nil {
		t.Errorf("packages = %+v, %v; want none", pkgs, err)
	}
}

func TestDiscoverWithErrorStops(t *testing.T) {
	var calls []string
	boom := errors.New("boom")
	list := []detector{
		fakeDetector(&calls, "a", []PackageConfig{{Name: versioning.CustomName("a1")}}, nil),
		fakeDetector(&calls, "b", nil, boom),
		fakeDetector(&calls, "c", []PackageConfig{{Name: versioning.CustomName("c1")}}, nil),
	}
	pkgs, err := discoverWith(rootOf(map[string]string{goModName: ""}), list)
	if !errors.Is(err, boom) || pkgs != nil {
		t.Errorf("packages = %+v, error = %v; want boom and no packages", pkgs, err)
	}
	if want := []string{"a", "b"}; !slices.Equal(calls, want) {
		t.Errorf("calls = %v, want %v", calls, want)
	}
}
