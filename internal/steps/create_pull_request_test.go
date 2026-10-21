package steps_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/celest-io/remeris/internal/credentials"
	"github.com/celest-io/remeris/internal/diag"
	"github.com/celest-io/remeris/internal/forge/github"
	"github.com/celest-io/remeris/internal/git"
	"github.com/celest-io/remeris/internal/steps"
	"github.com/celest-io/remeris/internal/variables"
	"github.com/celest-io/remeris/internal/workflow"
)

const (
	prPulls   = "/api/v3/repos/o/r/pulls"
	prEdit    = prPulls + "/7"
	prBase    = "main"
	prFeature = "feature/x"
	prTitle   = "chore: Release"
	prBody    = "line one\n\nline two"
	prNoPR    = "[]"
	prNoForg  = "No forge has been configured"
)

// branchGit answers CurrentBranch and nothing else.
type branchGit struct {
	git.Git

	branch string
	err    error
	calls  int
}

func (g *branchGit) CurrentBranch(context.Context) (string, error) {
	g.calls++
	return g.branch, g.err
}

type prAPI struct {
	*httptest.Server

	mu    sync.Mutex
	calls []apiCall
	// existing is the body of the list response.
	existing string
	// status makes every request fail with this status when not zero.
	status int
}

func newPRAPI(t *testing.T, existing string) *prAPI {
	t.Helper()
	f := &prAPI{existing: existing}
	f.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		f.mu.Lock()
		f.calls = append(f.calls, apiCall{r.Method, r.URL.Path, r.URL.RawQuery, r.Header.Get("Authorization"), string(body)})
		status := f.status
		f.mu.Unlock()
		if status != 0 {
			w.WriteHeader(status)
			_, _ = w.Write([]byte(`{"message":"Validation Failed"}`))
			return
		}
		switch r.Method + " " + r.URL.Path {
		case "GET " + prPulls:
			_, _ = w.Write([]byte(f.existing))
		case "POST " + prPulls:
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"number":8,"url":"https://api.github.com/repos/o/r/pulls/8"}`))
		case "PATCH " + prEdit:
			_, _ = w.Write([]byte(`{"number":7}`))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(f.Close)
	return f
}

func (f *prAPI) seen() []apiCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.calls)
}

func (f *prAPI) keys() []string {
	var out []string
	for _, c := range f.seen() {
		out = append(out, c.key())
	}
	return out
}

func (f *prAPI) forge(t *testing.T, store *credentials.Store) *github.Forge {
	t.Helper()
	return github.New(github.Config{Owner: "o", Repo: "r", TrustedAPIURL: f.URL + "/api/v3/"}, nil, store)
}

func tokenStore(t *testing.T) *credentials.Store {
	t.Helper()
	return &credentials.Store{Dir: t.TempDir(), Getenv: func(k string) string {
		if k == credentials.GitHubToken.Env() {
			return fakeToken
		}
		return ""
	}}
}

func prStep() *steps.CreatePullRequest {
	return &steps.CreatePullRequest{
		Base:  prBase,
		Title: variables.Template{Text: prTitle, Variables: []variables.Entry{}},
		Body:  variables.Template{Text: prBody, Variables: []variables.Entry{}},
	}
}

func runPR(state workflow.State, dryRun bool) (workflow.RunType[workflow.State], error) {
	rt := workflow.Real(state)
	if dryRun {
		rt = workflow.DryRun(state)
	}
	return prStep().Run(context.Background(), rt)
}

func decode(t *testing.T, c apiCall) map[string]any {
	t.Helper()
	var out map[string]any
	if err := json.Unmarshal([]byte(c.Body), &out); err != nil {
		t.Fatal(err)
	}
	return out
}

func TestCreatePullRequestCreates(t *testing.T) {
	cases := map[string]struct {
		branch    string
		wantQuery string
	}{
		"plain branch": {branch: prBase, wantQuery: "base=main&head=o%3Amain"},
		"slash":        {branch: prFeature, wantQuery: "base=main&head=o%3Afeature%2Fx"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			api := newPRAPI(t, prNoPR)
			store := tokenStore(t)
			state := workflow.State{Git: &branchGit{branch: tc.branch}, GitHub: api.forge(t, store)}

			if _, err := runPR(state, false); err != nil {
				t.Fatal(err)
			}

			if got, want := api.keys(), []string{"GET " + prPulls, "POST " + prPulls}; !slices.Equal(got, want) {
				t.Fatalf("requests = %q, want %q", got, want)
			}
			calls := api.seen()
			if q := calls[0].Query; q != tc.wantQuery {
				t.Errorf("query = %q, want %q (no state, like Knope)", q, tc.wantQuery)
			}
			if calls[0].Auth != "Bearer "+fakeToken {
				t.Errorf("Authorization = %q", calls[0].Auth)
			}
			got := decode(t, calls[1])
			want := map[string]any{"title": prTitle, "body": prBody, "head": tc.branch, "base": prBase}
			if len(got) != len(want) {
				t.Errorf("body = %v, want %v", got, want)
			}
			for k, v := range want {
				if got[k] != v {
					t.Errorf("%s = %v, want %v", k, got[k], v)
				}
			}
		})
	}
}

func TestCreatePullRequestUpdatesExisting(t *testing.T) {
	logs := captureDebugLogs(t)
	api := newPRAPI(t, `[{"number":7,"url":"https://api.github.com/repos/o/r/pulls/7"},{"number":9}]`)
	state := workflow.State{Git: &branchGit{branch: prFeature}, GitHub: api.forge(t, tokenStore(t))}

	if _, err := runPR(state, false); err != nil {
		t.Fatal(err)
	}

	if got, want := api.keys(), []string{"GET " + prPulls, "PATCH " + prEdit}; !slices.Equal(got, want) {
		t.Fatalf("requests = %q, want %q", got, want)
	}
	got := decode(t, api.seen()[1])
	if len(got) != 2 || got["title"] != prTitle || got["body"] != prBody {
		t.Errorf("body = %v, want title and body only", got)
	}
	want := "Updating existing pull request: https://api.github.com/repos/o/r/pulls/7"
	if lines := logLines(logs); !slices.Contains(lines, want) {
		t.Errorf("log = %q, want %q", lines, want)
	}
}

func TestCreatePullRequestAPIErrors(t *testing.T) {
	cases := map[string]struct {
		existing string
		status   int
		want     string
	}{
		"list":   {existing: prNoPR, status: http.StatusUnauthorized, want: "while fetching existing pull requests"},
		"create": {existing: prNoPR, status: 0, want: "while creating pull request"},
		"edit":   {existing: `[{"number":7}]`, status: 0, want: "while updating pull request"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			api := newPRAPI(t, tc.existing)
			api.status = tc.status
			if tc.status == 0 {
				// Fail only the write: the list answers, the create and the edit do not exist.
				api.Config.Handler = failWrites(api.Config.Handler)
			}
			state := workflow.State{Git: &branchGit{branch: prBase}, GitHub: api.forge(t, tokenStore(t))}

			_, err := runPR(state, false)

			if err == nil || !strings.Contains(err.Error(), "Trouble communicating with GitHub "+tc.want) {
				t.Fatalf("error = %v, want %q", err, tc.want)
			}
			if !strings.Contains(err.Error(), "with body: ") {
				t.Errorf("error = %v, want the response body", err)
			}
		})
	}
}

// failWrites answers every non-GET request with 422.
func failWrites(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			next.ServeHTTP(w, r)
			return
		}
		w.WriteHeader(http.StatusUnprocessableEntity)
		_, _ = w.Write([]byte(`{"message":"Validation Failed"}`))
	})
}

func TestCreatePullRequestDetachedHead(t *testing.T) {
	for _, dry := range []bool{false, true} {
		api := newPRAPI(t, prNoPR)
		g := &branchGit{err: diag.New("Not on the tip of a Git branch.").WithCode("remeris::git::not_a_branch")}
		state := workflow.State{Git: g, GitHub: api.forge(t, failingStore(t))}

		_, err := runPR(state, dry)

		if err == nil || err.Error() != "Not on the tip of a Git branch." {
			t.Errorf("dry=%v: error = %v", dry, err)
		}
		if n := len(api.seen()); n != 0 {
			t.Errorf("dry=%v: %d requests, want none", dry, n)
		}
	}
}

func TestCreatePullRequestNoForge(t *testing.T) {
	g := &branchGit{branch: prBase}
	_, err := runPR(workflow.State{Git: g}, false)
	if err == nil || err.Error() != prNoForg {
		t.Fatalf("error = %v", err)
	}
	d, ok := err.(diag.Diagnostic) //nolint:errorlint // the step returns the diagnostic itself
	if !ok {
		t.Fatalf("%T is not a diagnostic", err)
	}
	if d.Code() != "remeris::create_pull_request::forge::not_configured" ||
		d.Help() != "A forge must be configured in order to use the CreatePullRequest step" {
		t.Errorf("code = %q, help = %q", d.Code(), d.Help())
	}
	if g.calls != 0 {
		t.Error("the branch was read before the forge check")
	}
}

func TestCreatePullRequestGitHubThenGitea(t *testing.T) {
	api := newPRAPI(t, prNoPR)
	state := workflow.State{
		Git: &branchGit{branch: prBase}, GitHub: api.forge(t, tokenStore(t)), Gitea: giteaForge(),
	}

	_, err := runPR(state, false)

	if err == nil || err.Error() != "Step CreatePullRequest is not implemented yet (milestone M8)" {
		t.Fatalf("error = %v", err)
	}
	if got, want := api.keys(), []string{"GET " + prPulls, "POST " + prPulls}; !slices.Equal(got, want) {
		t.Errorf("requests = %q, want GitHub to run first: %q", got, want)
	}
}

func TestCreatePullRequestGiteaOnly(t *testing.T) {
	_, err := runPR(workflow.State{Gitea: giteaForge()}, true)
	if err == nil || !strings.Contains(err.Error(), "milestone M8") {
		t.Fatalf("error = %v", err)
	}
}

func TestCreatePullRequestDryRun(t *testing.T) {
	logs := captureLogs(t)
	api := newPRAPI(t, prNoPR)
	g := &branchGit{branch: prFeature}
	store := failingStore(t)
	state := workflow.State{Git: g, GitHub: api.forge(t, store)}

	if _, err := runPR(state, true); err != nil {
		t.Fatal(err)
	}

	want := []string{
		"Would create or update a pull request from feature/x to main:",
		"\tTitle: " + prTitle,
		"\tBody: " + prBody,
	}
	got := logs.String()
	for _, w := range want {
		if !strings.Contains(got, strings.ReplaceAll(strings.ReplaceAll(`msg="`+w+`"`, "\n", `\n`), "\t", `\t`)) {
			t.Errorf("log %q lacks %q", got, w)
		}
	}
	if n := len(api.seen()); n != 0 {
		t.Errorf("%d requests in a dry run", n)
	}
	if _, err := os.Stat(store.Dir); !os.IsNotExist(err) {
		t.Errorf("the credential store was touched: %v", err)
	}
}
