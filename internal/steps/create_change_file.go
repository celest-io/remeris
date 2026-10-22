package steps

import (
	"context"
	"errors"
	"log/slog"

	"github.com/celest-io/remeris/internal/prompt"
	"github.com/celest-io/remeris/internal/workflow"
	"github.com/celest-io/remeris/pkg/versioning"
	"github.com/celest-io/remeris/pkg/versioning/action"
	"github.com/celest-io/remeris/pkg/versioning/changesets"
	"github.com/celest-io/remeris/pkg/versioning/releasenotes"
)

// Questions of the step, word for word as Knope asks them (spec F11.4).
const (
	askPackages    = "Which packages does this change affect?"
	askAtLeastOne  = "At least one package must be selected"
	askTypeDefault = "What type of change is this?"
	askSummary     = "What is a short summary of this change?"
	askSummaryHelp = "This will be used as a header in the changelog"
)

var (
	errNoOptions  = errors.New("available options can not be empty")
	errNoPrompter = errors.New("no prompter available")
)

// CreateChangeFile asks which packages a change affects, how, and for a summary, then writes a change file to
// ".changeset/" (spec F11.4). The questions go to State.Prompter. A dry run asks nothing and writes nothing.
type CreateChangeFile struct{}

// Type returns "CreateChangeFile".
func (*CreateChangeFile) Type() string { return "CreateChangeFile" }

// NeedsPackages is true: the step works on the packages.
func (*CreateChangeFile) NeedsPackages() bool { return true }

// Run asks the questions and writes the file. Packages are written to the front matter in the order the prompter
// returns them, which is option order (quirk Q5); an existing file with the same name is overwritten.
func (*CreateChangeFile) Run(
	ctx context.Context,
	rt workflow.RunType[workflow.State],
) (workflow.RunType[workflow.State], error) {
	if rt.DryRun {
		slog.InfoContext(ctx, "Would create a new change file")
		return rt, nil
	}
	state := rt.State
	if state.Prompter == nil {
		return rt, prompt.Error(errNoPrompter)
	}
	packages, err := choosePackages(ctx, state.Prompter, state.Packages)
	if err != nil {
		return rt, err
	}
	var entries []changesets.PackageChange
	for _, pkg := range packages {
		pc, err := askChangeType(ctx, state.Prompter, pkg)
		if err != nil {
			return rt, err
		}
		entries = append(entries, pc)
	}
	summary, err := state.Prompter.Input(ctx, askSummary, askSummaryHelp)
	if err != nil {
		return rt, err
	}
	change := changesets.Change{ID: changesets.NormalizeID(summary), Versioning: entries, Summary: "# " + summary}
	// fsx.WriteFile creates .changeset/ when it is missing.
	err = writeFile(
		false,
		action.Action{Kind: action.KindWriteToFile, Path: changesets.Dir + "/" + change.FileName(), Content: change.String()},
	)
	return rt, err
}

// choosePackages is the only package when there is exactly one, and the user's selection otherwise.
func choosePackages(ctx context.Context, p prompt.Prompter, all []*versioning.Package) ([]*versioning.Package, error) {
	if len(all) == 1 {
		return all, nil
	}
	if len(all) == 0 {
		// Knope's inquire prompt refuses to run without options.
		return nil, prompt.Error(errNoOptions)
	}
	names := make([]string, len(all))
	for i, pkg := range all {
		names[i] = pkg.Name.String()
	}
	picked, err := p.MultiSelect(ctx, askPackages, names, askAtLeastOne)
	if err != nil {
		return nil, err
	}
	out := make([]*versioning.Package, len(picked))
	for i, idx := range picked {
		out[i] = all[idx]
	}
	return out, nil
}

// askChangeType asks for the change type of one package, from the types of its sections.
func askChangeType(ctx context.Context, p prompt.Prompter, pkg *versioning.Package) (changesets.PackageChange, error) {
	sections := pkg.ReleaseNotes.Sections
	if sections == nil {
		sections = releasenotes.DefaultSections()
	}
	options := sections.ChangesetTypes()
	if len(options) == 0 {
		return changesets.PackageChange{}, prompt.Error(errNoOptions)
	}
	message := askTypeDefault
	if custom := pkg.Name.Custom(); custom != nil {
		message = "What type of change is this for " + *custom + "?"
	}
	idx, err := p.Select(ctx, message, options)
	if err != nil {
		return changesets.PackageChange{}, err
	}
	return changesets.PackageChange{Package: pkg.Name.String(), Type: changesets.ParseChangeType(options[idx])}, nil
}
