package github

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/celest-io/remeris/internal/diag"
)

const (
	gheAPI        = gheRoot + "api/v3/"
	gheUpload     = "https://up.example.com/api/uploads/"
	evilOrigin    = "https://evil.example.net:8443"
	evilURL       = "https://evil.example.net/x"
	bearerFake    = "Bearer fake"
	aURL          = "https://a.example.com/x"
	aURL2         = "https://a.example.com/y"
	flagsAPIText  = "--github-api-url or REMERIS_GITHUB_API_URL"
	flagsUploadTx = "--github-upload-url or REMERIS_GITHUB_UPLOAD_URL"
)

func TestResolveURLs(t *testing.T) {
	t.Parallel()
	tests := map[string]struct {
		cfg                Config
		wantAPI, wantUp    string
		wantCode, wantText string
	}{
		"nothing set means github.com": {},
		"trusted only": {
			cfg: Config{TrustedAPIURL: gheAPI}, wantAPI: gheAPI, wantUp: gheRoot,
		},
		"trusted api and upload": {
			cfg:     Config{TrustedAPIURL: gheAPI, TrustedUploadURL: gheUpload},
			wantAPI: gheAPI, wantUp: gheUpload,
		},
		"config equals trusted": {
			cfg:     Config{APIURL: gheAPI, TrustedAPIURL: gheAPI},
			wantAPI: gheAPI, wantUp: gheRoot,
		},
		"config equals trusted after normalisation": {
			cfg:     Config{APIURL: "HTTPS://GHE.Example.com:443/api/v3", TrustedAPIURL: gheAPI},
			wantAPI: gheAPI, wantUp: gheRoot,
		},
		"config differs from trusted": {
			cfg:      Config{APIURL: "https://other.example.com/", TrustedAPIURL: gheAPI},
			wantCode: codeUntrustedAPI, wantText: "points to https://other.example.com, which remeris",
		},
		"config without trusted": {
			cfg:      Config{APIURL: evilAPI()},
			wantCode: codeUntrustedAPI, wantText: "The [github] api_url in knope.toml points to " + evilOrigin + ", which",
		},
		"upload equals trusted": {
			cfg:     Config{APIURL: gheAPI, UploadURL: gheUpload, TrustedAPIURL: gheAPI, TrustedUploadURL: gheUpload},
			wantAPI: gheAPI, wantUp: gheUpload,
		},
		"upload differs from trusted": {
			cfg: Config{
				APIURL: gheAPI, UploadURL: "https://evil.example.net/", TrustedAPIURL: gheAPI, TrustedUploadURL: gheUpload,
			},
			wantCode: codeUntrustedUp, wantText: "The [github] upload_url in knope.toml points to https://evil.example.net",
		},
		"upload without trusted upload": {
			cfg:      Config{APIURL: gheAPI, UploadURL: gheUpload, TrustedAPIURL: gheAPI},
			wantCode: codeUntrustedUp,
		},
		"trusted upload without any api url": {
			cfg: Config{TrustedUploadURL: gheUpload}, wantCode: codeUploadNoAPI,
		},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			api, up, err := resolveURLs(tc.cfg)
			if tc.wantCode != "" {
				var d diag.Diagnostic
				if !errors.As(err, &d) || d.Code() != tc.wantCode {
					t.Fatalf("err = %v, want code %s", err, tc.wantCode)
				}
				if !strings.Contains(err.Error(), tc.wantText) {
					t.Errorf("message = %q, want it to contain %q", err, tc.wantText)
				}
				for _, leak := range []string{"secret", "user:", "x=1"} {
					if strings.Contains(err.Error()+d.Help(), leak) {
						t.Errorf("the error leaks %q: %v / %s", leak, err, d.Help())
					}
				}
				return
			}
			if err != nil || api != tc.wantAPI || up != tc.wantUp {
				t.Errorf("resolveURLs = %q, %q, %v; want %q, %q", api, up, err, tc.wantAPI, tc.wantUp)
			}
		})
	}
}

