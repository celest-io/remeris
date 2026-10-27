package semver_test

import (
	"bytes"
	"context"
	"log/slog"
	"slices"
	"strings"
	"testing"

	"github.com/celest-io/remeris/pkg/versioning/semver"
	"github.com/celest-io/remeris/pkg/versioning/trace"
)

const (
	lblRC       = "Pre-release label rc selected. Determining next stable version..."
	noExisting  = "No existing pre-release version found; creating rc.0"
	useMinor    = "Using MINOR rule to bump from 1.2.3 to 1.3.0"
	v041        = "0.4.1"
	nameMajor   = "major"
	nameRelease = "release"
)

type lineHandler struct{ buf *bytes.Buffer }

func (lineHandler) Enabled(context.Context, slog.Level) bool { return true }
func (h lineHandler) Handle(_ context.Context, r slog.Record) error {
	h.buf.WriteString(r.Message + "\n")
	return nil
}
func (h lineHandler) WithAttrs([]slog.Attr) slog.Handler { return h }
func (h lineHandler) WithGroup(string) slog.Handler      { return h }

func capture() (*trace.Tracer, func() []string) {
	var buf bytes.Buffer
	return trace.New(slog.New(lineHandler{&buf})), func() []string {
		if buf.Len() == 0 {
			return nil
		}
		return strings.Split(strings.TrimSuffix(buf.String(), "\n"), "\n")
	}
}

func TestFromTagsTrace(t *testing.T) {
	t.Parallel()
	first := prefixName
	tests := map[string]struct {
		prefix *string
		tags   []string
		want   []string
	}{
		"no tags":                       {nil, nil, []string{"No tags found starting with v"}},
		"no tag with pattern":           {nil, []string{"x1.0.0", "first/v1.0.0"}, []string{"No tags found starting with v"}},
		"prefix without match":          {&first, []string{tag123}, []string{"No tags found starting with first/v"}},
		"match":                         {nil, []string{tag123}, nil},
		"match that is no version":      {nil, []string{"vnext", tag123}, nil},
		"only a tag that is no version": {nil, []string{"vnext"}, nil},
		"prefix match":                  {&first, []string{"first/v1.2.3"}, nil},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			tr, lines := capture()
			semver.FromTags(tc.prefix, tc.tags, tr)
			if got := lines(); !slices.Equal(got, tc.want) {
				t.Errorf("lines = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestCalculateNewVersionTrace(t *testing.T) {
	t.Parallel()
	tests := map[string]struct {
		stable string
		pres   []string
		rule   semver.Rule
		want   []string
		result string
	}{
		nameMajor: {v123, nil, semver.StableBump(semver.StableMajor),
			[]string{"Using MAJOR rule to bump from 1.2.3 to 2.0.0"}, v200},
		"minor": {v123, nil, semver.StableBump(semver.StableMinor),
			[]string{useMinor}, v130},
		"patch": {v123, nil, semver.StableBump(semver.StablePatch),
			[]string{"Using PATCH rule to bump from 1.2.3 to 1.2.4"}, v124},
		"major at 0": {v041, nil, semver.StableBump(semver.StableMajor),
			[]string{"Rule is MAJOR, but major component is 0. Bumping minor component from 0.4.1 to 0.5.0"}, "0.5.0"},
		"minor at 0": {v041, nil, semver.StableBump(semver.StableMinor),
			[]string{"Rule is MINOR, but major component is 0. Bumping patch component from 0.4.1 to 0.4.2"}, "0.4.2"},
		"patch at 0": {v041, nil, semver.StableBump(semver.StablePatch),
			[]string{"Using PATCH rule to bump from 0.4.1 to 0.4.2"}, "0.4.2"},
		"stable rule without stable": {"", []string{v130rc1}, semver.StableBump(semver.StableMajor), nil, v130},
		nameRelease:                  {"", []string{v130rc1}, semver.ReleaseBump(), nil, v130},
		"pre, stable, no existing": {v123, nil, semver.PreBump("rc", semver.StableMinor), []string{
			lblRC,
			useMinor,
			noExisting,
		}, v130rc0},
		"pre, stable, existing": {v123, []string{v130rc1}, semver.PreBump("rc", semver.StableMinor), []string{
			lblRC,
			useMinor,
			"Found existing pre-release version rc.1",
		}, "1.3.0-rc.2"},
		"pre, other label only": {v123, []string{v130beta0}, semver.PreBump("rc", semver.StableMinor), []string{
			lblRC,
			useMinor,
			noExisting,
		}, v130rc0},
		"pre, major 0, existing": {v041, []string{"0.5.0-rc.3"}, semver.PreBump("rc", semver.StableMajor), []string{
			lblRC,
			"Rule is MAJOR, but major component is 0. Bumping minor component from 0.4.1 to 0.5.0",
			"Found existing pre-release version rc.3",
		}, "0.5.0-rc.4"},
		"pre, major 0, no existing": {v041, nil, semver.PreBump("rc", semver.StableMinor), []string{
			lblRC,
			"Rule is MINOR, but major component is 0. Bumping patch component from 0.4.1 to 0.4.2",
			noExisting,
		}, "0.4.2-rc.0"},
		"pre, no stable, existing": {"", []string{v130rc1}, semver.PreBump("rc", semver.StablePatch), []string{
			lblRC,
			"Found existing pre-release version rc.1",
		}, "1.3.0-rc.2"},
		"pre, existing without counter": {v123, []string{"1.3.0-rc"}, semver.PreBump("rc", semver.StableMinor), []string{
			lblRC,
			useMinor,
			"Found existing pre-release version rc",
		}, v130rc0},
		"pre, no stable, no existing": {"", nil, semver.PreBump("rc", semver.StablePatch), []string{
			lblRC,
			noExisting,
		}, "0.0.0-rc.0"},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			tr, lines := capture()
			got, err := mustHistory(tc.stable, tc.pres...).CalculateNewVersion(tc.rule, tr)
			if err != nil {
				t.Fatal(err)
			}
			if got.String() != tc.result {
				t.Errorf("version = %s, want %s", got, tc.result)
			}
			if gotLines := lines(); !slices.Equal(gotLines, tc.want) {
				t.Errorf("lines = %q, want %q", gotLines, tc.want)
			}
		})
	}
}
