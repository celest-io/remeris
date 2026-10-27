package semver_test

import (
	"errors"
	"testing"

	"github.com/celest-io/remeris/internal/diag"
	"github.com/celest-io/remeris/pkg/versioning/semver"
)

const (
	v124rc0    = "1.2.4-rc.0"
	v124rc1    = "1.2.4-rc.1"
	v130rc1    = "1.3.0-rc.1"
	v100rc1    = "1.0.0-rc.1"
	tag123     = "v" + v123
	tag124rc0  = "v" + v124rc0
	v200rc2    = "2.0.0-rc.2"
	v130rc0    = "1.3.0-rc.0"
	v200rc0    = "2.0.0-rc.0"
	v100rc0    = "1.0.0-rc.0"
	v200       = "2.0.0"
	v124       = "1.2.4"
	v130       = "1.3.0"
	v130beta0  = "1.3.0-beta.0"
	v124beta1  = "1.2.4-beta.1"
	labelAlpha = "alpha"
	prefixName = "first"
	whatStable = "stable"
	whatLatest = "latest"
)

func mustHistory(stable string, pres ...string) semver.PackageVersions {
	var p semver.PackageVersions
	if stable != "" {
		p = semver.FromVersion(semver.MustParse(stable))
	}
	for _, s := range pres {
		p.Update(semver.MustParse(s))
	}
	return p
}

func checkOpt(t *testing.T, what, want string, v semver.Version, ok bool) {
	t.Helper()
	switch {
	case want == "" && ok:
		t.Errorf("%s = %s, want none", what, v)
	case want != "" && !ok:
		t.Errorf("%s: none, want %s", what, want)
	case want != "" && v.String() != want:
		t.Errorf("%s = %s, want %s", what, v, want)
	}
}

func TestCalculateNewVersion(t *testing.T) {
	t.Parallel()
	major, minor, patch := semver.StableMajor, semver.StableMinor, semver.StablePatch
	tests := map[string]struct {
		stable string
		pres   []string
		rule   semver.Rule
		want   string
	}{
		"major bump":               {v123, nil, semver.StableBump(major), v200},
		"major 0":                  {"0.1.2", nil, semver.StableBump(major), "0.2.0"},
		"major pre only":           {"", []string{v100rc0}, semver.StableBump(major), v100},
		"major unset":              {"", nil, semver.StableBump(major), v000},
		"major after pre 1":        {v123, []string{v124rc0}, semver.StableBump(major), v200},
		"major after pre 2":        {v123, []string{v130rc0}, semver.StableBump(major), v200},
		"major after pre 3":        {v123, []string{v200rc0}, semver.StableBump(major), v200},
		"minor":                    {v123, nil, semver.StableBump(minor), v130},
		"minor 0":                  {"0.1.2", nil, semver.StableBump(minor), "0.1.3"},
		"minor pre only":           {"", []string{v100rc0}, semver.StableBump(minor), v100},
		"minor unset":              {"", nil, semver.StableBump(minor), v000},
		"minor after pre 1":        {v123, []string{v124rc0}, semver.StableBump(minor), v130},
		"minor after pre 2":        {v123, []string{v130rc0}, semver.StableBump(minor), v130},
		"patch":                    {v123, nil, semver.StableBump(patch), v124},
		"patch 0":                  {"0.1.0", nil, semver.StableBump(patch), "0.1.1"},
		"patch pre only":           {"", []string{v100rc0}, semver.StableBump(patch), v100},
		"patch unset":              {"", nil, semver.StableBump(patch), v000},
		"patch after pre":          {v123, []string{v124rc0}, semver.StableBump(patch), v124},
		"pre bump":                 {v123, nil, semver.PreBump(lRc, minor), "1.3.0-rc.0"},
		"pre rc 0 then rc 1":       {v123, []string{v124rc0}, semver.PreBump(lRc, patch), v124rc1},
		"pre other label new core": {v123, []string{v124rc0}, semver.PreBump(labelAlpha, minor), "1.3.0-alpha.0"},
		"pre after same pre": {
			v123, []string{v130rc0, v124rc1, v200rc2}, semver.PreBump(lRc, minor), v130rc1,
		},
		"pre without stable": {
			"", []string{v130rc0, v124rc1, v200rc2}, semver.PreBump(lRc, minor), "2.0.0-rc.3",
		},
		"pre without anything": {"", nil, semver.PreBump(lRc, patch), "0.0.0-rc.0"},
		"pre after different pre version": {
			v123, []string{v124beta1, v124rc0}, semver.PreBump(lBeta, patch), "1.2.4-beta.2",
		},
		"pre after different pre label": {
			v123, []string{v130beta0}, semver.PreBump(lRc, minor), "1.3.0-rc.0",
		},
		"pre after label without counter": {
			v123, []string{"1.2.4-beta"}, semver.PreBump(lBeta, patch), "1.2.4-beta.0",
		},
		"pre with dotted label": {
			v123, []string{"1.2.4-alpha.beta.3"}, semver.PreBump(lAlphaBeta, patch), "1.2.4-alpha.beta.4",
		},
		"release": {
			"", []string{"1.2.3-rc.0", v124rc1, v200rc2}, semver.ReleaseBump(), v200,
		},
		"release on one pre": {v123, []string{"1.2.4-rc.4"}, semver.ReleaseBump(), v124},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			got, err := mustHistory(tc.stable, tc.pres...).CalculateNewVersion(tc.rule, nil)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got.String() != tc.want {
				t.Errorf("got %s, want %s", got, tc.want)
			}
		})
	}
}

