package github_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/celest-io/remeris/internal/forge/github"
)

func TestWrapError(t *testing.T) {
	tests := map[string]struct {
		handler func(http.ResponseWriter, *http.Request)
		want    string
	}{
		"not found with a JSON body": {
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusNotFound)
				_, _ = w.Write([]byte(`{"message":"Not Found","documentation_url":"https://docs.github.com/x"}`))
			},
			want: "Trouble communicating with GitHub while getting the repository: " +
				"HTTP status client error (404 Not Found) for url (URL) with body: " +
				`{"message":"Not Found","documentation_url":"https://docs.github.com/x"}`,
		},
		"primary rate limit": {
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("X-RateLimit-Remaining", "0")
				w.Header().Set("X-RateLimit-Reset", "4102444800")
				w.WriteHeader(http.StatusForbidden)
				_, _ = w.Write([]byte(`{"message":"API rate limit exceeded"}`))
			},
			want: "Trouble communicating with GitHub while getting the repository: " +
				"HTTP status client error (403 Forbidden) for url (URL) with body: " +
				`{"message":"API rate limit exceeded"}`,
		},
		"secondary rate limit": {
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusForbidden)
				_, _ = w.Write([]byte(`{"message":"slow down","documentation_url":"https://docs.github.com/rest#secondary-rate-limits"}`))
			},
			want: "Trouble communicating with GitHub while getting the repository: " +
				"HTTP status client error (403 Forbidden) for url (URL) with body: " +
				`{"message":"slow down","documentation_url":"https://docs.github.com/rest#secondary-rate-limits"}`,
		},
		"body that is not JSON": {
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusUnprocessableEntity)
				_, _ = w.Write([]byte("plain text"))
			},
			want: "Trouble communicating with GitHub while getting the repository: " +
				"HTTP status client error (422 Unprocessable Entity) for url (URL) with body: plain text",
		},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(tc.handler))
			t.Cleanup(srv.Close)
			cfg := github.Config{Owner: "o", Repo: "r", APIURL: srv.URL + apiPath}
			c, err := github.New(cfg, nil, storeIn(t, nil, nil)).MaybeAuthenticated(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			_, _, err = c.Repositories.Get(context.Background(), "o", "r")
			wrapped := github.WrapError("getting the repository", err)
			want := strings.Replace(tc.want, "(URL)", "("+srv.URL+repoPath+")", 1)
			if wrapped == nil || wrapped.Error() != want {
				t.Errorf("WrapError = %v\nwant %s", wrapped, want)
			}
			if !errors.Is(wrapped, err) {
				t.Error("the original error is not the cause")
			}
		})
	}
}

func TestWrapErrorTransport(t *testing.T) {
	cause := errors.New("connection refused")
	err := github.WrapError("creating a release", cause)
	want := "Trouble communicating with GitHub while creating a release: connection refused"
	if err == nil || err.Error() != want || !errors.Is(err, cause) {
		t.Errorf("WrapError = %v, want %s", err, want)
	}
	if github.WrapError("anything", nil) != nil {
		t.Error("WrapError(nil) is not nil")
	}
}
