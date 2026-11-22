// Package signing makes commits for the Commit step.
//
// A [Signer] makes one kind of commit. [Select] picks the signer from the options and from what is configured. Today
// there is one, the plain git commit; the ways to sign a commit are added behind the same interface.
//
// The package imports git, never workflow or steps.
package signing

import (
	"context"

	"github.com/celest-io/remeris/internal/diag"
	"github.com/celest-io/remeris/internal/git"
)

const codeOptions = "remeris::commit::options"

// Options are the commit options of one source: the Commit step, the command line and environment, or the [commit]
// table. A field that is not set (no list) leaves the decision to a lower source.
type Options struct {
	// Include and Exclude are glob patterns of the files to commit, as in internal/glob: the files that differ from HEAD
	// are the universe, Include narrows it (every file when it is empty) and Exclude then removes files.
	Include []string
	Exclude []string
}

// Over returns o with every field it leaves unset taken from lower. An empty list that is set (not nil) replaces the
// lower one.
func (o Options) Over(lower Options) Options {
	if o.Include == nil {
		o.Include = lower.Include
	}
	if o.Exclude == nil {
		o.Exclude = lower.Exclude
	}
	return o
}

// Settings are the resolved options: nothing is unset.
type Settings struct {
	Include []string
	Exclude []string
}

// Resolve applies the defaults.
func (o Options) Resolve() Settings { return Settings(o) }

// Request is one commit.
type Request struct {
	Message string
	// Changes are the files to commit, already filtered by include and exclude.
	Changes []git.Change
}

// Signer makes a commit.
type Signer interface {
	// Name says how the commit is made, for the dry run: "git".
	Name() string
	// Signed reports whether the commit it makes is signed.
	Signed() bool
	// Commit makes the commit and returns its id.
	Commit(ctx context.Context, req Request) (string, error)
}

// Config is the signing setup of a run, held by the workflow state: the options that come from outside the step (the
// command line and the environment over the [commit] table).
type Config struct {
	Options Options
}

// Env is what Select looks at.
type Env struct {
	// Git is the repository. It must be a git.Committer too, or nothing can be committed.
	Git git.Git
}

// Select picks the signer for settings. Today it is always the plain git commit, which follows the user's own Git
// configuration.
func Select(_ context.Context, _ Settings, env Env) (Signer, error) {
	c, ok := git.AsCommitter(env.Git)
	if !ok {
		return nil, diag.New("Could not open Git repository: not a Git repository").
			WithCode("remeris::git::open_repo").
			WithHelp("Make sure you are in a Git repository and that you have permission to access it.")
	}
	return &gitSigner{git: c}, nil
}
