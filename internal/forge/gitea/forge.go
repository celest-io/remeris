// Package gitea builds the Gitea client of remeris and turns its errors into diagnostics (spec F17).
//
// Unlike Knope, which sends the token as the `access_token` query parameter, the client authenticates with the header
// `Authorization: token <token>`: a token in a URL ends up in logs, proxies and error messages. The token is also
// replaced by a placeholder in every error this package produces.
//
// The `host` of the `[gitea]` table is trusted (it is a required key) but hardened: it must be a URL with a scheme,
// https unless the host is on this machine, without credentials, and the client follows no redirect that changes the
// host or the port or goes from https to http, and never sends the token to another origin.
//
// The package is a leaf: it imports only the standard library and internal/httpx, internal/credentials and
// internal/diag. The workflow state holds a *Forge, and the steps that talk to Gitea ask it for a [Client].
package gitea

import (
	"context"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"unicode"

	"github.com/celest-io/remeris/internal/credentials"
	"github.com/celest-io/remeris/internal/diag"
	"github.com/celest-io/remeris/internal/httpx"
)

const (
	service = "Gitea"
	// apiPath is appended to the host to get the API base.
	apiPath = "/api/v1"

	codeInvalidHost = "remeris::gitea::invalid_host"
	codeAPIResponse = "remeris::gitea::api_response_error"

	hostHelp = "Set host in the [gitea] table to the URL of your Gitea instance, for example https://gitea.example.com."
)

// urlPolicy words the diagnostics about the host (codes remeris::gitea::invalid_url, insecure_url and redirect);
// the rules are those of httpx.
var urlPolicy = httpx.URLPolicy{
	Service:      service,
	CodePrefix:   "remeris::gitea",
	URLName:      "Gitea host",
	InvalidHelp:  hostHelp,
	RedirectHelp: "Check host in the [gitea] table.",
}

// Config is the `[gitea]` table of the configuration.
type Config struct {
	Owner string
	Repo  string
	// Host is the URL of the Gitea instance as written in the configuration, such as https://codeberg.org. A path
	// prefix is allowed and trailing slashes are ignored. It is checked when a client is built, not before, so a dry
	// run prints it as written.
	Host string
}

// releasesURL is the path of the releases of the repository, relative to the API base.
func (c Config) releasesURL() string { return c.repoPath() + "/releases" }

// pullsURL is the path of the pull requests of the repository, relative to the API base.
func (c Config) pullsURL() string { return c.repoPath() + "/pulls" }

// pullURL is the path of pull request number n, relative to the API base.
func (c Config) pullURL(n int) string { return c.pullsURL() + "/" + strconv.Itoa(n) }

func (c Config) repoPath() string {
	return "/repos/" + url.PathEscape(c.Owner) + "/" + url.PathEscape(c.Repo)
}

// Forge builds the Gitea client once and caches it.
//
// Callers must not ask for a client in a dry run: building one reads the credential store (which may copy a
// Knope credential) and may prompt for the token. A dry run logs what would happen and leaves the forge alone; the
// steps enforce it. Creating a Forge with [New] reads nothing and touches no network.
//
// A Forge is safe for use by several goroutines.
type Forge struct {
	cfg   Config
	store *credentials.Store

	// tune, set by tests, adjusts the retry policy of the clients.
	tune func(*httpx.Transport)

	mu     sync.Mutex
	client *Client
}

// New returns a Forge for cfg. store finds the token and, through its Prompter, asks for it; nil means the zero
// Store (the real environment, no prompt).
func New(cfg Config, store *credentials.Store) *Forge {
	if store == nil {
		store = &credentials.Store{}
	}
	return &Forge{cfg: cfg, store: store}
}

// Config returns the `[gitea]` settings the forge was built with.
func (f *Forge) Config() Config { return f.cfg }

// RequireAuthentication returns the client, built on the first call and cached: the host is checked, then the token
// is taken from GITEA_TOKEN, the stored token or a prompt (the answer is stored). The token is therefore asked for
// at most once per run. A failure is not cached.
func (f *Forge) RequireAuthentication(ctx context.Context) (*Client, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.client != nil {
		return f.client, nil
	}
	// The host is checked first, so a bad one never prompts for a token.
	base, err := apiBase(f.cfg.Host)
	if err != nil {
		return nil, err
	}
	token, err := f.token(ctx)
	if err != nil {
		return nil, err
	}

	guard := httpx.NewGuard(http.DefaultTransport, nil)
	guard.Allow(base)
	retry := httpx.NewRetryTransport(guard)
	retry.Authenticated = true
	// Knope gives Gitea no hint about GITHUB_TOKEN: there is no variable that raises a Gitea rate limit.
	retry.RateLimitEnvVar = ""
	if f.tune != nil {
		f.tune(retry)
	}
	f.client = &Client{
		cfg:   f.cfg,
		base:  base,
		token: token,
		scrub: httpx.NewScrubber(token),
		http:  &http.Client{Transport: retry, CheckRedirect: urlPolicy.CheckRedirect},
	}
	return f.client, nil
}

func (f *Forge) token(ctx context.Context) (string, error) {
	raw, err := f.store.Get(ctx, credentials.GiteaToken(f.cfg.Host))
	if err != nil {
		return "", err
	}
	// A stored token is the raw file content, which may end in a newline that a header cannot carry.
	// The store never returns a blank token, so what is left is not empty.
	token := strings.TrimSpace(raw)
	slog.DebugContext(ctx, "Using the Gitea token")
	return token, nil
}

// apiBase checks host and returns the API base URL, "{host}/api/v1" without trailing slashes on the host.
func apiBase(host string) (string, error) {
	if strings.ContainsFunc(host, func(r rune) bool { return unicode.IsControl(r) || unicode.IsSpace(r) }) {
		return "", invalidHost("The [gitea] host contains spaces or control characters")
	}
	// Anything with an "@" could hold credentials, and is never echoed.
	if strings.Contains(host, "@") {
		return "", invalidHost("The [gitea] host must not contain a user name or password")
	}
	u, err := url.Parse(host)
	switch {
	case err != nil || u.Scheme == "" || u.Host == "":
		// The host is never echoed: it may hold a password that did not parse as userinfo.
		return "", invalidHost("The [gitea] host is not a URL with a scheme such as https://")
	case u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || strings.Contains(host, "#"):
		return "", invalidHost("The [gitea] host must not have a query or a fragment")
	}
	if err := urlPolicy.CheckSecure(host); err != nil {
		return "", err
	}
	return strings.TrimRight(host, "/") + apiPath, nil
}

func invalidHost(msg string) error {
	return diag.New(msg).WithCode(codeInvalidHost).WithHelp(hostHelp)
}
