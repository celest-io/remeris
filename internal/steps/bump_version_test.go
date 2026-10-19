package steps_test

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/celest-io/remeris/internal/steps"
	"github.com/celest-io/remeris/internal/workflow"
	"github.com/celest-io/remeris/pkg/versioning"
	"github.com/celest-io/remeris/pkg/versioning/action"
	"github.com/celest-io/remeris/pkg/versioning/semver"
	"github.com/celest-io/remeris/pkg/versioning/versionedfile"
)

const (
	cargoFile = "Cargo.toml"
	tagV123   = "v1.2.3"
	v123      = "1.2.3"
)

func cargo(version string) string { return "[package]\nname = \"x\"\nversion = \"" + version + "\"\n" }

// stateFor builds a state with one package per name ("" is the default package). All packages list the file.
func stateFor(t *testing.T, tags []string, content string, names ...string) workflow.State {
	t.Helper()
	cfg := versionedfile.Config{Path: cargoFile, Format: versionedfile.Cargo}
	f, err := versionedfile.New(cfg, content, nil)
	if err != nil {
		t.Fatal(err)
	}
	state := workflow.State{AllVersionedFiles: []versionedfile.File{f}, AllGitTags: tags}
	for _, name := range names {
		pn := versioning.DefaultName()
		if name != "" {
			pn = versioning.CustomName(name)
		}
		cfgs := []versionedfile.Config{cfg}
		pkg, err := versioning.NewPackage(pn, cfgs, state.AllVersionedFiles, tags, versionedfile.Standard, nil)
		if err != nil {
			t.Fatal(err)
		}
		state.Packages = append(state.Packages, pkg)
	}
	return state
}

func TestBumpVersionRuleMapping(t *testing.T) {
	t.Parallel()
	cases := map[string]struct {
		step steps.BumpVersion
		want semver.Rule
	}{
		"major":   {steps.BumpVersion{Rule: steps.RuleMajor}, semver.StableBump(semver.StableMajor)},
		"minor":   {steps.BumpVersion{Rule: steps.RuleMinor}, semver.StableBump(semver.StableMinor)},
		"patch":   {steps.BumpVersion{Rule: steps.RulePatch}, semver.StableBump(semver.StablePatch)},
		"pre":     {steps.BumpVersion{Rule: steps.RulePre, Label: "rc"}, semver.PreBump("rc", semver.StablePatch)},
		"release": {steps.BumpVersion{Rule: steps.RuleRelease}, semver.ReleaseBump()},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if got := tc.step.SemverRule(); got != tc.want {
				t.Errorf("rule = %+v, want %+v", got, tc.want)
			}
		})
	}
}

func TestBumpVersionNoPackages(t *testing.T) {
	t.Chdir(t.TempDir())
	logs := captureLogs(t)
	step := &steps.BumpVersion{Rule: steps.RuleMajor}

	rt, err := step.Run(context.Background(), workflow.Real(workflow.State{}))
	if err != nil {
		t.Fatal(err)
	}

	if logs.Len() != 0 || len(rt.State.PendingActions) != 0 {
		t.Errorf("nothing should happen: logs %q, pending %v", logs, rt.State.PendingActions)
	}
}

func TestBumpVersionDryRunThenReal(t *testing.T) {
	t.Chdir(t.TempDir())
	logs := captureLogs(t)
	if err := os.WriteFile(cargoFile, []byte(cargo(v123)), 0o600); err != nil {
		t.Fatal(err)
	}
	step := &steps.BumpVersion{Rule: steps.RuleMinor}
	tags := []string{tagV123}

	rt, err := step.Run(context.Background(), workflow.DryRun(stateFor(t, tags, cargo(v123), "")))
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(cargoFile); string(got) != cargo(v123) {
		t.Errorf("a dry run must not write: %q", got)
	}
	if want := "msg=\"Would add the following to Cargo.toml: version = 1.3.0\"\n"; logs.String() != want {
		t.Errorf("logs = %q, want %q", logs, want)
	}
	if rt.State.AllVersionedFiles != nil {
		t.Error("the versioned files are drained, so a later step does not write them again")
	}

	if _, err := step.Run(context.Background(), workflow.Real(stateFor(t, tags, cargo(v123), ""))); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(cargoFile); string(got) != cargo("1.3.0") {
		t.Errorf("content = %q", got)
	}
}