func TestUntrustedHelp(t *testing.T) {
	t.Parallel()
	_, _, err := resolveURLs(Config{APIURL: gheAPI})
	var d diag.Diagnostic
	if !errors.As(err, &d) {
		t.Fatal(err)
	}
	want := "A repository's configuration cannot choose where your GitHub token goes. If you trust " +
		"https://ghe.example.com, set " + flagsAPIText + " to the same URL."
	if d.Help() != want {
		t.Errorf("help = %q, want %q", d.Help(), want)
	}
	_, _, err = resolveURLs(Config{APIURL: gheAPI, UploadURL: gheUpload, TrustedAPIURL: gheAPI})
	if !errors.As(err, &d) || !strings.Contains(d.Help(), flagsUploadTx) {
		t.Errorf("upload help = %v", err)
	}
}

func TestNormalizeURL(t *testing.T) {
	t.Parallel()
	tests := map[string]struct{ in, want string }{
		"already normal":       {in: gheAPI, want: gheAPI},
		"no trailing slash":    {in: "https://ghe.example.com/api/v3", want: gheAPI},
		"several slashes":      {in: "https://ghe.example.com/api/v3///", want: gheAPI},
		"host only":            {in: "https://ghe.example.com", want: gheRoot},
		"case":                 {in: "HTTPS://GHE.Example.COM/Api/V3/", want: "https://ghe.example.com/Api/V3/"},
		"default https port":   {in: "https://ghe.example.com:443/", want: gheRoot},
		"default http port":    {in: "http://localhost:80", want: "http://localhost/"},
		"other port is kept":   {in: "https://ghe.example.com:8443/", want: "https://ghe.example.com:8443/"},
		"http port on https":   {in: "https://ghe.example.com:80/", want: "https://ghe.example.com:80/"},
		"ipv6":                 {in: "http://[::1]:8080", want: "http://[::1]:8080/"},
		"ipv6 default port":    {in: "http://[::1]:80/x", want: "http://[::1]/x/"},
		"not a url as written": {in: "://bad", want: "://bad"},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if got := normalizeURL(tc.in); got != tc.want {
				t.Errorf("normalizeURL(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestCheckSecure(t *testing.T) {
	t.Parallel()
	tests := map[string]struct{ url, wantCode string }{
		"https anywhere":     {url: gheRoot},
		"http localhost":     {url: "http://localhost:8080/"},
		"http LOCALHOST":     {url: "http://LocalHost/"},
		"http 127.0.0.1":     {url: "http://127.0.0.1:1234/"},
		"http 127.1.2.3":     {url: "http://127.1.2.3/"},
		"http ipv6 loopback": {url: "http://[::1]:80/"},
		"http other host":    {url: "http://ghe.example.com/", wantCode: codeInsecureURL},
		"http private ip":    {url: "http://10.0.0.1/", wantCode: codeInsecureURL},
		"http not loopback":  {url: "http://localhost.example.com/", wantCode: codeInsecureURL},
		"http unspecified":   {url: "http://0.0.0.0/", wantCode: codeInsecureURL},
		"ftp":                {url: "ftp://ghe.example.com/", wantCode: codeInvalidURL},
		"no host":            {url: "https:///x", wantCode: codeInvalidURL},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			err := checkSecure(tc.url)
			if tc.wantCode == "" {
				if err != nil {
					t.Fatal(err)
				}
				return
			}
			var d diag.Diagnostic
			if !errors.As(err, &d) || d.Code() != tc.wantCode {
				t.Fatalf("err = %v, want code %s", err, tc.wantCode)
			}
		})
	}
}

func TestCheckSecureNamesTheHost(t *testing.T) {
	t.Parallel()
	err := checkSecure("http://user:pw@GHE.example.com:81/x")
	if err == nil || !strings.Contains(err.Error(), "ghe.example.com") || strings.Contains(err.Error(), "pw") {
		t.Errorf("err = %v", err)
	}
}

// recorder is a base transport that remembers the Authorization header of the requests it gets.
type recorder struct{ auth []string }

func (r *recorder) RoundTrip(req *http.Request) (*http.Response, error) {
	r.auth = append(r.auth, req.Header.Get("Authorization"))
	return &http.Response{StatusCode: http.StatusOK, Body: http.NoBody, Request: req}, nil
}

// addAuth is a credential layer that adds a header, like oauth2.Transport does.
type addAuth struct{ next http.RoundTripper }

func (a addAuth) RoundTrip(req *http.Request) (*http.Response, error) {
	out := req.Clone(req.Context())
	out.Header.Set("Authorization", bearerFake)
	return a.next.RoundTrip(out)
}

func TestHostGuard(t *testing.T) {
	t.Parallel()
	tests := map[string]struct {
		url      string
		withAuth bool // the layer above the guard adds the header
		want     string
	}{
		"configured host gets credentials":     {url: "https://ghe.example.com/x", want: bearerFake},
		"default port is the same host":        {url: "https://GHE.example.com:443/x", want: bearerFake},
		"upload host gets credentials":         {url: "https://up.example.com/x", want: bearerFake},
		"other host":                           {url: evilURL},
		"other port":                           {url: "https://ghe.example.com:8443/x"},
		"downgrade to http":                    {url: "http://ghe.example.com/x"},
		"header added above is dropped":        {url: evilURL, withAuth: true},
		"header added above is kept when ok":   {url: "https://ghe.example.com/x", withAuth: true, want: bearerFake},
		"header added above for upload is ok":  {url: "https://up.example.com/x", withAuth: true, want: bearerFake},
		"subdomain is another host":            {url: "https://x.ghe.example.com/x"},
		"userinfo in the url is not a host":    {url: "https://ghe.example.com@evil.example.net/x"},
		"explicit credentials do not travel":   {url: evilURL, withAuth: true},
		"not the same scheme with the same IP": {url: "http://ghe.example.com:443/x"},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			base := &recorder{}
			var rt http.RoundTripper
			if tc.withAuth {
				g := newHostGuard(base, nil)
				g.allow(gheAPI, gheUpload)
				rt = addAuth{next: g}
			} else {
				g := newHostGuard(base, addAuth{next: base})
				g.allow(gheAPI, gheUpload)
				rt = g
			}
			req, err := http.NewRequest(http.MethodGet, tc.url, http.NoBody)
			if err != nil {
				t.Fatal(err)
			}
			resp, err := rt.RoundTrip(req)
			if err != nil {
				t.Fatal(err)
			}
			_, _ = io.Copy(io.Discard, resp.Body)
			if len(base.auth) != 1 || base.auth[0] != tc.want {
				t.Errorf("Authorization seen = %q, want %q", base.auth, tc.want)
			}
		})
	}
}

