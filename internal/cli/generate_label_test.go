package cli

import (
	"os"
	"strings"
	"testing"

	"github.com/celest-io/remeris/internal/config"
	"github.com/celest-io/remeris/internal/steps"
)

const labelRC = "rc"

func prepareReleaseLabels(cfg *config.Config) []string {
	var labels []string
	for _, w := range cfg.Workflows {
		for _, s := range w.Steps {
			if p, ok := s.(*steps.PrepareRelease); ok {
				labels = append(labels, p.PrereleaseLabel)
			}
		}
	}
	return labels
}

func TestGenerateIgnoresThePrereleaseLabel(t *testing.T) {
	cases := map[string]struct {
		env  string
		args []string
	}{
		"alone":      {args: []string{"--generate"}},
		"flag":       {args: []string{"--generate", "release", "--prerelease-label", labelRC}},
		"remeris env": {env: "REMERIS_PRERELEASE_LABEL", args: []string{"--generate", "release"}},
		"knope env":   {env: "KNOPE_PRERELEASE_LABEL", args: []string{"--generate", "release"}},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			cfg := defaultConfig(t)
			if tc.env != "" {
				t.Setenv(tc.env, labelRC)
			}
			if _, err := run(t, cfg, tc.args...); err != nil {
				t.Fatal(err)
			}
			got, err := os.ReadFile(config.KnopeFile)
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(string(got), "prerelease_label") {
				t.Errorf("the label reached the file:\n%s", got)
			}
			for _, l := range prepareReleaseLabels(cfg) {
				if l != "" {
					t.Errorf("the label was applied to the configuration: %q", l)
				}
			}
		})
	}
}

func TestValidateStillGetsThePrereleaseLabel(t *testing.T) {
	cfg := loadConfig(t, "[[workflows]]\nname = \"release\"\nsteps = [{ type = \"PrepareRelease\" }]\n")
	// The validation itself may fail (there are no packages); only the label matters here.
	_, _ = run(t, cfg, "--validate", "release", "--prerelease-label", labelRC)
	labels := prepareReleaseLabels(cfg)
	if len(labels) != 1 || labels[0] != labelRC {
		t.Errorf("labels = %v, want [%s]", labels, labelRC)
	}
}
