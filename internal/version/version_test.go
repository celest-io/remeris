package version

import "testing"

const unset = "unknown"

func TestFullAndServiceVersion(t *testing.T) {
	old := [7]string{Version, Revision, Branch, BuildUser, BuildDate, BuildNumber, GoVersion}
	t.Cleanup(func() {
		Version, Revision, Branch, BuildUser, BuildDate, BuildNumber, GoVersion = old[0], old[1], old[2], old[3], old[4], old[5], old[6]
	})

	tests := map[string]struct {
		set         func()
		wantFull    string
		wantService string
	}{
		"defaults": {
			set:         func() {},
			wantFull:    "unknown [unknown/unknown.unknown] (Go: unknown, User: unknown, Date: unknown)",
			wantService: "unknown/unknown.unknown",
		},
		"injected": {
			set: func() {
				Version, Revision, Branch, BuildUser = testModuleVersion, testRevision, "main", "ci"
				BuildDate, BuildNumber, GoVersion = testBuildDate, "42", testGoVersion
			},
			wantFull:    "v1.2.3 [main/abc123.42] (Go: go1.27.1, User: ci, Date: 2026-10-03_12:00:00)",
			wantService: "main/abc123.42",
		},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			Version, Revision, Branch, BuildUser = unset, unset, unset, unset
			BuildDate, BuildNumber, GoVersion = unset, unset, unset
			tc.set()
			if got := Full(); got != tc.wantFull {
				t.Errorf("Full() = %q, want %q", got, tc.wantFull)
			}
			if got := ServiceVersion(); got != tc.wantService {
				t.Errorf("ServiceVersion() = %q, want %q", got, tc.wantService)
			}
		})
	}
}
