package config

import (
	"github.com/celest-io/remeris/internal/steps"
	"github.com/celest-io/remeris/internal/workflow"
)

// DefaultWorkflows returns the workflows used when knope.toml defines none (spec F20.5).
func DefaultWorkflows(hasForge bool, packageCount int) []workflow.Workflow {
	commitMessage := "chore: prepare release $version"
	if packageCount >= 2 {
		commitMessage = "chore: prepare releases"
	}
	commit := &steps.Command{Command: `git commit -m "` + commitMessage + `"`}

	var release []workflow.Step
	if hasForge {
		release = []workflow.Step{&steps.PrepareRelease{}, commit, &steps.Command{Command: "git push"}, &steps.Release{}}
	} else {
		release = []workflow.Step{
			&steps.PrepareRelease{}, commit, &steps.Release{},
			&steps.Command{Command: "git push"}, &steps.Command{Command: "git push --tags"},
		}
	}

	workflows := []workflow.Workflow{
		{
			Name:     "release",
			HelpText: "Prepare and create a new release (update changelog, bump version, create release)",
			Steps:    release,
		},
		{
			Name:     "document-change",
			HelpText: "Create a new change file to be included in the next release",
			Steps:    []workflow.Step{&steps.CreateChangeFile{}},
		},
	}
	if packageCount == 1 {
		workflows = append(workflows, workflow.Workflow{
			Name:     "get-version",
			HelpText: "Get the current version of the project",
			Steps:    []workflow.Step{&steps.Command{Command: `echo "$version"`}},
		})
	}
	return workflows
}
