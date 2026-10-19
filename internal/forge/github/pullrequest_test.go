package github_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/celest-io/remeris/internal/credentials"
	"github.com/celest-io/remeris/internal/forge/github"
)

const pullsPath = "/api/v3/repos/o/r/commits/abc1234/pulls"

func TestPullRequestForCommit(t *testing.T) {
	t.Parallel()
	tests := map[string]struct {
		status    int
		body      string
		wantFound bool
		wantNum   int
		wantLogin string
		wantErr   string
	}{
		"first of several": {
			status: http.StatusOK, body: `[{"number":7,"user":{"login":"ann"}},{"number":8,"user":{"login":"bob"}}]`,
			wantFound: true, wantNum: 7, wantLogin: "ann",
		},
		"no user": {status: http.StatusOK, body: `[{"number":9,"user":null}]`, wantFound: true, wantNum: 9},
		"empty list": {status: http.StatusOK, body: `[]`},
		"error": {
			status: http.StatusNotFound, body: `{"message":"Not Found"}`,
			wantErr: "Trouble communicating with GitHub while fetching PRs for commit abc1234",
		},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			var mu sync.Mutex
			var paths []string
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				mu.Lock()
				paths = append(paths, r.URL.Path)
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
			num, login, found, err := client.PullRequestForCommit(context.Background(), "abc1234")
			if len(paths) != 1 || paths[0] != pullsPath {
				t.Errorf("paths = %v, want [%s]", paths, pullsPath)
			}
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("err = %v, want %q", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if found != tc.wantFound || num != tc.wantNum || login != tc.wantLogin {
				t.Errorf("got (%d, %q, %v)", num, login, found)
			}
		})
	}
}
