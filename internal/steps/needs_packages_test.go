package steps

import (
	"testing"

	"github.com/celest-io/remeris/internal/variables"
	"github.com/celest-io/remeris/internal/workflow"
)

const echoVersion = "echo $version"

func TestWorkflowNeedsPackages(t *testing.T) {
	cases := map[string]struct {
		steps []workflow.Step
		want  bool
	}{
		"plain command":      {[]workflow.Step{&Command{Command: "echo hi"}}, false},
		"default $version":   {[]workflow.Step{&Command{Command: echoVersion}}, true},
		"default $changelog": {[]workflow.Step{&Command{Command: "echo $changelog"}}, true},
		"substitution off":   {[]workflow.Step{&Command{Command: echoVersion, Variables: []variables.Entry{}}}, false},
		"issue branch only": {
			[]workflow.Step{
				&Command{Command: "echo $b", Variables: []variables.Entry{{Key: "$b", Variable: variables.IssueBranch}}},
			},
			false,
		},
		"custom key": {
			[]workflow.Step{
				&Command{Command: "echo $v", Variables: []variables.Entry{{Key: "$v", Variable: variables.Version}}},
			},
			true,
		},
		"prepare release":      {[]workflow.Step{&PrepareRelease{}}, true},
		"bump version":         {[]workflow.Step{&BumpVersion{}}, true},
		"release":              {[]workflow.Step{&Release{}}, true},
		"create change file":   {[]workflow.Step{&CreateChangeFile{}}, true},
		"switch branches only": {[]workflow.Step{&SwitchBranches{}}, false},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			if got := (workflow.Workflow{Steps: tc.steps}).NeedsPackages(); got != tc.want {
				t.Errorf("NeedsPackages() = %v, want %v", got, tc.want)
			}
		})
	}
}
