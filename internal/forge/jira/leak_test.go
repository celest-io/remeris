package jira

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"testing"

	"github.com/celest-io/remeris/internal/diag"
	"github.com/celest-io/remeris/internal/prompt"
)

// syncBuffer collects log output from the goroutines of the HTTP client.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// captureLogs sends slog to a buffer for the test.
func captureLogs(t *testing.T) *syncBuffer {
	t.Helper()
	logs := &syncBuffer{}
	old := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(logs, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(old) })
	return logs
}

// secrets are the forms of the credential that must never be shown. The email is not a secret by itself, but the
// blob and the header are.
var secrets = map[string]string{
	"token":  testToken,
	"blob":   testBlob,
	"header": testAuth,
}

func TestCredentialsNeverLeak(t *testing.T) {
	logs := captureLogs(t)
	closed := newServer(t, replying(http.StatusOK, ""))
	closedURL := closed.URL
	closed.Close()
	echo := testToken + " " + testBlob + " " + testAuth + " " + "tok%2DSENTINEL"

	tests := map[string]struct {
		url       string
		handler   func(http.ResponseWriter, *http.Request)
		transport http.RoundTripper
		call      func(*Client) error
	}{
		"500 that echoes everything": {
			handler: replying(http.StatusInternalServerError, `{"message":"invalid `+echo+`"}`),
			call:    search,
		},
		"401 that echoes everything": {handler: replying(http.StatusUnauthorized, echo), call: search},
		"404 on the transitions": {
			handler: replying(http.StatusNotFound, echo),
			call:    func(c *Client) error { return c.TransitionIssue(context.Background(), "PRJ-1", "Done") },
		},
		"bad JSON that echoes everything": {handler: replying(http.StatusOK, echo), call: search},
		"refused redirect whose Location echoes the token": {
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Location", "http://user:"+testToken+"@127.0.0.1:1/x/"+testBlob+"?t="+testToken)
				w.WriteHeader(http.StatusTemporaryRedirect)
			},
			call: search,
		},
		"closed server": {url: closedURL, call: search},
		"transport error that echoes the credentials": {
			transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
				return nil, errors.New("proxy rejected " + echo)
			}),
			call: search,
		},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			url := tc.url
			if url == "" {
				url = newServer(t, tc.handler).URL
			}
			c := newClient(t, url)
			if tc.transport != nil {
				c.http.Transport = tc.transport
			}

			err := tc.call(c)

			if err == nil {
				t.Fatal(errNoIssue)
			}
			var rendered strings.Builder
			diag.Render(&rendered, err)
			texts := map[string]string{
				"message": err.Error(), "help": helpOf(err), "rendering": rendered.String(), "logs": logs.String(),
				"%v": fmt.Sprintf("%v", err), "%+v": fmt.Sprintf("%+v", err),
			}
			for where, text := range texts {
				for what, secret := range secrets {
					if strings.Contains(text, secret) {
						t.Errorf("the %s is in the %s: %q", what, where, text)
					}
				}
			}
		})
	}
}

func search(c *Client) error {
	_, err := c.SearchIssues(context.Background(), "Done")
	return err
}

// TestPromptedCredentialsNeverLeak makes sure that an answer typed at the prompt is scrubbed like a stored one.
func TestPromptedCredentialsNeverLeak(t *testing.T) {
	logs := captureLogs(t)
	srv := newServer(t, replying(http.StatusBadGateway, "gateway saw "+testToken+" and "+testBlob))
	scripted := prompt.NewScripted(prompt.Answer{Text: testEmail}, prompt.Answer{Text: testToken})
	c, err := newForge(srv.URL, storeWith(t, nil, scripted)).RequireAuthentication(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	err = search(c)

	if err == nil || strings.Contains(err.Error(), testToken) || strings.Contains(err.Error(), testBlob) {
		t.Errorf("err = %v", err)
	}
	if got := logs.String(); strings.Contains(got, testToken) || strings.Contains(got, testBlob) ||
		!strings.Contains(got, "Using the Jira token") {
		t.Errorf("logs = %q", got)
	}
}

func TestTokenIsNeverInAURL(t *testing.T) {
	srv := newServer(t, replying(http.StatusOK, `{"issues":[],"transitions":[]}`))
	c := newClient(t, srv.URL)

	_ = search(c)
	_, _ = c.Transitions(context.Background(), "PRJ-1")

	for _, r := range srv.requests() {
		if strings.Contains(r.uri, testToken) || strings.Contains(r.uri, testBlob) || strings.Contains(r.uri, testEmail) {
			t.Errorf("uri = %q", r.uri)
		}
	}
}

// TestRedirectToAnotherOriginIsRefused follows a redirect to another port of the same machine.
func TestRedirectToAnotherOriginIsRefused(t *testing.T) {
	other := newServer(t, replying(http.StatusOK, `{"issues":[]}`))
	own := newServer(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Location", other.URL+"/x")
		w.WriteHeader(http.StatusTemporaryRedirect)
	})
	c := newClient(t, own.URL)

	err := search(c)

	if err == nil || codeOf(t, err) != "remeris::jira::redirect" {
		t.Fatalf("err = %v, want a refused redirect", err)
	}
	if n := len(other.requests()); n != 0 {
		t.Errorf("the other origin got %d requests", n)
	}
}

// TestRedirectWithinTheOriginKeepsTheCredential follows a redirect to another path of the Jira site.
func TestRedirectWithinTheOriginKeepsTheCredential(t *testing.T) {
	srv := newServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == searchURI {
			w.Header().Set("Location", "/moved")
			w.WriteHeader(http.StatusTemporaryRedirect)
			return
		}
		_, _ = w.Write([]byte(`{"issues":[]}`))
	})
	c := newClient(t, srv.URL)

	if err := search(c); err != nil {
		t.Fatal(err)
	}

	reqs := srv.requests()
	if len(reqs) != 2 || reqs[1].uri != "/moved" || reqs[1].auth != testAuth || reqs[1].method != http.MethodPost ||
		reqs[1].body != reqs[0].body {
		t.Errorf("requests = %+v", reqs)
	}
}

// TestGuardDropsTheHeaderForAnotherOrigin removes the redirect policy to prove the layer below it: even if a
// redirect were followed, the Authorization header would not reach another origin.
func TestGuardDropsTheHeaderForAnotherOrigin(t *testing.T) {
	other := newServer(t, replying(http.StatusOK, `{"issues":[]}`))
	own := newServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == searchURI {
			w.Header().Set("Location", other.URL+"/x")
			w.WriteHeader(http.StatusTemporaryRedirect)
			return
		}
		_, _ = w.Write([]byte(`{"issues":[]}`))
	})
	c := newClient(t, own.URL)
	c.http.CheckRedirect = nil

	if err := search(c); err != nil {
		t.Fatal(err)
	}

	got := other.requests()
	if len(got) != 1 {
		t.Fatalf("other requests = %+v", got)
	}
	if got[0].auth != "" {
		t.Errorf("the other origin got Authorization %q", got[0].auth)
	}
	if own.requests()[0].auth != testAuth {
		t.Errorf("own Authorization = %q", own.requests()[0].auth)
	}
}