func TestCheckRedirect(t *testing.T) {
	t.Parallel()
	tests := map[string]struct {
		from, to string
		hops     int
		wantErr  bool
	}{
		"same host":          {from: aURL, to: aURL2},
		"same host default":  {from: aURL, to: "https://a.example.com:443/y"},
		"http to https":      {from: "http://a.example.com/x", to: aURL2},
		"other host":         {from: aURL, to: "https://b.example.com/y", wantErr: true},
		"other port":         {from: "http://127.0.0.1:1/x", to: "http://127.0.0.1:2/y", wantErr: true},
		"https to http":      {from: aURL, to: "http://a.example.com/y", wantErr: true},
		"too many redirects": {from: aURL, to: aURL2, hops: 10, wantErr: true},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			hops := max(tc.hops, 1)
			prev := httptest.NewRequest(http.MethodGet, tc.from, http.NoBody)
			via := make([]*http.Request, hops)
			for i := range via {
				via[i] = prev
			}
			next := httptest.NewRequest(http.MethodGet, tc.to, http.NoBody)
			err := checkRedirect(next, via)
			if (err != nil) != tc.wantErr {
				t.Errorf("checkRedirect = %v, wantErr %v", err, tc.wantErr)
			}
		})
	}
}

// evilAPI is a URL with userinfo, a port and a query, none of which may show in a message.
func evilAPI() string {
	u := url.URL{
		Scheme: "https", User: url.UserPassword("user", "secret"), Host: "evil.example.net:8443",
		Path: "/api/v3/", RawQuery: "x=1",
	}
	return u.String()
}
