package workflow

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"

	"github.com/celest-io/remeris/internal/diag"
	"github.com/celest-io/remeris/pkg/versioning"
	"github.com/celest-io/remeris/pkg/versioning/semver"
)

type fakeStep struct {
	name string
	err  error
	ran  *[]string
}

func (f fakeStep) Type() string { return f.name }
func (f fakeStep) Run(_ context.Context, rt RunType[State]) (RunType[State], error) {
	*f.ran = append(*f.ran, f.name)
	return rt, f.err
}

func TestRunStopsAtFirstError(t *testing.T) {
	var ran []string
	boom := errors.New("boom")
	w := Workflow{Name: "release", Steps: []Step{
		fakeStep{"A", nil, &ran}, fakeStep{"B", boom, &ran}, fakeStep{"C", nil, &ran},
	}}

	err := Run(context.Background(), w, Real(State{}))

	if got := strings.Join(ran, ","); got != "A,B" {
		t.Errorf("steps run = %s, want A,B", got)
	}
	if err == nil || err.Error() != "Problem with workflow release" {
		t.Fatalf("err = %v", err)
	}
	if !errors.Is(err, boom) {
		t.Error("the step error should be reachable with errors.Is")
	}
	d, ok := err.(diag.Diagnostic)
	if !ok || len(d.Related()) != 1 || d.Related()[0] != boom {
		t.Errorf("the step error should be the related error, got %v", err)
	}
}

func TestRunSuccessAndEmptyWorkflow(t *testing.T) {
	var ran []string
	w := Workflow{Name: "ok", Steps: []Step{fakeStep{"A", nil, &ran}}}
	if err := Run(context.Background(), w, DryRun(State{})); err != nil || len(ran) != 1 {
		t.Errorf("Run() = %v, ran %v", err, ran)
	}
	if err := Run(context.Background(), Workflow{Name: "empty"}, Real(State{})); err != nil {
		t.Errorf("empty workflow: %v", err)
	}
}

func TestRunLogsEachStepAtDebug(t *testing.T) {
	var buf bytes.Buffer
	old := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(old) })

	var ran []string
	w := Workflow{Name: "w", Steps: []Step{fakeStep{"A", nil, &ran}, fakeStep{"B", nil, &ran}}}
	if err := Run(context.Background(), w, Real(State{})); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	if strings.Count(out, "level=DEBUG") != 2 || !strings.Contains(out, "Running step A") ||
		!strings.Contains(out, "Running step B") {
		t.Errorf("expected a debug line before each step, got:\n%s", out)
	}
}

func TestHasStep(t *testing.T) {
	var ran []string
	w := Workflow{Steps: []Step{fakeStep{"A", nil, &ran}, fakeStep{"B", nil, &ran}}}
	if !w.HasStep("B") || w.HasStep("C") {
		t.Error("HasStep gave the wrong answer")
	}
}

func TestRunTypeConstructors(t *testing.T) {
	if Real(State{}).DryRun || !DryRun(State{}).DryRun {
		t.Error("Real and DryRun set the wrong flag")
	}
}

func TestStateCloneIsolatesPackages(t *testing.T) {
	t.Parallel()
	v1, err := semver.Parse("1.0.0")
	if err != nil {
		t.Fatal(err)
	}
	orig := State{
		Packages:   []*versioning.Package{{Name: versioning.DefaultName(), Versions: semver.FromVersion(v1)}},
		AllGitTags: []string{"v1.0.0"},
	}

	c := orig.Clone()
	v2, err := semver.Parse("2.0.0")
	if err != nil {
		t.Fatal(err)
	}
	c.Packages[0].Versions = semver.FromVersion(v2)
	c.Packages[0].Versions.Update(v2)

	if got, _ := orig.Packages[0].LatestVersion(); got.String() != "1.0.0" {
		t.Errorf("original package version = %s, want 1.0.0", got)
	}
	if got, _ := c.Packages[0].LatestVersion(); got.String() != "2.0.0" {
		t.Errorf("cloned package version = %s, want 2.0.0", got)
	}
	if len(c.AllGitTags) != 1 {
		t.Errorf("tags = %v, want them carried over", c.AllGitTags)
	}
	if empty := (State{}).Clone(); empty.Packages != nil {
		t.Errorf("a state without packages must stay without: %v", empty.Packages)
	}
}
