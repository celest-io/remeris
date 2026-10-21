package steps_test

import (
	"encoding/json"
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
	"github.com/celest-io/remeris/internal/forge/github"
	"github.com/celest-io/remeris/internal/workflow"
	"github.com/celest-io/remeris/pkg/versioning"
	"github.com/celest-io/remeris/pkg/versioning/action"
	"github.com/celest-io/remeris/pkg/versioning/releasenotes"
	"github.com/celest-io/remeris/pkg/versioning/semver"
)

const (
	apiPrefix     = "/api/v3/repos/o/r/releases"
	uploadsPrefix = "/api/uploads/repos/o/r/releases/42/assets"
	headSHA       = "0123456789abcdef0123456789abcdef01234567"
	fakeToken     = "fake-token"
	createCall    = "POST " + apiPrefix
	publishCall   = "PATCH " + apiPrefix + "/42"
	firstThing    = "assets/first_thing"
	secondThing   = "assets/second_thing"
	nilGlobFiles  = "assets/*"
	globAssets    = "assets/*"
	keyBody       = "body"
	keyName       = "name"
	keyTag        = "tag_name"
	keyTarget     = "target_commitish"
	keyDraft      = "draft"
	keyPre        = "prerelease"
	keyGenerate   = "generate_release_notes"
	contentFirst  = "alpha"
	contentSecond = "beta"
)

type apiCall struct {
	Method, Path, Query, Auth, Body string
}

func (c apiCall) key() string { return c.Method + " " + c.Path }

// fakeAPI is a fake GitHub API: it records every request and answers the release endpoints.
type fakeAPI struct {
	*httptest.Server

	mu    sync.Mutex
	calls []apiCall
	// failCreate makes the create request fail with this status.
	failCreate int
}

func newFakeAPI(t *testing.T) *fakeAPI {
	t.Helper()
	f := &fakeAPI{}
	f.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		f.mu.Lock()
		f.calls = append(f.calls, apiCall{r.Method, r.URL.Path, r.URL.RawQuery, r.Header.Get("Authorization"), string(body)})
		failCreate := f.failCreate
		f.mu.Unlock()
		switch r.Method + " " + r.URL.Path {
		case createCall:
			if failCreate != 0 {
				w.WriteHeader(failCreate)
				_, _ = w.Write([]byte(`{"message":"Validation Failed"}`))
				return
			}
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"id":42}`))
		case "POST " + uploadsPrefix:
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"id":1}`))
		case publishCall:
			_, _ = w.Write([]byte(`{"id":42}`))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(f.Close)
	return f
}

func (f *fakeAPI) seen() []apiCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.calls)
}

func (f *fakeAPI) keys() []string {
	var out []string
	for _, c := range f.seen() {
		out = append(out, c.key())
	}
	return out
}

// created decodes the body of the one create request.
func (f *fakeAPI) created(t *testing.T) map[string]any {
	t.Helper()
	var out map[string]any
	for _, c := range f.seen() {
		if c.key() == createCall {
			if out != nil {
				t.Fatal("more than one release was created")
			}
			if err := json.Unmarshal([]byte(c.Body), &out); err != nil {
				t.Fatal(err)
			}
		}
	}
	if out == nil {
		t.Fatal("no release was created")
	}
	return out
}

// forge is a GitHub forge that talks to the fake API with a token from a fake environment.
func (f *fakeAPI) forge(t *testing.T) *github.Forge {
	t.Helper()
	store := &credentials.Store{Dir: t.TempDir(), Getenv: func(k string) string {
		if k == credentials.GitHubToken.Env() {
			return fakeToken
		}
		return ""
	}}
	return github.New(github.Config{Owner: "o", Repo: "r", TrustedAPIURL: f.URL + "/api/v3/"}, nil, store)
}

func release(title, notes string, name *string) action.Action {
	return action.CreateRelease(action.Release{Title: title, Version: semver.MustParse(strings.Fields(title)[0]),
		Notes: notes, PackageName: name})
}

// githubState is a state with the packages of releaseState (versions 1.2.0), Git at HEAD headSHA and the fake API.
func githubState(t *testing.T, api *fakeAPI, names ...string) (workflow.State, *recordingGit) {
	t.Helper()
	g := &recordingGit{head: headSHA, forbidTag: t}
	state := releaseState(t, nil, false, names...)
	state.Git, state.GitHub = g, api.forge(t)
	return state, g
}

