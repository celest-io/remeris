package versionedfile_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/celest-io/remeris/internal/diag"
	"github.com/celest-io/remeris/pkg/versioning/action"
	"github.com/celest-io/remeris/pkg/versioning/semver"
	"github.com/celest-io/remeris/pkg/versioning/versionedfile"
)

const (
	versionPattern = `version:\s+(?<version>\d+\.\d+\.\d+)`
	readmePath     = "README.md"
	testTxt        = "test.txt"
	v200           = "2.0.0"
	v100           = "1.0.0"
	codeNoMatchT   = "remeris::text_file::no_match"
	content123     = "version: 1.2.3"
	helpEnsure     = "Ensure the regex"
	content200NL   = "version: 2.0.0\n"
	line200        = "version: 2.0.0"
)

func mustVersion(t *testing.T, s string) semver.Version {
	t.Helper()
	v, err := semver.Parse(s)
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func newRegex(path, content string, patterns ...string) (versionedfile.File, error) {
	return versionedfile.New(versionedfile.Config{Path: path, Format: versionedfile.RegexFile, Regex: patterns}, content, nil)
}

func TestRegexVersion(t *testing.T) {
	t.Parallel()
	tests := map[string]struct {
		content  string
		patterns []string
		want     string
	}{
		"simple": {"version: 1.2.3\nother: stuff", []string{versionPattern}, "1.2.3"},
		"readme example": {
			"steps:\n      - uses: knope-dev/action@v1\n        with:\n          version: 0.21.4",
			[]string{versionPattern}, "0.21.4",
		},
		"first match of several": {"version: 1.0.0\n\nDownload version: 3.0.0\n", []string{versionPattern}, v100},
		"first pattern wins": {
			`{"version": "1.0.0", "image": "app:v2.0.0"}`,
			[]string{`app:v(?<version>\d+\.\d+\.\d+)`, `"version": "(?<version>\d+\.\d+\.\d+)"`}, v200,
		},
		"prerelease":  {"version: 1.0.0-rc.1", []string{`version: (?<version>.+)`}, "1.0.0-rc.1"},
		"go P syntax": {"version: 1.0.0", []string{`version: (?P<version>[\d.]+)`}, v100},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			f, err := newRegex(testTxt, tc.content, tc.patterns...)
			if err != nil {
				t.Fatal(err)
			}
			got, err := f.Version()
			if err != nil {
				t.Fatal(err)
			}
			if got.String() != tc.want {
				t.Errorf("got %s, want %s", got, tc.want)
			}
		})
	}
}

func TestRegexErrors(t *testing.T) {
	t.Parallel()
	tests := map[string]struct {
		content  string
		patterns []string
		atNew    bool
		code     string
		msg      string
		help     string
	}{
		"invalid pattern": {
			content123, []string{"[invalid(regex"}, true, "remeris::text_file::invalid_pattern",
			"Invalid regex pattern '[invalid(regex' for test.txt: ", "Check the regex pattern syntax",
		},
		"missing group": {
			content123, []string{`version:\s+(\d+\.\d+\.\d+)`}, true, "remeris::text_file::missing_version_group",
			`Regex pattern 'version:\s+(\d+\.\d+\.\d+)' must contain at least one named capture group called 'version'`,
			`Use a named capture group like (?<version>\d+\.\d+\.\d+) in your regex`,
		},
		"other named group": {
			content123, []string{`(?<ver>\d+)`}, true, "remeris::text_file::missing_version_group",
			"must contain at least one named capture group called 'version'", "Use a named capture group",
		},
		"no match": {
			"no version here", []string{versionPattern}, false, codeNoMatchT,
			"Regex '" + versionPattern + "' did not match any content in test.txt",
			"Ensure the regex matches the version string in the file",
		},
		"second pattern does not match": {
			content123, []string{versionPattern, `app:v(?<version>\d+)`}, false, codeNoMatchT,
			"Regex 'app:v(?<version>\\d+)' did not match any content in test.txt", helpEnsure,
		},
		"optional group not taking part": {
			"x", []string{`x(?:(?<version>\d+))?`}, false, codeNoMatchT,
			"did not match any content in test.txt", helpEnsure,
		},
		"invalid matched version": {
			"version: 1.2",
			[]string{`version: (?<version>[\d.]+)`},
			false,
			"remeris::text_file::invalid_version",
			"Matched version '1.2' in test.txt is not a valid semantic version: ",
			"The matched string must be a valid semantic version",
		},
		"invalid version in second pattern": {
			"a: 1.0.0 b: nope",
			[]string{`a: (?<version>\S+)`, `b: (?<version>\S+)`},
			false,
			"remeris::text_file::invalid_version",
			"Matched version 'nope' in test.txt is not a valid semantic version: ",
			"The matched string",
		},
		"no patterns": {
			content123, []string{}, false, codeNoMatchT,
			"Regex '' did not match any content in test.txt", helpEnsure,
		},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			f, err := newRegex(testTxt, tc.content, tc.patterns...)
			if !tc.atNew {
				if err != nil {
					t.Fatalf("New: %v", err)
				}
				_, err = f.Version()
			}
			var d diag.Diagnostic
			if err == nil || !errors.As(err, &d) {
				t.Fatalf("got %v, want a diagnostic", err)
			}
			if !strings.HasPrefix(err.Error(), tc.msg) && !strings.Contains(err.Error(), tc.msg) {
				t.Errorf("message %q does not contain %q", err.Error(), tc.msg)
			}
			if d.Code() != tc.code || !strings.HasPrefix(d.Help(), tc.help) {
				t.Errorf("code %q help %q, want %q %q", d.Code(), d.Help(), tc.code, tc.help)
			}
		})
	}
}

