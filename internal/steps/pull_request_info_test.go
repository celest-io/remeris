package steps_test

import (
	"net/http"
	"net/http/httptest"
	"os"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/celest-io/remeris/internal/credentials"
	"github.com/celest-io/remeris/internal/forge/github"
	"github.com/celest-io/remeris/internal/git"
	"github.com/celest-io/remeris/internal/steps"
	"github.com/celest-io/remeris/pkg/versioning/releasenotes"
)

const (
	hashA       = "aaaaaaa"
	prTemplate  = "- $summary (#$pr_number by @$pr_author_login)"
	pullsPrefix = "/api/v3/repos/o/r/commits/"
)

// pullsServer answers GET .../commits/{sha}/pulls with bodies[sha] (404 for an unknown sha) and records the
// requests as "path" and the Authorization header.
type pullsServer struct {
	*httptest.Server

	mu    sync.Mutex
	paths []string
	auths []string
}

func newPullsServer(t *testing.T, bodies map[string]string, failing ...string) *pullsServer {
	t.Helper()
	s := &pullsServer{}
	s.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		s.paths = append(s.paths, r.URL.Path)
		s.auths = append(s.auths, r.Header.Get("Authorization"))
		s.mu.Unlock()
		sha := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, pullsPrefix), "/pulls")
		if slices.Contains(failing, sha) {
			w.WriteHeader(http.StatusUnprocessableEntity)
			_, _ = w.Write([]byte(`{"message":"boom"}`))
			return
		}
		body, ok := bodies[sha]
		if !ok {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(s.Close)
	return s
}

func (s *pullsServer) forge(t *testing.T, token string) *github.Forge {
	t.Helper()
	store := &credentials.Store{Dir: t.TempDir(), Getenv: func(k string) string {
		if k == credentials.GitHubToken.Env() {
			return token
		}
		return ""
	}}
	return github.New(github.Config{Owner: "o", Repo: "r", TrustedAPIURL: s.URL + "/api/v3/"}, nil, store)
}

func hashedCommits(hashes ...string) []git.Commit {
	out := make([]git.Commit, len(hashes))
	for i, h := range hashes {
		out[i] = git.Commit{Message: "feat: Change " + h, Info: &git.CommitInfo{Hash: h, AuthorName: authorAnn}}
	}
	return out
}

func changelog(t *testing.T) string {
	t.Helper()
	got, err := os.ReadFile(changelogFile)
	if err != nil {
		t.Fatal(err)
	}
	return string(got)
}

func TestPrepareReleasePullRequestInfo(t *testing.T) {
	cases := map[string]struct {
		token     string
		bodies    map[string]string
		failing   []string
		wantAuth  string
		wantLines []string
		wantLogs  []string
	}{
		"first pull request wins, short hash used": {
			token:     "tok",
			bodies:    map[string]string{hashA: `[{"number":5,"user":{"login":"ann"}},{"number":6,"user":{"login":"bob"}}]`},
			wantAuth:  "Bearer tok",
			wantLines: []string{"- Change aaaaaaa (#5 by @ann)\n"},
		},
		"anonymous without a token": {
			bodies:    map[string]string{hashA: `[{"number":5,"user":{"login":"ann"}}]`},
			wantLines: []string{"- Change aaaaaaa (#5 by @ann)\n"},
		},
		"empty list": {
			bodies:    map[string]string{hashA: `[]`},
			wantLines: []string{"- Change aaaaaaa\n"},
			wantLogs:  []string{"No PR found for commit aaaaaaa"},
		},
		"one failure does not stop the others": {
			bodies:    map[string]string{"bbbbbbb": `[{"number":8,"user":{"login":"bob"}}]`},
			failing:   []string{hashA},
			wantLines: []string{"- Change bbbbbbb (#8 by @bob)\n", "- Change aaaaaaa\n"},
			wantLogs:  []string{"Failed to fetch PR info for commit aaaaaaa: "},
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			prepareDir(t, nil)
			srv := newPullsServer(t, tc.bodies, tc.failing...)
			hashes := []string{hashA}
			if len(tc.failing) > 0 {
				hashes = append(hashes, "bbbbbbb")
			}
			state := stateFor(t, []string{tagV123}, cargo(v123), "")
			state.Git = &recordingGit{commits: hashedCommits(hashes...)}
			state.GitHub = srv.forge(t, tc.token)
			state.Packages[0].ReleaseNotes.ChangeTemplates = []releasenotes.ChangeTemplate{prTemplate, summaryLine}
			cl := releasenotes.NewChangelog(changelogFile, "# Changelog\n")
			state.Packages[0].ReleaseNotes.Changelog = &cl
			logs := captureDebugLogs(t)

			if _, err := runPrepare(t, &steps.PrepareRelease{}, state, false); err != nil {
				t.Fatal(err)
			}

			got := changelog(t)
			for _, line := range tc.wantLines {
				if !strings.Contains(got, line) {
					t.Errorf("changelog lacks %q:\n%s", line, got)
				}
			}
			for _, h := range hashes {
				if !slices.Contains(srv.paths, pullsPrefix+h+"/pulls") {
					t.Errorf("paths = %v, want one for %s", srv.paths, h)
				}
			}
			for _, auth := range srv.auths {
				if auth != tc.wantAuth {
					t.Errorf("Authorization = %q, want %q", auth, tc.wantAuth)
				}
			}
			for _, want := range tc.wantLogs {
				if !strings.Contains(logs.String(), want) {
					t.Errorf("logs lack %q:\n%s", want, logs)
				}
			}
		})
	}
}

func TestPrepareReleasePullRequestInfoDryRun(t *testing.T) {
	prepareDir(t, map[string]string{changeFile: patchNote})
	srv := newPullsServer(t, nil)
	state := stateFor(t, []string{tagV123}, cargo(v123), "")
	state.Git = &recordingGit{commits: hashedCommits(hashA)}
	state.GitHub = srv.forge(t, "tok")
	state.Packages[0].ReleaseNotes.ChangeTemplates = []releasenotes.ChangeTemplate{prTemplate, summaryLine}
	logs := captureDebugLogs(t)

	if _, err := runPrepare(t, &steps.PrepareRelease{}, state, true); err != nil {
		t.Fatal(err)
	}

	if len(srv.paths) != 0 {
		t.Errorf("a dry run called the API: %v", srv.paths)
	}
	want := "Would fetch Pull Request info from GitHub repo o/r for 2 changes"
	if !strings.Contains(logs.String(), want) {
		t.Errorf("logs lack %q:\n%s", want, logs)
	}
	if _, err := os.Stat(changelogFile); err == nil {
		t.Error("a dry run wrote the changelog")
	}
}
