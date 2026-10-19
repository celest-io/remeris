package github_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/celest-io/remeris/internal/diag"
	"github.com/celest-io/remeris/internal/forge/github"
	"github.com/celest-io/remeris/internal/prompt"
)

// sink is an httptest server that records every request, standing in for the host a redirect points to.
type sink struct {
	*httptest.Server
	mu   sync.Mutex
	seen []request
}

func newSink(t *testing.T) *sink {
	t.Helper()
	s := &sink{}
	s.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		s.seen = append(s.seen, request{r.Method, r.URL.Path, r.Header.Get("Authorization"), r.UserAgent()})
		s.mu.Unlock()
		_, _ = w.Write([]byte(`{"name":"r","id":77}`))
	}))
	t.Cleanup(s.Close)
	return s
}

func (s *sink) requests() []request {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]request(nil), s.seen...)
}

// redirector answers every request under the API path with a redirect to target, and counts them.
func redirector(t *testing.T, code int, target string) *sink {
	t.Helper()
	s := &sink{}
	s.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		s.seen = append(s.seen, request{r.Method, r.URL.Path, r.Header.Get("Authorization"), r.UserAgent()})
		s.mu.Unlock()
		http.Redirect(w, r, target+r.URL.Path, code) //nolint:gosec // the test server redirects on purpose
	}))
	t.Cleanup(s.Close)
	return s
}

func codeOf(t *testing.T, err error) string {
	t.Helper()
	var d diag.Diagnostic
	if !errors.As(err, &d) {
		t.Fatalf("err = %v, want a diagnostic", err)
	}
	return d.Code()
}

func TestRedirectToAnotherHostGetsNoToken(t *testing.T) {
	for _, code := range []int{http.StatusMovedPermanently, http.StatusFound, http.StatusTemporaryRedirect,
		http.StatusPermanentRedirect} {
		t.Run(http.StatusText(code), func(t *testing.T) {
			other := newSink(t)
			api := redirector(t, code, other.URL)
			cfg := github.Config{Owner: "o", Repo: "r", TrustedAPIURL: api.URL + apiPath}
			forge := github.New(cfg, nil, storeIn(t, map[string]string{envName: envValue}, failingPrompter{t: t}))
			c, err := forge.RequireAuthentication(context.Background())
			if err != nil {
				t.Fatal(err)
			}

			_, _, err = c.Repositories.Get(context.Background(), "o", "r")
			if err == nil {
				t.Fatal("the redirect to another host was followed")
			}
			if strings.Contains(err.Error(), envValue) {
				t.Errorf("the error leaks the token: %v", err)
			}
			if got := other.requests(); len(got) != 0 {
				t.Errorf("the other host saw %+v", got)
			}
			if got := api.requests(); len(got) == 0 || got[0].Auth != "Bearer "+envValue {
				t.Errorf("the API host saw %+v", got)
			}
		})
	}
}

