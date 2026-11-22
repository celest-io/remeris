// Package signing makes commits for the Commit step, signed or not.
//
// A [Signer] makes one kind of commit. Three exist: the plain git commit, the git commit signed with the user's own
// key, and a commit made through a forge API, which the forge signs. The API signers sit behind [APICommitter], so a
// forge other than GitHub only has to implement that interface. [Select] picks the signer from the options and from
// what is configured.
//
// The package imports git and the GitHub forge, never workflow or steps.
package signing

import (
	"context"
	"strings"

	"github.com/celest-io/remeris/internal/diag"
	"github.com/celest-io/remeris/internal/git"
)

// Method says how a signed commit is made.
type Method string

const (
	// MethodAuto signs with git when git is set up to sign, else through the forge API when it is configured.
	MethodAuto Method = "auto"
	// MethodGit signs with the user's own key, through the git command.
	MethodGit Method = "git"
	// MethodAPI makes the commit through the forge API, which signs it.
	MethodAPI Method = "api"
)

const codeOptions = "remeris::commit::options"

// ParseMethod reads a signer name. The empty string is not set and gives "" and no error.
func ParseMethod(s string) (Method, error) {
	switch m := Method(strings.ToLower(strings.TrimSpace(s))); m {
	case "", MethodAuto, MethodGit, MethodAPI:
		return m, nil
	default:
		return "", diag.Newf("%q is not a signer", s).
			WithCode(codeOptions).
			WithHelp(`The signer is "auto", "git" or "api".`)
	}
}

// Options are the signing options of one source: the Commit step, the command line and environment, or the
// [commit] table. A field that is not set (nil, "" or no list) leaves the decision to a lower source.
type Options struct {
	// Sign forces signing (true) or turns it off (false). Unset signs when Git is set up to sign.
	Sign *bool
	// Signer limits the way to sign: auto, git or api.
	Signer Method
	// Include and Exclude are glob patterns of the files to commit, as in internal/glob: the files that differ from HEAD
	// are the universe, Include narrows it (every file when it is empty) and Exclude then removes files.
	Include []string
	Exclude []string
}

// Over returns o with every field it leaves unset taken from lower.
func (o Options) Over(lower Options) Options {
	if o.Sign == nil {
		o.Sign = lower.Sign
	}
	if o.Signer == "" {
		o.Signer = lower.Signer
	}
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
	// Sign is nil when no source said anything.
	Sign    *bool
	Signer  Method
	Include []string
	Exclude []string
}

// Resolve applies the defaults: the auto signer.
func (o Options) Resolve() Settings {
	s := Settings(o)
	if s.Signer == "" {
		s.Signer = MethodAuto
	}
	return s
}

// Request is one commit.
type Request struct {
	Message string
	// Changes are the files to commit, already filtered by include and exclude.
	Changes []git.Change
}

// Signer makes a commit.
type Signer interface {
	// Name says how the commit is made, for the dry run: "git", "git with your signing key" or "the GitHub API".
	Name() string
	// Signed reports whether the commit it makes is signed.
	Signed() bool
	// Commit makes the commit and returns its id.
	Commit(ctx context.Context, req Request) (string, error)
}

// APICommitter makes commits through a forge API. A forge that signs the commits it makes this way implements it.
type APICommitter interface {
	// Name is the forge, such as "GitHub".
	Name() string
	// Configured reports whether the forge is set up (its table is in the configuration and credentials are given)
	// without reading any secret or using the network, so the answer is the same in a dry run.
	Configured() bool
	// CommitFiles makes the commit on in.Branch and returns its id.
	CommitFiles(ctx context.Context, in APICommit) (string, error)
}

// APICommit is a commit made through a forge API.
type APICommit struct {
	Branch string
	// ExpectedHead is the commit the branch must be at on the forge.
	ExpectedHead string
	Message      string
	Additions    []File
	Deletions    []string
}

// File is a file added or changed by an API commit.
type File struct {
	// Path is slash-separated and relative to the root of the repository.
	Path    string
	Content []byte
}

// Config is the signing setup of a run, held by the workflow state: the options that come from outside the step (the
// command line and the environment over the [commit] table), and the forges that can make a signed commit.
type Config struct {
	Options Options
	API     []APICommitter
}

// Env is what Select looks at.
type Env struct {
	// Git is the repository. It must be a git.Committer too, or nothing can be committed.
	Git git.Git
	// API are the forges that can sign through their API, in order of preference.
	API []APICommitter
}

// Select picks the signer for settings.
//
//   - sign = false: the plain git commit with signing turned off, whatever the Git configuration says.
//   - sign unset: git with the user's own key when Git is set up to sign (commit.gpgsign true or user.signingkey set),
//     else the plain git commit. The forge API is never picked on its own: it makes the commit on the remote branch,
//     which is not something a local commit should do because a token happens to be around.
//   - sign = true: signing is forced and never skipped. Git when it is set up to sign, else the first configured forge,
//     else an error with the setup guide.
//
// The signer limits the ways to sign: "git" never uses a forge and "api" never uses git (so without sign = true it
// makes a plain commit).
func Select(ctx context.Context, s Settings, env Env) (Signer, error) {
	c, ok := git.AsCommitter(env.Git)
	if !ok {
		return nil, diag.New("Could not open Git repository: not a Git repository").
			WithCode("remeris::git::open_repo").
			WithHelp("Make sure you are in a Git repository and that you have permission to access it.")
	}
	if s.Sign != nil && !*s.Sign {
		return &gitSigner{git: c, noSign: true}, nil
	}

	var cfg git.SigningConfig
	if s.Signer != MethodAPI {
		var err error
		if cfg, err = c.SigningConfig(ctx); err != nil {
			return nil, err
		}
	}
	if cfg.Configured() {
		return &gitSigner{git: c, sign: true, cfg: cfg}, nil
	}
	if s.Sign == nil {
		return &gitSigner{git: c}, nil
	}

	if s.Signer != MethodGit {
		for _, a := range env.API {
			if a.Configured() {
				return &apiSigner{git: env.Git, committer: c, api: a}, nil
			}
		}
	}
	return nil, NotConfigured(s.Signer, env.API)
}