func TestReleaseGitHubCreatesRelease(t *testing.T) {
	foo := fooName
	cases := map[string]struct {
		action action.Action
		want   map[string]any
	}{
		"default package": {
			action: release("1.2.0 (2026-01-01)", "\n## Features\n\n- New feature\n\n", nil),
			want: map[string]any{keyName: "1.2.0 (2026-01-01)", keyTag: tagV120, keyBody: "## Features\n\n- New feature",
				keyPre: false, keyGenerate: false},
		},
		"named package": {
			action: release("1.2.0 (2026-01-01)", "## Features\n\n- x", &foo),
			want: map[string]any{keyName: "foo 1.2.0 (2026-01-01)", keyTag: fooTagV120, keyBody: "## Features\n\n- x",
				keyPre: false, keyGenerate: false},
		},
		keyPre: {
			action: release("1.2.0-rc.1 (2026-01-01)", "notes", nil),
			want: map[string]any{keyName: "1.2.0-rc.1 (2026-01-01)", keyTag: "v1.2.0-rc.1", keyBody: "notes",
				keyPre: true, keyGenerate: false},
		},
		"no notes asks GitHub to generate them": {
			action: release("1.2.0", " \n ", nil),
			want:   map[string]any{keyName: "1.2.0", keyTag: tagV120, keyPre: false, keyGenerate: true},
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			api := newFakeAPI(t)
			state, _ := githubState(t, api, "", fooName)
			state.PendingActions = []action.Action{tc.action}

			rt, err := runRelease(state, false)
			if err != nil {
				t.Fatal(err)
			}

			got := api.created(t)
			tc.want[keyTarget], tc.want[keyDraft] = headSHA, false
			if len(got) != len(tc.want) {
				t.Errorf("body = %v, want %v", got, tc.want)
			}
			for k, v := range tc.want {
				if got[k] != v {
					t.Errorf("%s = %v, want %v", k, got[k], v)
				}
			}
			if keys := api.keys(); !slices.Equal(keys, []string{createCall}) {
				t.Errorf("requests = %q, want the create request only", keys)
			}
			if auth := api.seen()[0].Auth; auth != "Bearer "+fakeToken {
				t.Errorf("Authorization = %q", auth)
			}
			if len(rt.State.PendingActions) != 0 {
				t.Errorf("pending = %v", rt.State.PendingActions)
			}
			// recordingGit fails the test on CreateTag: the forge makes the tag.
		})
	}
}

func TestReleaseGitHubUnknownHeadOmitsTarget(t *testing.T) {
	api := newFakeAPI(t)
	state, g := githubState(t, api, "")
	g.headErr = io.ErrUnexpectedEOF
	state.PendingActions = []action.Action{release("1.2.0", "x", nil)}

	if _, err := runRelease(state, false); err != nil {
		t.Fatal(err)
	}

	if _, ok := api.created(t)[keyTarget]; ok {
		t.Errorf("target_commitish was sent: %s", api.seen()[0].Body)
	}
}

