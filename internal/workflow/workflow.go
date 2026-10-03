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
)

// State is what steps pass along a workflow (spec F4.3). Packages, versioned files, git tags and
// the selected issue are added to it by later milestones.
type State struct{}

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
	return fmt.Sprintf("%s %+v", step.Type(), reflect.Indirect(reflect.ValueOf(step)).Interface())
}

// Problem wraps a step error as `Problem with workflow {name}`. The step error is both the
// cause and the related error, so it renders beneath the workflow's message.
func Problem(name string, err error) error {
	return diag.New("Problem with workflow " + name).WithRelated(err).WithCause(err)
}
