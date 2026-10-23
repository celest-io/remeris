package cli

import (
	"errors"
	"strings"
	"testing"

	"github.com/celest-io/remeris/internal/config"
	"github.com/celest-io/remeris/internal/diag"
)

// Knope defines --generate only without a knope.toml and --upgrade and --validate only with one (main.rs, build_cli),
// so the other combinations are an unknown argument, as clap reports it.
func TestSpecialModesExistOnlyWithTheirConfigSource(t *testing.T) {
	type source struct {
		cfg  func(t *testing.T) *config.Config
		mode string
	}
	file := func(t *testing.T) *config.Config { return loadConfig(t, twoWorkflows) }
	hybrid := func(t *testing.T) *config.Config { return loadConfig(t, "") }
	none := func(t *testing.T) *config.Config { return defaultConfig(t) }
	cases := map[string]source{
		"generate with a file":        {file, "--" + flagGenerate},
		"generate with a hybrid file": {hybrid, "--" + flagGenerate},
		"upgrade without a file":      {none, "--" + flagUpgrade},
		"validate without a file":     {none, "--" + flagValidate},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := run(t, tc.cfg(t), tc.mode)
			want := "unexpected argument '" + tc.mode + "' found"
			if err == nil || err.Error() != want {
				t.Fatalf("err = %v, want %q", err, want)
			}
			var d *diag.Error
			if !errors.As(err, &d) || !strings.Contains(d.Help(), "-- "+tc.mode) {
				t.Errorf("help = %v", err)
			}
		})
	}
}

func TestUnknownFlagsAreUnexpectedArguments(t *testing.T) {
	cases := map[string]struct {
		args []string
		want string
	}{
		"long flag on the root":  {[]string{"--nope"}, "unexpected argument '--nope' found"},
		"short flag on the root": {[]string{"-z"}, "unexpected argument '-z' found"},
		"flag of a workflow":     {[]string{wfPlain, "--nope"}, "unexpected argument '--nope' found"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := run(t, loadConfig(t, twoWorkflows), tc.args...)
			if err == nil || err.Error() != tc.want {
				t.Errorf("err = %v, want %q", err, tc.want)
			}
		})
	}
}

// clap rejects a name that is no workflow before it looks at the mode flags.
func TestSpecialModeDoesNotHideAnUnknownWorkflow(t *testing.T) {
	_, err := run(t, loadConfig(t, twoWorkflows), "--validate", "nosuch")
	if err == nil || err.Error() != "No workflow named nosuch" {
		t.Errorf("err = %v", err)
	}
}