func TestCalculateNewVersionInvalidLabel(t *testing.T) {
	t.Parallel()
	_, err := mustHistory(v123).CalculateNewVersion(semver.PreBump("a..b", semver.StablePatch), nil)
	var pe *semver.ParseError
	if !errors.As(err, &pe) {
		t.Fatalf("got %v, want a *ParseError", err)
	}
}

func TestReleaseWithoutPreRelease(t *testing.T) {
	t.Parallel()
	tests := map[string]semver.PackageVersions{
		"empty history": {},
		"stable only":   mustHistory(v123),
	}
	for name, p := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			_, err := p.CalculateNewVersion(semver.ReleaseBump(), nil)
			if !errors.Is(err, semver.ErrPreReleaseNotFound) {
				t.Fatalf("got %v, want ErrPreReleaseNotFound", err)
			}
			const want = "No prerelease version found, but a Release rule was requested"
			if err.Error() != want {
				t.Errorf("message = %q, want %q", err, want)
			}
			var d diag.Diagnostic
			if !errors.As(err, &d) || d.Code() != "remeris::pre_release_not_found" {
				t.Errorf("want a diagnostic with code remeris::pre_release_not_found, got %v", err)
			}
		})
	}
}

func TestFromTags(t *testing.T) {
	t.Parallel()
	first, vName := prefixName, "v"
	tests := map[string]struct {
		prefix     *string
		tags       []string
		wantStable string
		wantLatest string
	}{
		"collect all newer pre-releases": {
			nil,
			[]string{"v2.0.0-alpha.0", "v1.3.0-beta.0", "v1.3.0-alpha.1", "v1.3.0-alpha.0", tag124rc0, tag123},
			v123, "2.0.0-alpha.0",
		},
		"no tags":                {nil, nil, "", ""},
		"stable stops the scan":  {nil, []string{tag123, tag124rc0, "v2.0.0"}, v123, v123},
		"older pre-releases":     {nil, []string{tag123, "v1.2.0-rc.1"}, v123, v123},
		"unparsable skipped":     {nil, []string{"vfoo", "v1.2", tag124rc0, "v1.2.3-", tag123}, v123, v124rc0},
		"other tags ignored":     {nil, []string{"first/v1.2.4", "x1.2.3", "1.2.3", tag123}, v123, v123},
		"prefix":                 {&first, []string{"first/v1.3.0", tag123, "second/v1.0.0"}, v130, v130},
		"prefix without match":   {&first, []string{tag123}, "", ""},
		"no prefix ignores some": {nil, []string{"first/v1.3.0", tag123}, v123, v123},
		"package named v":        {&vName, []string{"v/v1.4.0", tag123}, "1.4.0", "1.4.0"},
		"pattern appears twice":  {&first, []string{"first/vfirst/v1.0.0", "first/v1.0.0"}, v100, v100},
		"pre only":               {nil, []string{"v1.0.0-rc.1", "v1.0.0-rc.0"}, "", v100rc1},
		"build metadata kept":    {nil, []string{"v1.2.3+abc"}, "1.2.3+abc", "1.2.3+abc"},
		"non-counter pre":        {nil, []string{"v1.2.4-rc4", tag123}, v123, "1.2.4-rc4"},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			p := semver.FromTags(tc.prefix, tc.tags, nil)
			v, ok := p.LastStable()
			checkOpt(t, whatStable, tc.wantStable, v, ok)
			v, ok = p.Latest()
			checkOpt(t, whatLatest, tc.wantLatest, v, ok)
		})
	}
}

