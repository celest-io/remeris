package action_test

import (
	"reflect"
	"testing"

	"github.com/celest-io/remeris/pkg/versioning/action"
	"github.com/celest-io/remeris/pkg/versioning/semver"
)

const (
	tagV123   = "v1.2.3"
	ver123    = "1.2.3"
	pkgTag123 = "pkg/v1.2.3"
)

func strp(s string) *string { return &s }

func TestReleaseTag(t *testing.T) {
	t.Parallel()
	tests := map[string]struct {
		version string
		name    *string
		want    string
	}{
		"default package":      {ver123, nil, tagV123},
		"named package":        {ver123, strp("pkg"), pkgTag123},
		"name with slash":      {"0.1.0", strp("a/b"), "a/b/v0.1.0"},
		"prerelease and build": {"1.0.0-rc.1+b5", nil, "v1.0.0-rc.1+b5"},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if got := action.ReleaseTag(semver.MustParse(tc.version), tc.name); got != tc.want {
				t.Errorf("ReleaseTag = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestIsReleaseTag(t *testing.T) {
	t.Parallel()
	tests := map[string]struct {
		tag  string
		name *string
		want bool
	}{
		"default":                    {tagV123, nil, true},
		"default prerelease":         {"v1.0.0-rc.1", nil, true},
		"named":                      {pkgTag123, strp("pkg"), true},
		"name with slash":            {"a/b/v1.0.0", strp("a/b"), true},
		"named tag for default":      {pkgTag123, nil, false},
		"default tag for named":      {"v1.2.3", strp("pkg"), false},
		"other package":              {"other/v1.2.3", strp("pkg"), false},
		"no v prefix":                {ver123, nil, false},
		"double v":                   {"vv1.2.3", nil, false},
		"invalid version":            {"v1.2", nil, false},
		"invalid version named":      {"pkg/vnext", strp("pkg"), false},
		"empty":                      {"", nil, false},
		"only prefix":                {"v", nil, false},
		"name prefix of other name":  {"pkg-extra/v1.0.0", strp("pkg"), false},
		"leading zero is not semver": {"v01.2.3", nil, false},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if got := action.IsReleaseTag(tc.tag, tc.name); got != tc.want {
				t.Errorf("IsReleaseTag(%q) = %v, want %v", tc.tag, got, tc.want)
			}
		})
	}
}

func TestConstructors(t *testing.T) {
	t.Parallel()
	rel := action.Release{Title: "1.0.0", Version: semver.MustParse("1.0.0"), Notes: "n"}
	tests := map[string]struct {
		got  action.Action
		want action.Action
	}{
		"write": {
			action.WriteToFile("a", "c", "d"),
			action.Action{Kind: action.KindWriteToFile, Path: "a", Content: "c", Diff: "d"},
		},
		"remove": {action.RemoveFile("a"), action.Action{Kind: action.KindRemoveFile, Path: "a"}},
		"tag":    {action.AddTag("v1"), action.Action{Kind: action.KindAddTag, Tag: "v1"}},
		"release": {
			action.CreateRelease(rel),
			action.Action{Kind: action.KindCreateRelease, Release: &rel},
		},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if !tc.got.Equal(tc.want) {
				t.Errorf("got %+v, want %+v", tc.got, tc.want)
			}
		})
	}
}

func TestDedupe(t *testing.T) {
	t.Parallel()
	rel := func(notes string, pkg *string) action.Release {
		return action.Release{Title: "t", Version: semver.MustParse("1.0.0"), Notes: notes, PackageName: pkg}
	}
	tests := map[string]struct {
		in   []action.Action
		want []action.Action
	}{
		"nil":   {nil, nil},
		"empty": {[]action.Action{}, nil},
		"keeps first, preserves order": {
			[]action.Action{action.RemoveFile("b"), action.RemoveFile("a"), action.RemoveFile("b")},
			[]action.Action{action.RemoveFile("b"), action.RemoveFile("a")},
		},
		"writes differing by content are kept": {
			[]action.Action{action.WriteToFile("a", "1", "d"), action.WriteToFile("a", "2", "d")},
			[]action.Action{action.WriteToFile("a", "1", "d"), action.WriteToFile("a", "2", "d")},
		},
		"identical writes collapse": {
			[]action.Action{action.WriteToFile("a", "1", "d"), action.WriteToFile("a", "1", "d")},
			[]action.Action{action.WriteToFile("a", "1", "d")},
		},
		"writes differing by diff are kept": {
			[]action.Action{action.WriteToFile("a", "1", "d1"), action.WriteToFile("a", "1", "d2")},
			[]action.Action{action.WriteToFile("a", "1", "d1"), action.WriteToFile("a", "1", "d2")},
		},
		"same path different kind": {
			[]action.Action{action.RemoveFile("a"), action.WriteToFile("a", "", "")},
			[]action.Action{action.RemoveFile("a"), action.WriteToFile("a", "", "")},
		},
		"tags": {
			[]action.Action{action.AddTag("x"), action.AddTag("x"), action.AddTag("y")},
			[]action.Action{action.AddTag("x"), action.AddTag("y")},
		},
		"releases compare by contents, not pointer": {
			[]action.Action{action.CreateRelease(rel("n", nil)), action.CreateRelease(rel("n", nil))},
			[]action.Action{action.CreateRelease(rel("n", nil))},
		},
		"releases with different notes": {
			[]action.Action{action.CreateRelease(rel("n", nil)), action.CreateRelease(rel("m", nil))},
			[]action.Action{action.CreateRelease(rel("n", nil)), action.CreateRelease(rel("m", nil))},
		},
		"releases with equal package names": {
			[]action.Action{action.CreateRelease(rel("n", strp("p"))), action.CreateRelease(rel("n", strp("p")))},
			[]action.Action{action.CreateRelease(rel("n", strp("p")))},
		},
		"releases default vs named package": {
			[]action.Action{action.CreateRelease(rel("n", nil)), action.CreateRelease(rel("n", strp("p")))},
			[]action.Action{action.CreateRelease(rel("n", nil)), action.CreateRelease(rel("n", strp("p")))},
		},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			got := action.Dedupe(tc.in)
			if len(got) != len(tc.want) {
				t.Fatalf("got %d actions, want %d: %+v", len(got), len(tc.want), got)
			}
			for i := range got {
				if !got[i].Equal(tc.want[i]) {
					t.Errorf("[%d] = %+v, want %+v", i, got[i], tc.want[i])
				}
			}
		})
	}
}

func TestDedupeDoesNotModifyInput(t *testing.T) {
	t.Parallel()
	in := []action.Action{action.AddTag("x"), action.AddTag("x"), action.AddTag("y")}
	want := append([]action.Action(nil), in...)
	action.Dedupe(in)
	if !reflect.DeepEqual(in, want) {
		t.Errorf("input modified: %+v", in)
	}
}
