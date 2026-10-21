package steps_test

import (
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
	"github.com/celest-io/remeris/internal/forge/gitea"
	"github.com/celest-io/remeris/internal/prompt"
	"github.com/celest-io/remeris/internal/workflow"
)

const (
	gtPulls = "/api/v1/repos/o/r/pulls"
	gtEdit  = gtPulls + "/7"
	gtList  = "base_branch=main&limit=50&page=1&state=open"
)

// prGitea is a fake Gitea API for the pull request endpoints.
type prGitea struct {
	*httptest.Server

	mu    sync.Mutex
	calls []apiCall
	// existing is the body of the list response.
	existing string
	// status makes every request fail with this status when not zero.
	status int
}

func newPRGitea(t *testing.T, existing string) *prGitea {
	t.Helper()
	f := &prGitea{existing: existing}
	f.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		f.mu.Lock()
		f.calls = append(f.calls, apiCall{r.Method, r.URL.Path, r.URL.RawQuery, r.Header.Get("Authorization"), string(body)})
		status := f.status
		f.mu.Unlock()
		if status != 0 {
			w.WriteHeader(status)
			_, _ = w.Write([]byte("rejected"))
			return
		}
		switch r.Method {
		case http.MethodGet:
			_, _ = w.Write([]byte(f.existing))
		case http.MethodPost:
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"number":8,"url":"https://g/api/v1/repos/o/r/pulls/8"}`))
		default:
			_, _ = w.Write([]byte(`{}`))
		}
	}))
	t.Cleanup(f.Close)
	return f
}

func (f *prGitea) seen() []apiCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.calls)
}

func (f *prGitea) keys() []string {
	var out []string
	for _, c := range f.seen() {
		out = append(out, c.key())
	}
	return out
}

func (f *prGitea) forge(store *credentials.Store) *gitea.Forge {
	return gitea.New(gitea.Config{Owner: "o", Repo: "r", Host: f.URL}, store)
}

func giteaTokenStore(t *testing.T) *credentials.Store {
	t.Helper()
	return &credentials.Store{Dir: t.TempDir(), Getenv: func(k string) string {
		if k == "GITEA_TOKEN" {
			return giteaToken
		}
		return ""
	}}
}

// giteaPR is one entry of the list response.
func giteaPR(number, head, owner string) string {
	return `{"number":` + number + `,"url":"https://g/api/v1/repos/o/r/pulls/` + number + `","head":{"ref":"` + head +
		`","repo":{"owner":{"login":"` + owner + `"}}},"base":{"ref":"main"}}`
}

func TestCreatePullRequestGitea(t *testing.T) {
	cases := map[string]struct {
		branch   string
		existing string
		wantKeys []string
		wantBody string
	}{
		"creates": {
			branch: prBase, existing: prNoPR,
			wantKeys: []string{"GET " + gtPulls, "POST " + gtPulls},
			wantBody: `{"title":"chore: Release","body":"line one\n\nline two","head":"main","base":"main"}`,
		},
		"creates from a branch with a slash": {
			branch: prFeature, existing: prNoPR,
			wantKeys: []string{"GET " + gtPulls, "POST " + gtPulls},
			wantBody: `{"title":"chore: Release","body":"line one\n\nline two","head":"feature/x","base":"main"}`,
		},
		"updates the matching one, not the first": {
			branch: prFeature, existing: "[" + giteaPR("3", "other", "o") + "," + giteaPR("7", prFeature, "o") + "]",
			wantKeys: []string{"GET " + gtPulls, "PATCH " + gtEdit},
			wantBody: `{"body":"line one\n\nline two","title":"chore: Release"}`,
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			logs := captureDebugLogs(t)
			api := newPRGitea(t, tc.existing)
			state := workflow.State{Git: &branchGit{branch: tc.branch}, Gitea: api.forge(giteaTokenStore(t))}

			if _, err := runPR(state, false); err != nil {
				t.Fatal(err)
			}

			if got := api.keys(); !slices.Equal(got, tc.wantKeys) {
				t.Fatalf("requests = %q, want %q", got, tc.wantKeys)
			}
			calls := api.seen()
			if calls[0].Query != gtList {
				t.Errorf("query = %q, want %q", calls[0].Query, gtList)
			}
			for _, c := range calls {
				if c.Auth != "token "+giteaToken {
					t.Errorf("%s: Authorization = %q", c.key(), c.Auth)
				}
			}
			if got := calls[1].Body; got != tc.wantBody {
				t.Errorf("body = %q, want %q", got, tc.wantBody)
			}
			want := "No matching existing pull request found, creating a new one."
			if strings.HasPrefix(tc.wantKeys[1], "PATCH") {
				want = "Updating existing pull request: https://g/api/v1/repos/o/r/pulls/7"
			}
			if lines := logLines(logs); !slices.Contains(lines, want) {
				t.Errorf("log = %q, want %q", lines, want)
			}
		})
	}
}

func TestCreatePullRequestGiteaErrors(t *testing.T) {
	cases := map[string]struct {
		existing string
		status   int
		want     string
	}{
		"list request": {existing: prNoPR, status: http.StatusForbidden, want: "while fetching existing pull requests"},
		"create":       {existing: prNoPR, want: "while creating pull request"},
		"update":       {existing: "[" + giteaPR("7", prBase, "o") + "]", want: "while updating pull request"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			api := newPRGitea(t, tc.existing)
			api.status = tc.status
			if tc.status == 0 {
				api.Config.Handler = failWrites(api.Config.Handler)
			}
			state := workflow.State{Git: &branchGit{branch: prBase}, Gitea: api.forge(giteaTokenStore(t))}

			_, err := runPR(state, false)

			if err == nil || !strings.Contains(err.Error(), giteaPrefix+api.URL+" "+tc.want) {
				t.Fatalf("error = %v, want %q", err, tc.want)
			}
			if strings.Contains(err.Error(), giteaToken) {
				t.Errorf("the token leaked: %v", err)
			}
		})
	}
}

