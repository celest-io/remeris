package github_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/celest-io/remeris/internal/credentials"
	"github.com/celest-io/remeris/internal/diag"
	"github.com/celest-io/remeris/internal/forge/github"
)

const (
	graphqlPath = "/api/graphql"
	wantAPICode = "remeris::issues::github::api"
	firstTitle  = "first"
	secondTitle = "second"
	okBody      = `{"data":{"repository":{"issues":{"nodes":[{"number":4,"title":"first"},{"number":9,"title":"second"}]}}}}`
)

type graphqlRequest struct {
	Query     string         `json:"query"`
	Variables map[string]any `json:"variables"`
}

func TestListOpenIssues(t *testing.T) {
	t.Parallel()
	tests := map[string]struct {
		labels     []string
		status     int
		body       string
		wantLabels any
		want       []github.Issue
		wantErr    string
		wantCode   string
	}{
		"issues": {
			status: http.StatusOK, body: okBody, wantLabels: nil,
			want: []github.Issue{{Number: 4, Title: firstTitle}, {Number: 9, Title: secondTitle}},
		},
		"labels are a variable": {
			labels: []string{"a", `b") { x } #`}, status: http.StatusOK, body: okBody,
			wantLabels: []any{"a", `b") { x } #`},
			want:       []github.Issue{{Number: 4, Title: firstTitle}, {Number: 9, Title: secondTitle}},
		},
		"empty labels": {labels: []string{}, status: http.StatusOK, body: okBody, wantLabels: []any{},
			want: []github.Issue{{Number: 4, Title: firstTitle}, {Number: 9, Title: secondTitle}}},
		"no issues": {
			status: http.StatusOK, body: `{"data":{"repository":{"issues":{"nodes":[]}}}}`, wantLabels: nil,
		},
		"errors with status 200": {
			status:     http.StatusOK,
			body:       `{"data":{"repository":null},"errors":[{"message":"Could not resolve to a Repository"}]}`,
			wantLabels: nil,
			wantErr:    "Could not communicate with GitHub while loading issues: Could not resolve to a Repository",
			wantCode:   wantAPICode,
		},
		"status error": {
			status: http.StatusNotFound, body: `{"message":"Not Found"}`, wantLabels: nil,
			wantErr:  "Could not communicate with GitHub while loading issues: non-200 OK status code: 404 Not Found",
			wantCode: wantAPICode,
		},
		"bad json": {
			status: http.StatusOK, body: `<html>`, wantLabels: nil,
			wantErr:  "Could not communicate with GitHub while loading issues:",
			wantCode: wantAPICode,
		},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			var mu sync.Mutex
			var paths []string
			var got graphqlRequest
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				data, _ := io.ReadAll(r.Body)
				mu.Lock()
				paths = append(paths, r.Method+" "+r.URL.Path)
				_ = json.Unmarshal(data, &got)
				mu.Unlock()
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			}))
			t.Cleanup(srv.Close)
			f := github.New(github.Config{Owner: "o", Repo: "r", TrustedAPIURL: srv.URL + "/api/v3/"}, nil,
				&credentials.Store{Dir: t.TempDir(), Getenv: func(string) string { return "" }})
			client, err := f.MaybeAuthenticated(context.Background())
			if err != nil {
				t.Fatal(err)
			}

			issues, err := client.ListOpenIssues(context.Background(), tc.labels)

			if len(paths) != 1 || paths[0] != "POST "+graphqlPath {
				t.Fatalf("requests = %v, want one POST %s", paths, graphqlPath)
			}
			for _, part := range []string{
				"repository(name: $repo, owner: $owner)", "issues(states: OPEN, first: 30, labels: $labels)",
				"$labels:[String!]", "$owner:String!", "$repo:String!", "number", "title",
			} {
				if !strings.Contains(got.Query, part) {
					t.Errorf("query %q lacks %q", got.Query, part)
				}
			}
			if strings.Contains(got.Query, "b\") { x }") {
				t.Errorf("a label reached the query text: %q", got.Query)
			}
			if got.Variables["repo"] != "r" || got.Variables["owner"] != "o" {
				t.Errorf("variables = %v", got.Variables)
			}
			if fmt.Sprint(got.Variables["labels"]) != fmt.Sprint(tc.wantLabels) {
				t.Errorf("labels = %#v, want %#v", got.Variables["labels"], tc.wantLabels)
			}
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("err = %v, want %q", err, tc.wantErr)
				}
				if c, ok := err.(diag.Diagnostic); !ok ||
					c.Code() != tc.wantCode { //nolint:errorlint // the error is a diagnostic itself
					t.Errorf("error = %#v, want code %q", err, tc.wantCode)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if fmt.Sprint(issues) != fmt.Sprint(tc.want) {
				t.Errorf("issues = %v, want %v", issues, tc.want)
			}
		})
	}
}

func TestListOpenIssuesEndpoint(t *testing.T) {
	t.Parallel()
	tests := map[string]struct {
		apiURL   string
		wantPath string
	}{
		"enterprise server":   {apiURL: "/api/v3/", wantPath: "/api/graphql"},
		"enterprise no slash": {apiURL: "/api/v3", wantPath: "/api/graphql"},
		"context path":        {apiURL: "/ghe/api/v3/", wantPath: "/ghe/api/graphql"},
		// go-github appends api/v3/ to a path that lacks it, so this is a context path too.
		"go-github adds api/v3": {apiURL: "/custom/", wantPath: "/custom/api/graphql"},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			var path string
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				path = r.URL.Path
				_, _ = w.Write([]byte(okBody))
			}))
			t.Cleanup(srv.Close)
			f := github.New(github.Config{Owner: "o", Repo: "r", TrustedAPIURL: srv.URL + tc.apiURL}, nil,
				&credentials.Store{Dir: t.TempDir(), Getenv: func(string) string { return "" }})
			client, err := f.MaybeAuthenticated(context.Background())
			if err != nil {
				t.Fatal(err)
			}

			_, err = client.ListOpenIssues(context.Background(), nil)

			if err != nil {
				t.Fatal(err)
			}
			if path != tc.wantPath {
				t.Errorf("path = %q, want %q", path, tc.wantPath)
			}
		})
	}
}

func TestListOpenIssuesSendsToken(t *testing.T) {
	t.Parallel()
	var auth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth = r.Header.Get("Authorization")
		_, _ = w.Write([]byte(okBody))
	}))
	t.Cleanup(srv.Close)
	f := github.New(github.Config{Owner: "o", Repo: "r", TrustedAPIURL: srv.URL + "/api/v3/"}, nil,
		&credentials.Store{Dir: t.TempDir(), Getenv: func(k string) string {
			if k == credentials.GitHubToken.Env() {
				return "fake-token"
			}
			return ""
		}})
	client, err := f.RequireAuthentication(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.ListOpenIssues(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	if auth != "Bearer fake-token" {
		t.Errorf("Authorization = %q", auth)
	}
}
