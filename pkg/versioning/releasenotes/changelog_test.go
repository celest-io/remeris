package releasenotes

import (
	"testing"
	"time"

	"github.com/celest-io/remeris/pkg/versioning/action"
	"github.com/celest-io/remeris/pkg/versioning/semver"
)

const (
	clPath    = "CHANGELOG.md"
	clNotes   = "## Features\n\n- New feature"
	clTitle11 = "1.1.0 (2023-05-02)"
	v012      = "0.1.2"
	nameEmpty = "empty"
	v100      = "1.0.0"
	v110rc1   = "1.1.0-rc.1"
	nameCRLF  = "crlf"
	noteA     = "- a"
	title020  = "0.2.0 (d)"
	title100  = "1.0.0 (d)"
	v110      = "1.1.0"
)

func TestParseTitle(t *testing.T) {
	t.Parallel()
	tests := map[string]struct {
		line  string
		ok    bool
		level HeaderLevel
		v     string
		date  string
	}{
		"no date":          {line: "## 0.1.2", ok: true, level: H2, v: v012},
		"with date":        {line: "## 0.1.2 (2023-05-02)", ok: true, level: H2, v: v012, date: "2023-05-02"},
		"no version":       {line: "## 2023-05-02"},
		"bad version":      {line: "## sad"},
		"h1":               {line: "# 0.1.2 (2023-05-02)", ok: true, level: H1, v: v012, date: "2023-05-02"},
		"h3":               {line: "### 0.1.2"},
		"no space":         {line: "##0.1.2"},
		"only header":      {line: "##"},
		nameEmpty:          {line: ""},
		"date without ()":  {line: "## 1.0.0 - 2020-12-25", ok: true, level: H2, v: v100, date: "2020-12-25"},
		"invalid date":     {line: "## 1.0.0 (2020-13-45)", ok: true, level: H2, v: v100},
		"prerelease":       {line: "## 1.1.0-rc.1", ok: true, level: H2, v: v110rc1},
		"tabs":             {line: "#\t1.0.0\t(2020-01-02)", ok: true, level: H1, v: v100, date: "2020-01-02"},
		"leading space":    {line: " ## 1.0.0", ok: true, level: H2, v: v100},
		"extra words":      {line: "## 1.0.0 Some name", ok: true, level: H2, v: v100},
		"date after words": {line: "## 1.0.0 Name (2020-01-02)", ok: true, level: H2, v: v100, date: "2020-01-02"},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			level, v, date, ok := parseTitle(tc.line)
			if ok != tc.ok {
				t.Fatalf("ok = %v, want %v", ok, tc.ok)
			}
			if !ok {
				return
			}
			if level != tc.level || v.String() != tc.v {
				t.Errorf("got %v %s, want %v %s", level, v, tc.level, tc.v)
			}
			var want time.Time
			if tc.date != "" {
				want, _ = time.Parse(time.DateOnly, tc.date)
			}
			if !date.Equal(want) {
				t.Errorf("date = %v, want %v", date, want)
			}
		})
	}
}

