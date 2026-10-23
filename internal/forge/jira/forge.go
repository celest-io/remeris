// Package jira builds the Jira client of remeris and turns its errors into diagnostics (spec F19.3 and F19.4).
//
// Knope sends `Authorization: Basic base64(email:token)` to the `url` of the `[jira]` table, whatever that URL is,
// and never checks the status of a response. This client keeps the wire format and hardens the rest:
//
//   - the `url` is trusted (it is a required key) but must be a URL with a scheme, https unless the host is on this
//     machine, without credentials, query or fragment; the client follows no redirect that changes the host or the
//     port or goes from https to http, and never sends the credential to another origin;
//   - the credential is added by a RoundTripper under the retry policy, only for requests to the origin of the URL;
//     it never goes in a URL;
//   - the token and the Basic blob (in its base64 forms) are replaced by a placeholder in every error;
//   - values written into the JQL and the path are quoted and escaped, the status of every response is checked and
//     the size of a response is limited.
//
// The package is a leaf: it imports only the standard library and internal/httpx, internal/credentials and
// internal/diag. The workflow state will hold a *Forge, and the Jira steps ask it for a [Client].
package jira

import (
	"context"
	"encoding/base64"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"unicode"

	"github.com/celest-io/remeris/internal/credentials"
	"github.com/celest-io/remeris/internal/diag"
	"github.com/celest-io/remeris/internal/httpx"
)

const (
	service = "Jira"

	codeInvalidURL = "remeris::jira::invalid_url"

	urlHelp = "Set url in the [jira] table to the URL of your Jira site, for example https://example.atlassian.net."
)

// urlPolicy words the diagnostics about the URL (codes remeris::jira::invalid_url, insecure_url and redirect); the
// rules are those of httpx.
var urlPolicy = httpx.URLPolicy{
	Service:      service,
	CodePrefix:   "remeris::jira",
	URLName:      "Jira URL",
	InvalidHelp:  urlHelp,
	RedirectHelp: "Check url in the [jira] table.",
}

// Config is the `[jira]` table of the configuration.
type Config struct {
	// URL is the Jira site as written in the configuration, such as https://example.atlassian.net. A context path is
	// allowed and trailing slashes are ignored. It is checked when a client is built, not before, so a dry run
	// prints it as written.
	URL string
	// Project is the key of the project whose issues are queried.
	Project string
}

// Forge builds the Jira client once and caches it.
//
// Callers must not ask for a client in a dry run: building one reads the credential store (which may copy a Knope
// credential) and may prompt for the email and the token. A dry run logs what would happen and leaves the forge
// alone; the steps enforce it. Creating a Forge with [New] reads nothing and touches no network.
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

// New returns a Forge for cfg. store finds the email and the token and, through its Prompter, asks for them; nil
// means the zero Store (the real environment, no prompt).
func New(cfg Config, store *credentials.Store) *Forge {
	if store == nil {
		store = &credentials.Store{}
	}
	return &Forge{cfg: cfg, store: store}
}

// Config returns the `[jira]` settings the forge was built with.
func (f *Forge) Config() Config { return f.cfg }

// RequireAuthentication returns the client, built on the first call and cached: the URL is checked, then the email
// and the token are taken from the credential store, in that order (the user is asked for what is missing and the
// answer is stored). The credentials are therefore asked for at most once per run. A failure is not cached.
func (f *Forge) RequireAuthentication(ctx context.Context) (*Client, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.client != nil {
		return f.client, nil
	}
	// The URL is checked first, so a bad one never prompts.
	base, err := apiBase(f.cfg.URL)
	if err != nil {
		return nil, err
	}
	email, err := f.value(ctx, credentials.Email)
	if err != nil {
		return nil, err
	}
	token, err := f.value(ctx, credentials.JiraToken)
	if err != nil {
		return nil, err
	}
	slog.DebugContext(ctx, "Using the Jira token")
	pair := []byte(email + ":" + token)
	blob := base64.StdEncoding.EncodeToString(pair)
	// The forms a server or a proxy may echo the blob in.
	rawBlob := base64.RawStdEncoding.EncodeToString(pair)
	urlBlob := base64.URLEncoding.EncodeToString(pair)
	rawURLBlob := base64.RawURLEncoding.EncodeToString(pair)

	transport := http.DefaultTransport
	guard := httpx.NewGuard(transport, nil)
	guard.Allow(base)
	guard.SetCreds(basicAuth{base: transport, header: "Basic " + blob})
	retry := httpx.NewRetryTransport(guard)
	retry.Authenticated = true
	// There is no variable that raises a Jira rate limit.
	retry.RateLimitEnvVar = ""
	if f.tune != nil {
		f.tune(retry)
	}
	f.client = &Client{
		cfg:   f.cfg,
		base:  base,
		scrub: httpx.NewScrubber(token, blob, rawBlob, urlBlob, rawURLBlob),
		http:  &http.Client{Transport: retry, CheckRedirect: urlPolicy.CheckRedirect},
	}
	return f.client, nil
}

// value returns the stored or prompted value of key without the white space around it: a stored file holds the raw
// content, which may end in a newline that a header cannot carry. The store never returns a blank value.
func (f *Forge) value(ctx context.Context, key credentials.Key) (string, error) {
	raw, err := f.store.Get(ctx, key)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(raw), nil
}

// basicAuth is the credential layer under the guard: it adds the Authorization header to the requests the guard
// lets through, which are those for the Jira origin.
type basicAuth struct {
	base   http.RoundTripper
	header string
}

func (b basicAuth) RoundTrip(req *http.Request) (*http.Response, error) {
	out := req.Clone(req.Context())
	out.Header.Set("Authorization", b.header)
	return b.base.RoundTrip(out)
}

// apiBase checks the URL and returns it without trailing slashes.
func apiBase(raw string) (string, error) {
	if strings.ContainsFunc(raw, func(r rune) bool { return unicode.IsControl(r) || unicode.IsSpace(r) }) {
		return "", invalidURL("The [jira] url contains spaces or control characters")
	}
	// Anything with an "@" could hold credentials, and is never echoed.
	if strings.Contains(raw, "@") {
		return "", invalidURL("The [jira] url must not contain a user name or password")
	}
	u, err := url.Parse(raw)
	switch {
	case err != nil || u.Scheme == "" || u.Host == "":
		// The URL is never echoed: it may hold a password that did not parse as userinfo.
		return "", invalidURL("The [jira] url is not a URL with a scheme such as https://")
	case u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || strings.Contains(raw, "#"):
		return "", invalidURL("The [jira] url must not have a query or a fragment")
	}
	if err := urlPolicy.CheckSecure(raw); err != nil {
		return "", err
	}
	return strings.TrimRight(raw, "/"), nil
}

func invalidURL(msg string) error {
	return diag.New(msg).WithCode(codeInvalidURL).WithHelp(urlHelp)
}
