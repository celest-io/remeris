package github

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/celest-io/remeris/internal/diag"
)

const (
	gheFrom       = "https://ghe.example.com/x"
	gheAPI        = gheRoot + "api/v3/"
	gheUpload     = "https://up.example.com/api/uploads/"
	evilOrigin    = "https://evil.example.net:8443"
	evilURL       = "https://evil.example.net/x"
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

// evilAPI is a URL with userinfo, a port and a query, none of which may show in a message.
func evilAPI() string {
	u := url.URL{
		Scheme: "https", User: url.UserPassword("user", "secret"), Host: "evil.example.net:8443",
		Path: "/api/v3/", RawQuery: "x=1",
	}
	return u.String()
}

// TestURLPolicyCodes pins the diagnostic codes and wording the shared httpx rules give for GitHub.
func TestURLPolicyCodes(t *testing.T) {
	t.Parallel()
	tests := map[string]struct{ url, wantCode, wantText string }{
		"plain http": {
			url: "http://ghe.example.com/", wantCode: "remeris::github::insecure_url",
			wantText: "Refusing to send GitHub credentials over plain http to ghe.example.com",
		},
		"bad scheme": {
			url: "ftp://ghe.example.com/", wantCode: codeInvalidURL,
			wantText: "The GitHub API URL must use http or https: ftp://ghe.example.com",
		},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			err := urlPolicy.CheckSecure(tc.url)
			var d diag.Diagnostic
			if !errors.As(err, &d) || d.Code() != tc.wantCode || err.Error() != tc.wantText {
				t.Errorf("err = %v, want code %s and message %q", err, tc.wantCode, tc.wantText)
			}
		})
	}
}

// TestURLPolicyRedirectWording pins the refusals of redirects: their message, help and code.
func TestURLPolicyRedirectWording(t *testing.T) {
	t.Parallel()
	tests := map[string]struct {
		from, to string
		hops     int
		wantMsg  string
		wantHelp string
	}{
		"other host": {
			from: gheFrom, to: "https://evil.example.net/y",
			wantMsg: "GitHub redirected the request from https://ghe.example.com to https://evil.example.net, " +
				"which remeris does not follow",
			wantHelp: "Check the API URL.",
		},
		"downgrade": {
			from: gheFrom, to: "http://ghe.example.com/y",
			wantMsg: "GitHub redirected the request from https to plain http at http://ghe.example.com, " +
				"which remeris does not follow",
		},
		"too many": {
			from: gheFrom, to: "https://ghe.example.com/y", hops: 10,
			wantMsg: "Stopped after 10 redirects from GitHub",
		},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			via := make([]*http.Request, max(tc.hops, 1))
			for i := range via {
				via[i] = httptest.NewRequest(http.MethodGet, tc.from, http.NoBody)
			}
			err := urlPolicy.CheckRedirect(httptest.NewRequest(http.MethodGet, tc.to, http.NoBody), via)
			var d diag.Diagnostic
			if !errors.As(err, &d) {
				t.Fatalf("err = %v", err)
			}
			if d.Code() != "remeris::github::redirect" || err.Error() != tc.wantMsg || d.Help() != tc.wantHelp {
				t.Errorf("code %q, message %q, help %q", d.Code(), err, d.Help())
			}
		})
	}
}
