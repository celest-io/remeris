// Package github builds the GitHub client of remeris and turns its errors into diagnostics (spec F16.0, F16.1).
//
// Every GitHub call goes through go-github. The client is built from an authentication chain:
//
//  1. a GitHub App (flags and environment variables only, decision D11), exchanged for an installation token;
//  2. the GITHUB_TOKEN environment variable;
//  3. the token stored by an earlier run;
//  4. a prompt, for [Forge.RequireAuthentication] only.
//
// The package is a leaf: it must not import workflow or steps. The workflow state holds a *Forge, and the steps
// that talk to GitHub ask it for a [Client].
package github

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"

	gh "github.com/google/go-github/v92/github"
	"github.com/jferrl/go-githubauth"
	"golang.org/x/oauth2"

	"github.com/celest-io/remeris/internal/credentials"
	"github.com/celest-io/remeris/internal/diag"
	"github.com/celest-io/remeris/internal/httpx"
)

const (
	service   = "GitHub"
	userAgent = "Remeris"

	codeAuth = "remeris::github::authentication"
)

// Config is the `[github]` table of the configuration.
type Config struct {
	Owner string
	Repo  string
	// APIURL is the API base URL of a GitHub Enterprise instance (or a test server). Empty means github.com.
	// It goes through go-github's WithEnterpriseURLs, which appends "api/v3/" to the path unless it already ends
	// in "/api/v3/" or the host is an "api." host.
	APIURL string
	// UploadURL is the base URL for release assets. Empty means the host of APIURL, so go-github uploads to
	// "{host}/api/uploads/".
	UploadURL string
}

// App is the GitHub App the user asked for with the --github-app-* flags or their environment variables.
type App struct {
	// ID is the App ID.
	ID int64
	// InstallationID is the installation to act as. 0 means find the installation of the repository.
	InstallationID int64
	// PrivateKey returns the PEM private key. It is called once, when a client is built, so a key file is never
	// read in a dry run or by --validate.
	PrivateKey func() ([]byte, error)
}

// Client is a go-github client together with the repository it was built for. Embedding *gh.Client lets steps
// call c.Repositories, c.PullRequests and so on, and gives later changes a place to hang helper methods.
type Client struct {
	*gh.Client
	// Owner and Repo are those of the `[github]` table.
	Owner string
	Repo  string
	// Authenticated is false when the client sends no credentials.
	Authenticated bool
}

// Forge builds the GitHub client once and caches it.
//
// Callers must not ask for a client in a dry run: building one reads the credential store (which may copy a
// Knope credential), may read the private key and call GitHub, and RequireAuthentication may prompt. A dry run
// logs what would happen and leaves the forge alone.
//
// A Forge is safe for use by several goroutines.
type Forge struct {
	cfg   Config
	app   *App
	store *credentials.Store

	mu            sync.Mutex
	authenticated *Client
	anonymous     *Client
}

// New returns a Forge for cfg. app is nil when no GitHub App is configured. store finds the token and, through
// its Prompter, asks for it; nil means the zero Store (the real environment, no prompt).
func New(cfg Config, app *App, store *credentials.Store) *Forge {
	if store == nil {
		store = &credentials.Store{}
	}
	return &Forge{cfg: cfg, app: app, store: store}
}

// Config returns the `[github]` settings the forge was built with.
func (f *Forge) Config() Config { return f.cfg }

// RequireAuthentication returns a client that sends credentials, from the first source that has some: the
// GitHub App, GITHUB_TOKEN, the stored token, then a prompt (the answer is stored). It fails when none works.
// A client built by [Forge.MaybeAuthenticated] without credentials is not reused.
func (f *Forge) RequireAuthentication(ctx context.Context) (*Client, error) {
	return f.client(ctx, true)
}

// MaybeAuthenticated returns a client that sends credentials when the GitHub App, GITHUB_TOKEN or the stored
// token provides some, and an anonymous one otherwise. It never prompts.
func (f *Forge) MaybeAuthenticated(ctx context.Context) (*Client, error) {
	return f.client(ctx, false)
}

func (f *Forge) client(ctx context.Context, require bool) (*Client, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.authenticated != nil {
		return f.authenticated, nil
	}
	if !require && f.anonymous != nil {
		return f.anonymous, nil
	}

	src, err := f.tokenSource(ctx, require)
	if err != nil {
		return nil, err
	}
	if src == nil {
		slog.DebugContext(ctx, "Using GitHub without authentication")
		c, err := f.newClient(f.transport(http.DefaultTransport, false), false)
		if err != nil {
			return nil, err
		}
		f.anonymous = c
		return c, nil
	}
	c, err := f.newClient(f.transport(&oauth2.Transport{Source: src, Base: http.DefaultTransport}, true), true)
	if err != nil {
		return nil, err
	}
	f.authenticated = c
	return c, nil
}

// transport wraps base with the retry policy. authenticated only changes the rate-limit hint.
func (f *Forge) transport(base http.RoundTripper, authenticated bool) http.RoundTripper {
	t := httpx.NewRetryTransport(base)
	t.Authenticated = authenticated
	return t
}

