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
	"github.com/celest-io/remeris/pkg/versioning/semver"
	"github.com/celest-io/remeris/pkg/versioning/versionedfile"
)

const goModFile = "go.mod"

func TestBumpVersionOverrideIsUsedAsGiven(t *testing.T) {
	cases := map[string]struct {
		override string
		want     string
	}{
		"higher":                {"5.0.0", `version = "5.0.0"`},
		"lower than the latest": {"0.0.1", `version = "0.0.1"`},
		"prerelease":            {"2.0.0-rc.1", `version = "2.0.0-rc.1"`},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Chdir(t.TempDir())
			if err := os.WriteFile(cargoFile, []byte(cargo(v123)), 0o600); err != nil {
				t.Fatal(err)
			}
			state := stateFor(t, []string{tagV123}, cargo(v123), "")
			v := semver.MustParse(tc.override)
			state.Packages[0].Override = &v

			_, err := (&steps.BumpVersion{Rule: steps.RulePatch}).Run(context.Background(), workflow.Real(state))
			if err != nil {
				t.Fatal(err)
			}

			if got, _ := os.ReadFile(cargoFile); !strings.Contains(string(got), tc.want) {
				t.Errorf("content = %q, want %q", got, tc.want)
			}
			if state.Packages[0].Override == nil {
				t.Error("BumpVersion must not consume the override: PrepareRelease takes it")
			}
		})
	}
}

func TestBumpVersionOverrideGoModBumpsMajor(t *testing.T) {
	v200 := semver.MustParse("2.0.0")
	cases := map[string]struct {
		override *semver.Version
		wantErr  error
		want     string
	}{
		"without override the rule cannot reach v2": {nil, versionedfile.ErrBumpingToV2, ""},
		"override bumps the major":                  {&v200, nil, "module github.com/x/y/v2 // v2.0.0"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Chdir(t.TempDir())
			if err := os.WriteFile(goModFile, []byte("module github.com/x/y\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			cfg := versionedfile.Config{Path: goModFile, Format: versionedfile.GoMod}
			f, err := versionedfile.New(cfg, "module github.com/x/y\n", []string{tagV123})
			if err != nil {
				t.Fatal(err)
			}
			state := workflow.State{AllVersionedFiles: []versionedfile.File{f}}
			pkg, err := versioning.NewPackage(versioning.DefaultName(), []versionedfile.Config{cfg},
				state.AllVersionedFiles, []string{tagV123}, versionedfile.Standard, nil)
			if err != nil {
				t.Fatal(err)
			}
			pkg.Override = tc.override
			state.Packages = []*versioning.Package{pkg}

			_, err = (&steps.BumpVersion{Rule: steps.RuleMajor}).Run(context.Background(), workflow.Real(state))

			if tc.wantErr != nil {
				if !errors.Is(err, tc.wantErr) {
					t.Fatalf("err = %v, want %v", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if got, _ := os.ReadFile(goModFile); !strings.Contains(string(got), tc.want) {
				t.Errorf("content = %q, want %q", got, tc.want)
			}
		})
	}
}