func TestCreatePullRequestGiteaDryRun(t *testing.T) {
	one := []string{
		"Would create or update a pull request from feature/x to main:",
		"\tTitle: " + prTitle,
		"\tBody: " + prBody,
	}
	cases := map[string]struct {
		github, gitea bool
		want          []string
	}{
		"gitea only":                   {gitea: true, want: one},
		"github only":                  {github: true, want: one},
		"both, printed once per forge": {github: true, gitea: true, want: slices.Concat(one, one)},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			logs := captureLogs(t)
			ghAPI, gtAPI := newPRAPI(t, prNoPR), newPRGitea(t, prNoPR)
			scripted := prompt.NewScripted()
			store := failingStore(t)
			store.Prompter = scripted
			g := &branchGit{branch: prFeature}
			state := workflow.State{Git: g}
			if tc.github {
				state.GitHub = ghAPI.forge(t, store)
			}
			if tc.gitea {
				state.Gitea = gtAPI.forge(store)
			}

			if _, err := runPR(state, true); err != nil {
				t.Fatal(err)
			}

			var want strings.Builder
			for _, l := range tc.want {
				want.WriteString(`msg="` + strings.ReplaceAll(strings.ReplaceAll(l, "\n", `\n`), "\t", `\t`) + "\"\n")
			}
			if logs.String() != want.String() {
				t.Errorf("log = %q, want %q", logs.String(), want.String())
			}
			if g.calls != 1 {
				t.Errorf("the branch was read %d times, want once", g.calls)
			}
			if n := len(ghAPI.seen()) + len(gtAPI.seen()); n != 0 {
				t.Errorf("%d requests in a dry run", n)
			}
			if _, err := os.Stat(store.Dir); !os.IsNotExist(err) {
				t.Errorf("the credential store was touched: %v", err)
			}
			if asked := scripted.Asked(); len(asked) != 0 {
				t.Errorf("prompted in a dry run: %v", asked)
			}
		})
	}
}

