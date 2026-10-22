package steps_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/celest-io/remeris/internal/credentials"
	"github.com/celest-io/remeris/internal/forge/gitea"
	"github.com/celest-io/remeris/internal/prompt"
	"github.com/celest-io/remeris/internal/workflow"
	"github.com/celest-io/remeris/pkg/versioning/action"
)

const (
	codebergHost  = "https://codeberg.org"
	giteaReleases = "/api/v1/repos/o/r/releases"
	giteaCreate   = "POST " + giteaReleases
	giteaToken    = "gitea-fake-token" //nolint:gosec // G101: a sentinel, not a credential
	giteaPrefix   = "Trouble communicating with "
	pkgFooNotes   = "second"
)

// fakeGitea is a fake Gitea API: it records every request and answers the release endpoint.
type fakeGitea struct {
	*httptest.Server

	mu    sync.Mutex
	calls []apiCall
	// status is the status of the answer; 0 means 201.
	status int
}

func newFakeGitea(t *testing.T) *fakeGitea {
	t.Helper()
	f := &fakeGitea{}
	f.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		f.mu.Lock()
		f.calls = append(f.calls, apiCall{r.Method, r.URL.Path, r.URL.RawQuery, r.Header.Get("Authorization"), string(body)})
		status := f.status
		f.mu.Unlock()
		if status == 0 {
			status = http.StatusCreated
		}
		w.WriteHeader(status)
		if status == http.StatusCreated {
			_, _ = w.Write([]byte(`{"id":1}`))
			return
		}
		_, _ = w.Write([]byte("rejected"))
	}))
	t.Cleanup(f.Close)
	return f
}

func (f *fakeGitea) seen() []apiCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.calls)
}

func (f *fakeGitea) bodies() []string {
	var out []string
	for _, c := range f.seen() {
		out = append(out, c.Body)
	}
	return out
}

// forge is a Gitea forge for the fake API with the token GITEA_TOKEN.
func (f *fakeGitea) forge(t *testing.T) *gitea.Forge {
	t.Helper()
	store := &credentials.Store{Dir: t.TempDir(), Getenv: func(k string) string {
		if k == "GITEA_TOKEN" {
			return giteaToken
		}
		return ""
	}}
	return gitea.New(gitea.Config{Owner: "o", Repo: "r", Host: f.URL}, store)
}

func giteaState(t *testing.T, api *fakeGitea, names ...string) (workflow.State, *recordingGit) {
	t.Helper()
	g := &recordingGit{head: headSHA, forbidTag: t}
	state := releaseState(t, nil, false, names...)
	state.Git, state.Gitea = g, api.forge(t)
	return state, g
}

func TestReleaseGiteaCreatesRelease(t *testing.T) {
	foo := fooName
	cases := map[string]struct {
		action action.Action
		want   string
	}{
		"unnamed package": {
			action: release("1.2.0 (2026-01-01)", "\n## Features\n\n- New feature\n\n", nil),
			want: `{"tag_name":"v1.2.0","name":"1.2.0 (2026-01-01)","body":"## Features\n\n- New feature","prerelease":false,` +
				`"generate_release_notes":false,"draft":false,"target_commitish":"` + headSHA + `"}`,
		},
		"package with a name": {
			action: release("1.2.0 (2026-01-01)", "notes", &foo),
			want: `{"tag_name":"foo/v1.2.0","name":"foo 1.2.0 (2026-01-01)","body":"notes","prerelease":false,` +
				`"generate_release_notes":false,"draft":false,"target_commitish":"` + headSHA + `"}`,
		},
		"prerelease": {
			action: release("1.2.0-rc.1", "notes", nil),
			want: `{"tag_name":"v1.2.0-rc.1","name":"1.2.0-rc.1","body":"notes","prerelease":true,` +
				`"generate_release_notes":false,"draft":false,"target_commitish":"` + headSHA + `"}`,
		},
		"no notes asks Gitea to generate them": {
			action: release("1.2.0", " \n ", nil),
			want: `{"tag_name":"v1.2.0","name":"1.2.0","prerelease":false,"generate_release_notes":true,"draft":false,` +
				`"target_commitish":"` + headSHA + `"}`,
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			api := newFakeGitea(t)
			state, _ := giteaState(t, api, "", fooName)
			state.PendingActions = []action.Action{tc.action}

			rt, err := runRelease(state, false)
			if err != nil {
				t.Fatal(err)
			}

			calls := api.seen()
			if len(calls) != 1 || calls[0].key() != giteaCreate {
				t.Fatalf("requests = %+v, want the create request only", calls)
			}
			if calls[0].Body != tc.want {
				t.Errorf("body = %s\nwant   %s", calls[0].Body, tc.want)
			}
			if calls[0].Auth != "token "+giteaToken || calls[0].Query != "" {
				t.Errorf("Authorization = %q, query = %q: the token goes in the header only", calls[0].Auth, calls[0].Query)
			}
			if len(rt.State.PendingActions) != 0 {
				t.Errorf("pending = %v", rt.State.PendingActions)
			}
			// recordingGit fails the test on CreateTag: the forge makes the tag.
		})
	}
}

