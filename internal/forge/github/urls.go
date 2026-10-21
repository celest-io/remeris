package github

import (
	"net/url"
	"strings"

	"github.com/celest-io/remeris/internal/diag"
	"github.com/celest-io/remeris/internal/httpx"
)

const (
	codeInvalidURL   = "remeris::github::invalid_url"
	codeUntrustedAPI = "remeris::github::untrusted_api_url"
	codeUntrustedUp  = "remeris::github::untrusted_upload_url"
	codeUploadNoAPI  = "remeris::github::upload_url_without_api_url"
	flagAPIURL       = "--github-api-url or REMERIS_GITHUB_API_URL"
	flagUploadURL    = "--github-upload-url or REMERIS_GITHUB_UPLOAD_URL"
)

// resolveURLs decides which API and upload base URLs the client uses, from the [github] values of the configuration
// and the trusted values of the command line (spec F16.0, decision on untrusted api_url).
//
// A repository's configuration cannot choose where the GitHub token goes, so a configured URL is used only when the
// trusted value is the same URL after [normalizeURL]:
//
//  1. neither is set: the empty api, which means github.com;
//  2. only the trusted API URL is set: it is used;
//  3. the configured API URL is set: it must equal the trusted one, or the call fails;
//  4. the upload URL follows the same rule against the trusted upload URL. With a trusted API URL and no upload
//     setting at all, upload is derived from the API URL by [defaultUploadURL]. A trusted upload URL needs an API URL.
//
// It returns the URLs to hand to go-github, unnormalised, as the user wrote the trusted values.
func resolveURLs(cfg Config) (api, upload string, err error) {
	if cfg.APIURL != "" && !sameURL(cfg.APIURL, cfg.TrustedAPIURL) {
		return "", "", untrustedError(cfg.APIURL, "api_url", flagAPIURL, codeUntrustedAPI)
	}
	if cfg.UploadURL != "" && !sameURL(cfg.UploadURL, cfg.TrustedUploadURL) {
		return "", "", untrustedError(cfg.UploadURL, "upload_url", flagUploadURL, codeUntrustedUp)
	}
	api, upload = cfg.TrustedAPIURL, cfg.TrustedUploadURL
	switch {
	case api == "" && upload != "":
		return "", "", diag.New("A GitHub upload URL is set without an API URL").
			WithCode(codeUploadNoAPI).
			WithHelp("Also set " + flagAPIURL + ", or remove " + flagUploadURL + ".")
	case api != "" && upload == "":
		upload = defaultUploadURL(api)
	}
	return api, upload, nil
}

func untrustedError(configured, key, flags, code string) error {
	host := httpx.OriginOf(configured)
	return diag.New("The [github] " + key + " in knope.toml points to " + host +
		", which remeris does not send GitHub credentials to").
		WithCode(code).
		WithHelp("A repository's configuration cannot choose where your GitHub token goes. If you trust " + host +
			", set " + flags + " to the same URL.")
}

// sameURL reports whether a and b are the same URL after normalisation. An empty trusted value never matches.
func sameURL(configured, trusted string) bool {
	return trusted != "" && normalizeURL(configured) == normalizeURL(trusted)
}

// normalizeURL lowercases the scheme and the host, removes the default port of the scheme and ends the path with
// exactly one slash. A value that is not a URL is compared as written.
func normalizeURL(s string) string {
	u, err := url.Parse(strings.TrimSpace(s))
	if err != nil || u.Host == "" {
		return s
	}
	u.Scheme = strings.ToLower(u.Scheme)
	u.Host = httpx.HostPort(u)
	u.Path = strings.TrimRight(u.Path, "/") + "/"
	u.RawPath = ""
	return u.String()
}

// urlPolicy is the wording of the diagnostics about GitHub's URLs (codes remeris::github::invalid_url, insecure_url
// and redirect); the rules are those of httpx.
var urlPolicy = httpx.URLPolicy{
	Service:      service,
	CodePrefix:   "remeris::github",
	URLName:      "GitHub API URL",
	InvalidHelp:  "Check api_url and upload_url in the [github] table.",
	RedirectHelp: "Check the API URL.",
}
