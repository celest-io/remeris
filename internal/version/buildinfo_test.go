package version

import (
	"runtime/debug"
	"testing"
)

const (
	testModuleVersion = "v1.2.3"
	testRevision      = "abc123"
	testGoVersion     = "go1.27.1"
	testBuildDate     = "2026-10-03_12:00:00"
	testBuildDateDST  = "2026-10-03_12:30:00"
)

func TestWithBuildInfo(t *testing.T) {
	stamped := &debug.BuildInfo{
		GoVersion: testGoVersion,
		Main:      debug.Module{Version: testModuleVersion},
		Settings: []debug.BuildSetting{
			{Key: vcsRevision, Value: testRevision},
			{Key: vcsModified, Value: "false"},
			{Key: vcsTime, Value: "2026-10-03T12:00:00Z"},
		},
	}
	dirty := &debug.BuildInfo{
		Settings: []debug.BuildSetting{
			{Key: vcsRevision, Value: testRevision},
			{Key: vcsModified, Value: "true"},
			{Key: vcsTime, Value: "2026-10-03T14:30:00+02:00"},
		},
	}
	devel := &debug.BuildInfo{Main: debug.Module{Version: develVersion}}
	unparsedTime := &debug.BuildInfo{Settings: []debug.BuildSetting{{Key: vcsTime, Value: "yesterday"}}}
	unknownBuild := Build{Version: unknown, Revision: unknown, BuildDate: unknown, GoVersion: unknown}

	tests := map[string]struct {
		bi   *debug.BuildInfo
		in   Build
		want Build
	}{
		"ldflags set win over build info": {
			bi: stamped,
			in: Build{Version: "v9.9.9", Revision: "ldflags", BuildDate: "2026-01-01_00:00:00", GoVersion: "go1.0"},
			want: Build{
				Version: "v9.9.9", Revision: "ldflags", BuildDate: "2026-01-01_00:00:00", GoVersion: "go1.0",
			},
		},
		"unknown with module version": {
			bi:   stamped,
			in:   unknownBuild,
			want: Build{Version: testModuleVersion, Revision: testRevision, BuildDate: testBuildDate, GoVersion: testGoVersion},
		},
		"devel module version is skipped": {
			bi:   devel,
			in:   unknownBuild,
			want: unknownBuild,
		},
		"empty module version is skipped": {
			bi:   &debug.BuildInfo{},
			in:   unknownBuild,
			want: unknownBuild,
		},
		"modified tree marks the revision dirty": {
			bi:   dirty,
			in:   unknownBuild,
			want: Build{Version: unknown, Revision: testRevision + "-dirty", BuildDate: testBuildDateDST, GoVersion: unknown},
		},
		"ldflags revision is not marked dirty": {
			bi:   dirty,
			in:   Build{Version: unknown, Revision: "fromldflags", BuildDate: unknown, GoVersion: unknown},
			want: Build{Version: unknown, Revision: "fromldflags", BuildDate: testBuildDateDST, GoVersion: unknown},
		},
		"unparsable commit time is kept": {
			bi:   unparsedTime,
			in:   unknownBuild,
			want: Build{Version: unknown, Revision: unknown, BuildDate: "yesterday", GoVersion: unknown},
		},
		"no build info changes nothing": {
			bi:   nil,
			in:   unknownBuild,
			want: unknownBuild,
		},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			if got := withBuildInfo(tc.bi, tc.in); got != tc.want {
				t.Errorf("withBuildInfo() = %+v, want %+v", got, tc.want)
			}
		})
	}
}
