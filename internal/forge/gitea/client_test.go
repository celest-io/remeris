package gitea

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/celest-io/remeris/internal/prompt"
)

const (
	userinfoText = "user name or password"
	insecureCode = "remeris::gitea::insecure_url"
	// userinfoHost is a host the client must refuse.
	userinfoHost = "https://user:pw@gitea.example.com" //nolint:gosec // G101: not a credential, an invalid host
	prefix       = "/gitea"
	fromURL      = "https://gitea.example.com/a"
	bareHost     = "gitea.example.com"
)

func TestRequestShape(t *testing.T) {
	tests := map[string]struct {
		hostSuffix string
		wantPrefix string
	}{
		"host as is":                {hostSuffix: "", wantPrefix: ""},
		"trailing slash":            {hostSuffix: "/", wantPrefix: ""},
		"several trailing slashes":  {hostSuffix: "///", wantPrefix: ""},
		"path prefix":               {hostSuffix: prefix, wantPrefix: prefix},
		"path prefix with slash":    {hostSuffix: "/gitea/", wantPrefix: "/gitea"},
		"two segments path prefix":  {hostSuffix: "/a/b", wantPrefix: "/a/b"},
		"path prefix several slash": {hostSuffix: "/gitea//", wantPrefix: "/gitea"},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			srv := newServer(t, replying(http.StatusOK, `{"id":7}`))
			c := newClient(t, srv.URL+tc.hostSuffix)
			var out struct{ ID int }

			err := c.do(context.Background(), http.MethodPost, c.cfg.releasesURL(), nil,
				map[string]string{"tag_name": "v1"}, &out, activity)

			if err != nil {
				t.Fatal(err)
			}
			if out.ID != 7 {
				t.Errorf("decoded %+v", out)
			}
			got := srv.requests()
			if len(got) != 1 {
				t.Fatalf("requests = %+v", got)
			}
			r := got[0]
			if want := tc.wantPrefix + releasesPath; r.uri != want {
				t.Errorf("request URI = %q, want %q", r.uri, want)
			}
			if want := "token " + testToken; r.auth != want {
				t.Errorf("Authorization = %q, want %q", r.auth, want)
			}
			if strings.Contains(r.uri, "access_token") || strings.Contains(r.uri, testToken) {
				t.Errorf("the token is in the URL: %q", r.uri)
			}
			if r.method != http.MethodPost || r.accept != "application/json" || r.contentType != "application/json" ||
				r.userAgent != "Remeris" || r.body != `{"tag_name":"v1"}` {
				t.Errorf("request = %+v", r)
			}
		})
	}
}

func TestQueryAndNoBody(t *testing.T) {
	srv := newServer(t, replying(http.StatusOK, `[]`))
	c := newClient(t, srv.URL)

	err := c.do(context.Background(), http.MethodGet, c.cfg.pullsURL(), url.Values{"state": {"open"}, "head": {"o:b"}}, nil,
		new([]string), "fetching existing pull requests")

	if err != nil {
		t.Fatal(err)
	}
	r := srv.requests()[0]
	if want := "/api/v1/repos/o/r/pulls?head=o%3Ab&state=open"; r.uri != want {
		t.Errorf("request URI = %q, want %q", r.uri, want)
	}
	if r.body != "" || r.contentType != "" || r.auth != "token "+testToken {
		t.Errorf("request = %+v", r)
	}
}

func TestPaths(t *testing.T) {
	c := Config{Owner: "owner", Repo: "repo"}
	tests := map[string]struct{ got, want string }{
		"releases": {c.releasesURL(), "/repos/owner/repo/releases"},
		"pulls":    {c.pullsURL(), "/repos/owner/repo/pulls"},
		"pull":     {c.pullURL(42), "/repos/owner/repo/pulls/42"},
		"escaped":  {Config{Owner: "a b", Repo: "c/d"}.releasesURL(), "/repos/a%20b/c%2Fd/releases"},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			if tc.got != tc.want {
				t.Errorf("path = %q, want %q", tc.got, tc.want)
			}
		})
	}
}

