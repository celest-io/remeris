package httpx

import (
	"net"
	"net/http"
	"net/url"
	"strings"

	"github.com/celest-io/remeris/internal/diag"
)

const (
	schemeHTTP  = "http"
	schemeHTTPS = "https"

	maxRedirects = 10

	// UnknownHost is what a message prints for a URL that has no host.
	UnknownHost = "an unknown host"
)

// Guard is the http.RoundTripper that decides, per request, whether credentials may go along. It sits between the
// retry policy and the network, under the code that adds the credentials.
//
// A request to one of the allowed origins (scheme, host and port) goes to the credential layer, which adds the
// credential, or straight to the base transport when there is none because the layer above has added it already.
// A request to any other origin, such as the target of a redirect, goes to the base transport with its
// Authorization header removed.
type Guard struct {
	base    http.RoundTripper
	creds   http.RoundTripper
	origins map[string]bool
}

// NewGuard returns a Guard over base. creds adds the credential for the allowed origins; nil means the layer above
// the guard adds it. Set the allowed origins with [Guard.Allow] before use.
func NewGuard(base, creds http.RoundTripper) *Guard {
	return &Guard{base: base, creds: creds, origins: map[string]bool{}}
}

// SetCreds sets the credential layer. It must be called before the guard is used.
func (g *Guard) SetCreds(creds http.RoundTripper) { g.creds = creds }

// Allow adds the origins of the URLs. It must be called before the guard is used.
func (g *Guard) Allow(urls ...string) {
	for _, u := range urls {
		g.origins[OriginOf(u)] = true
	}
}

// WithoutCreds returns a guard for the same origins that adds no credential itself.
func (g *Guard) WithoutCreds() *Guard {
	return &Guard{base: g.base, origins: g.origins}
}

// RoundTrip implements http.RoundTripper.
func (g *Guard) RoundTrip(req *http.Request) (*http.Response, error) {
	if g.origins[URLOrigin(req.URL)] {
		if g.creds != nil {
			return g.creds.RoundTrip(req)
		}
		return g.base.RoundTrip(req)
	}
	out := req.Clone(req.Context())
	out.Header.Del("Authorization")
	return g.base.RoundTrip(out)
}

// URLPolicy holds the wording of the diagnostics about the URL of a service, so that each forge keeps its own
// names while sharing the rules.
type URLPolicy struct {
	// Service is the name used in messages, such as "GitHub".
	Service string
	// CodePrefix starts the diagnostic codes, such as "remeris::github". The codes are CodePrefix + "::invalid_url",
	// "::insecure_url" and "::redirect".
	CodePrefix string
	// URLName names the URL in messages about its scheme, such as "GitHub API URL".
	URLName string
	// InvalidHelp is the help of the "not valid" and "must use http or https" errors.
	InvalidHelp string
	// RedirectHelp is the help of the error for a redirect to another host.
	RedirectHelp string
}

// CheckRedirect is the CheckRedirect of every http.Client of a forge. It follows a redirect inside the same
// origin and refuses one that changes the host or the port, or goes from https to http.
func (p URLPolicy) CheckRedirect(req *http.Request, via []*http.Request) error {
	code := p.CodePrefix + "::redirect"
	if len(via) >= maxRedirects {
		return diag.Newf("Stopped after %d redirects from %s", maxRedirects, p.Service).WithCode(code)
	}
	from := via[len(via)-1].URL
	switch {
	case HostPort(req.URL) != HostPort(from):
		err := diag.New(p.Service + " redirected the request from " + URLOrigin(from) + " to " + URLOrigin(req.URL) +
			", which remeris does not follow").WithCode(code)
		if p.RedirectHelp != "" {
			err = err.WithHelp(p.RedirectHelp)
		}
		return err
	case from.Scheme == schemeHTTPS && req.URL.Scheme != schemeHTTPS:
		return diag.New(p.Service + " redirected the request from https to plain http at " + URLOrigin(req.URL) +
			", which remeris does not follow").WithCode(code)
	}
	return nil
}

// CheckSecure refuses a URL that would carry a token over plain http to a host that is not on this machine, and
// any scheme other than http and https.
func (p URLPolicy) CheckSecure(raw string) error {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return diag.New("The " + p.URLName + " is not valid").
			WithCode(p.CodePrefix + "::invalid_url").
			WithHelp(p.InvalidHelp)
	}
	switch strings.ToLower(u.Scheme) {
	case schemeHTTPS:
		return nil
	case schemeHTTP:
		if IsLoopback(u.Hostname()) {
			return nil
		}
		return diag.New("Refusing to send " + p.Service + " credentials over plain http to " + strings.ToLower(u.Hostname())).
			WithCode(p.CodePrefix + "::insecure_url").
			WithHelp("Use an https URL. Plain http is accepted only for localhost and other loopback addresses.")
	default:
		return diag.New("The " + p.URLName + " must use http or https: " + OriginOf(raw)).
			WithCode(p.CodePrefix + "::invalid_url").
			WithHelp(p.InvalidHelp)
	}
}

// HostPort is the lowercase host of u, with the port unless it is the default one of the scheme.
func HostPort(u *url.URL) string {
	host := strings.ToLower(u.Hostname())
	port := u.Port()
	if port == "" || (port == "80" && strings.EqualFold(u.Scheme, schemeHTTP)) ||
		(port == "443" && strings.EqualFold(u.Scheme, schemeHTTPS)) {
		if strings.Contains(host, ":") {
			return "[" + host + "]"
		}
		return host
	}
	return net.JoinHostPort(host, port)
}

// OriginOf is scheme://host[:port] of a URL given as text, without userinfo, path or query. It is what messages
// print about a URL, and it is how the credential guard compares hosts. A URL without a host gives [UnknownHost].
func OriginOf(s string) string {
	u, err := url.Parse(s)
	if err != nil || u.Host == "" {
		return UnknownHost
	}
	return URLOrigin(u)
}

// URLOrigin is scheme://host[:port] of u.
func URLOrigin(u *url.URL) string {
	return strings.ToLower(u.Scheme) + "://" + HostPort(u)
}

// IsLoopback reports whether host is localhost or a loopback IP address (127.0.0.0/8, ::1).
func IsLoopback(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}
