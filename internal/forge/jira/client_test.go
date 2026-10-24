package jira

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
)

const (
	issuesJSON      = `{"issues":[{"key":"PRJ-1","fields":{"summary":"First"}},{"key":"PRJ-2","fields":{"summary":"Second"}}]}`
	transitionsJSON = `{"transitions":[{"id":"11","name":"To Do"},{"id":"31","name":"Done"}]}`
	errNoIssue      = "want an error"
)

func TestSearchIssues(t *testing.T) {
	srv := newServer(t, replying(http.StatusOK, issuesJSON))
	c := newClient(t, srv.URL)

	issues, err := c.SearchIssues(context.Background(), "In Progress")

	if err != nil {
		t.Fatal(err)
	}
	if len(issues) != 2 || issues[0] != (Issue{Key: "PRJ-1", Summary: "First"}) || issues[1].Summary != "Second" {
		t.Errorf("issues = %+v", issues)
	}
	reqs := srv.requests()
	if len(reqs) != 1 {
		t.Fatalf("requests = %+v", reqs)
	}
	r := reqs[0]
	if r.method != http.MethodPost || r.uri != searchURI || r.auth != testAuth || r.contentType != "application/json" {
		t.Errorf("request = %+v", r)
	}
	var body struct {
		JQL        string   `json:"jql"`
		Fields     []string `json:"fields"`
		MaxResults int      `json:"maxResults"`
	}
	if err := json.Unmarshal([]byte(r.body), &body); err != nil {
		t.Fatal(err)
	}
	if body.JQL != jqlInProgress || len(body.Fields) != 1 || body.Fields[0] != "summary" ||
		body.MaxResults != 50 {
		t.Errorf("body = %+v", body)
	}
}