func TestNewChangelogLevel(t *testing.T) {
	t.Parallel()
	tests := map[string]struct {
		content string
		want    HeaderLevel
	}{
		nameEmpty:              {content: "", want: H2},
		"only title":           {content: "# Changelog\n", want: H2},
		"second is h1":         {content: "# CHANGELOG\n\n# 1.0.0\n\n## Breaking\n", want: H1},
		"second is h2":         {content: "# Changelog\n\n## 1.0.0\n", want: H2},
		"no title, h2 first":   {content: "## 1.0.0\n\n## 0.9.0\n", want: H2},
		"h3 counts as h2":      {content: "# T\n### x\n", want: H2},
		"no leading title, h1": {content: "# 1.0.0\n\n# 0.9.0\n", want: H1},
		"plain text ignored":   {content: "intro\n# T\ntext\n# 1.0.0\n", want: H1},
		nameCRLF:               {content: "# T\r\n\r\n# 1.0.0\r\n", want: H1},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if got := NewChangelog(clPath, tc.content).ReleaseHeaderLevel; got != tc.want {
				t.Errorf("level = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestSplitLines(t *testing.T) {
	t.Parallel()
	tests := map[string]struct {
		in   string
		want []string
	}{
		nameEmpty:           {in: "", want: nil},
		"newline only":      {in: "\n", want: []string{""}},
		"trailing newline":  {in: "a\nb\n", want: []string{"a", "b"}},
		"no trailing":       {in: "a\nb", want: []string{"a", "b"}},
		nameCRLF:            {in: "a\r\nb\r\n", want: []string{"a", "b"}},
		"blank line":        {in: "a\n\nb", want: []string{"a", "", "b"}},
		"lone cr at end":    {in: "a\r", want: []string{"a\r"}},
		"cr in the middle":  {in: "a\rb\n", want: []string{"a\rb"}},
		"crlf last no lf":   {in: "a\r\nb\r", want: []string{"a", "b\r"}},
		"two trailing nl":   {in: "a\n\n", want: []string{"a", ""}},
		"only crlf":         {in: "\r\n", want: []string{""}},
		"windows blank mid": {in: "a\r\n\r\nb", want: []string{"a", "", "b"}},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			got := splitLines(tc.in)
			if len(got) != len(tc.want) {
				t.Fatalf("got %q, want %q", got, tc.want)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Fatalf("got %q, want %q", got, tc.want)
				}
			}
		})
	}
}