func TestFromTagsKeepsEveryLabel(t *testing.T) {
	t.Parallel()
	tags := []string{"v1.3.0-beta.0", "v1.3.0-alpha.1", "v1.3.0-alpha.0", tag123}
	p := semver.FromTags(nil, tags, nil)
	tests := map[string]string{labelAlpha: "1.3.0-alpha.2", lBeta: "1.3.0-beta.1"}
	for label, want := range tests {
		got, err := p.CalculateNewVersion(semver.PreBump(label, semver.StableMinor), nil)
		if err != nil || got.String() != want {
			t.Errorf("label %s: got %s, %v; want %s", label, got, err, want)
		}
	}
}

func TestUpdate(t *testing.T) {
	t.Parallel()
	tests := map[string]struct {
		stable     string
		pres       []string
		update     string
		wantStable string
		wantLatest string
	}{
		"newer stable clears pre-releases": {v123, []string{v124rc0}, v124, v124, v124},
		"older stable ignored":             {v123, []string{v124rc0}, "1.0.0", v123, v124rc0},
		"equal stable ignored":             {v123, []string{v124rc0}, "1.2.3+other", v123, v124rc0},
		"first stable":                     {"", []string{v124rc0}, v123, v123, v123},
		"pre is recorded":                  {v123, nil, v124rc0, v123, v124rc0},
		"higher pre replaces":              {v123, []string{v124rc0}, v124rc1, v123, v124rc1},
		"lower pre ignored":                {v123, []string{"1.2.4-rc.2"}, v124rc1, v123, "1.2.4-rc.2"},
		"equal pre ignored":                {v123, []string{"1.2.4-rc.2+a"}, "1.2.4-rc.2+b", v123, "1.2.4-rc.2+a"},
		"counterless pre below counter":    {v123, []string{v124rc0}, "1.2.4-rc", v123, v124rc0},
		"other label kept":                 {v123, []string{v124beta1}, v124rc0, v123, v124rc0},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			p := mustHistory(tc.stable, tc.pres...)
			p.Update(semver.MustParse(tc.update))
			v, ok := p.LastStable()
			checkOpt(t, whatStable, tc.wantStable, v, ok)
			v, ok = p.Latest()
			checkOpt(t, whatLatest, tc.wantLatest, v, ok)
		})
	}
}