func TestReleaseGiteaUnknownHeadOmitsTarget(t *testing.T) {
	api := newFakeGitea(t)
	state, g := giteaState(t, api, "")
	g.headErr = io.ErrUnexpectedEOF
	state.PendingActions = []action.Action{release("1.2.0", "x", nil)}

	if _, err := runRelease(state, false); err != nil {
		t.Fatal(err)
	}

	if bodies := api.bodies(); len(bodies) != 1 || strings.Contains(bodies[0], "target_commitish") {
		t.Errorf("bodies = %q, want no target_commitish", bodies)
	}
}

func TestReleaseGiteaStillTagsGoModules(t *testing.T) {
	api := newFakeGitea(t)
	state := releaseState(t, []string{tagV100, subTagV100}, true, "")
	g := &recordingGit{head: headSHA}
	state.Git, state.Gitea = g, api.forge(t)
	state.PendingActions = []action.Action{action.AddTag(subTagV120), release(v120, "x", nil)}

	if _, err := runRelease(state, false); err != nil {
		t.Fatal(err)
	}

	if !slices.Equal(g.tagged, []string{subTagV120}) {
		t.Errorf("local tags = %q, want only the Go module tag", g.tagged)
	}
	if len(api.seen()) != 1 {
		t.Errorf("requests = %+v", api.seen())
	}
}

func TestReleaseGiteaAPIErrorStopsTheRest(t *testing.T) {
	api := newFakeGitea(t)
	api.status = http.StatusUnprocessableEntity
	state, _ := giteaState(t, api, "", fooName)
	foo := fooName
	state.PendingActions = []action.Action{release("1.2.0", "x", nil), release("1.2.0", pkgFooNotes, &foo)}

	_, err := runRelease(state, false)

	want := giteaPrefix + api.URL + " while creating a release: HTTP status client error (422 Unprocessable Entity) " +
		"for url (" + api.URL + giteaReleases + ") with body: rejected"
	if err == nil || err.Error() != want {
		t.Errorf("err = %v\nwant   %q", err, want)
	}
	if n := len(api.seen()); n != 1 {
		t.Errorf("%d requests, want a stop at the first failure", n)
	}
}

