package github_test

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/celest-io/remeris/internal/credentials"
	"github.com/celest-io/remeris/internal/forge/github"
	"github.com/celest-io/remeris/internal/prompt"
)

const (
	// Fake secrets, made up for the tests.
	envValue     = "fake-env-token"
	storedValue  = "fake-stored-token"
	typedValue   = "fake-typed-token"
	installValue = "fake-installation-token"

	userAgent = "Remeris"
	ghePath   = "https://ghe.example.com/api/v3/"
	apiPath   = "/api/v3/"
	repoPath  = apiPath + "repos/o/r"
	appID     = 4242
	instID    = 77
	envName   = "GITHUB_TOKEN"
)

// request is what the fake API saw of one request.
type request struct {
	Method, Path, Auth, UserAgent string
}

// fakeGitHub is an httptest server that serves the endpoints the tests use and records the requests.
type fakeGitHub struct {
	*httptest.Server
	mu       sync.Mutex
	requests []request
	// jwtIssuers are the iss claims of the App JWTs seen.
	jwtIssuers []string
	tokenCalls int
}

func newFakeGitHub(t *testing.T) *fakeGitHub {
	t.Helper()
	f := &fakeGitHub{}
	mux := http.NewServeMux()
	mux.HandleFunc("GET "+repoPath, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"name":"r"}`))
	})
	mux.HandleFunc("GET "+repoPath+"/installation", func(w http.ResponseWriter, r *http.Request) {
		f.noteJWT(r)
		_, _ = w.Write([]byte(`{"id":` + "77" + `}`))
	})
	mux.HandleFunc("POST "+apiPath+"app/installations/{id}/access_tokens", func(w http.ResponseWriter, r *http.Request) {
		f.noteJWT(r)
		f.mu.Lock()
		f.tokenCalls++
		f.mu.Unlock()
		if r.PathValue("id") != "77" {
			http.NotFound(w, r)
			return
		}
		w.WriteHeader(http.StatusCreated)
		exp := time.Now().Add(time.Hour).UTC().Format(time.RFC3339)
		_, _ = w.Write([]byte(`{"token":"` + installValue + `","expires_at":"` + exp + `"}`))
	})
	f.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		f.requests = append(f.requests, request{r.Method, r.URL.Path, r.Header.Get("Authorization"), r.UserAgent()})
		f.mu.Unlock()
		mux.ServeHTTP(w, r)
	}))
	t.Cleanup(f.Close)
	return f
}

// noteJWT records the issuer of the JWT in the Authorization header.
func (f *fakeGitHub) noteJWT(r *http.Request) {
	parts := strings.Split(strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "), ".")
	if len(parts) != 3 {
		return
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return
	}
	var claims struct {
		Iss string `json:"iss"`
	}
	if json.Unmarshal(payload, &claims) != nil {
		return
	}
	f.mu.Lock()
	f.jwtIssuers = append(f.jwtIssuers, claims.Iss)
	f.mu.Unlock()
}

func (f *fakeGitHub) seen() []request {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]request(nil), f.requests...)
}

func (f *fakeGitHub) config() github.Config {
	return github.Config{Owner: "o", Repo: "r", TrustedAPIURL: f.URL + apiPath}
}

// last returns the last request.
func (f *fakeGitHub) last(t *testing.T) request {
	t.Helper()
	all := f.seen()
	if len(all) == 0 {
		t.Fatal("the server saw no request")
	}
	return all[len(all)-1]
}

// failingPrompter fails the test when asked anything.
type failingPrompter struct {
	prompt.Prompter
	t *testing.T
}

func (p failingPrompter) Password(context.Context, string) (string, error) {
	p.t.Helper()
	p.t.Error("the user must not be asked")
	return "", errors.New("asked")
}

// captureLog sends the default logger's debug output to the returned buffer until the test ends.
func captureLog(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	old := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(old) })
	return &buf
}

func envWith(env map[string]string) func(string) string {
	return func(k string) string { return env[k] }
}

func storeIn(t *testing.T, env map[string]string, p prompt.Prompter) *credentials.Store {
	t.Helper()
	return &credentials.Store{Dir: t.TempDir(), Getenv: envWith(env), Prompter: p}
}

func writeStored(t *testing.T, s *credentials.Store, token string) {
	t.Helper()
	dir := filepath.Join(s.Dir, "remeris")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "github_token"), []byte(token), 0o600); err != nil {
		t.Fatal(err)
	}
}

func getRepo(t *testing.T, c *github.Client) {
	t.Helper()
	if _, _, err := c.Repositories.Get(context.Background(), "o", "r"); err != nil {
		t.Fatalf("Repositories.Get: %v", err)
	}
}

// appKey returns a freshly generated PEM private key, never written to a file of the repository.
func appKey(t *testing.T) []byte {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	return pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})
}

func TestTokenSources(t *testing.T) {
	tests := map[string]struct {
		env      map[string]string
		stored   string
		wantAuth string
		wantLog  string
	}{
		"environment variable": {
			env: map[string]string{envName: envValue}, wantAuth: "Bearer " + envValue, wantLog: "Using GITHUB_TOKEN",
		},
		"environment beats the stored token": {
			env: map[string]string{envName: envValue}, stored: storedValue,
			wantAuth: "Bearer " + envValue, wantLog: "Using GITHUB_TOKEN",
		},
		"stored token": {
			stored: storedValue, wantAuth: "Bearer " + storedValue, wantLog: "Using the stored GitHub token",
		},
	}
	for name, tc := range tests {
		for _, require := range []bool{true, false} {
			t.Run(name+map[bool]string{true: " (require)", false: " (maybe)"}[require], func(t *testing.T) {
				logs := captureLog(t)
				srv := newFakeGitHub(t)
				store := storeIn(t, tc.env, failingPrompter{t: t})
				if tc.stored != "" {
					writeStored(t, store, tc.stored)
				}
				forge := github.New(srv.config(), nil, store)
				get := forge.MaybeAuthenticated
				if require {
					get = forge.RequireAuthentication
				}
				c, err := get(context.Background())
				if err != nil {
					t.Fatal(err)
				}
				getRepo(t, c)
				got := srv.last(t)
				if got.Auth != tc.wantAuth || got.UserAgent != userAgent || got.Path != repoPath {
					t.Errorf("request = %+v, want auth %q, user agent Remeris, path %s", got, tc.wantAuth, repoPath)
				}
				if !c.Authenticated {
					t.Error("Authenticated = false")
				}
				if !strings.Contains(logs.String(), tc.wantLog) {
					t.Errorf("log = %q, want %q", logs, tc.wantLog)
				}
				for _, secret := range []string{envValue, storedValue} {
					if strings.Contains(logs.String(), secret) {
						t.Errorf("the log leaks %q: %s", secret, logs)
					}
				}
			})
		}
	}
}

func TestRequireAuthenticationPrompts(t *testing.T) {
	logs := captureLog(t)
	srv := newFakeGitHub(t)
	p := prompt.NewScripted(prompt.Answer{Message: credentials.GitHubToken.Prompt(), Text: typedValue})
	store := storeIn(t, nil, p)
	forge := github.New(srv.config(), nil, store)

	c, err := forge.RequireAuthentication(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	getRepo(t, c)
	if got := srv.last(t).Auth; got != "Bearer "+typedValue {
		t.Errorf("Authorization = %q", got)
	}
	saved, err := os.ReadFile(filepath.Join(store.Dir, "remeris", "github_token"))
	if err != nil || string(saved) != typedValue {
		t.Errorf("stored token = %q, %v", saved, err)
	}
	if strings.Contains(logs.String(), typedValue) {
		t.Errorf("the log leaks the token: %s", logs)
	}
}

func TestRequireAuthenticationEmptyAnswer(t *testing.T) {
	srv := newFakeGitHub(t)
	store := storeIn(t, nil, prompt.NewScripted(prompt.Answer{Text: ""}))
	_, err := github.New(srv.config(), nil, store).RequireAuthentication(context.Background())
	if err == nil || err.Error() != "No GitHub token was given" {
		t.Errorf("err = %v", err)
	}
}

func TestMaybeAuthenticatedIsAnonymous(t *testing.T) {
	logs := captureLog(t)
	srv := newFakeGitHub(t)
	forge := github.New(srv.config(), nil, storeIn(t, nil, failingPrompter{t: t}))

	c, err := forge.MaybeAuthenticated(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	getRepo(t, c)
	got := srv.last(t)
	if got.Auth != "" || got.UserAgent != userAgent {
		t.Errorf("request = %+v, want no Authorization and user agent Remeris", got)
	}
	if c.Authenticated {
		t.Error("Authenticated = true")
	}
	if !strings.Contains(logs.String(), "Using GitHub without authentication") {
		t.Errorf("log = %q", logs)
	}
}

func TestClientCaching(t *testing.T) {
	srv := newFakeGitHub(t)
	p := prompt.NewScripted(prompt.Answer{Text: typedValue})
	forge := github.New(srv.config(), nil, storeIn(t, nil, p))
	ctx := context.Background()

	anon1, err := forge.MaybeAuthenticated(ctx)
	if err != nil {
		t.Fatal(err)
	}
	anon2, _ := forge.MaybeAuthenticated(ctx)
	if anon1 != anon2 {
		t.Error("the anonymous client is built twice")
	}

	authed1, err := forge.RequireAuthentication(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if authed1 == anon1 || !authed1.Authenticated {
		t.Fatalf("RequireAuthentication reused the anonymous client: %+v", authed1)
	}
	authed2, _ := forge.RequireAuthentication(ctx)
	maybe, _ := forge.MaybeAuthenticated(ctx)
	if authed1 != authed2 || authed1 != maybe {
		t.Error("the authenticated client is not cached or not used by MaybeAuthenticated")
	}
	if len(p.Asked()) != 1 {
		t.Errorf("asked %d times, want 1", len(p.Asked()))
	}
}

func TestEnterpriseURLs(t *testing.T) {
	tests := map[string]struct {
		api, upload       string
		wantBase, wantUpl string
	}{
		"api only": {
			api: "https://ghe.example.com", wantBase: ghePath,
			wantUpl: "https://ghe.example.com/api/uploads/",
		},
		"both": {
			api: ghePath, upload: "https://up.example.com/api/uploads/",
			wantBase: ghePath, wantUpl: "https://up.example.com/api/uploads/",
		},
		"none": {wantBase: "https://api.github.com/", wantUpl: "https://uploads.github.com/"},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			cfg := github.Config{Owner: "o", Repo: "r", TrustedAPIURL: tc.api, TrustedUploadURL: tc.upload}
			c, err := github.New(cfg, nil, storeIn(t, nil, nil)).MaybeAuthenticated(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			if c.BaseURL() != tc.wantBase || c.UploadURL() != tc.wantUpl {
				t.Errorf("URLs = %s, %s; want %s, %s", c.BaseURL(), c.UploadURL(), tc.wantBase, tc.wantUpl)
			}
			if c.Owner != "o" || c.Repo != "r" {
				t.Errorf("repository = %s/%s", c.Owner, c.Repo)
			}
		})
	}
}

func TestInvalidAPIURL(t *testing.T) {
	cfg := github.Config{Owner: "o", Repo: "r", TrustedAPIURL: "://bad"}
	_, err := github.New(cfg, nil, storeIn(t, nil, nil)).MaybeAuthenticated(context.Background())
	if err == nil || !strings.Contains(err.Error(), "The GitHub API URL is not valid") {
		t.Errorf("err = %v", err)
	}
}

func TestGitHubApp(t *testing.T) {
	tests := map[string]struct {
		installationID int64
		wantLookup     bool
	}{
		"installation id given":  {installationID: instID},
		"installation looked up": {wantLookup: true},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			logs := captureLog(t)
			srv := newFakeGitHub(t)
			key := appKey(t)
			app := &github.App{
				ID: appID, InstallationID: tc.installationID,
				PrivateKey: func() ([]byte, error) { return key, nil },
			}
			// An App wins over GITHUB_TOKEN and the stored token.
			store := storeIn(t, map[string]string{envName: envValue}, failingPrompter{t: t})
			forge := github.New(srv.config(), app, store)

			c, err := forge.RequireAuthentication(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			getRepo(t, c)
			getRepo(t, c)

			got := srv.last(t)
			if got.Auth != "Bearer "+installValue || got.UserAgent != userAgent {
				t.Errorf("request = %+v, want the installation token", got)
			}
			if srv.tokenCalls != 1 {
				t.Errorf("installation tokens requested %d times, want 1", srv.tokenCalls)
			}
			for _, iss := range srv.jwtIssuers {
				if iss != "4242" {
					t.Errorf("JWT issuer = %q, want 4242", iss)
				}
			}
			lookups := 0
			for _, r := range srv.seen() {
				if r.Path == repoPath+"/installation" {
					lookups++
				}
			}
			if (lookups == 1) != tc.wantLookup || lookups > 1 {
				t.Errorf("installation lookups = %d, want lookup = %v", lookups, tc.wantLookup)
			}
			if !strings.Contains(logs.String(), "Using GitHub App installation 77") {
				t.Errorf("log = %q", logs)
			}
			for _, secret := range []string{installValue, envValue, "PRIVATE KEY"} {
				if strings.Contains(logs.String(), secret) {
					t.Errorf("the log leaks %q: %s", secret, logs)
				}
			}
		})
	}
}

func TestGitHubAppBadKey(t *testing.T) {
	const secret = "SECRET-KEY-CONTENT"
	srv := newFakeGitHub(t)
	app := &github.App{ID: appID, InstallationID: instID, PrivateKey: func() ([]byte, error) { return []byte(secret), nil }}
	_, err := github.New(srv.config(), app, storeIn(t, nil, nil)).MaybeAuthenticated(context.Background())
	if err == nil {
		t.Fatal("want an error")
	}
	for e := err; e != nil; e = errors.Unwrap(e) {
		if strings.Contains(e.Error(), secret) {
			t.Errorf("the error leaks the key: %v", e)
		}
	}
	if !strings.Contains(err.Error(), "private key is not a valid RSA private key") {
		t.Errorf("err = %v", err)
	}
}

func TestGitHubAppKeyReadError(t *testing.T) {
	boom := errors.New("boom")
	app := &github.App{ID: appID, PrivateKey: func() ([]byte, error) { return nil, boom }}
	_, err := github.New(github.Config{Owner: "o", Repo: "r"}, app, storeIn(t, nil, nil)).
		MaybeAuthenticated(context.Background())
	if !errors.Is(err, boom) {
		t.Errorf("err = %v, want boom", err)
	}
}

// An empty stored token (an empty answer saved by an earlier prompt) is no token: no "Authorization: Bearer " is sent.
func TestEmptyStoredTokenIsNoToken(t *testing.T) {
	srv := newFakeGitHub(t)
	store := storeIn(t, nil, failingPrompter{t: t})
	writeStored(t, store, "")
	c, err := github.New(srv.config(), nil, store).MaybeAuthenticated(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	getRepo(t, c)
	if got := srv.last(t); got.Auth != "" || c.Authenticated {
		t.Errorf("auth = %q, authenticated = %v, want an anonymous request", got.Auth, c.Authenticated)
	}
}

// TestNoRealGitHubInTests proves the guard of TestMain: a forge without a trusted URL talks to api.github.com,
// and the request must fail before it leaves the machine.
func TestNoRealGitHubInTests(t *testing.T) {
	c, err := github.New(github.Config{Owner: "o", Repo: "r"}, nil, nil).MaybeAuthenticated(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	_, _, err = c.Repositories.GetRelease(context.Background(), "o", "r", 1)

	if err == nil || !strings.Contains(err.Error(), "tests must use httptest") {
		t.Errorf("err = %v, want the guard's error", err)
	}
}