func TestSameOriginRedirectKeepsTheHeader(t *testing.T) {
	srv := newServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == releasesPath {
			http.Redirect(w, r, "/api/v1/moved", http.StatusTemporaryRedirect)
			return
		}
		_, _ = w.Write([]byte(`{}`))
	})
	c := newClient(t, srv.URL)

	err := c.do(context.Background(), http.MethodGet, c.cfg.releasesURL(), nil, nil, new(map[string]any), activity)

	if err != nil {
		t.Fatal(err)
	}
	reqs := srv.requests()
	if len(reqs) != 2 || reqs[1].uri != "/api/v1/moved" || reqs[1].auth != "token "+testToken {
		t.Errorf("requests = %+v", reqs)
	}
}

func TestRedirectToAnotherOriginIsRefused(t *testing.T) {
	for _, code := range []int{http.StatusMovedPermanently, http.StatusFound, http.StatusTemporaryRedirect,
		http.StatusPermanentRedirect} {
		t.Run(http.StatusText(code), func(t *testing.T) {
			other := newServer(t, replying(http.StatusOK, `{}`))
			srv := newServer(t, func(w http.ResponseWriter, r *http.Request) {
				http.Redirect(w, r, other.URL+r.URL.Path, code) //nolint:gosec // the test server redirects on purpose
			})
			c := newClient(t, srv.URL)

			err := c.do(context.Background(), http.MethodPost, c.cfg.releasesURL(), nil, map[string]int{"a": 1}, nil, activity)

			if err == nil || codeOf(t, err) != "remeris::gitea::redirect" {
				t.Fatalf("err = %v, want a redirect refusal", err)
			}
			if got := other.requests(); len(got) != 0 {
				t.Errorf("the other origin got %+v", got)
			}
			if strings.Contains(err.Error(), testToken) {
				t.Errorf("the error holds the token: %v", err)
			}
		})
	}
}

func TestRedirectDowngradeIsRefused(t *testing.T) {
	c := newClient(t, "https://gitea.example.com")
	tests := map[string]struct {
		from, to string
		wantErr  bool
	}{
		"https to http":        {from: fromURL, to: "http://gitea.example.com/b", wantErr: true},
		"other host":           {from: fromURL, to: "https://evil.example.net/b", wantErr: true},
		"other port":           {from: fromURL, to: "https://gitea.example.com:8443/b", wantErr: true},
		"same origin":          {from: fromURL, to: "https://gitea.example.com/b"},
		"same origin explicit": {from: fromURL, to: "https://gitea.example.com:443/b"},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			prev := httptest.NewRequest(http.MethodGet, tc.from, http.NoBody)
			next := httptest.NewRequest(http.MethodGet, tc.to, http.NoBody)

			err := c.http.CheckRedirect(next, []*http.Request{prev})

			if (err != nil) != tc.wantErr {
				t.Errorf("CheckRedirect = %v, wantErr %v", err, tc.wantErr)
			}
		})
	}
}

