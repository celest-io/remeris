package steps_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/celest-io/remeris/internal/credentials"
	"github.com/celest-io/remeris/internal/diag"
	"github.com/celest-io/remeris/internal/forge/jira"
	"github.com/celest-io/remeris/internal/prompt"
	"github.com/celest-io/remeris/internal/steps"
	"github.com/celest-io/remeris/internal/workflow"
)

const (
	fakeSummaryWant = "Test issue"
	jiraSeven       = "seven"
	jiraKey         = "JQ-7"
	jiraSearchPath  = "/rest/api/3/search/jql"
	jiraTransPath   = "/rest/api/3/issue/JQ-7/transitions"
	jiraTestEmail   = "jira-dev@example.com"
	jiraTestToken   = "jira-SENTINEL-token-77" //nolint:gosec // G101: a sentinel, not a credential
	jiraStatusOpen  = "Open"
	jiraStatusDone  = "Done"
	jiraListBody    = `{"issues":[{"key":"JQ-7","fields":{"summary":"seven"}},{"key":"JQ-9","fields":{"summary":"nine"}}]}`
	jiraTransBody   = `{"transitions":[{"id":"31","name":"Done"}]}`
	jiraNotConfMsg  = "Jira is not configured"
	jiraNotConfHelp = "Jira must be configured in order to select a Jira issue"
	jiraNotConfCode = "remeris::issues::jira::not_configured"
	jiraNoIssueMsg  = "No issue selected"
	jiraNoIssueHelp = "You must use the SelectJiraIssue step before TransitionJiraIssue in the same workflow"
)

// jiraFake is a Jira site that records its requests.
type jiraFake struct {
	*httptest.Server
	mu   sync.Mutex
	reqs []string
}

func (f *jiraFake) requests() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.reqs...)
}

