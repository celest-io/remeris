package github

import (
	"net/http"

	"github.com/celest-io/remeris/internal/diag"
)

const maxRedirects = 10

// hostGuard is the http.RoundTripper that decides, per request, whether credentials may go along. It sits between
// the retry policy and the network, under the code that adds the credentials.
//
// A request to one of the allowed origins (scheme, host and port of the API and upload URLs) goes to creds, which
// adds the credential, or straight to base when creds is nil because the layer above has added it already. A
// request to any other origin, such as the target of a redirect, goes to base with its Authorization header removed.
type hostGuard struct {
	base    http.RoundTripper
	creds   http.RoundTripper
	origins map[string]bool
}

func newHostGuard(base, creds http.RoundTripper) *hostGuard {
	return &hostGuard{base: base, creds: creds, origins: map[string]bool{}}
}

// allow adds the origins of the URLs. It must be called before the guard is used.
func (g *hostGuard) allow(urls ...string) {
	for _, u := range urls {
		g.origins[originOf(u)] = true
	}
}

// withoutCreds returns a guard for the same origins that adds no credential itself.
func (g *hostGuard) withoutCreds() *hostGuard {
	return &hostGuard{base: g.base, origins: g.origins}
}

// RoundTrip implements http.RoundTripper.
func (g *hostGuard) RoundTrip(req *http.Request) (*http.Response, error) {
	if g.origins[urlOrigin(req.URL)] {
		if g.creds != nil {
			return g.creds.RoundTrip(req)
		}
		return g.base.RoundTrip(req)
	}
	out := req.Clone(req.Context())
	out.Header.Del("Authorization")
	return g.base.RoundTrip(out)
}

// checkRedirect is the CheckRedirect of every http.Client of the forge. It follows a redirect inside the same
// origin and refuses one that changes the host or the port, or goes from https to http.
func checkRedirect(req *http.Request, via []*http.Request) error {
	if len(via) >= maxRedirects {
		return diag.Newf("Stopped after %d redirects from GitHub", maxRedirects).WithCode("remeris::github::redirect")
	}
	from := via[len(via)-1].URL
	switch {
	case hostPort(req.URL) != hostPort(from):
		return diag.New("GitHub redirected the request from " + urlOrigin(from) + " to " + urlOrigin(req.URL) +
			", which remeris does not follow").
			WithCode("remeris::github::redirect").
			WithHelp("Check the API URL.")
	case from.Scheme == schemeHTTPS && req.URL.Scheme != schemeHTTPS:
		return diag.New("GitHub redirected the request from https to plain http at " + urlOrigin(req.URL) +
			", which remeris does not follow").
			WithCode("remeris::github::redirect")
	}
	return nil
}