func TestHostValidation(t *testing.T) {
	tests := map[string]struct {
		host     string
		wantCode string
		wantText string
		// noEcho is text that the message must not repeat.
		noEcho string
	}{
		"no scheme": {
			host:     "corp.internal",
			wantCode: codeInvalidHost,
			wantText: "is not a URL with a scheme",
			noEcho:   "corp.internal",
		},
		"password that does not parse": {
			host: "https://user:pw%40host", wantCode: codeInvalidHost, wantText: "is not a URL with a scheme", noEcho: "pw",
		},
		"backslash hides an authority": {
			host: `https://good.example\@evil.example`, wantCode: codeInvalidHost, wantText: userinfoText,
			noEcho: "evil",
		},
		"fragment hides an authority": {
			host: "https://evil.example#@good.example", wantCode: codeInvalidHost, wantText: userinfoText,
			noEcho: "evil",
		},
		"one slash":                       {host: "https:/h", wantCode: codeInvalidHost, noEcho: "https:/h"},
		"no scheme with two slashes":      {host: "//h", wantCode: codeInvalidHost, noEcho: "//h"},
		"opaque":                          {host: "https:h", wantCode: codeInvalidHost, noEcho: "https:h"},
		"upper case scheme":               {host: "HTTPS://Host"},
		"localhost as a prefix of a name": {host: "https://localhost.evil.example"},
		"http to localhost as a prefix of a name": {
			host: "http://localhost.evil.example", wantCode: insecureCode,
		},
		"http to a name that starts like an ip": {
			host: "http://127.0.0.1.evil.example", wantCode: insecureCode,
		},
		// The rule is net.IP.IsLoopback, which sees through the IPv4-mapped form, so this is accepted.
		"http to a mapped loopback": {host: "http://[::ffff:127.0.0.1]"},
		"https ipv6 loopback":       {host: "https://[::1]"},
		"host and port":             {host: "localhost:3000", wantCode: codeInvalidHost},
		"empty":                     {host: "", wantCode: codeInvalidHost},
		"userinfo": {
			host:     userinfoHost,
			wantCode: codeInvalidHost,
			wantText: userinfoText,
		},
		"userinfo no host": {host: "user:pw@gitea.example.com", wantCode: codeInvalidHost, wantText: userinfoText},
		"control char":     {host: "https://gitea.example.com/\x00", wantCode: codeInvalidHost, wantText: "control characters"},
		"newline":          {host: "https://gitea.example.com/a\nb", wantCode: codeInvalidHost, wantText: "control characters"},
		"space":            {host: "https://gitea.example.com/a b", wantCode: codeInvalidHost},
		"query":            {host: "https://gitea.example.com?x=1", wantCode: codeInvalidHost, wantText: "query or a fragment"},
		"fragment":         {host: "https://gitea.example.com/#f", wantCode: codeInvalidHost, wantText: "query or a fragment"},
		"plain http":       {host: "http://gitea.example.com", wantCode: insecureCode},
		"plain http ip":    {host: "http://10.0.0.1:3000", wantCode: insecureCode},
		"ftp":              {host: "ftp://gitea.example.com", wantCode: "remeris::gitea::invalid_url"},
		"https":            {host: "https://gitea.example.com"},
		"https prefix":     {host: "https://gitea.example.com/gitea/"},
		"http localhost":   {host: "http://localhost:3000"},
		"http loopback":    {host: "http://127.0.0.1:3000/"},
		"http ipv6":        {host: "http://[::1]:3000"},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			scripted := prompt.NewScripted()
			f := newForge(tc.host, storeWith(t, map[string]string{"GITEA_TOKEN": testToken}, scripted))

			c, err := f.RequireAuthentication(context.Background())

			if tc.wantCode == "" {
				if err != nil || c == nil {
					t.Fatalf("err = %v", err)
				}
				return
			}
			if err == nil || codeOf(t, err) != tc.wantCode {
				t.Fatalf("err = %v, want code %s", err, tc.wantCode)
			}
			if !strings.Contains(err.Error(), tc.wantText) {
				t.Errorf("message = %q, want it to contain %q", err, tc.wantText)
			}
			if tc.noEcho != "" && strings.Contains(err.Error()+helpOf(err), tc.noEcho) {
				t.Errorf("the error repeats %q: %v", tc.noEcho, err)
			}
			for _, leak := range []string{"pw", "user:", "\x00", "x=1"} {
				if strings.Contains(err.Error()+helpOf(err), leak) {
					t.Errorf("the error leaks %q: %v", leak, err)
				}
			}
			if f.client != nil {
				t.Error("a client was cached after a failure")
			}
		})
	}
}

func TestHostMissingSchemeIsOnlyAnErrorOnUse(t *testing.T) {
	scripted := prompt.NewScripted()
	f := New(Config{Owner: "o", Repo: "r", Host: bareHost}, storeWith(t, nil, scripted))

	if got := f.Config().Host; got != bareHost {
		t.Errorf("host = %q: a dry run prints it as written", got)
	}
	if _, err := f.RequireAuthentication(context.Background()); err == nil {
		t.Fatal("want invalid_host")
	}
	if len(scripted.Asked()) != 0 {
		t.Errorf("a bad host must not prompt for a token: %+v", scripted.Asked())
	}
}

