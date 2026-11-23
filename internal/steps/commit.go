package steps

import (
	"context"
	"log/slog"
	"strings"

	"github.com/celest-io/remeris/internal/diag"
	"github.com/celest-io/remeris/internal/git"
	"github.com/celest-io/remeris/internal/signing"
	"github.com/celest-io/remeris/internal/variables"
	"github.com/celest-io/remeris/internal/workflow"
)

const codeCommit = "remeris::commit"

// Commit commits the changed files, signed or not. It is remeris' own step (Knope has none: its workflows run
// "git commit" through a Command step).
//
// Message is a template, like the command of a Command step. The files are the ones that differ from HEAD (modified,
// untracked and deleted ones; ignored files never) narrowed by Include and Exclude, which are globs. Other staged
// changes are left alone. With nothing left to commit the step does nothing.
//
// Include and Exclude may also come from --commit-include and --commit-exclude, their REMERIS_* variables and the
// `[commit]` table. The step's own keys win over those. The commit follows the user's own Git configuration, like a
// plain "git commit" does.
type Commit struct {
	Message string `toml:"message"`
	// Variables is nil when the key is absent, which selects the default variables. See variables.Template.
	Variables []variables.Entry `toml:"variables"`
	Include   []string          `toml:"include"`
	Exclude   []string          `toml:"exclude"`
}

func (*Commit) Type() string { return "Commit" }

// NeedsPackages is true when the message uses the Version or ChangelogEntry variable.
func (c *Commit) NeedsPackages() bool {
	return variables.Template{Text: c.Message, Variables: c.Variables}.NeedsPackages()
}

// Run makes the commit. A dry run reads Git (its configuration and what changed) but changes nothing, reads no
// credentials and uses no network.
func (c *Commit) Run(ctx context.Context, rt workflow.RunType[workflow.State]) (workflow.RunType[workflow.State], error) {
	message, err := variables.Template{Text: c.Message, Variables: c.Variables}.Replace(rt.State)
	if err != nil {
		return rt, err
	}
	if strings.TrimSpace(message) == "" {
		return rt, diag.New("The commit message is empty").
			WithCode(codeCommit).
			WithHelp("Set `message` on the Commit step.")
	}
	settings := signing.Options{Include: c.Include, Exclude: c.Exclude}.Over(rt.State.Signing.Options).Resolve()

	g, err := ensureGit(ctx, &rt.State)
	if err != nil {
		return rt, err
	}
	signer, err := signing.Select(ctx, settings, signing.Env{Git: g})
	if err != nil {
		return rt, err
	}
	committer, _ := git.AsCommitter(g) // Select checked it
	changes, err := committer.Changes(ctx)
	if err != nil {
		return rt, err
	}
	if changes, err = signing.Filter(changes, settings.Include, settings.Exclude); err != nil {
		return rt, err
	}

	if rt.DryRun {
		describeCommit(ctx, g, signer, message, changes)
		return rt, nil
	}
	if len(changes) == 0 {
		slog.InfoContext(ctx, "Nothing to commit")
		return rt, nil
	}
	sha, err := signer.Commit(ctx, signing.Request{Message: message, Changes: changes})
	if err != nil {
		return rt, err
	}
	slog.InfoContext(ctx, "Created commit "+sha[:min(len(sha), 7)]+": "+headline(message))
	return rt, nil
}

// describeCommit logs what a real run would do.
func describeCommit(ctx context.Context, g git.Git, signer signing.Signer, message string, changes []git.Change) {
	kind := "a commit"
	on := ""
	if branch, err := g.CurrentBranch(ctx); err == nil {
		on = " on " + branch
	}
	slog.InfoContext(ctx, "Would create "+kind+" using "+signer.Name()+on+": "+message)
	if len(changes) == 0 {
		return
	}
	slog.InfoContext(ctx, "Would commit these files:")
	for _, ch := range changes {
		slog.InfoContext(ctx, "  "+ch.Path)
	}
}

func headline(message string) string {
	first, _, _ := strings.Cut(strings.TrimSpace(message), "\n")
	return first
}