func TestBumpVersionReleaseWithoutPreRelease(t *testing.T) {
	t.Chdir(t.TempDir())
	step := &steps.BumpVersion{Rule: steps.RuleRelease}
	state := stateFor(t, []string{tagV123}, cargo(v123), "")

	_, err := step.Run(context.Background(), workflow.Real(state))

	if !errors.Is(err, semver.ErrPreReleaseNotFound) {
		t.Fatalf("err = %v", err)
	}
	if want := "No prerelease version found, but a Release rule was requested"; err.Error() != want {
		t.Errorf("message = %q", err)
	}
}

func TestBumpVersionPackagesSharingAFile(t *testing.T) {
	t.Chdir(t.TempDir())
	logs := captureLogs(t)
	if err := os.WriteFile(cargoFile, []byte(cargo("1.0.0")), 0o600); err != nil {
		t.Fatal(err)
	}
	state := stateFor(t, nil, cargo("1.0.0"), "a", "b")

	rt, err := (&steps.BumpVersion{Rule: steps.RuleMajor}).Run(context.Background(), workflow.Real(state))
	if err != nil {
		t.Fatal(err)
	}

	if got, _ := os.ReadFile(cargoFile); !strings.Contains(string(got), `version = "2.0.0"`) {
		t.Errorf("content = %q", got)
	}
	if logs.Len() != 0 || len(rt.State.PendingActions) != 0 {
		t.Errorf("logs %q, pending %v", logs, rt.State.PendingActions)
	}
}

func TestBumpVersionLeavesPendingActionsEmpty(t *testing.T) {
	t.Chdir(t.TempDir())
	const goMod = "module example.com/m\n\ngo 1.22\n"
	if err := os.WriteFile("go.mod", []byte(goMod), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := versionedfile.Config{Path: "go.mod", Format: versionedfile.GoMod}
	f, err := versionedfile.New(cfg, goMod, []string{tagV123})
	if err != nil {
		t.Fatal(err)
	}
	all := []versionedfile.File{f}
	pkg, err := versioning.NewPackage(
		versioning.DefaultName(), []versionedfile.Config{cfg}, all, []string{tagV123}, versionedfile.Standard, nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	state := workflow.State{Packages: []*versioning.Package{pkg}, AllVersionedFiles: all}

	rt, err := (&steps.BumpVersion{Rule: steps.RuleMinor}).Run(context.Background(), workflow.Real(state))
	if err != nil {
		t.Fatal(err)
	}

	if len(rt.State.PendingActions) != 0 {
		t.Errorf("PendingActions = %v, want none", rt.State.PendingActions)
	}
}

// taggingFile is a versioned file whose Write also returns an AddTag action.
type taggingFile struct{ versionedfile.File }

func (f taggingFile) SetVersion(v semver.Version, dep *string, gv versionedfile.GoVersioning) (versionedfile.File, error) {
	inner, err := f.File.SetVersion(v, dep, gv)
	return taggingFile{inner}, err
}

func (f taggingFile) Write() []action.Action { return append(f.File.Write(), action.AddTag("v9.9.9")) }

func TestBumpVersionDropsTagActions(t *testing.T) {
	t.Chdir(t.TempDir())
	cfg := versionedfile.Config{Path: cargoFile, Format: versionedfile.Cargo}
	inner, err := versionedfile.New(cfg, cargo(v123), nil)
	if err != nil {
		t.Fatal(err)
	}
	all := []versionedfile.File{taggingFile{inner}}
	pkg, err := versioning.NewPackage(
		versioning.DefaultName(), []versionedfile.Config{cfg}, all, nil, versionedfile.Standard, nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	state := workflow.State{Packages: []*versioning.Package{pkg}, AllVersionedFiles: all}

	rt, err := (&steps.BumpVersion{Rule: steps.RulePatch}).Run(context.Background(), workflow.DryRun(state))
	if err != nil {
		t.Fatal(err)
	}

	if len(rt.State.PendingActions) != 0 {
		t.Errorf("PendingActions = %v, want none", rt.State.PendingActions)
	}
}
