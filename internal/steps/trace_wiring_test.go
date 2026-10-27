package steps_test

import (
	"context"
	"slices"
	"testing"

	"github.com/celest-io/remeris/internal/steps"
	"github.com/celest-io/remeris/internal/workflow"
)

const noTagsFoo = "No tags found starting with foo/v"

// These tests prove that the steps hand the core a tracer on the default logger: without it, --verbose loses the
// debug lines that Knope prints from the versioning core.

func TestPrepareReleaseTracesThroughTheCore(t *testing.T) {
	prepareDir(t, nil)
	logs := captureDebugLogs(t)
	state := stateFor(t, nil, cargo(v123), fooName)
	state.Git = &recordingGit{commits: commits(fixCommit)}

	if _, err := runPrepare(t, &steps.PrepareRelease{}, state, true); err != nil {
		t.Fatal(err)
	}

	want := []string{
		"Getting conventional commits since last release of package foo",
		noTagsFoo,
		"Determining new version for foo",
		"commit " + fixCommit + `\n\timplies rule PATCH`,
		"Using PATCH rule to bump from 1.2.3 to 1.2.4",
	}
	if got := logLines(logs); len(got) < len(want) || !slices.Equal(got[:len(want)], want) {
		t.Errorf("debug lines = %q\nwant         %q", got, want)
	}
}

func TestBumpVersionTracesThroughTheCore(t *testing.T) {
	t.Chdir(t.TempDir())
	logs := captureDebugLogs(t)
	state := stateFor(t, []string{tagV123}, cargo(v123), "")

	step := &steps.BumpVersion{Rule: steps.RuleMinor}
	if _, err := step.Run(context.Background(), workflow.DryRun(state)); err != nil {
		t.Fatal(err)
	}

	if got := logLines(logs); !slices.Contains(got, "Using MINOR rule to bump from 1.2.3 to 1.3.0") {
		t.Errorf("debug lines = %q, want the bump line", got)
	}
}