func TestReleaseGiteaPromptsOnce(t *testing.T) {
	api := newFakeGitea(t)
	scripted := prompt.NewScripted(prompt.Answer{Text: giteaToken + "\n"})
	store := &credentials.Store{
		Dir: t.TempDir(), Getenv: func(string) string { return "" }, Prompter: scripted,
	}
	state, _ := giteaState(t, api, "", fooName)
	state.Gitea = gitea.New(gitea.Config{Owner: "o", Repo: "r", Host: api.URL}, store)
	foo := fooName
	state.PendingActions = []action.Action{release("1.2.0", "x", nil), release("1.2.0", pkgFooNotes, &foo)}

	if _, err := runRelease(state, false); err != nil {
		t.Fatal(err)
	}

	if asked := scripted.Asked(); len(asked) != 1 || asked[0].Kind != prompt.KindPassword {
		t.Errorf("asked = %+v, want the token once", asked)
	}
	calls := api.seen()
	if len(calls) != 2 {
		t.Fatalf("requests = %+v", calls)
	}
	for _, c := range calls {
		if c.Auth != "token "+giteaToken {
			t.Errorf("Authorization = %q", c.Auth)
		}
	}
}

// untouchedStore is a credential store that fails the test when anything reads the environment or asks the user,
// and is returned with the prompter that records the questions.
func untouchedStore(t *testing.T) (*credentials.Store, *prompt.Scripted) {
	t.Helper()
	scripted := prompt.NewScripted()
	return &credentials.Store{
		Dir: filepath.Join(t.TempDir(), "never"),
		Getenv: func(k string) string {
			t.Errorf("the environment variable %s was read in a dry run", k)
			return ""
		},
		Prompter: scripted,
	}, scripted
}

func TestReleaseGiteaDryRun(t *testing.T) {
	foo := fooName
	cases := map[string]struct {
		host    string
		head    string
		headErr error
		release action.Action
		want    string
	}{
		"release with notes": {
			host:    codebergHost,
			head:    headSHA,
			release: release("1.2.0 (2026-01-01)", "\n## Features\n\n- x\n", nil),
			want: "Would create a release on Gitea [https://codeberg.org] with name 1.2.0 (2026-01-01) and tag v1.2.0 at commit " +
				headSHA + " and body:\n## Features\n\n- x",
		},
		"named prerelease, notes generated, unknown commit": {
			host:    codebergHost,
			headErr: io.ErrUnexpectedEOF,
			release: release("1.2.0-rc.1", "", &foo),
			want: "Would create a prerelease on Gitea [https://codeberg.org] with name foo 1.2.0-rc.1 and tag foo/v1.2.0-rc.1 " +
				"and autogenerated body",
		},
		"host with a password": { //nolint:gosec // G101: a sentinel, not a credential
			host: "https://alice:s3cret@gitea.example.com", head: headSHA, release: release("1.2.0", "x", nil),
			want: "Would create a release on Gitea [https://gitea.example.com] with name 1.2.0 and tag v1.2.0 at commit " +
				headSHA + " and body:\nx",
		},
		"host that is not a URL is printed as written": {
			host: "gitea.example.com", head: headSHA, release: release("1.2.0", "x", nil),
			want: "Would create a release on Gitea [gitea.example.com] with name 1.2.0 and tag v1.2.0 at commit " +
				headSHA + " and body:\nx",
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			logs := captureLogs(t)
			api := newFakeGitea(t)
			g := &recordingGit{head: tc.head, headErr: tc.headErr, forbidTag: t}
			state := releaseState(t, nil, false, "", fooName)
			store, scripted := untouchedStore(t)
			state.Git = g
			state.Gitea = gitea.New(gitea.Config{Owner: "o", Repo: "r", Host: tc.host}, store)
			state.PendingActions = []action.Action{tc.release}

			if _, err := runRelease(state, true); err != nil {
				t.Fatal(err)
			}

			if !strings.Contains(logs.String(), "msg="+strconv.Quote(tc.want)) {
				t.Errorf("log lacks %q:\n%s", tc.want, logs)
			}
			if strings.Contains(logs.String(), "s3cret") {
				t.Errorf("the password is in the output:\n%s", logs)
			}
			if calls := api.seen(); len(calls) != 0 {
				t.Errorf("a dry run made requests: %v", calls)
			}
			if asked := scripted.Asked(); len(asked) != 0 {
				t.Errorf("a dry run asked the user: %+v", asked)
			}
			if _, err := os.Stat(store.Dir); err == nil {
				t.Error("a dry run touched the credential directory")
			}
		})
	}
}