func TestWithRelease(t *testing.T) {
	t.Parallel()
	tests := map[string]struct {
		content string
		title   string
		notes   string
		want    string
		diff    string
	}{
		"missing file (empty), H2": {
			content: "", title: clTitle11, notes: clNotes,
			want: "## 1.1.0 (2023-05-02)\n\n### Features\n\n- New feature\n",
			diff: "\n## 1.1.0 (2023-05-02)\n\n### Features\n\n- New feature\n",
		},
		"H1 changelog, before existing release": {
			content: "# CHANGELOG\n\n# 1.0.0\n\n## Breaking Changes\n\n### Well for one\n",
			title:   clTitle11,
			notes:   clNotes,
			want:    "# CHANGELOG\n\n# 1.1.0 (2023-05-02)\n\n## Features\n\n- New feature\n\n# 1.0.0\n\n## Breaking Changes\n\n### Well for one\n",
			diff:    "\n# 1.1.0 (2023-05-02)\n\n## Features\n\n- New feature\n",
		},
		"second prerelease (H2 default, insert before older)": {
			content: "## 1.1.0-rc.1\n\n### Features\n\n- New feature in first RC\n",
			title:   "1.1.0-rc.2 ([DATE])", notes: "## Features\n\n- New feature in second RC",
			want: "## 1.1.0-rc.2 ([DATE])\n\n### Features\n\n- New feature in second RC\n\n" +
				"## 1.1.0-rc.1\n\n### Features\n\n- New feature in first RC\n",
			diff: "\n## 1.1.0-rc.2 ([DATE])\n\n### Features\n\n- New feature in second RC\n",
		},
		"title only, append at end": {
			content: "# Changelog\n", title: "1.0.0 (2023-05-02)", notes: noteA,
			want: "# Changelog\n## 1.0.0 (2023-05-02)\n\n- a\n",
			diff: "\n## 1.0.0 (2023-05-02)\n\n- a\n",
		},
		"no trailing newline stays without": {
			content: "# Changelog", title: "1.0.0 (2023-05-02)", notes: noteA,
			want: "# Changelog\n## 1.0.0 (2023-05-02)\n\n- a",
			diff: "\n## 1.0.0 (2023-05-02)\n\n- a\n",
		},
		"no trailing newline, insert before release": {
			content: "# Changelog\n\n## 0.1.0\n\n- old", title: title020, notes: noteA,
			want: "# Changelog\n\n## 0.2.0 (d)\n\n- a\n\n## 0.1.0\n\n- old\n",
			diff: "\n## 0.2.0 (d)\n\n- a\n",
		},
		"crlf input becomes lf": {
			content: "# Changelog\r\n\r\n## 0.1.0\r\n\r\n- old\r\n", title: title020, notes: noteA,
			want: "# Changelog\n\n## 0.2.0 (d)\n\n- a\n\n## 0.1.0\n\n- old\n",
			diff: "\n## 0.2.0 (d)\n\n- a\n",
		},
		"release title of the other level is found": {
			content: "# Changelog\n\n# 0.1.0\n\n- old\n", title: title020, notes: noteA,
			want: "# Changelog\n\n# 0.2.0 (d)\n\n- a\n\n# 0.1.0\n\n- old\n",
			diff: "\n# 0.2.0 (d)\n\n- a\n",
		},
		"H2 changelog with a ## line (found as a title of another level)": {
			content: "# Changelog\n\n## 0.2.0\n\n- b\n\n# 0.1.0\n", title: "0.3.0 (d)", notes: noteA,
			want: "# Changelog\n\n## 0.3.0 (d)\n\n- a\n\n## 0.2.0\n\n- b\n\n# 0.1.0\n",
			diff: "\n## 0.3.0 (d)\n\n- a\n",
		},
		"complex change in H2 gets extra #": {
			content: "# Changelog\n\n## 0.1.0\n", title: title020, notes: "## Features\n\n### Title\n\ndetails\n\n- x",
			want: "# Changelog\n\n## 0.2.0 (d)\n\n### Features\n\n#### Title\n\ndetails\n\n- x\n\n## 0.1.0\n",
			diff: "\n## 0.2.0 (d)\n\n### Features\n\n#### Title\n\ndetails\n\n- x\n",
		},
		"empty notes": {
			content: "", title: title100, notes: "",
			want: "## 1.0.0 (d)\n\n",
			diff: "\n## 1.0.0 (d)\n\n\n",
		},
		"title with non-release lines kept": {
			content: "# Changelog\n\nSome intro text\n\n## Not a version\n\n## 1.0.0\n", title: "1.1.0 (d)", notes: noteA,
			want: "# Changelog\n\nSome intro text\n\n## Not a version\n\n## 1.1.0 (d)\n\n- a\n\n## 1.0.0\n",
			diff: "\n## 1.1.0 (d)\n\n- a\n",
		},
		"notes crlf and trailing newline normalised": {
			content: "", title: title100, notes: "- a\r\n- b\n",
			want: "## 1.0.0 (d)\n\n- a\n- b\n",
			diff: "\n## 1.0.0 (d)\n\n- a\n- b\n",
		},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			orig := NewChangelog(clPath, tc.content)
			rel := action.Release{Title: tc.title, Version: semver.MustParse("9.9.9"), Notes: tc.notes}
			got, act := orig.WithRelease(rel)
			if got.Content != tc.want {
				t.Errorf("content =\n%q\nwant\n%q", got.Content, tc.want)
			}
			wantAct := action.WriteToFile(clPath, tc.want, tc.diff)
			if !act.Equal(wantAct) {
				t.Errorf("action = %+v\nwant %+v", act, wantAct)
			}
			if got.Path != clPath || got.ReleaseHeaderLevel != orig.ReleaseHeaderLevel {
				t.Errorf("path or level changed: %+v", got)
			}
			if orig.Content != tc.content {
				t.Errorf("receiver was modified")
			}
		})
	}
}

func TestWithReleaseTwice(t *testing.T) {
	t.Parallel()
	c := NewChangelog(clPath, "")
	c, _ = c.WithRelease(action.Release{Title: title100, Notes: noteA})
	c, _ = c.WithRelease(action.Release{Title: "1.1.0 (d)", Notes: "- b"})
	want := "## 1.1.0 (d)\n\n- b\n\n## 1.0.0 (d)\n\n- a\n"
	if c.Content != want {
		t.Errorf("content = %q, want %q", c.Content, want)
	}
}

