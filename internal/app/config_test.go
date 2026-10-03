package app

import (
	"context"
	"reflect"
	"testing"

	"github.com/urfave/cli/v3"
)

// run builds the same shape of command tree as internal/cli and returns the options seen by the
// workflow's action.
func run(t *testing.T, args ...string) (*GlobalOptions, *WorkflowOptions) {
	t.Helper()
	global, workflow := DefaultGlobalOptions(), DefaultWorkflowOptions()
	root := &cli.Command{
		Name: "remeris",
		Flags: []cli.Flag{
			&cli.BoolFlag{Name: "dry-run"},
			&cli.BoolFlag{Name: "verbose", Aliases: []string{"v"}},
			&cli.BoolFlag{Name: "validate", Local: true},
		},
		Before: func(ctx context.Context, cmd *cli.Command) (context.Context, error) {
			return ctx, LoadConfig(cmd, global)
		},
		Action: func(context.Context, *cli.Command) error { return nil },
		Commands: []*cli.Command{{
			Name: "release",
			Flags: []cli.Flag{
				&cli.StringSliceFlag{Name: "override-version"},
				&cli.StringFlag{Name: "prerelease-label", Sources: cli.EnvVars("REMERIS_PRERELEASE_LABEL", "KNOPE_PRERELEASE_LABEL")},
			},
			Action: func(_ context.Context, cmd *cli.Command) error { return LoadConfig(cmd, workflow) },
		}},
	}
	if err := root.Run(context.Background(), append([]string{"remeris"}, args...)); err != nil {
		t.Fatalf("Run(%v): %v", args, err)
	}
	return global, workflow
}

func TestLoadConfigDefaults(t *testing.T) {
	global, workflow := run(t, "release")
	if !reflect.DeepEqual(global, DefaultGlobalOptions()) {
		t.Errorf("global = %+v", global)
	}
	if !reflect.DeepEqual(workflow, DefaultWorkflowOptions()) {
		t.Errorf("workflow = %+v", workflow)
	}
}

func TestLoadConfigRootFlags(t *testing.T) {
	cases := map[string]struct {
		args []string
		want GlobalOptions
	}{
		"before the workflow":        {[]string{"--dry-run", "-v", "release"}, GlobalOptions{DryRun: true, Verbose: true}},
		"after the workflow":         {[]string{"release", "--dry-run", "--verbose"}, GlobalOptions{DryRun: true, Verbose: true}},
		"only dry run":               {[]string{"release", "--dry-run"}, GlobalOptions{DryRun: true}},
		"local flag on the root":     {[]string{"--validate"}, GlobalOptions{Validate: true}},
		"local flag with a workflow": {[]string{"--validate", "release"}, GlobalOptions{Validate: true}},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			global, _ := run(t, tc.args...)
			if *global != tc.want {
				t.Errorf("global = %+v, want %+v", *global, tc.want)
			}
		})
	}
}

func TestLoadConfigWorkflowFlags(t *testing.T) {
	cases := map[string]struct {
		args []string
		env  map[string]string
		want WorkflowOptions
	}{
		"flag": {
			args: []string{"release", "--prerelease-label", "rc"},
			want: WorkflowOptions{PrereleaseLabel: "rc"},
		},
		"remeris env var": {
			env:  map[string]string{"REMERIS_PRERELEASE_LABEL": "beta"},
			args: []string{"release"},
			want: WorkflowOptions{PrereleaseLabel: "beta"},
		},
		"knope env var as fallback": {
			env:  map[string]string{"KNOPE_PRERELEASE_LABEL": "alpha"},
			args: []string{"release"},
			want: WorkflowOptions{PrereleaseLabel: "alpha"},
		},
		"remeris env var wins over knope": {
			env:  map[string]string{"REMERIS_PRERELEASE_LABEL": "new", "KNOPE_PRERELEASE_LABEL": "old"},
			args: []string{"release"},
			want: WorkflowOptions{PrereleaseLabel: "new"},
		},
		"flag wins over env var": {
			env:  map[string]string{"REMERIS_PRERELEASE_LABEL": "env"},
			args: []string{"release", "--prerelease-label=flag"},
			want: WorkflowOptions{PrereleaseLabel: "flag"},
		},
		"one version override": {
			args: []string{"release", "--override-version", "1.2.3"},
			want: WorkflowOptions{OverrideVersion: []string{"1.2.3"}},
		},
		"repeated overrides": {
			args: []string{"release", "--override-version", "a=1.0.0", "--override-version=b=2.0.0"},
			want: WorkflowOptions{OverrideVersion: []string{"a=1.0.0", "b=2.0.0"}},
		},
		"root and workflow flags together": {
			args: []string{"--dry-run", "release", "--prerelease-label", "rc", "--override-version", "1.0.0"},
			want: WorkflowOptions{PrereleaseLabel: "rc", OverrideVersion: []string{"1.0.0"}},
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			for k, v := range tc.env {
				t.Setenv(k, v)
			}
			_, workflow := run(t, tc.args...)
			if !reflect.DeepEqual(*workflow, tc.want) {
				t.Errorf("workflow = %+v, want %+v", *workflow, tc.want)
			}
		})
	}
}

func TestLoadConfigKeepsPresetValuesForUnsetFlags(t *testing.T) {
	cfg := &WorkflowOptions{PrereleaseLabel: "preset"}
	cmd := &cli.Command{
		Name:   "remeris",
		Flags:  []cli.Flag{&cli.StringFlag{Name: "prerelease-label"}},
		Action: func(_ context.Context, cmd *cli.Command) error { return LoadConfig(cmd, cfg) },
	}
	if err := cmd.Run(context.Background(), []string{"remeris"}); err != nil {
		t.Fatal(err)
	}
	if cfg.PrereleaseLabel != "preset" {
		t.Errorf("an unset flag must not overwrite the default, got %q", cfg.PrereleaseLabel)
	}
}

func TestCommandPath(t *testing.T) {
	var gotRoot, gotChild, gotGrandchild string
	root := &cli.Command{
		Name: "remeris",
		Commands: []*cli.Command{{
			Name: "release",
			Commands: []*cli.Command{{
				Name: "deep",
				Action: func(_ context.Context, cmd *cli.Command) error {
					gotGrandchild, gotChild, gotRoot = commandPath(cmd), commandPath(cmd.Lineage()[1]), commandPath(cmd.Root())
					return nil
				},
			}},
		}},
	}
	if err := root.Run(context.Background(), []string{"remeris", "release", "deep"}); err != nil {
		t.Fatal(err)
	}
	if gotRoot != "remeris" || gotChild != "remeris.release" || gotGrandchild != "remeris.release.deep" {
		t.Errorf("paths = %q, %q, %q", gotRoot, gotChild, gotGrandchild)
	}
}