func TestSearchIssuesEscapesJQL(t *testing.T) {
	tests := map[string]struct {
		status, project, want string
	}{
		"quote closes the literal": {
			status: `Done" OR project = SECRET OR status = "x`, project: testProject,
			want: `status = "Done\" OR project = SECRET OR status = \"x" AND project = "PRJ"`,
		},
		"backslash cannot escape the closing quote": {
			status: `Done\`, project: testProject, want: `status = "Done\\" AND project = "PRJ"`,
		},
		"hostile project": {
			status: statusDone, project: `P" OR 1=1 --`, want: `status = "Done" AND project = "P\" OR 1=1 --"`,
		},
		"empty": {status: "", project: testProject, want: `status = "" AND project = "PRJ"`},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			srv := newServer(t, replying(http.StatusOK, `{"issues":[]}`))
			c := newClient(t, srv.URL)
			c.cfg.Project = tc.project

			issues, err := c.SearchIssues(context.Background(), tc.status)

			if err != nil || len(issues) != 0 {
				t.Fatalf("issues = %v, err = %v", issues, err)
			}
			var body struct {
				JQL string `json:"jql"`
			}
			if err := json.Unmarshal([]byte(srv.requests()[0].body), &body); err != nil {
				t.Fatal(err)
			}
			if body.JQL != tc.want {
				t.Errorf("jql = %q, want %q", body.JQL, tc.want)
			}
		})
	}
}

func TestContextPathIsKept(t *testing.T) {
	srv := newServer(t, replying(http.StatusOK, `{"issues":[]}`))
	c := newClient(t, srv.URL+"/jira//")

	if _, err := c.SearchIssues(context.Background(), statusDone); err != nil {
		t.Fatal(err)
	}

	if got := srv.requests()[0].uri; got != "/jira"+searchURI {
		t.Errorf("uri = %q", got)
	}
}

func TestTransitionIssue(t *testing.T) {
	srv := newServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			_, _ = io.WriteString(w, transitionsJSON)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
	c := newClient(t, srv.URL)

	if err := c.TransitionIssue(context.Background(), "PRJ-7", statusDone); err != nil {
		t.Fatal(err)
	}

	reqs := srv.requests()
	if len(reqs) != 2 {
		t.Fatalf("requests = %+v", reqs)
	}
	const path = "/rest/api/3/issue/PRJ-7/transitions"
	if reqs[0].method != http.MethodGet || reqs[0].uri != path || reqs[0].auth != testAuth {
		t.Errorf("first = %+v", reqs[0])
	}
	if reqs[1].method != http.MethodPost || reqs[1].uri != path || reqs[1].auth != testAuth ||
		reqs[1].body != `{"transition":{"id":"31"}}` {
		t.Errorf("second = %+v", reqs[1])
	}
}

func TestTransitionNotFound(t *testing.T) {
	srv := newServer(t, replying(http.StatusOK, transitionsJSON))
	c := newClient(t, srv.URL)

	err := c.TransitionIssue(context.Background(), "PRJ-7", "done")

	if err == nil {
		t.Fatal(errNoIssue)
	}
	if err.Error() != "The specified transition name was not found in the Jira project" ||
		codeOf(t, err) != "remeris::issues::jira::transition" ||
		helpOf(
			err,
		) != "The `transition` field in TransitionJiraIssue must correspond to a valid transition in the Jira project" {
		t.Errorf("err = %q, code %q, help %q", err, codeOf(t, err), helpOf(err))
	}
	if n := len(srv.requests()); n != 1 {
		t.Errorf("%d requests: nothing may be posted when the transition is not found", n)
	}
}

func TestTransitionsListsThem(t *testing.T) {
	srv := newServer(t, replying(http.StatusOK, transitionsJSON))
	c := newClient(t, srv.URL)

	got, err := c.Transitions(context.Background(), "PRJ-7")

	if err != nil || len(got) != 2 || got[1] != (Transition{ID: "31", Name: statusDone}) {
		t.Errorf("transitions = %+v, err = %v", got, err)
	}
}

// TestIssueKeyIsOneSegment sends keys that try to reach another endpoint.
func TestIssueKeyIsOneSegment(t *testing.T) {
	tests := map[string]string{
		"slash":      "PRJ-1/../../../../user",
		"query":      "PRJ-1?expand=x",
		"fragment":   "PRJ-1#x",
		"percent":    "PRJ-1%2F..",
		"space":      "PRJ 1",
		"backslash":  `PRJ-1\x`,
		"newline":    "PRJ-1\nX-Header: y",
		"unicode":    "PRJ-é",
		"percent 00": "PRJ-1%00",
	}
	for name, key := range tests {
		t.Run(name, func(t *testing.T) {
			srv := newServer(t, replying(http.StatusOK, `{"transitions":[]}`))
			c := newClient(t, srv.URL)

			if _, err := c.Transitions(context.Background(), key); err != nil {
				t.Fatal(err)
			}

			uri := srv.requests()[0].uri
			rest, ok := strings.CutPrefix(uri, "/rest/api/3/issue/")
			segment, ok2 := strings.CutSuffix(rest, "/transitions")
			if !ok || !ok2 || strings.ContainsAny(segment, "/?#\\ \n") {
				t.Errorf("uri = %q: the key must be a single escaped segment", uri)
			}
		})
	}
}

func TestInvalidIssueKeys(t *testing.T) {
	srv := newServer(t, replying(http.StatusOK, `{"transitions":[]}`))
	c := newClient(t, srv.URL)
	for _, key := range []string{"", ".", ".."} {
		err := c.TransitionIssue(context.Background(), key, statusDone)

		if err == nil || codeOf(t, err) != "remeris::jira::invalid_issue_key" {
			t.Errorf("key %q: err = %v", key, err)
		}
	}
	if n := len(srv.requests()); n != 0 {
		t.Errorf("%d requests for invalid keys", n)
	}
}

func TestStatusIsChecked(t *testing.T) {
	tests := map[string]struct {
		status   int
		body     string
		wantErr  string
		wantHelp bool
		requests int
	}{
		"401": {
			status: http.StatusUnauthorized, body: "no way", requests: 1, wantHelp: true,
			wantErr: "Problem communicating with Jira while querying for issues: " +
				"HTTP status client error (401 Unauthorized) for url (",
		},
		"403": {status: http.StatusForbidden, requests: 1, wantHelp: true, wantErr: "(403 Forbidden)"},
		"404": {status: http.StatusNotFound, requests: 1, wantErr: "(404 Not Found)"},
		// The default policy retries a 5xx five times.
		"500": {
			status:   http.StatusInternalServerError,
			body:     "boom",
			requests: 6,
			wantErr:  "(500 Internal Server Error) for url",
		},
		"unexpected redirect": {
			status:   http.StatusNotModified,
			requests: 1,
			wantErr:  "unexpected HTTP status 304 Not Modified",
		},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			srv := newServer(t, replying(tc.status, tc.body))
			c := newClient(t, srv.URL)

			_, err := c.SearchIssues(context.Background(), statusDone)

			if err == nil {
				t.Fatal(errNoIssue)
			}
			if !strings.Contains(err.Error(), tc.wantErr) ||
				!strings.HasPrefix(err.Error(), "Problem communicating with Jira") {
				t.Errorf("err = %q, want it to contain %q", err, tc.wantErr)
			}
			if tc.body != "" && !strings.Contains(err.Error(), "with body: "+tc.body) {
				t.Errorf("err = %q: the body should be shown", err)
			}
			if (helpOf(err) != "") != tc.wantHelp {
				t.Errorf("help = %q, want help: %v", helpOf(err), tc.wantHelp)
			}
			if n := len(srv.requests()); n != tc.requests {
				t.Errorf("%d requests, want %d", n, tc.requests)
			}
		})
	}
}

func TestTransitionStatusErrorsKeepKnopesActivity(t *testing.T) {
	srv := newServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			_, _ = io.WriteString(w, transitionsJSON)
			return
		}
		w.WriteHeader(http.StatusBadRequest)
	})
	c := newClient(t, srv.URL)

	err := c.TransitionIssue(context.Background(), "PRJ-7", statusDone)
	if err == nil || !strings.HasPrefix(err.Error(), "Problem communicating with Jira while transitioning issue: ") {
		t.Errorf("err = %v", err)
	}

	failing := newServer(t, replying(http.StatusNotFound, ""))
	_, err = newClient(t, failing.URL).Transitions(context.Background(), "PRJ-7")
	if err == nil || !strings.HasPrefix(err.Error(), "Problem communicating with Jira while getting transitions: ") {
		t.Errorf("err = %v", err)
	}
}

func TestBadJSON(t *testing.T) {
	srv := newServer(t, replying(http.StatusOK, `<html>`))
	c := newClient(t, srv.URL)

	_, err := c.SearchIssues(context.Background(), statusDone)

	if err == nil || !strings.HasPrefix(err.Error(), "Could not deserialize response from Jira into JSON: ") ||
		codeOf(t, err) != "remeris::jira::deserialize" {
		t.Errorf("err = %v", err)
	}
}

func TestLongErrorBodyIsCut(t *testing.T) {
	srv := newServer(t, replying(http.StatusBadRequest, strings.Repeat("x", 10_000)))
	c := newClient(t, srv.URL)

	_, err := c.SearchIssues(context.Background(), statusDone)

	if err == nil || len(err.Error()) > 3_000 || !strings.HasSuffix(err.Error(), "...") {
		t.Errorf("error of %d bytes", len(err.Error()))
	}
}

func TestResponseSizeIsLimited(t *testing.T) {
	chunk := []byte(strings.Repeat(" ", 1<<20))
	srv := newServer(t, func(w http.ResponseWriter, _ *http.Request) {
		for range maxBody>>20 + 1 {
			_, _ = w.Write(chunk)
		}
	})
	c := newClient(t, srv.URL)

	_, err := c.SearchIssues(context.Background(), statusDone)

	if err == nil || !strings.Contains(err.Error(), "the response is larger than 32 MiB") {
		t.Errorf("err = %v", err)
	}
}

func TestContextCancellationIsReported(t *testing.T) {
	srv := newServer(t, replying(http.StatusOK, issuesJSON))
	c := newClient(t, srv.URL)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := c.SearchIssues(ctx, statusDone)

	if err == nil || !strings.HasPrefix(err.Error(), "Problem communicating with Jira while querying for issues: ") {
		t.Errorf("err = %v", err)
	}
}