func TestTokenIsRequestedOnce(t *testing.T) {
	srv := newServer(t, replying(http.StatusOK, `{}`))
	scripted := prompt.NewScripted(prompt.Answer{Text: testToken + "\n"})
	f := newForge(srv.URL, storeWith(t, nil, scripted))
	if len(scripted.Asked()) != 0 {
		t.Fatal("New must not ask for the token: a dry run never builds a client")
	}

	for range 2 {
		c, err := f.RequireAuthentication(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if err := c.do(context.Background(), http.MethodGet, "/version", nil, nil, nil, "checking"); err != nil {
			t.Fatal(err)
		}
	}

	asked := scripted.Asked()
	if len(asked) != 1 || asked[0].Kind != prompt.KindPassword ||
		!strings.HasPrefix(asked[0].Message, "No Gitea token found, generate one from "+srv.URL+"/user/settings/applications") {
		t.Errorf("asked = %+v, want one password prompt", asked)
	}
	for _, r := range srv.requests() {
		if r.auth != "token "+testToken {
			t.Errorf("Authorization = %q: the newline of the answer must be trimmed", r.auth)
		}
	}
}

func TestEmptyTokenIsAnError(t *testing.T) {
	f := newForge("https://gitea.example.com", storeWith(t, nil, prompt.NewScripted(prompt.Answer{Text: "  "})))

	_, err := f.RequireAuthentication(context.Background())

	if err == nil || codeOf(t, err) != "remeris::app_config::blank_answer" || err.Error() != "No Gitea token was given" {
		t.Errorf("err = %v", err)
	}
}

func TestErrorWording(t *testing.T) {
	const missing = "no such release"
	tests := map[string]struct {
		status       int
		body         string
		out          any
		wantCode     string
		wantMessage  string
		wantHelpPart string
	}{
		"status error": {
			status: http.StatusNotFound, body: missing, out: new(map[string]any),
			wantCode: "remeris::api_request_error",
			wantMessage: "Trouble communicating with {host} while creating a release: " +
				"HTTP status client error (404 Not Found) for url ({host}" + releasesPath + ") with body: " + missing,
			wantHelpPart: "There was a problem communicating with {host}, this may be a network issue or a permissions issue.",
		},
		"server error": {
			status: http.StatusBadGateway, body: "", out: nil,
			wantCode: "remeris::api_request_error",
			wantMessage: "Trouble communicating with {host} while creating a release: " +
				"HTTP status server error (502 Bad Gateway) for url ({host}" + releasesPath + ") with body: ",
			wantHelpPart: "There was a problem communicating with {host}, this may be a network issue or a permissions issue.",
		},
		"decode error": {
			status: http.StatusOK, body: `{"id":`, out: new(map[string]any),
			wantCode:    "remeris::gitea::api_response_error",
			wantMessage: "Trouble decoding the response from Gitea while creating a release: unexpected end of JSON input",
			wantHelpPart: "Failure to decode a response from the Gitea instance at {host} is probably a bug. " +
				"Please report it at https://github.com/celest-io/remeris",
		},
		"body is ignored without a target": {status: http.StatusOK, body: `not json`},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			srv := newServer(t, replying(tc.status, tc.body))
			c := newClient(t, srv.URL)

			err := c.do(context.Background(), http.MethodPost, c.cfg.releasesURL(), nil, nil, tc.out, activity)

			if tc.wantCode == "" {
				if err != nil {
					t.Fatal(err)
				}
				return
			}
			if err == nil {
				t.Fatal("want an error")
			}
			if want := strings.ReplaceAll(tc.wantMessage, "{host}", srv.URL); err.Error() != want {
				t.Errorf("message = %q, want %q", err.Error(), want)
			}
			if want := strings.ReplaceAll(tc.wantHelpPart, "{host}", srv.URL); helpOf(err) != want {
				t.Errorf("help = %q, want %q", helpOf(err), want)
			}
			if got := codeOf(t, err); got != tc.wantCode {
				t.Errorf("code = %q, want %q", got, tc.wantCode)
			}
		})
	}
}

func TestStatusErrorUsesTheHostAsWritten(t *testing.T) {
	srv := newServer(t, replying(http.StatusForbidden, "nope"))
	c := newClient(t, srv.URL+"/")

	err := c.do(context.Background(), http.MethodGet, "/user", nil, nil, nil, "fetching existing pull requests")

	want := "Trouble communicating with " + srv.URL + "/ while fetching existing pull requests: " +
		"HTTP status client error (403 Forbidden) for url (" + srv.URL + "/api/v1/user) with body: nope"
	if err == nil || err.Error() != want {
		t.Errorf("err = %v, want %q", err, want)
	}
}

func TestWrapErrorKeepsTheCause(t *testing.T) {
	c := newClient(t, "https://gitea.example.com")
	if c.WrapError(activity, nil) != nil {
		t.Error("nil should stay nil")
	}
	cause := context.Canceled

	err := c.WrapError(activity, cause)

	if want := "Trouble communicating with https://gitea.example.com while creating a release: context canceled"; err.Error() != want {
		t.Errorf("message = %q, want %q", err, want)
	}
	if !errors.Is(err, cause) {
		t.Error("the cause should be reachable with errors.Is")
	}
}