func TestCreatePullRequestGiteaDetachedHead(t *testing.T) {
	for _, dry := range []bool{false, true} {
		api := newPRGitea(t, prNoPR)
		g := &branchGit{err: diag.New("Not on the tip of a Git branch.").WithCode("remeris::git::not_a_branch")}
		state := workflow.State{Git: g, Gitea: api.forge(failingStore(t))}

		_, err := runPR(state, dry)

		if err == nil || err.Error() != "Not on the tip of a Git branch." {
			t.Errorf("dry=%v: error = %v", dry, err)
		}
		if n := len(api.seen()); n != 0 {
			t.Errorf("dry=%v: %d requests, want none", dry, n)
		}
	}
}

func TestCreatePullRequestBothForges(t *testing.T) {
	t.Run("both succeed, GitHub first", func(t *testing.T) {
		gh, gt := newPRAPI(t, prNoPR), newPRGitea(t, prNoPR)
		state := workflow.State{
			Git: &branchGit{branch: prFeature}, GitHub: gh.forge(t, tokenStore(t)), Gitea: gt.forge(giteaTokenStore(t)),
		}

		if _, err := runPR(state, false); err != nil {
			t.Fatal(err)
		}

		if got, want := gh.keys(), []string{"GET " + prPulls, "POST " + prPulls}; !slices.Equal(got, want) {
			t.Errorf("GitHub requests = %q, want %q", got, want)
		}
		if got, want := gt.keys(), []string{"GET " + gtPulls, "POST " + gtPulls}; !slices.Equal(got, want) {
			t.Errorf("Gitea requests = %q, want %q", got, want)
		}
	})
	t.Run("a GitHub failure stops before Gitea", func(t *testing.T) {
		gh, gt := newPRAPI(t, prNoPR), newPRGitea(t, prNoPR)
		gh.status = http.StatusUnauthorized
		state := workflow.State{
			Git: &branchGit{branch: prBase}, GitHub: gh.forge(t, tokenStore(t)), Gitea: gt.forge(giteaTokenStore(t)),
		}

		_, err := runPR(state, false)

		if err == nil || !strings.Contains(err.Error(), "Trouble communicating with GitHub") {
			t.Fatalf("error = %v", err)
		}
		if n := len(gt.seen()); n != 0 {
			t.Errorf("%d Gitea requests after a GitHub failure", n)
		}
	})
	t.Run("a Gitea failure is returned after GitHub succeeded", func(t *testing.T) {
		gh, gt := newPRAPI(t, prNoPR), newPRGitea(t, prNoPR)
		gt.status = http.StatusUnauthorized
		state := workflow.State{
			Git: &branchGit{branch: prBase}, GitHub: gh.forge(t, tokenStore(t)), Gitea: gt.forge(giteaTokenStore(t)),
		}

		_, err := runPR(state, false)

		if err == nil || !strings.Contains(err.Error(), giteaPrefix+gt.URL+" while fetching existing pull requests") {
			t.Fatalf("error = %v", err)
		}
		if n := len(gh.seen()); n != 2 {
			t.Errorf("%d GitHub requests, want 2", n)
		}
	})
}

func TestCreatePullRequestGiteaAsksForTheTokenOnce(t *testing.T) {
	api := newPRGitea(t, prNoPR)
	scripted := prompt.NewScripted(prompt.Answer{Text: giteaToken + "\n"})
	store := &credentials.Store{Dir: t.TempDir(), Getenv: func(string) string { return "" }, Prompter: scripted}
	state := workflow.State{Git: &branchGit{branch: prBase}, Gitea: api.forge(store)}

	if _, err := runPR(state, false); err != nil {
		t.Fatal(err)
	}

	if asked := scripted.Asked(); len(asked) != 1 || asked[0].Kind != prompt.KindPassword {
		t.Errorf("asked = %v, want one password prompt", asked)
	}
	for _, c := range api.seen() {
		if c.Auth != "token "+giteaToken {
			t.Errorf("%s: Authorization = %q", c.key(), c.Auth)
		}
	}
}
