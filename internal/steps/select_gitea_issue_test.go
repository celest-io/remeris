package steps_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/celest-io/remeris/internal/credentials"
	"github.com/celest-io/remeris/internal/diag"
	"github.com/celest-io/remeris/internal/forge/gitea"
	"github.com/celest-io/remeris/internal/prompt"
	"github.com/celest-io/remeris/internal/steps"
	"github.com/celest-io/remeris/internal/workflow"
)

const (
	giteaIssuesAPI = "/api/v1/repos/o/r/issues"
	giteaTokenEnv  = "GITEA_TOKEN"
)

// issuesGitea answers the issues endpoint with a fixed status and body and counts the requests.
func issuesGitea(t *testing.T, status int, body string) (*httptest.Server, *atomic.Int32, *atomic.Value) {
	t.Helper()
	var calls atomic.Int32
	var uri atomic.Value
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		uri.Store(r.URL.RequestURI())
		_, _ = io.Copy(io.Discard, r.Body)
		if r.URL.Path != giteaIssuesAPI {
			http.NotFound(w, r)
			return
		}
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv, &calls, &uri
}

func giteaForge(host string, store *credentials.Store) *gitea.Forge {
	return gitea.New(gitea.Config{Owner: "o", Repo: "r", Host: host}, store)
}

func TestSelectGiteaIssueNotConfigured(t *testing.T) {
	logs := captureLogs(t)
	for _, dryRun := range []bool{false, true} {
		rt := workflow.Real(workflow.State{})
		if dryRun {
			rt = workflow.DryRun(workflow.State{})
		}
		_, err := (&steps.SelectGiteaIssue{}).Run(context.Background(), rt)
		d, ok := err.(diag.Diagnostic)
		if !ok || err.Error() != "Gitea is not configured" {
			t.Fatalf("dry-run=%v: err = %v", dryRun, err)
		}
		if d.Code() != "remeris::issues::gitea::not_configured" ||
			d.Help() != "Gitea must be configured in order to use the SelectGiteaIssue step" {
			t.Errorf("dry-run=%v: code = %q, help = %q", dryRun, d.Code(), d.Help())
		}
	}
	if logs.Len() != 0 {
		t.Errorf("logs = %q", logs)
	}
}

func TestSelectGiteaIssueDryRun(t *testing.T) {
	cases := map[string]struct {
		labels []string
		want   string
	}{
		"any labels": {nil, `msg="Would query configured Gitea instance for issues with any labels"` + "\n"},
		"labels":     {[]string{"a", "b"}, `msg="Would query configured Gitea instance for issues with labels a, b"` + "\n"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			logs := captureLogs(t)
			srv, calls, _ := issuesGitea(t, http.StatusOK, "[]")
			scripted := prompt.NewScripted()
			// An empty store with a prompter that must stay unused fails any credential lookup that asks.
			store := &credentials.Store{Dir: t.TempDir(), Getenv: func(string) string { return "" }, Prompter: scripted}
			state := workflow.State{Gitea: giteaForge(srv.URL, store), Prompter: scripted}

			rt, err := (&steps.SelectGiteaIssue{Labels: tc.labels}).Run(context.Background(), workflow.DryRun(state))

			if err != nil {
				t.Fatal(err)
			}
			want := tc.want + `msg="Would prompt user to select an issue"` + "\n"
			if got := logs.String(); got != want {
				t.Errorf("logs = %q, want %q", got, want)
			}
			if rt.State.Issue == nil || *rt.State.Issue != (workflow.Issue{Key: "123", Summary: "Test issue"}) {
				t.Errorf("issue = %v", rt.State.Issue)
			}
			if calls.Load() != 0 || len(scripted.Asked()) != 0 {
				t.Errorf("requests = %d, questions = %v", calls.Load(), scripted.Asked())
			}
		})
	}
}

func TestSelectGiteaIssueReal(t *testing.T) {
	const list = `[{"number":4,"title":"alpha"},{"number":9,"title":"beta"}]`
	cases := map[string]struct {
		labels    []string
		status    int
		body      string
		answers   []prompt.Answer
		noPrompt  bool
		wantIssue string
		wantLog   string
		wantErr   string
		wantQuery string
		wantAsked int
	}{
		"selects": {
			status: http.StatusOK, body: list, answers: []prompt.Answer{{Index: 1}},
			wantIssue: "9: beta", wantLog: `msg="Selected item: 9: beta"`, wantAsked: 1,
			wantQuery: "limit=30&state=open&type=issues",
		},
		"with labels": {
			labels: []string{
				"x",
				"y",
			},
			status:    http.StatusOK,
			body:      list,
			answers:   []prompt.Answer{{}},
			wantIssue: "4: alpha",
			wantLog:   `msg="Selected item: 4: alpha"`,
			wantAsked: 1,
			wantQuery: "labels=x%2Cy&limit=30&state=open&type=issues",
		},
		"no issues":     {status: http.StatusOK, body: prNoPR, wantErr: failedInput},
		"no way to ask": {status: http.StatusOK, body: list, noPrompt: true, wantErr: failedInput},
		"api error": {
			status: http.StatusNotFound, body: `nope`,
			wantErr: "Trouble communicating with",
		},
		"prompt fails": {
			status: http.StatusOK, body: list, answers: []prompt.Answer{{Err: prompt.Error(nil)}},
			wantErr: failedInput, wantAsked: 1,
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			logs := captureLogs(t)
			srv, _, uri := issuesGitea(t, tc.status, tc.body)
			scripted := prompt.NewScripted(tc.answers...)
			store := &credentials.Store{Dir: t.TempDir(), Getenv: func(k string) string {
				if k == giteaTokenEnv {
					return giteaToken
				}
				return ""
			}}
			state := workflow.State{Gitea: giteaForge(srv.URL, store), Prompter: scripted}
			if tc.noPrompt {
				state.Prompter = nil
			}

			rt, err := (&steps.SelectGiteaIssue{Labels: tc.labels}).Run(context.Background(), workflow.Real(state))

			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("err = %v, want %q", err, tc.wantErr)
				}
				if rt.State.Issue != nil {
					t.Errorf("issue = %v after a failure", rt.State.Issue)
				}
				if strings.Contains(err.Error(), giteaToken) {
					t.Errorf("the token is in the error: %v", err)
				}
			} else if err != nil {
				t.Fatal(err)
			}
			if got := len(scripted.Asked()); got != tc.wantAsked {
				t.Errorf("questions = %d, want %d", got, tc.wantAsked)
			}
			if tc.wantIssue == "" {
				return
			}
			if rt.State.Issue == nil || rt.State.Issue.String() != tc.wantIssue {
				t.Errorf("issue = %v, want %q", rt.State.Issue, tc.wantIssue)
			}
			if got := strings.TrimSpace(logs.String()); got != tc.wantLog {
				t.Errorf("logs = %q, want %q", got, tc.wantLog)
			}
			if got := uri.Load(); got != giteaIssuesAPI+"?"+tc.wantQuery {
				t.Errorf("request = %v, want query %q", got, tc.wantQuery)
			}
			asked := scripted.Asked()[0]
			if asked.Options[0] != "4: alpha" || asked.Options[1] != "9: beta" {
				t.Errorf("options = %q", asked.Options)
			}
		})
	}
}
