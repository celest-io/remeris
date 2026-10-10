package cli

import (
	"bytes"
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/urfave/cli/v3"

	"github.com/celest-io/remeris/internal/config"
)

const (
	flagVerbose  = "verbose"
	flagValidate = "validate"
	flagUpgrade  = "upgrade"
	flagGenerate = "generate"
	rootName     = "remeris"
	flagDryRun   = "dry-run"
	wfPrep       = "prep"
	wfPlain      = "plain"
	argDryRun    = "--dry-run"
)

func loadConfig(t *testing.T, content string) *config.Config {
	t.Helper()
	path := filepath.Join(t.TempDir(), "knope.toml")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	return cfg
}

func run(t *testing.T, cfg *config.Config, args ...string) (stdout string, err error) {
	t.Helper()
	old := slog.Default()
	t.Cleanup(func() { slog.SetDefault(old) })
	var out, errOut bytes.Buffer
	err = New(cfg, &out, &errOut).Run(context.Background(), append([]string{rootName}, args...))
	return out.String(), err
}

func flagNames(cmd *cli.Command) []string {
	var names []string
	for _, f := range cmd.Flags {
		names = append(names, f.Names()[0])
	}
	return names
}

func subcommand(root *cli.Command, name string) *cli.Command {
	for _, c := range root.Commands {
		if c.Name == name {
			return c
		}
	}
	return nil
}

func has(names []string, name string) bool {
	for _, n := range names {
		if n == name {
			return true
		}
	}
	return false
}

const twoWorkflows = `
[[workflows]]
name = "plain"
help_text = "Just a command"
steps = [{ type = "Command", command = "echo hi" }]

[[workflows]]
name = "prep"
steps = [{ type = "PrepareRelease" }]

[[workflows]]
name = "bump"
steps = [{ type = "BumpVersion", rule = "Patch" }]
`

func TestRootFlagsDependOnConfigSource(t *testing.T) {
	cases := map[string]struct {
		cfg          *config.Config
		want, absent []string
	}{
		"default config": {
			config.Default(),
			[]string{flagDryRun, flagVerbose, flagGenerate},
			[]string{flagUpgrade, flagValidate},
		},
		"file config": {
			loadConfig(t, twoWorkflows),
			[]string{flagDryRun, flagVerbose, flagUpgrade, flagValidate},
			[]string{flagGenerate},
		},
		"hybrid config": {
			loadConfig(t, ""),
			[]string{flagDryRun, flagVerbose, flagUpgrade, flagValidate},
			[]string{flagGenerate},
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			names := flagNames(New(tc.cfg, nil, nil))
			for _, w := range tc.want {
				if !has(names, w) {
					t.Errorf("missing flag --%s in %v", w, names)
				}
			}
			for _, a := range tc.absent {
				if has(names, a) {
					t.Errorf("unexpected flag --%s in %v", a, names)
				}
			}
		})
	}
}

func TestWorkflowSubcommandsAndHelpText(t *testing.T) {
	root := New(loadConfig(t, twoWorkflows), nil, nil)
	var names []string
	for _, c := range root.Commands {
		names = append(names, c.Name)
	}
	if strings.Join(names, ",") != "plain,prep,bump" {
		t.Errorf("subcommands = %v", names)
	}
	if got := subcommand(root, wfPlain).Usage; got != "Just a command" {
		t.Errorf("usage = %q", got)
	}
}

func TestWorkflowFlagConditions(t *testing.T) {
	const onePackage = "[package]\n"
	const twoPackages = "[packages.a]\n[packages.b]\n"
	cases := map[string]struct {
		toml                    string
		workflow                string
		wantOverride, wantLabel bool
	}{
		"no packages: no override flag":    {twoWorkflows, wfPrep, false, true},
		"no PrepareRelease: no label":      {twoWorkflows, "bump", false, false},
		"plain workflow: neither":          {twoWorkflows, wfPlain, false, false},
		"one package, PrepareRelease":      {onePackage + twoWorkflows, wfPrep, true, true},
		"one package, BumpVersion":         {onePackage + twoWorkflows, "bump", true, false},
		"one package, plain":               {onePackage + twoWorkflows, wfPlain, false, false},
		"several packages, PrepareRelease": {twoPackages + twoWorkflows, wfPrep, true, true},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			names := flagNames(subcommand(New(loadConfig(t, tc.toml), nil, nil), tc.workflow))
			if got := has(names, "override-version"); got != tc.wantOverride {
				t.Errorf("override-version present = %v, want %v (flags %v)", got, tc.wantOverride, names)
			}
			if got := has(names, "prerelease-label"); got != tc.wantLabel {
				t.Errorf("prerelease-label present = %v, want %v (flags %v)", got, tc.wantLabel, names)
			}
		})
	}
}