func (f *Forge) newClient(rt http.RoundTripper, authenticated bool) (*Client, error) {
	opts := []gh.ClientOptionsFunc{gh.WithTransport(rt), gh.WithUserAgent(userAgent)}
	if f.cfg.APIURL != "" {
		upload := f.cfg.UploadURL
		if upload == "" {
			upload = defaultUploadURL(f.cfg.APIURL)
		}
		opts = append(opts, gh.WithEnterpriseURLs(f.cfg.APIURL, upload))
	}
	api, err := gh.NewClient(opts...)
	if err != nil {
		return nil, diag.New("The GitHub API URL is not valid: " + err.Error()).
			WithCode("remeris::github::invalid_url").
			WithHelp("Check api_url and upload_url in the [github] table.").
			WithCause(err)
	}
	return &Client{Client: api, Owner: f.cfg.Owner, Repo: f.cfg.Repo, Authenticated: authenticated}, nil
}

// defaultUploadURL is the upload base go-github derives the GitHub Enterprise upload URL from when only the API URL
// is configured: the API URL without its "api/v3/" suffix, so that "https://ghe/api/v3/" uploads to
// "https://ghe/api/uploads/" and not to "https://ghe/api/v3/api/uploads/".
func defaultUploadURL(apiURL string) string {
	base := strings.TrimSuffix(apiURL, "/") + "/"
	return strings.TrimSuffix(base, "api/v3/")
}

// tokenSource walks the authentication chain. It returns nil, nil when the request is to be anonymous.
func (f *Forge) tokenSource(ctx context.Context, require bool) (oauth2.TokenSource, error) {
	if f.app != nil && f.app.ID != 0 {
		return f.appTokenSource(ctx)
	}
	token, found, err := f.store.Lookup(ctx, credentials.GitHubToken)
	if err != nil {
		return nil, err
	}
	switch {
	case found && f.fromEnv(token):
		slog.DebugContext(ctx, "Using "+credentials.GitHubToken.Env())
	case found:
		slog.DebugContext(ctx, "Using the stored GitHub token")
	case require:
		if token, err = f.store.Get(ctx, credentials.GitHubToken); err != nil {
			return nil, err
		}
		if token == "" {
			return nil, diag.New("No GitHub token was given").
				WithCode(codeAuth).
				WithHelp("Set the GITHUB_TOKEN environment variable or enter a token when asked.")
		}
		slog.DebugContext(ctx, "Using the stored GitHub token")
	default:
		return nil, nil //nolint:nilnil // nil means anonymous
	}
	return oauth2.StaticTokenSource(&oauth2.Token{AccessToken: token}), nil
}

// fromEnv reports whether token is the value of the environment variable, as opposed to the stored file.
func (f *Forge) fromEnv(token string) bool {
	getenv := f.store.Getenv
	if getenv == nil {
		getenv = os.Getenv
	}
	return getenv(credentials.GitHubToken.Env()) == token
}

// appTokenSource exchanges the App's JWT for installation tokens, which go-githubauth refreshes by itself.
// Its calls go to the same API URL as the client's and through the same retry policy.
func (f *Forge) appTokenSource(ctx context.Context) (oauth2.TokenSource, error) {
	if f.app.PrivateKey == nil {
		return nil, diag.New("A GitHub App is configured without a private key").WithCode(codeAuth)
	}
	key, err := f.app.PrivateKey()
	if err != nil {
		return nil, err
	}
	appTS, err := githubauth.NewApplicationTokenSource(f.app.ID, key)
	if err != nil {
		// The error of the JWT library never repeats the key; it is still not echoed in the message.
		return nil, diag.New("The GitHub App private key is not a valid RSA private key in PEM format").
			WithCode(codeAuth).
			WithHelp("Check the file given to --github-app-private-key-path or the content of " +
				"REMERIS_GITHUB_APP_PRIVATE_KEY.").
			WithCause(err)
	}
	appTS = oauth2.ReuseTokenSource(nil, appTS)

	probe, err := f.newClient(http.DefaultTransport, false)
	if err != nil {
		return nil, err
	}
	baseURL := probe.BaseURL()
	// Calls made as the App itself (the JWT) or to get installation tokens.
	jwtTransport := f.transport(http.DefaultTransport, true)

	id := f.app.InstallationID
	if id == 0 {
		lookup, err := f.newClient(f.transport(&oauth2.Transport{Source: appTS, Base: http.DefaultTransport}, true), true)
		if err != nil {
			return nil, err
		}
		inst, _, err := lookup.Apps.GetRepositoryInstallation(ctx, f.cfg.Owner, f.cfg.Repo)
		if err != nil {
			return nil, WrapError("finding the GitHub App installation", err)
		}
		if inst.GetID() == 0 {
			return nil, diag.New("GitHub did not return an installation of the GitHub App for " + f.cfg.Owner + "/" +
				f.cfg.Repo).WithCode(codeAuth)
		}
		id = inst.GetID()
	}
	slog.DebugContext(ctx, "Using GitHub App installation "+strconv.FormatInt(id, 10))
	return githubauth.NewInstallationTokenSource(id, appTS,
		githubauth.WithHTTPClient(&http.Client{Transport: jwtTransport}),
		githubauth.WithBaseURL(baseURL),
		// The source outlives the call that built it.
		githubauth.WithContext(context.WithoutCancel(ctx)),
	), nil
}
