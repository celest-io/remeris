package steps

import (
	"context"

	"github.com/celest-io/remeris/internal/workflow"
)

// PrepareRelease updates versions and changelogs from the pending changes (milestone M5).
type PrepareRelease struct {
	PrereleaseLabel           string `toml:"prerelease_label"`
	AllowEmpty                bool   `toml:"allow_empty"`
	IgnoreConventionalCommits bool   `toml:"ignore_conventional_commits"`
}

func (*PrepareRelease) Type() string { return "PrepareRelease" }

// Run fails until the step is implemented, in a dry run too, so --validate reports it.
func (s *PrepareRelease) Run(_ context.Context, rt workflow.RunType[workflow.State]) (workflow.RunType[workflow.State], error) {
	return rt, notImplemented(s, "M5")
}