func TestOverrideVersionUsageDependsOnPackageCount(t *testing.T) {
	usage := func(toml string) string {
		cmd := subcommand(New(loadConfig(t, toml+twoWorkflows), nil, nil), wfPrep)
		for _, f := range cmd.Flags {
			if f.Names()[0] == "override-version" {
				return f.(*cli.StringSliceFlag).Usage
			}
		}
		return ""
	}
	if got := usage("[package]\n"); strings.Contains(got, "package_name=version") {
		t.Errorf("single package usage mentions name=version: %q", got)
	}
	if got := usage("[packages.a]\n[packages.b]\n"); !strings.Contains(got, "package_name=version") {
		t.Errorf("multi package usage should mention name=version: %q", got)
	}
}

func TestPrereleaseLabelFlagReadsEnv(t *testing.T) {
	cmd := subcommand(New(loadConfig(t, twoWorkflows), nil, nil), wfPrep)
	var sources []string
	for _, f := range cmd.Flags {
		if f.Names()[0] == "prerelease-label" {
			sources = f.(*cli.StringFlag).Sources.EnvKeys()
		}
	}
	if strings.Join(sources, ",") != "REMERIS_PRERELEASE_LABEL,KNOPE_PRERELEASE_LABEL" {
		t.Errorf("env sources = %v", sources)
	}
}

func TestNoWorkflowSelected(t *testing.T) {
	_, err := run(t, loadConfig(t, twoWorkflows))
	if err == nil || err.Error() != "No workflow selected. Run 'remeris --help' for a list of options." {
		t.Errorf("err = %v", err)
	}
}

func TestUnknownWorkflow(t *testing.T) {
	_, err := run(t, loadConfig(t, twoWorkflows), "nope")
	if err == nil || err.Error() != "No workflow named nope" {
		t.Errorf("err = %v", err)
	}
}

func TestDryRunPrintsWouldRun(t *testing.T) {
	cfg := loadConfig(t, twoWorkflows)
	for _, args := range [][]string{{argDryRun, wfPlain}, {wfPlain, argDryRun}} {
		out, err := run(t, cfg, args...)
		if err != nil || out != "Would run echo hi\n" {
			t.Errorf("%v: out = %q, err = %v", args, out, err)
		}
	}
}

func TestVerboseLogsEachStep(t *testing.T) {
	cfg := loadConfig(t, twoWorkflows)
	out, err := run(t, cfg, argDryRun, "-v", wfPlain)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(out, "Running step Command ") || !strings.HasSuffix(out, "Would run echo hi\n") {
		t.Errorf("out = %q", out)
	}
	quiet, _ := run(t, cfg, argDryRun, wfPlain)
	if strings.Contains(quiet, "Running step") {
		t.Errorf("debug output without --verbose: %q", quiet)
	}
}

func TestStepFailureIsWrappedInWorkflowProblem(t *testing.T) {
	_, err := run(t, loadConfig(t, twoWorkflows), wfPrep)
	if err == nil || err.Error() != "Problem with workflow prep" {
		t.Fatalf("err = %v", err)
	}
}

func TestValidate(t *testing.T) {
	t.Run("collects every failing workflow and stays quiet", func(t *testing.T) {
		out, err := run(t, loadConfig(t, twoWorkflows), "--validate")
		if err == nil || err.Error() != "There are problems with the defined workflows" {
			t.Fatalf("err = %v", err)
		}
		if out != "" {
			t.Errorf("--validate must suppress logs, got %q", out)
		}
		type related interface{ Related() []error }
		got := err.(related).Related()
		if len(got) != 1 || got[0].Error() != "Problem with workflow prep" {
			t.Errorf("related = %v", got)
		}
	})
	t.Run("passes when every workflow works as a dry run", func(t *testing.T) {
		out, err := run(
			t,
			loadConfig(t, "[[workflows]]\nname=\"a\"\nsteps=[{type=\"Command\", command=\"false\"}]\n"),
			"--validate",
		)
		if err != nil || out != "" {
			t.Errorf("out = %q, err = %v", out, err)
		}
	})
	t.Run("wins over a workflow", func(t *testing.T) {
		out, err := run(t, loadConfig(t, twoWorkflows), "--validate", wfPlain)
		if err == nil || out != "" {
			t.Errorf("--validate should run instead of the workflow, out = %q, err = %v", out, err)
		}
	})
}

