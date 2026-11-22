package github_test

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/celest-io/remeris/internal/credentials"
	"github.com/celest-io/remeris/internal/diag"
	"github.com/celest-io/remeris/internal/forge/github"
)

const (
	graphQLOnGHE = "/api/graphql"
	commitOID    = "0123456789abcdef0123456789abcdef01234567"
	headOID      = "abc"
)

// graphQLServer answers the GraphQL endpoint with reply and records the requests.
func graphQLServer(t *testing.T, path, reply string) (*httptest.Server, *[]map[string]any) {
	t.Helper()
	var seen []map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != path {
			http.NotFound(w, r)
			return
		}
		body, _ := io.ReadAll(r.Body)
		var got map[string]any
		if err := json.Unmarshal(body, &got); err != nil {
			t.Errorf("body is not JSON: %v", err)
		}
		got["auth"] = r.Header.Get("Authorization")
		seen = append(seen, got)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(reply))
	}))
	t.Cleanup(srv.Close)
	return srv, &seen
}

func commitClient(t *testing.T, srv *httptest.Server, apiPath string) *github.Client {
	t.Helper()
	f := github.New(github.Config{Owner: "o", Repo: "r", APIURL: srv.URL + apiPath, TrustedAPIURL: srv.URL + apiPath}, nil,
		&credentials.Store{Dir: t.TempDir(), Getenv: func(k string) string {
			if k == "GITHUB_TOKEN" {
				return envValue
			}
			return ""
		}})
	c, err := f.RequireAuthentication(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestCreateCommitOnBranch(t *testing.T) {
	t.Parallel()
	tests := map[string]struct {
		apiPath, wantPath string
	}{
		"GitHub Enterprise Server is below /api/v3/": {apiPath: "/api/v3/", wantPath: graphQLOnGHE},
		"go-github adds api/v3 to a custom path":     {apiPath: "/custom/", wantPath: "/custom/api/graphql"},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			srv, seen := graphQLServer(t, tc.wantPath, `{"data":{"createCommitOnBranch":{"commit":{"oid":"`+commitOID+`"}}}}`)
			c := commitClient(t, srv, tc.apiPath)

			sha, err := c.CreateCommitOnBranch(t.Context(), github.CommitInput{
				Branch: "release", ExpectedHead: headOID, Message: "chore: release\n\nbody line\n",
				Additions: []github.CommitFile{{Path: "docs/a.md", Content: []byte("hi\n")}},
				Deletions: []string{"old.md"},
			})
			if err != nil || sha != commitOID {
				t.Fatalf("sha %q, error %v", sha, err)
			}
			if len(*seen) != 1 {
				t.Fatalf("%d requests", len(*seen))
			}
			req := (*seen)[0]
			if req["auth"] != "Bearer "+envValue {
				t.Errorf("authorization = %v", req["auth"])
			}
			input := req["variables"].(map[string]any)["input"].(map[string]any) //nolint:forcetypeassert // test data
			want := `{"branch":{"branchName":"release","repositoryNameWithOwner":"o/r"},` +
				`"expectedHeadOid":"abc",` +
				`"fileChanges":{"additions":[{"contents":"aGkK","path":"docs/a.md"}],"deletions":[{"path":"old.md"}]},` +
				`"message":{"body":"body line","headline":"chore: release"}}`
			got, _ := json.Marshal(input)
			if string(got) != want {
				t.Errorf("input =\n%s\nwant\n%s", got, want)
			}
			if !strings.Contains(req["query"].(string), "createCommitOnBranch") { //nolint:forcetypeassert // test data
				t.Errorf("query = %v", req["query"])
			}
		})
	}
}

func TestCreateCommitOnBranchErrors(t *testing.T) {
	t.Parallel()
	tests := map[string]struct {
		reply string
		want  string
	}{
		"a GraphQL error": {
			reply: `{"errors":[{"message":"Expected branch to point to \"abc\" but it did not"}]}`,
			want:  "Expected branch to point to",
		},
		"no commit in the answer": {reply: `{"data":{"createCommitOnBranch":{"commit":{}}}}`, want: "did not return"},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			srv, _ := graphQLServer(t, graphQLOnGHE, tc.reply)
			c := commitClient(t, srv, "/api/v3/")
			_, err := c.CreateCommitOnBranch(t.Context(), github.CommitInput{Branch: "b", ExpectedHead: headOID, Message: "m"})
			var d diag.Diagnostic
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error = %v", err)
			}
			if !asDiag(err, &d) || d.Code() != "remeris::github::commit" {
				t.Fatalf("not the commit diagnostic: %v", err)
			}
		})
	}
}

func TestCreateCommitOnBranchHTTPError(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"message":"Resource not accessible by integration"}`))
	}))
	t.Cleanup(srv.Close)
	c := commitClient(t, srv, "/api/v3/")
	_, err := c.CreateCommitOnBranch(t.Context(), github.CommitInput{Branch: "b", ExpectedHead: headOID, Message: "m"})
	if err == nil || !strings.Contains(err.Error(), "Trouble communicating with GitHub while creating a commit") {
		t.Fatalf("error = %v", err)
	}
}

func TestHasCredentials(t *testing.T) {
	t.Parallel()
	store := func(token string) *credentials.Store {
		return &credentials.Store{Dir: t.TempDir(), Getenv: func(k string) string {
			if k == "GITHUB_TOKEN" {
				return token
			}
			return ""
		}}
	}
	cfg := github.Config{Owner: "o", Repo: "r"}
	tests := map[string]struct {
		forge *github.Forge
		want  bool
	}{
		"nothing":  {forge: github.New(cfg, nil, store("")), want: false},
		"a token":  {forge: github.New(cfg, nil, store("t")), want: true},
		"an App":   {forge: github.New(cfg, &github.App{ID: 1}, store("")), want: true},
		"App id 0": {forge: github.New(cfg, &github.App{}, store("")), want: false},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if got := tc.forge.HasCredentials(); got != tc.want {
				t.Fatalf("HasCredentials = %v, want %v", got, tc.want)
			}
		})
	}
}

func asDiag(err error, d *diag.Diagnostic) bool {
	for err != nil {
		if v, ok := err.(diag.Diagnostic); ok { //nolint:errorlint // walks the chain by hand to find the first diagnostic
			*d = v
			return true
		}
		u, ok := err.(interface{ Unwrap() error }) //nolint:errorlint // see above
		if !ok {
			return false
		}
		err = u.Unwrap()
	}
	return false
}

func TestHasCredentialsDefaultStore(t *testing.T) { //nolint:paralleltest // sets an environment variable
	cfg := github.Config{Owner: "o", Repo: "r"}
	t.Setenv("GITHUB_TOKEN", "")
	if github.New(cfg, nil, nil).HasCredentials() {
		t.Fatal("no token is set")
	}
	t.Setenv("GITHUB_TOKEN", "t")
	if !github.New(cfg, nil, nil).HasCredentials() {
		t.Fatal("the token variable is set")
	}
}