func TestRegexNilPatterns(t *testing.T) {
	t.Parallel()
	f, err := versionedfile.New(versionedfile.Config{Path: testTxt, Format: versionedfile.RegexFile}, "x", nil)
	if f != nil || err == nil || err.Error() != "Regex '' did not match any content in test.txt" {
		t.Errorf("got %v, %v", f, err)
	}
}

func TestRegexSetVersion(t *testing.T) {
	t.Parallel()
	const multi = "# Example\nversion: 1.0.0\n\n## Installation\nDownload version: 1.0.0\n\n## Usage\nCurrent version: 1.0.0\n"
	tests := map[string]struct {
		content  string
		patterns []string
		version  string
		want     string
		diff     string
	}{
		"simple": {
			"version: 1.2.3\nother: stuff", []string{versionPattern}, v200,
			"version: 2.0.0\nother: stuff", line200,
		},
		"multiple matches all updated": {
			multi, []string{versionPattern}, v200,
			"# Example\nversion: 2.0.0\n\n## Installation\nDownload version: 2.0.0\n\n## Usage\nCurrent version: 2.0.0\n",
			"version: 2.0.0\nDownload version: 2.0.0\nCurrent version: 2.0.0",
		},
		"multiple patterns": {
			`{"version": "1.0.0", "image": "app:v1.0.0"}`,
			[]string{`"version": "(?<version>\d+\.\d+\.\d+)"`, `app:v(?<version>\d+\.\d+\.\d+)`}, v200,
			`{"version": "2.0.0", "image": "app:v2.0.0"}`, `{"version": "2.0.0", "image": "app:v2.0.0"}`,
		},
		"multiple patterns on separate lines": {
			"a 1.0.0\nb 1.0.0\nc 1.0.0\n", []string{`a (?<version>\S+)`, `c (?<version>\S+)`}, v200,
			"a 2.0.0\nb 1.0.0\nc 2.0.0\n", "a 2.0.0\nc 2.0.0",
		},
		"version text twice in one match (Q8)": {
			"pkg 1.0.0 (was 1.0.0)\nkeep 1.0.0\n", []string{`pkg (?<version>1\.0\.0) \(was 1\.0\.0\)`}, "1.1.0",
			"pkg 1.1.0 (was 1.1.0)\nkeep 1.0.0\n", "pkg 1.1.0 (was 1.1.0)",
		},
		"Q8 substring inside match": {
			"v=1.0.0 build 11.0.0\n", []string{`v=(?<version>1\.0\.0) build 11\.0\.0`}, "2.0.0",
			"v=2.0.0 build 12.0.0\n", "v=2.0.0 build 12.0.0",
		},
		"several changed lines trimmed": {
			"  version: 1.0.0  \nsame\n\tversion: 1.0.0\n", []string{versionPattern}, v200,
			"  version: 2.0.0  \nsame\n\tversion: 2.0.0\n", "version: 2.0.0\nversion: 2.0.0",
		},
		"crlf": {
			"version: 1.0.0\r\nx\r\n", []string{versionPattern}, v200, "version: 2.0.0\r\nx\r\n", line200,
		},
		"unchanged":               {content200NL, []string{versionPattern}, v200, content200NL, ""},
		"no match leaves content": {"nothing\n", []string{versionPattern}, v200, "nothing\n", ""},
		"unicode kept": {
			"héllo version: 1.0.0 ✓\n", []string{versionPattern}, v200, "héllo version: 2.0.0 ✓\n", "héllo version: 2.0.0 ✓",
		},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			f, err := newRegex(readmePath, tc.content, tc.patterns...)
			if err != nil {
				t.Fatal(err)
			}
			updated, err := f.SetVersion(mustVersion(t, tc.version), nil, versionedfile.GoVersioning(0))
			if err != nil {
				t.Fatal(err)
			}
			if updated.Path() != readmePath {
				t.Errorf("Path = %q", updated.Path())
			}
			got := updated.Write()
			if tc.diff == "" {
				if got != nil {
					t.Errorf("want no actions, got %v", got)
				}
				return
			}
			want := []action.Action{action.WriteToFile(readmePath, tc.want, tc.diff)}
			if len(got) != 1 || !got[0].Equal(want[0]) {
				t.Errorf("got %+v, want %+v", got, want)
			}
		})
	}
}