func TestGenerateAndUpgradeAreNotImplemented(t *testing.T) {
	_, err := run(t, config.Default(), "--generate")
	if err == nil || err.Error() != "--generate is not implemented yet (milestone M10)" {
		t.Errorf("generate: %v", err)
	}
	_, err = run(t, loadConfig(t, twoWorkflows), "--upgrade")
	if err == nil || err.Error() != "--upgrade is not implemented yet (milestone M10)" {
		t.Errorf("upgrade: %v", err)
	}
}

func TestUnavailableSpecialFlagsAreRejected(t *testing.T) {
	if _, err := run(t, loadConfig(t, twoWorkflows), "--generate"); err == nil {
		t.Error("--generate must not exist when knope.toml does")
	}
	if _, err := run(t, config.Default(), "--validate"); err == nil {
		t.Error("--validate must not exist without knope.toml")
	}
}

func TestVersionFlags(t *testing.T) {
	for _, flag := range []string{"--version", "-V"} {
		out, err := run(t, config.Default(), flag)
		if err != nil || !strings.HasPrefix(out, "remeris ") {
			t.Errorf("%s: out = %q, err = %v", flag, out, err)
		}
	}
}

func TestMainRendersErrors(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := os.WriteFile("knope.toml", []byte(twoWorkflows), 0o600); err != nil {
		t.Fatal(err)
	}
	old := slog.Default()
	t.Cleanup(func() { slog.SetDefault(old) })

	var out, errOut bytes.Buffer
	code := Main(context.Background(), []string{rootName, wfPrep}, &out, &errOut)

	want := "Error: Problem with workflow prep\n  ╰─▶ No packages to operate on\n" +
		"      help: There must be at least one package for remeris to work with, " +
		"no supported package files were found in this directory.\n"
	if code != 1 || errOut.String() != want {
		t.Errorf("code = %d, stderr = %q, want %q", code, errOut.String(), want)
	}
	if code := Main(context.Background(), []string{rootName, argDryRun, wfPlain}, &out, &errOut); code != 0 {
		t.Errorf("successful run exited %d", code)
	}
}

func TestMainReportsConfigErrors(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := os.WriteFile("knope.toml", []byte("[[workflows]]\nname = \"bad name\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	var out, errOut bytes.Buffer
	code := Main(context.Background(), []string{rootName}, &out, &errOut)
	if code != 1 || !strings.HasPrefix(errOut.String(), "Error: Workflow names may only contain") {
		t.Errorf("code = %d, stderr = %q", code, errOut.String())
	}
}

func TestPlainHandler(t *testing.T) {
	var buf bytes.Buffer
	log := slog.New(newPlainHandler(&buf, slog.LevelInfo)).With("k", "v").WithGroup("g")
	log.Debug("hidden")
	log.Info("shown", "a", 1)
	log.Warn("careful")
	if got := buf.String(); got != "shown\ncareful\n" {
		t.Errorf("output = %q", got)
	}
}

func TestSetupLogging(t *testing.T) {
	old := slog.Default()
	t.Cleanup(func() { slog.SetDefault(old) })
	var buf bytes.Buffer
	cases := map[string]struct {
		verbose, quiet bool
		want           string
	}{
		"default":                 {false, false, "info\n"},
		flagVerbose:               {true, false, "debug\ninfo\n"},
		"quiet":                   {false, true, ""},
		"quiet wins over verbose": {true, true, ""},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			buf.Reset()
			setupLogging(&buf, tc.verbose, tc.quiet)
			slog.Debug("debug")
			slog.Info("info")
			if buf.String() != tc.want {
				t.Errorf("output = %q, want %q", buf.String(), tc.want)
			}
		})
	}
}