func TestGetRelease(t *testing.T) {
	t.Parallel()
	const h2 = "# Changelog\n\n## 1.1.0 (2023-05-02)\n\n### Features\n\n- New\n\n#### Detail\n\n## 1.0.0\n\n- first\n\n## 0.9.0\n"
	const h1 = "# CHANGELOG\n\n# 1.1.0 (2023-05-02)\n\n## Features\n\n- New\n\n## Fixes\n\n- Fix\n\n# 1.0.0\n\n- first\n"
	tests := map[string]struct {
		content   string
		version   string
		wantOK    bool
		wantTitle string
		wantNotes string
	}{
		"H2 found with date": {
			content: h2, version: v110, wantOK: true, wantTitle: clTitle11,
			wantNotes: "\n## Features\n\n- New\n\n### Detail\n",
		},
		"H2 found without date": {content: h2, version: v100, wantOK: true, wantTitle: v100, wantNotes: "\n- first\n"},
		"H2 empty notes":        {content: h2, version: "0.9.0"},
		"H2 not found":          {content: h2, version: "2.0.0"},
		"H1 found": {
			content: h1, version: v110, wantOK: true, wantTitle: clTitle11,
			wantNotes: "\n## Features\n\n- New\n\n## Fixes\n\n- Fix\n",
		},
		"H1 last release": {content: h1, version: v100, wantOK: true, wantTitle: v100, wantNotes: "\n- first"},
		"prefix of another version is skipped": {
			content:   "# C\n\n## 1.0.10\n\n- x\n\n## 1.0.1\n\n- y\n",
			version:   "1.0.1",
			wantOK:    true,
			wantTitle: "1.0.1",
			wantNotes: "\n- y",
		},
		"prerelease": {
			content: "## 1.1.0-rc.2 (d)\n\n- b\n\n## 1.1.0-rc.1\n\n- a\n", version: v110rc1, wantOK: true,
			wantTitle: v110rc1, wantNotes: "\n- a",
		},
		"stable does not match its prerelease": {content: "## 1.1.0-rc.1\n\n- a\n", version: v110},
		"empty changelog":                      {content: "", version: v100},
		"wrong level title ignored": {
			content: "# C\n\n## 1.0.0\n\n- a\n\n# 0.9.0\n\n- b\n", version: "0.9.0",
		},
		nameCRLF: {
			content: "# C\r\n\r\n## 1.0.0 (d)\r\n\r\n### F\r\n\r\n- a\r\n", version: v100, wantOK: true,
			wantTitle: title100, wantNotes: "\n## F\n\n- a",
		},
		"build metadata": {
			content: "## 1.0.0+build.5\n\n- a\n", version: "1.0.0+build.5", wantOK: true,
			wantTitle: "1.0.0+build.5", wantNotes: "\n- a",
		},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			got, ok := NewChangelog(clPath, tc.content).GetRelease(semver.MustParse(tc.version))
			if ok != tc.wantOK {
				t.Fatalf("ok = %v, want %v (%+v)", ok, tc.wantOK, got)
			}
			if !ok {
				return
			}
			if got.Title != tc.wantTitle || got.Notes != tc.wantNotes || !got.Version.Equal(semver.MustParse(tc.version)) ||
				got.PackageName != nil {
				t.Errorf("got %+v, want title %q notes %q", got, tc.wantTitle, tc.wantNotes)
			}
		})
	}
}

// A written release can be read back for both header levels.
func TestWriteThenRead(t *testing.T) {
	t.Parallel()
	notes := "## Features\n\n### Big\n\ndetails\n\n- small"
	for name, seed := range map[string]string{"H2": "# Changelog\n\n## 0.1.0\n\n- old\n", "H1": "# Changelog\n\n# 0.1.0\n\n- old\n"} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			v := semver.MustParse("0.2.0")
			c, _ := NewChangelog(clPath, seed).WithRelease(action.Release{Title: title020, Version: v, Notes: notes})
			got, ok := c.GetRelease(v)
			if !ok || got.Notes != "\n"+notes+"\n" || got.Title != title020 {
				t.Errorf("got %+v ok=%v", got, ok)
			}
		})
	}
}
