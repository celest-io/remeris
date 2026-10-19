// Package workflow defines workflows, their steps and the runner (spec F4).
//
// The package has no dependency on any step implementation. Steps live in internal/steps and
// are attached to a Workflow when the configuration is decoded.
package workflow

import (
	"context"
	"fmt"
	"log/slog"
	"reflect"

	"github.com/celest-io/remeris/internal/diag"
	"github.com/celest-io/remeris/internal/forge/github"
	"github.com/celest-io/remeris/internal/git"
	"github.com/celest-io/remeris/internal/prompt"
	"github.com/celest-io/remeris/pkg/versioning"
	"github.com/celest-io/remeris/pkg/versioning/action"
	"github.com/celest-io/remeris/pkg/versioning/versionedfile"
)

// State is what steps pass along a workflow (spec F4.3). It is a plain value: the internal/project package
// builds it once per run, before the workflow starts. The selected issue, the forge clients and the pending
// actions are added to it by later milestones.
type State struct {
	// Packages are the configured packages, in declaration order. Empty when there are none.
	Packages []*versioning.Package
	// AllVersionedFiles holds one parsed file per versioned_files entry of every package, in order (spec F7.1).
	AllVersionedFiles []versionedfile.File
	// AllGitTags are the tags reachable from HEAD, newest first. Empty without packages or when Git failed.
	AllGitTags []string
	// Git is the repository access, or nil when there is no package or opening it failed. Opening fails only when
	// neither the git command is on PATH nor the built-in backend can open the directory: with the git command
	// it does not fail outside a repository, and reading the tags then does (their failure leaves AllGitTags empty).
	Git git.Git
	// PendingActions are the AddTag and CreateRelease actions that earlier steps returned and a later Release step
	// executes (spec F4.3, F13.4). Writes and removals are executed by the step that produced them. Only
	// PrepareRelease (M5) fills it; BumpVersion discards what it gets back, as Knope does.
	PendingActions []action.Action
	// Prompter asks the user questions (CreateChangeFile). The CLI sets the terminal; tests set prompt.Scripted.
	// It is nil when nothing can be asked, and steps then fail with a prompt error.
	Prompter prompt.Prompter
	// IgnoreConventionalCommits is the `ignore_conventional_commits` setting of `[changes]`.
	IgnoreConventionalCommits bool
	// GitHub is the GitHub forge, or nil when the configuration has no `[github]` table. It builds its client on
	// demand and caches it; a step must not ask for the client in a dry run. Clones of the state share it.
	GitHub *github.Forge
	// GiteaConfigured is true when the configuration has a `[gitea]` table. Release needs it to pick the forge.
	GiteaConfigured bool
}

// RunType says whether a step may change the world. A dry run must do no writes, make no network
// calls and run no external commands: it logs what it would have done instead.
type RunType[S any] struct {
	DryRun bool
	State  S
}

// Real returns a RunType that is allowed to act.
func Real[S any](state S) RunType[S] { return RunType[S]{State: state} }

// DryRun returns a RunType that only reports.
func DryRun[S any](state S) RunType[S] { return RunType[S]{DryRun: true, State: state} }

// Step is one entry of a workflow's `steps`. It is a tagged union keyed by the TOML `type`.
type Step interface {
	// Type is the step's `type` value in knope.toml, such as "Command".
	Type() string
	// Run executes the step and returns the state for the next one.
	Run(ctx context.Context, rt RunType[State]) (RunType[State], error)
}

// Workflow is a named list of steps. Each workflow becomes a subcommand.
type Workflow struct {
	Name     string
	HelpText string
	Steps    []Step
}

// HasStep reports whether any step has the given type.
func (w Workflow) HasStep(stepType string) bool {
	for _, step := range w.Steps {
		if step.Type() == stepType {
			return true
		}
	}
	return false
}

// Run executes the steps in order and stops at the first error, which is wrapped by Problem.
func Run(ctx context.Context, w Workflow, rt RunType[State]) error {
	for _, step := range w.Steps {
		slog.DebugContext(ctx, "Running step "+describe(step))
		var err error
		if rt, err = step.Run(ctx, rt); err != nil {
			return Problem(w.Name, err)
		}
	}
	return nil
}

// describe renders a step for the debug log: its type and field values.
func describe(step Step) string {
	v := reflect.Indirect(reflect.ValueOf(step))
	if v.Kind() == reflect.Struct && v.NumField() == 0 {
		return step.Type() // like Rust's Debug of a unit variant: "Release", not "Release {}"
	}
	return fmt.Sprintf("%s %+v", step.Type(), v.Interface())
}

// Problem wraps a step error as `Problem with workflow {name}`. The step error is both the
// cause and the related error, so it renders beneath the workflow's message.
func Problem(name string, err error) error {
	return diag.New("Problem with workflow " + name).WithRelated(err).WithCause(err)
}

// Clone returns a copy of the state that a workflow can change without the others seeing it: the packages are
// deep copies. The parsed files are immutable values (SetVersion returns new ones) and the tags, the repository
// handle and the settings are only read, so they are shared.
func (s State) Clone() State {
	if s.Packages != nil {
		pkgs := make([]*versioning.Package, len(s.Packages))
		for i, p := range s.Packages {
			pkgs[i] = p.Clone()
		}
		s.Packages = pkgs
	}
	return s
}