func TestLatest(t *testing.T) {
	t.Parallel()
	tests := map[string]struct {
		stable string
		pres   []string
		want   string
	}{
		"empty history":             {"", nil, ""},
		"stable only":               {v123, nil, v123},
		"largest core wins":         {v123, []string{"2.0.0-alpha.4", v124rc0, v130beta0}, "2.0.0-alpha.4"},
		"several labels in a core":  {v123, []string{"1.3.0-alpha.9", v130beta0, v130rc1}, v130rc1},
		"precedence beats label":    {"", []string{"1.0.0-zeta.1", "1.0.0-alpha.2"}, "1.0.0-zeta.1"},
		"precedence on label":       {"", []string{v100rc1, "1.0.0-beta.10"}, v100rc1},
		"counterless below counter": {"", []string{"1.0.0-rc", "1.0.0-beta.1"}, "1.0.0-rc"},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			v, ok := mustHistory(tc.stable, tc.pres...).Latest()
			checkOpt(t, whatLatest, tc.want, v, ok)
		})
	}
}

func TestFromVersion(t *testing.T) {
	t.Parallel()
	stable := semver.FromVersion(semver.MustParse(v123))
	v, ok := stable.LastStable()
	checkOpt(t, whatStable, v123, v, ok)
	pre := semver.FromVersion(semver.MustParse(v124rc0))
	v, ok = pre.LastStable()
	checkOpt(t, whatStable, "", v, ok)
	v, ok = pre.Latest()
	checkOpt(t, whatLatest, v124rc0, v, ok)
}

func TestZeroValue(t *testing.T) {
	t.Parallel()
	var p semver.PackageVersions
	p.Update(semver.MustParse(v124rc0))
	v, ok := p.Latest()
	checkOpt(t, whatLatest, v124rc0, v, ok)
}

// TestFromTagsThenFiles follows Knope's package loading: the history from tags is updated with the version read
// from the package's files.
func TestFromTagsThenFiles(t *testing.T) {
	t.Parallel()
	tags := []string{tag124rc0, tag123}
	tests := map[string]struct {
		files      string
		wantStable string
		wantLatest string
	}{
		"files newer than the stable tag clear pre-releases": {v130, v130, v130},
		"files equal to the stable tag keep pre-releases":    {v123, v123, v124rc0},
		"files older than the stable tag are ignored":        {v100, v123, v124rc0},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			p := semver.FromTags(nil, tags, nil)
			p.Update(semver.MustParse(tc.files))
			v, ok := p.LastStable()
			checkOpt(t, whatStable, tc.wantStable, v, ok)
			v, ok = p.Latest()
			checkOpt(t, whatLatest, tc.wantLatest, v, ok)
		})
	}
}

func TestCloneIsIndependent(t *testing.T) {
	t.Parallel()
	a := semver.FromTags(nil, []string{tag124rc0, tag123}, nil)
	b := a.Clone()
	b.Update(semver.MustParse(v124rc1))
	b.Update(semver.MustParse(v130))
	if v, _ := a.Latest(); v.String() != v124rc0 {
		t.Errorf("original Latest() = %s after updating the clone, want %s", v, v124rc0)
	}
	if v, _ := a.LastStable(); v.String() != v123 {
		t.Errorf("original LastStable() = %s after updating the clone, want %s", v, v123)
	}
}

func TestUpdateCopiesTheVersion(t *testing.T) {
	t.Parallel()
	v := semver.MustParse(v124rc0)
	p := semver.FromVersion(v)
	v.Pre[0].Str = "changed"
	if got, _ := p.Latest(); got.String() != v124rc0 {
		t.Errorf("Latest() = %s after changing the caller's version, want %s", got, v124rc0)
	}
}

func TestPreCounterOverflow(t *testing.T) {
	t.Parallel()
	p := semver.FromTags(nil, []string{"v1.0.0-rc.18446744073709551615"}, nil)
	_, err := p.CalculateNewVersion(semver.PreBump("rc", semver.StablePatch), nil)
	var d diag.Diagnostic
	if !errors.As(err, &d) || d.Code() != "remeris::pre_release_counter_overflow" {
		t.Fatalf("CalculateNewVersion() error = %v, want the counter overflow diagnostic", err)
	}
}