func TestRegexImmutableAndRepeated(t *testing.T) {
	t.Parallel()
	f, err := newRegex(readmePath, "version: 1.0.0\n", versionPattern)
	if err != nil {
		t.Fatal(err)
	}
	two, err := f.SetVersion(mustVersion(t, v200), nil, versionedfile.GoVersioning(0))
	if err != nil {
		t.Fatal(err)
	}
	if f.Write() != nil {
		t.Error("the receiver changed")
	}
	if v, _ := f.Version(); v.String() != v100 {
		t.Errorf("receiver version %s", v)
	}
	if v, err := two.Version(); err != nil || v.String() != v200 {
		t.Errorf("updated version %v, %v", v, err)
	}
	// A second identical set keeps the earlier diff, like Knope.
	again, err := two.SetVersion(mustVersion(t, v200), nil, versionedfile.GoVersioning(0))
	if err != nil {
		t.Fatal(err)
	}
	if got := again.Write(); len(got) != 1 || got[0].Diff != line200 || got[0].Content != content200NL {
		t.Errorf("got %+v", got)
	}
}

func TestRegexDependency(t *testing.T) {
	t.Parallel()
	f, err := newRegex(readmePath, "version: 1.0.0", versionPattern)
	if err != nil {
		t.Fatal(err)
	}
	dep := "left-pad"
	got, err := f.SetVersion(mustVersion(t, v200), &dep, versionedfile.GoVersioning(0))
	want := versionedfile.UnsupportedDependency(readmePath)
	if got != nil || err == nil || err.Error() != want.Error() {
		t.Errorf("got %v, %v; want %v", got, err, want)
	}
}

// TestRegexFixtures holds the in/out pairs of Knope's tests/bump_version/text_file/minor and
// tests/prepare_release/readme_version.
func TestRegexFixtures(t *testing.T) {
	t.Parallel()
	tests := map[string]struct {
		in, out, version, diff string
		current                string
	}{
		"bump_version/text_file/minor": {
			in:      "steps:\n  - uses: knope-dev/action@v1\n    with:\n      version: 1.0.0\n",
			out:     "steps:\n  - uses: knope-dev/action@v1\n    with:\n      version: 1.1.0\n",
			version: "1.1.0",
			diff:    "version: 1.1.0",
			current: v100,
		},
		"prepare_release/readme_version": {
			in: "# My Package\n\nInstall using:\n\n```yaml\nsteps:\n  - uses: knope-dev/action@v1\n    with:\n" +
				"      version: 0.1.0\n```\n",
			out: "# My Package\n\nInstall using:\n\n```yaml\nsteps:\n  - uses: knope-dev/action@v1\n    with:\n" +
				"      version: 0.1.1\n```\n",
			version: "0.1.1",
			diff:    "version: 0.1.1",
			current: "0.1.0",
		},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			f, err := newRegex(readmePath, tc.in, versionPattern)
			if err != nil {
				t.Fatal(err)
			}
			if v, err := f.Version(); err != nil || v.String() != tc.current {
				t.Fatalf("Version = %v, %v", v, err)
			}
			updated, err := f.SetVersion(mustVersion(t, tc.version), nil, versionedfile.GoVersioning(0))
			if err != nil {
				t.Fatal(err)
			}
			got := updated.Write()
			if len(got) != 1 || got[0].Content != tc.out || got[0].Diff != tc.diff || got[0].Path != readmePath {
				t.Errorf("got %+v", got)
			}
		})
	}
}