// newJiraFake answers the search with list, the transitions with trans and a transition with the status given.
func newJiraFake(t *testing.T, searchStatus int, list string) *jiraFake {
	t.Helper()
	f := &jiraFake{}
	f.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		f.mu.Lock()
		f.reqs = append(f.reqs, r.Method+" "+r.URL.Path+" "+string(body))
		f.mu.Unlock()
		switch {
		case r.URL.Path == jiraSearchPath:
			w.WriteHeader(searchStatus)
			_, _ = io.WriteString(w, list)
		case r.URL.Path == jiraTransPath && r.Method == http.MethodGet:
			_, _ = io.WriteString(w, jiraTransBody)
		case r.URL.Path == jiraTransPath:
			w.WriteHeader(http.StatusNoContent)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(f.Close)
	return f
}

// jiraStore is an empty credential store whose prompter gives the email and then the token.
func jiraStore(t *testing.T, p prompt.Prompter) *credentials.Store {
	t.Helper()
	return &credentials.Store{Dir: t.TempDir(), Getenv: func(string) string { return "" }, Prompter: p}
}

func jiraForge(url string, store *credentials.Store) *jira.Forge {
	return jira.New(jira.Config{URL: url, Project: "JQ"}, store)
}

func assertJiraDiag(t *testing.T, err error, msg, help, code string) {
	t.Helper()
	d, ok := err.(diag.Diagnostic)
	if !ok || err.Error() != msg {
		t.Fatalf("err = %v, want %q", err, msg)
	}
	if d.Help() != help || (code != "" && d.Code() != code) {
		t.Errorf("code = %q, help = %q", d.Code(), d.Help())
	}
}

func TestSelectJiraIssueNotConfigured(t *testing.T) {
	logs := captureLogs(t)
	for _, dryRun := range []bool{false, true} {
		rt := workflow.Real(workflow.State{})
		if dryRun {
			rt = workflow.DryRun(workflow.State{})
		}
		_, err := (&steps.SelectJiraIssue{Status: jiraStatusOpen}).Run(context.Background(), rt)
		assertJiraDiag(t, err, jiraNotConfMsg, jiraNotConfHelp, jiraNotConfCode)
	}
	if logs.Len() != 0 {
		t.Errorf("logs = %q", logs)
	}
}

func TestSelectJiraIssueDryRun(t *testing.T) {
	logs := captureLogs(t)
	srv := newJiraFake(t, http.StatusOK, jiraListBody)
	scripted := prompt.NewScripted()
	state := workflow.State{Jira: jiraForge(srv.URL, jiraStore(t, scripted)), Prompter: scripted}

	rt, err := (&steps.SelectJiraIssue{Status: "In Progress"}).Run(context.Background(), workflow.DryRun(state))

	if err != nil {
		t.Fatal(err)
	}
	want := `msg="Would query configured Jira instance for issues with status In Progress"` + "\n" +
		`msg="Would prompt user to select an issue and move workflow to IssueSelected state."` + "\n"
	if got := logs.String(); got != want {
		t.Errorf("logs = %q, want %q", got, want)
	}
	if rt.State.Issue == nil || *rt.State.Issue != (workflow.Issue{Key: "FAKE-123", Summary: fakeSummaryWant}) {
		t.Errorf("issue = %v", rt.State.Issue)
	}
	if n := len(srv.requests()); n != 0 || len(scripted.Asked()) != 0 {
		t.Errorf("requests = %d, questions = %v", n, scripted.Asked())
	}
}

func TestSelectJiraIssueReal(t *testing.T) {
	cases := map[string]struct {
		status    int
		body      string
		answers   []prompt.Answer
		wantIssue string
		wantErr   string
		wantAsked int
	}{
		"selects one": {
			status: http.StatusOK, body: jiraListBody, answers: []prompt.Answer{
				{Text: jiraTestEmail}, {Text: jiraTestToken}, {Index: 1},
			},
			wantIssue: "JQ-9: nine", wantAsked: 3,
		},
		"empty list": {
			status:    http.StatusOK,
			body:      `{"issues":[]}`,
			answers:   []prompt.Answer{{Text: jiraTestEmail}, {Text: jiraTestToken}},
			wantErr:   failedInput,
			wantAsked: 2,
		},
		"forbidden": {
			status:    http.StatusForbidden,
			body:      `denied`,
			answers:   []prompt.Answer{{Text: jiraTestEmail}, {Text: jiraTestToken}},
			wantErr:   "Problem communicating with Jira while querying for issues",
			wantAsked: 2,
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			logs := captureLogs(t)
			srv := newJiraFake(t, tc.status, tc.body)
			scripted := prompt.NewScripted(tc.answers...)
			state := workflow.State{Jira: jiraForge(srv.URL, jiraStore(t, scripted)), Prompter: scripted}

			rt, err := (&steps.SelectJiraIssue{Status: jiraStatusOpen}).Run(context.Background(), workflow.Real(state))

			if got := len(scripted.Asked()); got != tc.wantAsked {
				t.Errorf("questions = %d, want %d", got, tc.wantAsked)
			}
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("err = %v, want %q", err, tc.wantErr)
				}
				if rt.State.Issue != nil {
					t.Errorf("issue = %v after a failure", rt.State.Issue)
				}
				assertNoJiraSecret(t, err.Error(), logs.String())
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if rt.State.Issue == nil || rt.State.Issue.String() != tc.wantIssue {
				t.Errorf("issue = %v, want %q", rt.State.Issue, tc.wantIssue)
			}
			if got, want := strings.TrimSpace(logs.String()), `msg="Selected item : `+tc.wantIssue+`"`; got != want {
				t.Errorf("logs = %q, want %q", got, want)
			}
			assertNoJiraSecret(t, logs.String())
			// The credentials came in order: the email, the token, then the choice of the issue.
			asked := scripted.Asked()
			if strings.Contains(asked[0].Message, "token") || !strings.Contains(asked[1].Message, "token") {
				t.Errorf("questions = %q then %q, want the email first", asked[0].Message, asked[1].Message)
			}
			if opts := asked[2].Options; len(opts) != 2 || opts[0] != "JQ-7: seven" || opts[1] != "JQ-9: nine" {
				t.Errorf("options = %q", opts)
			}
			if reqs := srv.requests(); len(reqs) != 1 ||
				!strings.Contains(reqs[0], `"jql":"status = \"Open\" AND project = \"JQ\""`) {
				t.Errorf("requests = %q", reqs)
			}
		})
	}
}

func assertNoJiraSecret(t *testing.T, texts ...string) {
	t.Helper()
	for _, s := range texts {
		if strings.Contains(s, jiraTestToken) {
			t.Errorf("the token is in %q", s)
		}
	}
}

