package jira

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/celest-io/remeris/internal/credentials"
	"github.com/celest-io/remeris/internal/diag"
	"github.com/celest-io/remeris/internal/httpx"
	"github.com/celest-io/remeris/internal/prompt"
)

const (
	testEmail       = "dev@example.com"
	testToken       = "tok-SENTINEL-4f9c2a" //nolint:gosec // G101: a sentinel, not a credential
	testBlob        = "ZGV2QGV4YW1wbGUuY29tOnRvay1TRU5USU5FTC00ZjljMmE="
	testAuth        = "Basic " + testBlob
	searchURI       = "/rest/api/3/search/jql"
	testProject     = "PRJ"
	codeInsecureURL = "remeris::jira::insecure_url"
)

// seen is one request as the server got it.
type seen struct {
	method, uri, auth, contentType, body string
}

// server is an httptest server that records its requests.
type server struct {
	*httptest.Server
	mu   sync.Mutex
	reqs []seen
}

func newServer(t *testing.T, handler func(w http.ResponseWriter, r *http.Request)) *server {
	t.Helper()
	s := &server{}
	s.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		s.mu.Lock()
		s.reqs = append(s.reqs, seen{
			method: r.Method, uri: r.RequestURI, auth: r.Header.Get("Authorization"),
			contentType: r.Header.Get("Content-Type"), body: string(body),
		})
		s.mu.Unlock()
		handler(w, r)
	}))
	t.Cleanup(s.Close)
	return s
}

func (s *server) requests() []seen {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]seen(nil), s.reqs...)
}

// replying answers every request with status and body.
func replying(status int, body string) func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(status)
		_, _ = io.WriteString(w, body)
	}
}

// storeWith returns a credential store in a new directory with the given environment.
func storeWith(t *testing.T, env map[string]string, p prompt.Prompter) *credentials.Store {
	t.Helper()
	return &credentials.Store{Dir: t.TempDir(), Getenv: func(k string) string { return env[k] }, Prompter: p}
}

// storedCredentials returns a store that holds the email and the token as files, and no prompt.
func storedCredentials(t *testing.T) *credentials.Store {
	t.Helper()
	store := storeWith(t, nil, nil)
	store.Prompter = prompt.NewScripted(prompt.Answer{Text: testEmail}, prompt.Answer{Text: testToken})
	if _, err := store.Get(context.Background(), credentials.Email); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Get(context.Background(), credentials.JiraToken); err != nil {
		t.Fatal(err)
	}
	store.Prompter = nil
	return store
}

// newForge returns a forge for url that does not wait between retries.
func newForge(url string, store *credentials.Store) *Forge {
	f := New(Config{URL: url, Project: testProject}, store)
	f.tune = func(t *httpx.Transport) {
		t.Sleep = func(context.Context, time.Duration) error { return nil }
	}
	return f
}

func newClient(t *testing.T, url string) *Client {
	t.Helper()
	c, err := newForge(url, storedCredentials(t)).RequireAuthentication(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func codeOf(t *testing.T, err error) string {
	t.Helper()
	var d diag.Diagnostic
	if !errors.As(err, &d) {
		t.Fatalf("err = %v, want a diagnostic", err)
	}
	return d.Code()
}

func helpOf(err error) string {
	var d diag.Diagnostic
	if errors.As(err, &d) {
		return d.Help()
	}
	return ""
}

// roundTripFunc is a transport made of a function.
type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
