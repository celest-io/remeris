package httpx

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/celest-io/remeris/internal/diag"
)

const (
	ghAPI      = "https://ghe.example.com/api/v3/"
	ghUpload   = "https://up.example.com/api/uploads/"
	evilURL    = "https://evil.example.net/x"
	bearerFake = "Bearer fake"
	aURL       = "https://a.example.com/x"
	aURL2      = "https://a.example.com/y"
	gheRoot    = "https://ghe.example.com/"

	serviceName  = "GitHub"
	insecureCode = "remeris::github::insecure_url"
)

var testPolicy = URLPolicy{
	Service:      serviceName,
	CodePrefix:   "remeris::github",
	URLName:      "GitHub API URL",
	InvalidHelp:  "Check api_url and upload_url in the [github] table.",
	RedirectHelp: "Check the API URL.",
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
		"http other host":    {url: "http://ghe.example.com/", wantCode: insecureCode},
		"http private ip":    {url: "http://10.0.0.1/", wantCode: insecureCode},
		"http not loopback":  {url: "http://localhost.example.com/", wantCode: insecureCode},
		"http unspecified":   {url: "http://0.0.0.0/", wantCode: insecureCode},
		"ftp":                {url: "ftp://ghe.example.com/", wantCode: "remeris::github::invalid_url"},
		"no host":            {url: "https:///x", wantCode: "remeris::github::invalid_url"},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			err := testPolicy.CheckSecure(tc.url)
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
	err := testPolicy.CheckSecure("http://user:pw@GHE.example.com:81/x")
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
				g := NewGuard(base, nil)
				g.Allow(ghAPI, ghUpload)
				rt = addAuth{next: g}
			} else {
				g := NewGuard(base, addAuth{next: base})
				g.Allow(ghAPI, ghUpload)
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
			err := testPolicy.CheckRedirect(next, via)
			if (err != nil) != tc.wantErr {
				t.Errorf("checkRedirect = %v, wantErr %v", err, tc.wantErr)
			}
		})
	}
}