func writeAssets(t *testing.T) {
	t.Helper()
	t.Chdir(t.TempDir())
	for name, content := range map[string]string{firstThing: contentFirst, secondThing: contentSecond} {
		if err := os.MkdirAll(filepath.Dir(name), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(name, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
}

func TestReleaseGitHubUploadsAssets(t *testing.T) {
	cases := map[string]struct {
		assets    *github.Assets
		wantNames []string
		wantBody  []string
	}{
		"glob": {
			assets:    github.GlobAssets(globAssets),
			wantNames: []string{"first_thing", "second_thing"}, wantBody: []string{contentFirst, contentSecond},
		},
		"list": {
			assets:    github.ListAssets([]github.AssetEntry{{Path: firstThing, Name: "alt"}, {Path: secondThing}}),
			wantNames: []string{"alt", "second_thing"}, wantBody: []string{contentFirst, contentSecond},
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			writeAssets(t)
			api := newFakeAPI(t)
			state, _ := githubState(t, api, "")
			state.ReleaseAssets = []workflow.PackageAssets{{Name: versioning.DefaultName(), Assets: tc.assets}}
			state.PendingActions = []action.Action{release("1.2.0", "x", nil)}

			if _, err := runRelease(state, false); err != nil {
				t.Fatal(err)
			}

			want := []string{createCall, "POST " + uploadsPrefix, "POST " + uploadsPrefix, publishCall}
			if keys := api.keys(); !slices.Equal(keys, want) {
				t.Fatalf("requests = %q, want %q", keys, want)
			}
			calls := api.seen()
			if got := api.created(t)[keyDraft]; got != true {
				t.Errorf("draft = %v, want true while the assets are uploaded", got)
			}
			for i := range tc.wantNames {
				c := calls[1+i]
				if want := "name=" + tc.wantNames[i]; c.Query != want {
					t.Errorf("upload %d query = %q, want %q", i, c.Query, want)
				}
				if c.Body != tc.wantBody[i] {
					t.Errorf("upload %d body = %q, want %q", i, c.Body, tc.wantBody[i])
				}
			}
			if body := calls[3].Body; !strings.Contains(body, `"draft":false`) {
				t.Errorf("publish body = %s", body)
			}
		})
	}
}

func TestReleaseGitHubAssetsOfAnotherPackage(t *testing.T) {
	writeAssets(t)
	api := newFakeAPI(t)
	state, _ := githubState(t, api, "", fooName)
	state.ReleaseAssets = []workflow.PackageAssets{
		{Name: versioning.CustomName(fooName), Assets: github.GlobAssets(globAssets)},
	}
	state.PendingActions = []action.Action{release("1.2.0", "x", nil)}

	if _, err := runRelease(state, false); err != nil {
		t.Fatal(err)
	}

	if keys := api.keys(); !slices.Equal(keys, []string{createCall}) {
		t.Errorf("requests = %q: the default package has no assets", keys)
	}
}

func TestReleaseGitHubListEntryIsNotAGlob(t *testing.T) {
	writeAssets(t)
	api := newFakeAPI(t)
	state, _ := githubState(t, api, "")
	state.ReleaseAssets = []workflow.PackageAssets{{
		Name: versioning.DefaultName(), Assets: github.ListAssets([]github.AssetEntry{{Path: nilGlobFiles}}),
	}}
	state.PendingActions = []action.Action{release("1.2.0", "x", nil)}

	_, err := runRelease(state, false)

	if err == nil || !strings.HasPrefix(err.Error(), "Could not read asset file assets/*: ") {
		t.Errorf("err = %v", err)
	}
	if keys := api.keys(); !slices.Equal(keys, []string{createCall}) {
		t.Errorf("requests = %q", keys)
	}
}

func TestReleaseGitHubPreparedWithoutPrepareRelease(t *testing.T) {
	api := newFakeAPI(t)
	state, _ := githubState(t, api, "")
	state.AllGitTags = []string{tagV100}
	changelog := releasenotes.NewChangelog("CHANGELOG.md",
		"## 1.2.0 (2026-01-01)\n\n### Features\n\n- New\n\n## 1.0.0\n\nOld\n")
	state.Packages[0].ReleaseNotes.Changelog = &changelog

	if _, err := runRelease(state, false); err != nil {
		t.Fatal(err)
	}

	got := api.created(t)
	if got[keyName] != "1.2.0 (2026-01-01)" || got[keyTag] != tagV120 || got[keyBody] != "## Features\n\n- New" {
		t.Errorf("body = %v", got)
	}
}

func TestReleaseGitHubNoChangelogGeneratesNotes(t *testing.T) {
	api := newFakeAPI(t)
	state, _ := githubState(t, api, "")
	state.AllGitTags = []string{tagV100}

	if _, err := runRelease(state, false); err != nil {
		t.Fatal(err)
	}

	got := api.created(t)
	if got[keyGenerate] != true || got[keyName] != v120 || got[keyTag] != tagV120 {
		t.Errorf("body = %v", got)
	}
	if _, ok := got[keyBody]; ok {
		t.Errorf("body sent: %v", got)
	}
}

func TestReleaseGitHubStillTagsGoModules(t *testing.T) {
	cases := map[string]struct {
		pending []action.Action
		prep    bool
	}{
		"prepared in this workflow": {pending: []action.Action{action.AddTag(subTagV120), release(v120, "x", nil)}},
		"found by Release":          {prep: true},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			api := newFakeAPI(t)
			state := releaseState(t, []string{tagV100, subTagV100}, true, "")
			g := &recordingGit{head: headSHA}
			state.Git, state.GitHub, state.PendingActions = g, api.forge(t), tc.pending

			if _, err := runRelease(state, false); err != nil {
				t.Fatal(err)
			}

			if !slices.Equal(g.tagged, []string{subTagV120}) {
				t.Errorf("local tags = %q, want only the Go module tag", g.tagged)
			}
			if got := api.created(t)[keyTag]; got != tagV120 {
				t.Errorf("tag_name = %v", got)
			}
		})
	}
}

