package steps_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/celest-io/remeris/internal/credentials"
	"github.com/celest-io/remeris/internal/forge/github"
	"github.com/celest-io/remeris/internal/prompt"
	"github.com/celest-io/remeris/internal/steps"
	"github.com/celest-io/remeris/internal/workflow"
)

const issuesAPI = "/api/graphql"
const failedInput = "Failed to get user input"

// gqlIssues is the GraphQL response that lists the given JSON nodes.
func gqlIssues(nodes string) string {
	return `{"data":{"repository":{"issues":{"nodes":[` + nodes + `]}}}}`
}

func issuesServer(t *testing.T, status int, body string) (*httptest.Server, *atomic.Int32) {
	t.Helper()
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.URL.Path != issuesAPI {
			http.NotFound(w, r)
			return
		}
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv, &calls
}

func TestSelectGitHubIssueNotConfigured(t *testing.T) {
	logs := captureLogs(t)
	for _, dryRun := range []bool{false, true} {
		rt := workflow.Real(workflow.State{})
		if dryRun {
			rt = workflow.DryRun(workflow.State{})
		}
		_, err := (&steps.SelectGitHubIssue{}).Run(context.Background(), rt)
		if err == nil || err.Error() != "GitHub is not configured" {
			t.Fatalf("dry-run=%v: err = %v", dryRun, err)
		}
	}
	if logs.Len() != 0 {
		t.Errorf("logs = %q", logs)
	}
}

func TestSelectGitHubIssueDryRun(t *testing.T) {
	cases := map[string]struct {
		labels []string
		want   string
	}{
		"any labels": {nil, `msg="Would query configured GitHub instance for issues with any labels"` + "\n"},
		"labels":     {[]string{"a", "b"}, `msg="Would query configured GitHub instance for issues with labels a, b"` + "\n"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			logs := captureLogs(t)
			srv, calls := issuesServer(t, http.StatusOK, "[]")
			scripted := prompt.NewScripted()
			// An empty store with no prompter would fail any credential lookup that asks.
			store := &credentials.Store{Dir: t.TempDir(), Getenv: func(string) string { return "" }, Prompter: scripted}
			forge := github.New(github.Config{Owner: "o", Repo: "r", TrustedAPIURL: srv.URL + "/api/v3/"}, nil, store)
			state := workflow.State{GitHub: forge, Prompter: scripted}

			rt, err := (&steps.SelectGitHubIssue{Labels: tc.labels}).Run(context.Background(), workflow.DryRun(state))

			if err != nil {
				t.Fatal(err)
			}
			want := tc.want + `msg="Would prompt user to select an issue and move workflow to IssueSelected state."` + "\n"
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

func TestSelectGitHubIssueReal(t *testing.T) {
	list := gqlIssues(`{"number":4,"title":"first"},{"number":9,"title":"second"}`)
	cases := map[string]struct {
		status    int
		body      string
		answers   []prompt.Answer
		noPrompt  bool
		wantIssue string
		wantLog   string
		wantErr   string
		wantAsked int
	}{
		"selects": {
			status: http.StatusOK, body: list, answers: []prompt.Answer{{Message: "Select an Issue", Index: 1}},
			wantIssue: "9: second", wantLog: `msg="Selected item : 9: second"`, wantAsked: 1,
		},
		"no issues": {status: http.StatusOK, body: gqlIssues(""), wantErr: failedInput},
		"no prompter": {
			status: http.StatusOK, body: list, noPrompt: true, wantErr: failedInput,
		},
		"api error": {
			status: http.StatusOK, body: `{"errors":[{"message":"boom"}]}`,
			wantErr: "Could not communicate with GitHub while loading issues: boom",
		},
		"prompt fails": {
			status: http.StatusOK, body: list, answers: []prompt.Answer{{Err: prompt.Error(nil)}},
			wantErr: failedInput, wantAsked: 1,
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			logs := captureLogs(t)
			srv, _ := issuesServer(t, tc.status, tc.body)
			scripted := prompt.NewScripted(tc.answers...)
			forge := github.New(github.Config{Owner: "o", Repo: "r", TrustedAPIURL: srv.URL + "/api/v3/"}, nil, tokenStore(t))
			state := workflow.State{GitHub: forge, Prompter: scripted}
			if tc.noPrompt {
				state.Prompter = nil
			}

			rt, err := (&steps.SelectGitHubIssue{}).Run(context.Background(), workflow.Real(state))

			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("err = %v, want %q", err, tc.wantErr)
				}
				if rt.State.Issue != nil {
					t.Errorf("issue = %v after a failure", rt.State.Issue)
				}
			} else if err != nil {
				t.Fatal(err)
			}
			if got := len(scripted.Asked()); got != tc.wantAsked {
				t.Errorf("questions = %d, want %d", got, tc.wantAsked)
			}
			if tc.wantIssue != "" {
				if rt.State.Issue == nil || rt.State.Issue.String() != tc.wantIssue {
					t.Errorf("issue = %v, want %q", rt.State.Issue, tc.wantIssue)
				}
				if got := strings.TrimSpace(logs.String()); got != tc.wantLog {
					t.Errorf("logs = %q, want %q", got, tc.wantLog)
				}
				asked := scripted.Asked()[0]
				if asked.Options[0] != "4: first" || asked.Options[1] != "9: second" {
					t.Errorf("options = %q", asked.Options)
				}
			}
		})
	}
}
