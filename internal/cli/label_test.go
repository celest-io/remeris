package cli

import (
	"testing"

	"github.com/celest-io/remeris/internal/steps"
)

func TestSetPrereleaseLabel(t *testing.T) {
	cases := map[string]struct {
		label string
		want  string
	}{
		"label set":   {"rc", "rc"},
		"empty label": {"", ""},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			cfg := loadConfig(t, twoWorkflows)

			setPrereleaseLabel(cfg, tc.label)

			found := false
			for _, w := range cfg.Workflows {
				for _, step := range w.Steps {
					if p, ok := step.(*steps.PrepareRelease); ok {
						found = true
						if p.PrereleaseLabel != tc.want {
							t.Errorf("label = %q, want %q", p.PrereleaseLabel, tc.want)
						}
					}
				}
			}
			if !found {
				t.Fatal("no PrepareRelease step in the fixture")
			}
		})
	}
}