func TestReleaseGitHubAPIError(t *testing.T) {
	api := newFakeAPI(t)
	api.failCreate = http.StatusUnprocessableEntity
	state, _ := githubState(t, api, "")
	state.PendingActions = []action.Action{release("1.2.0", "x", nil)}

	_, err := runRelease(state, false)

	if err == nil || !strings.HasPrefix(err.Error(), "Trouble communicating with GitHub while creating a release: ") {
		t.Errorf("err = %v", err)
	}
	if err != nil && !strings.Contains(err.Error(), "Validation Failed") {
		t.Errorf("err = %v, want the message of GitHub", err)
	}
}

func TestReleaseGitHubThenGiteaStub(t *testing.T) {
	api := newFakeAPI(t)
	state, _ := githubState(t, api, "")
	state.Gitea = giteaForge()
	state.PendingActions = []action.Action{release("1.2.0", "x", nil)}

	_, err := runRelease(state, false)

	if want := "Step Release is not implemented yet (milestone M8)"; err == nil || err.Error() != want {
		t.Errorf("err = %v, want %q", err, want)
	}
	if keys := api.keys(); !slices.Equal(keys, []string{createCall}) {
		t.Errorf("requests = %q: GitHub runs before Gitea", keys)
	}
}

// failingStore is a credential store that fails the test when anything reads it or asks the user.
func failingStore(t *testing.T) *credentials.Store {
	t.Helper()
	return &credentials.Store{
		Dir: filepath.Join(t.TempDir(), "never"),
		Getenv: func(k string) string {
			t.Errorf("the environment variable %s was read in a dry run", k)
			return ""
		},
	}
}

func TestReleaseGitHubDryRun(t *testing.T) {
	foo := fooName
	cases := map[string]struct {
		head    string
		headErr error
		assets  *github.Assets
		release action.Action
		want    []string
	}{
		"release with notes": {
			head: headSHA, release: release("1.2.0 (2026-01-01)", "\n## Features\n\n- x\n", nil),
			want: []string{"Would create a release on GitHub with name 1.2.0 (2026-01-01) and tag v1.2.0 at commit " +
				headSHA + " and body:\n## Features\n\n- x"},
		},
		"named prerelease, notes generated, unknown commit": {
			headErr: io.ErrUnexpectedEOF, release: release("1.2.0-rc.1", "", &foo),
			want: []string{"Would create a prerelease on GitHub with name foo 1.2.0-rc.1 and tag foo/v1.2.0-rc.1 " +
				"and autogenerated body"},
		},
		"glob assets": {
			head: headSHA, assets: github.GlobAssets(globAssets), release: release("1.2.0", "x", nil),
			want: []string{
				"Would create a release on GitHub with name 1.2.0 and tag v1.2.0 at commit " + headSHA + " and body:\nx",
				"Would upload assets to GitHub:",
				"- first_thing from assets/first_thing",
				"- second_thing from assets/second_thing",
			},
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			writeAssets(t)
			logs := captureLogs(t)
			api := newFakeAPI(t)
			g := &recordingGit{head: tc.head, headErr: tc.headErr, forbidTag: t}
			state := releaseState(t, nil, false, "", fooName)
			state.Git = g
			store := failingStore(t)
			state.GitHub = github.New(github.Config{Owner: "o", Repo: "r", TrustedAPIURL: api.URL + "/api/v3/"}, nil, store)
			if tc.assets != nil {
				state.ReleaseAssets = []workflow.PackageAssets{{Name: versioning.DefaultName(), Assets: tc.assets}}
			}
			state.PendingActions = []action.Action{tc.release}

			if _, err := runRelease(state, true); err != nil {
				t.Fatal(err)
			}

			for _, line := range tc.want {
				if !strings.Contains(logs.String(), "msg="+strconv.Quote(line)) {
					t.Errorf("log lacks %q:\n%s", line, logs)
				}
			}
			if calls := api.seen(); len(calls) != 0 {
				t.Errorf("a dry run made requests: %v", calls)
			}
			if _, err := os.Stat(store.Dir); err == nil {
				t.Error("a dry run touched the credential directory")
			}
		})
	}
}
