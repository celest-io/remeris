package versionedfile

import (
	"testing"

	"github.com/celest-io/remeris/pkg/versioning/semver"
)

const (
	modRepo     = "module github.com/owner/repo"
	modRepoPath = "github.com/owner/repo"
)

func TestModuleLine(t *testing.T) {
	t.Parallel()
	v214 := semver.MustParse("2.1.4")
	v314 := semver.MustParse("3.1.4")
	tests := map[string]struct {
		line    string
		want    moduleLine
		display string
	}{
		"basic": {modRepo, moduleLine{module: modRepoPath}, modRepo},
		"major version": {
			modRepo + "/v2", moduleLine{module: modRepoPath, major: 2, hasMajor: true}, modRepo + "/v2",
		},
		"version": {
			modRepo + " // v2.1.4", moduleLine{module: modRepoPath, version: &v214}, modRepo + " // v2.1.4",
		},
		"major version and version": {
			modRepo + "/v2 // v3.1.4",
			moduleLine{module: modRepoPath, major: 2, hasMajor: true, version: &v314},
			modRepo + "/v2 // v3.1.4",
		},
		"plus sign in the major suffix, like Rust's u64 parsing": {
			modRepo + "/v+2", moduleLine{module: modRepoPath, major: 2, hasMajor: true}, modRepo + "/v2",
		},
		"lone plus sign is part of the path": {
			modRepo + "/v+", moduleLine{module: modRepoPath + "/v+"}, modRepo + "/v+",
		},
		"random comment": {
			modRepo + "/v2 // comment that is not the thing you expect",
			moduleLine{module: modRepoPath, major: 2, hasMajor: true},
			modRepo + "/v2",
		},
		"v1 suffix is dropped when rebuilt": {
			modRepo + "/v1", moduleLine{module: modRepoPath, major: 1, hasMajor: true}, modRepo,
		},
		"not a major": {modRepo + "/vx", moduleLine{module: modRepoPath + "/vx"}, modRepo + "/vx"},
		"comment without v": {
			modRepo + " // 2.1.4", moduleLine{module: modRepoPath}, modRepo,
		},
		"extra spaces": {
			"module   " + modRepoPath + "  //   v2.1.4",
			moduleLine{module: modRepoPath, version: &v214},
			modRepo + " // v2.1.4",
		},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			got, err := parseModuleLine(tc.line)
			if err != nil {
				t.Fatal(err)
			}
			if got.module != tc.want.module || got.major != tc.want.major || got.hasMajor != tc.want.hasMajor ||
				(got.version == nil) != (tc.want.version == nil) ||
				(got.version != nil && !got.version.Equal(*tc.want.version)) {
				t.Errorf("parse = %+v, want %+v", got, tc.want)
			}
			if got.String() != tc.display {
				t.Errorf("String() = %q, want %q", got.String(), tc.display)
			}
		})
	}
}

func TestModuleLineMissingPath(t *testing.T) {
	t.Parallel()
	_, err := parseModuleLine("module ")
	if err == nil || err.Error() != "missing module path" {
		t.Errorf("got %v", err)
	}
}