func TestTransitionJiraIssueChecks(t *testing.T) {
	logs := captureLogs(t)
	issue := &workflow.Issue{Key: jiraKey, Summary: jiraSeven}
	cases := map[string]struct {
		state    workflow.State
		wantMsg  string
		wantHelp string
		wantCode string
	}{
		// The issue is checked before the [jira] table, so with neither the first error wins.
		"neither": {workflow.State{}, jiraNoIssueMsg, jiraNoIssueHelp, "remeris::issues::jira::no_issue_selected"},
		"no issue": {
			workflow.State{Jira: jiraForge("https://jira.example", nil)}, jiraNoIssueMsg, jiraNoIssueHelp,
			"remeris::issues::jira::no_issue_selected",
		},
		"no jira": {workflow.State{Issue: issue}, jiraNotConfMsg, jiraNotConfHelp, jiraNotConfCode},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			for _, rt := range []workflow.RunType[workflow.State]{workflow.Real(tc.state), workflow.DryRun(tc.state)} {
				_, err := (&steps.TransitionJiraIssue{Status: jiraStatusDone}).Run(context.Background(), rt)
				assertJiraDiag(t, err, tc.wantMsg, tc.wantHelp, tc.wantCode)
			}
		})
	}
	if logs.Len() != 0 {
		t.Errorf("logs = %q", logs)
	}
}

func TestTransitionJiraIssueDryRun(t *testing.T) {
	logs := captureLogs(t)
	srv := newJiraFake(t, http.StatusOK, jiraListBody)
	scripted := prompt.NewScripted()
	state := workflow.State{
		Jira: jiraForge(srv.URL, jiraStore(t, scripted)), Prompter: scripted,
		Issue: &workflow.Issue{Key: jiraKey, Summary: jiraSeven},
	}

	_, err := (&steps.TransitionJiraIssue{Status: jiraStatusDone}).Run(context.Background(), workflow.DryRun(state))

	if err != nil {
		t.Fatal(err)
	}
	if got, want := logs.String(), `msg="Would transition currently selected issue to status Done"`+"\n"; got != want {
		t.Errorf("logs = %q, want %q", got, want)
	}
	if n := len(srv.requests()); n != 0 || len(scripted.Asked()) != 0 {
		t.Errorf("requests = %d, questions = %v", n, scripted.Asked())
	}
}

func TestTransitionJiraIssueReal(t *testing.T) {
	cases := map[string]struct {
		status      string
		wantErr     string
		wantRequest int
	}{
		"transitions": {status: jiraStatusDone, wantRequest: 2},
		"unknown": {
			status:      "Nowhere",
			wantErr:     "The specified transition name was not found in the Jira project",
			wantRequest: 1,
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			logs := captureLogs(t)
			srv := newJiraFake(t, http.StatusOK, jiraListBody)
			scripted := prompt.NewScripted(prompt.Answer{Text: jiraTestEmail}, prompt.Answer{Text: jiraTestToken})
			state := workflow.State{
				Jira: jiraForge(srv.URL, jiraStore(t, scripted)), Prompter: scripted,
				Issue: &workflow.Issue{Key: jiraKey, Summary: jiraSeven},
			}

			_, err := (&steps.TransitionJiraIssue{Status: tc.status}).Run(context.Background(), workflow.Real(state))

			reqs := srv.requests()
			if len(reqs) != tc.wantRequest {
				t.Errorf("requests = %q", reqs)
			}
			if tc.wantErr != "" {
				if err == nil || err.Error() != tc.wantErr {
					t.Fatalf("err = %v, want %q", err, tc.wantErr)
				}
				if logs.Len() != 0 {
					t.Errorf("logs = %q", logs)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if got, want := strings.TrimSpace(logs.String()), `msg="JQ-7 transitioned to Done"`; got != want {
				t.Errorf("logs = %q, want %q", got, want)
			}
			if want := `POST ` + jiraTransPath + ` {"transition":{"id":"31"}}`; reqs[1] != want {
				t.Errorf("transition request = %q, want %q", reqs[1], want)
			}
			assertNoJiraSecret(t, logs.String())
		})
	}
}

func TestTransitionJiraIssueAPIErrorHidesToken(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = io.WriteString(w, "bad credentials "+jiraTestToken)
	}))
	t.Cleanup(srv.Close)
	scripted := prompt.NewScripted(prompt.Answer{Text: jiraTestEmail}, prompt.Answer{Text: jiraTestToken})
	state := workflow.State{
		Jira: jiraForge(srv.URL, jiraStore(t, scripted)), Prompter: scripted,
		Issue: &workflow.Issue{Key: jiraKey, Summary: jiraSeven},
	}

	_, err := (&steps.TransitionJiraIssue{Status: jiraStatusDone}).Run(context.Background(), workflow.Real(state))

	if err == nil || !strings.Contains(err.Error(), "Problem communicating with Jira while getting transitions") {
		t.Fatalf("err = %v", err)
	}
	if d, ok := err.(diag.Diagnostic); ok {
		assertNoJiraSecret(t, err.Error(), d.Help())
	}
}
