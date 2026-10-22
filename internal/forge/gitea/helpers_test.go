package gitea

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
	testToken    = "tok-SENTINEL-4f9c2a" //nolint:gosec // G101: a sentinel, not a credential
	releasesPath = "/api/v1/repos/o/r/releases"
	activity     = "creating a release"
)

// seen is one request as the server got it.
type seen struct {
	method, uri, auth, accept, contentType, userAgent, body string
}

// server is an httptest server that records its requests and answers with a fixed status and body.
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
			method: r.Method, uri: r.URL.RequestURI(), auth: r.Header.Get("Authorization"), accept: r.Header.Get("Accept"),
			contentType: r.Header.Get("Content-Type"), userAgent: r.UserAgent(), body: string(body),
		})
		s.mu.Unlock()
		handler(w, r)
	}))
	t.Cleanup(s.Close)
	return s
}

// replying answers every request with status and body.
func replying(status int, body string) func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(status)
		_, _ = io.WriteString(w, body)
	}
}

func (s *server) requests() []seen {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]seen(nil), s.reqs...)
}

// storeWith returns a credential store in a new directory with the given environment.
func storeWith(t *testing.T, env map[string]string, p prompt.Prompter) *credentials.Store {
	t.Helper()
	return &credentials.Store{Dir: t.TempDir(), Getenv: func(k string) string { return env[k] }, Prompter: p}
}

func tokenStore(t *testing.T) *credentials.Store {
	t.Helper()
	return storeWith(t, map[string]string{"GITEA_TOKEN": testToken}, nil)
}

// newForge returns a forge for host that does not wait between retries.
func newForge(host string, store *credentials.Store) *Forge {
	f := New(Config{Owner: "o", Repo: "r", Host: host}, store)
	f.tune = func(t *httpx.Transport) {
		t.Sleep = func(context.Context, time.Duration) error { return nil }
	}
	return f
}

func newClient(t *testing.T, host string) *Client {
	t.Helper()
	c, err := newForge(host, tokenStore(t)).RequireAuthentication(context.Background())
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