func TestRedirectOnTheSameHostIsFollowed(t *testing.T) {
	srv := newFakeGitHub(t)
	mux := http.NewServeMux()
	mux.HandleFunc(repoPath, func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, repoPath+"-moved", http.StatusTemporaryRedirect)
	})
	mux.HandleFunc(repoPath+"-moved", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"name":"r"}`))
	})
	var mu sync.Mutex
	var auths []string
	front := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		auths = append(auths, r.URL.Path+" "+r.Header.Get("Authorization"))
		mu.Unlock()
		mux.ServeHTTP(w, r)
	}))
	t.Cleanup(front.Close)
	_ = srv

	cfg := github.Config{Owner: "o", Repo: "r", TrustedAPIURL: front.URL + apiPath}
	c, err := github.New(cfg, nil, storeIn(t, map[string]string{envName: envValue}, nil)).
		RequireAuthentication(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	getRepo(t, c)
	want := []string{repoPath + " Bearer " + envValue, repoPath + "-moved Bearer " + envValue}
	if strings.Join(auths, "|") != strings.Join(want, "|") {
		t.Errorf("requests = %q, want %q", auths, want)
	}
}

func TestAppRequestsNeverReachAnotherHost(t *testing.T) {
	tests := map[string]struct{ installationID int64 }{
		"installation lookup with the JWT":    {},
		"installation token with the JWT":     {installationID: instID},
		"installation looked up, then tokens": {},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			other := newSink(t)
			api := redirector(t, http.StatusTemporaryRedirect, other.URL)
			key := appKey(t)
			app := &github.App{
				ID: appID, InstallationID: tc.installationID,
				PrivateKey: func() ([]byte, error) { return key, nil },
			}
			cfg := github.Config{Owner: "o", Repo: "r", TrustedAPIURL: api.URL + apiPath}
			forge := github.New(cfg, app, storeIn(t, nil, failingPrompter{t: t}))

			c, err := forge.RequireAuthentication(context.Background())
			if err == nil {
				// With an installation ID the token is fetched by the first API call.
				_, _, err = c.Repositories.Get(context.Background(), "o", "r")
			}
			if err == nil {
				t.Fatal("the redirect was followed")
			}
			if got := other.requests(); len(got) != 0 {
				t.Errorf("the other host saw %+v", got)
			}
			if strings.Contains(err.Error(), "PRIVATE KEY") {
				t.Errorf("the error leaks the key: %v", err)
			}
		})
	}
}

func TestInsecureURLIsRefused(t *testing.T) {
	tests := map[string]struct {
		api, upload string
		wantCode    string
	}{
		"http api host": {api: "http://ghe.example.com/", wantCode: "remeris::github::insecure_url"},
		"http upload host": {
			api:      "https://ghe.example.com/",
			upload:   "http://up.example.com/",
			wantCode: "remeris::github::insecure_url",
		},
		"https api host":     {api: "https://ghe.example.com/"},
		"http loopback ip":   {api: "http://127.0.0.1:9/"},
		"http loopback name": {api: "http://localhost:9/"},
		"http loopback ipv6": {api: "http://[::1]:9/"},
	}
	for name, tc := range tests {
		for _, require := range []bool{true, false} {
			t.Run(name, func(t *testing.T) {
				cfg := github.Config{Owner: "o", Repo: "r", TrustedAPIURL: tc.api, TrustedUploadURL: tc.upload}
				forge := github.New(cfg, nil, storeIn(t, map[string]string{envName: envValue}, failingPrompter{t: t}))
				get := forge.MaybeAuthenticated
				if require {
					get = forge.RequireAuthentication
				}
				_, err := get(context.Background())
				if tc.wantCode == "" {
					if err != nil {
						t.Fatal(err)
					}
					return
				}
				if codeOf(t, err) != tc.wantCode || !strings.Contains(err.Error(), "example.com") ||
					strings.Contains(err.Error(), envValue) {
					t.Errorf("err = %v", err)
				}
			})
		}
	}
}

// countingServer fails the test if it gets a request.
func countingServer(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("the server got a request: %s %s", r.Method, r.URL)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestUntrustedConfigURLFailsBeforeAnyRequest(t *testing.T) {
	tests := map[string]struct {
		cfg      func(url string) github.Config
		wantCode string
		wantText string
	}{
		"api url without a trusted value": {
			cfg:      func(u string) github.Config { return github.Config{APIURL: u + apiPath} },
			wantCode: "remeris::github::untrusted_api_url",
			wantText: "The [github] api_url in knope.toml points to http://127.0.0.1:",
		},
		"api url differs from the trusted value": {
			cfg: func(u string) github.Config {
				return github.Config{APIURL: u + apiPath, TrustedAPIURL: "https://ghe.example.com/api/v3/"}
			},
			wantCode: "remeris::github::untrusted_api_url",
		},
		"upload url without a trusted value": {
			cfg: func(u string) github.Config {
				return github.Config{APIURL: u + apiPath, UploadURL: u + "/up/", TrustedAPIURL: u + apiPath}
			},
			wantCode: "remeris::github::untrusted_upload_url",
		},
	}
	for name, tc := range tests {
		for _, require := range []bool{true, false} {
			t.Run(name, func(t *testing.T) {
				srv := countingServer(t)
				cfg := tc.cfg(srv.URL)
				cfg.Owner, cfg.Repo = "o", "r"
				forge := github.New(cfg, nil, storeIn(t, map[string]string{envName: envValue}, failingPrompter{t: t}))
				get := forge.MaybeAuthenticated
				if require {
					get = forge.RequireAuthentication
				}
				_, err := get(context.Background())
				if err == nil || codeOf(t, err) != tc.wantCode {
					t.Fatalf("err = %v, want code %s", err, tc.wantCode)
				}
				if tc.wantText != "" && !strings.Contains(err.Error(), tc.wantText) {
					t.Errorf("err = %q, want %q", err, tc.wantText)
				}
			})
		}
	}
}

func TestTrustedURLs(t *testing.T) {
	tests := map[string]func(url string) github.Config{
		"trusted only": func(u string) github.Config { return github.Config{TrustedAPIURL: u + apiPath} },
		"config matches": func(u string) github.Config {
			return github.Config{APIURL: u + apiPath, TrustedAPIURL: u + apiPath}
		},
		"config matches after normalisation": func(u string) github.Config {
			return github.Config{APIURL: strings.ToUpper(u[:4]) + u[4:] + "/api/v3", TrustedAPIURL: u + apiPath}
		},
		"upload matches": func(u string) github.Config {
			return github.Config{
				APIURL: u + apiPath, UploadURL: u + "/up/", TrustedAPIURL: u + apiPath, TrustedUploadURL: u + "/up",
			}
		},
	}
	for name, mk := range tests {
		t.Run(name, func(t *testing.T) {
			srv := newFakeGitHub(t)
			cfg := mk(srv.URL)
			cfg.Owner, cfg.Repo = "o", "r"
			c, err := github.New(cfg, nil, storeIn(t, map[string]string{envName: envValue}, nil)).
				RequireAuthentication(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			getRepo(t, c)
			if got := srv.last(t); got.Auth != "Bearer "+envValue {
				t.Errorf("request = %+v", got)
			}
		})
	}
}

func TestFailedBuildIsNotCached(t *testing.T) {
	srv := newFakeGitHub(t)
	key := appKey(t)
	calls := 0
	app := &github.App{ID: appID, InstallationID: instID, PrivateKey: func() ([]byte, error) {
		calls++
		if calls == 1 {
			return nil, errors.New("key file is not there yet")
		}
		return key, nil
	}}
	forge := github.New(srv.config(), app, storeIn(t, nil, failingPrompter{t: t}))

	if _, err := forge.RequireAuthentication(context.Background()); err == nil {
		t.Fatal("the first build must fail")
	}
	c, err := forge.RequireAuthentication(context.Background())
	if err != nil {
		t.Fatalf("the second call failed: %v", err)
	}
	getRepo(t, c)
	if got := srv.last(t).Auth; got != "Bearer "+installValue {
		t.Errorf("Authorization = %q", got)
	}
	again, _ := forge.RequireAuthentication(context.Background())
	if again != c || calls != 2 {
		t.Errorf("the client built after the failure is not cached (key read %d times)", calls)
	}
}

func TestConcurrentRequireAuthentication(t *testing.T) {
	srv := newFakeGitHub(t)
	p := prompt.NewScripted(prompt.Answer{Text: typedValue})
	forge := github.New(srv.config(), nil, storeIn(t, nil, p))

	const workers = 16
	clients := make([]*github.Client, workers)
	errs := make([]error, workers)
	var wg sync.WaitGroup
	for i := range workers {
		wg.Go(func() {
			clients[i], errs[i] = forge.RequireAuthentication(context.Background())
		})
	}
	wg.Wait()
	for i := range workers {
		if errs[i] != nil {
			t.Fatalf("worker %d: %v", i, errs[i])
		}
		if clients[i] != clients[0] {
			t.Errorf("worker %d got another client", i)
		}
	}
	if n := len(p.Asked()); n != 1 {
		t.Errorf("asked %d times, want 1", n)
	}
}