func TestReleaseBothForgesInterleave(t *testing.T) {
	logs := captureLogs(t)
	foo := fooName
	state := releaseState(t, nil, false, "", fooName)
	store, _ := untouchedStore(t)
	state.Git = &recordingGit{head: headSHA, forbidTag: t}
	state.GitHub = newTestForge()
	state.Gitea = gitea.New(gitea.Config{Owner: "o", Repo: "r", Host: codebergHost}, store)
	state.PendingActions = []action.Action{release("1.2.0", "a", nil), release("1.2.0", "b", &foo)}

	if _, err := runRelease(state, true); err != nil {
		t.Fatal(err)
	}

	var got []string
	for _, l := range logLines(logs) {
		got = append(got, strings.SplitN(strings.TrimPrefix(l, "Would create a release on "), " with name", 2)[0])
	}
	want := []string{"GitHub", "Gitea [https://codeberg.org]", "GitHub", "Gitea [https://codeberg.org]"}
	if !slices.Equal(got, want) {
		t.Errorf("order = %q, want %q\n%s", got, want, logs)
	}
	if !strings.Contains(logs.String(), "name 1.2.0 and tag v1.2.0") ||
		!strings.Contains(logs.String(), "name foo 1.2.0 and tag foo/v1.2.0") {
		t.Errorf("log = %s", logs)
	}
}

func TestReleaseBothForgesCreate(t *testing.T) {
	gh := newFakeAPI(t)
	gt := newFakeGitea(t)
	state, _ := githubState(t, gh, "")
	state.Gitea = gt.forge(t)
	state.PendingActions = []action.Action{release("1.2.0", "x", nil)}

	if _, err := runRelease(state, false); err != nil {
		t.Fatal(err)
	}

	if len(gh.seen()) != 1 || len(gt.seen()) != 1 {
		t.Errorf("GitHub requests = %v, Gitea requests = %v, want one each", gh.keys(), gt.seen())
	}
}

func TestReleaseGitHubFailureMeansNoGiteaRequest(t *testing.T) {
	gh := newFakeAPI(t)
	gh.failCreate = http.StatusUnprocessableEntity
	gt := newFakeGitea(t)
	foo := fooName
	state, _ := githubState(t, gh, "", fooName)
	state.Gitea = gt.forge(t)
	state.PendingActions = []action.Action{release("1.2.0", "x", nil), release("1.2.0", pkgFooNotes, &foo)}

	_, err := runRelease(state, false)

	if err == nil || !strings.HasPrefix(err.Error(), giteaPrefix+"GitHub while creating a release: ") {
		t.Errorf("err = %v, want the error of GitHub", err)
	}
	if calls := gt.seen(); len(calls) != 0 {
		t.Errorf("Gitea got %+v after GitHub failed", calls)
	}
	if keys := gh.keys(); len(keys) != 1 {
		t.Errorf("GitHub requests = %q, want a stop at the first failure", keys)
	}
}

func TestReleaseGiteaFailureAfterGitHubSuccess(t *testing.T) {
	gh := newFakeAPI(t)
	gt := newFakeGitea(t)
	gt.status = http.StatusForbidden
	foo := fooName
	state, _ := githubState(t, gh, "", fooName)
	state.Gitea = gt.forge(t)
	state.PendingActions = []action.Action{release("1.2.0", "x", nil), release("1.2.0", pkgFooNotes, &foo)}

	_, err := runRelease(state, false)

	if err == nil || !strings.HasPrefix(err.Error(), giteaPrefix+gt.URL+" while creating a release: ") {
		t.Errorf("err = %v, want the error of Gitea", err)
	}
	if len(gh.keys()) != 1 || len(gt.seen()) != 1 {
		t.Errorf("GitHub requests = %q, Gitea requests = %+v: nothing runs after the failure", gh.keys(), gt.seen())
	}
}
