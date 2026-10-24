package jira

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestQuotedConfigurationsAreUnwrapped(t *testing.T) {
	tests := map[string]struct{ status, project, want string }{
		"single quotes":    {status: "'In Progress'", project: testProject, want: jqlInProgress},
		"double quotes":    {status: `"In Progress"`, project: testProject, want: jqlInProgress},
		"quoted project":   {status: statusDone, project: "'PRJ'", want: `status = "Done" AND project = "PRJ"`},
		"one pair only":    {status: `"'x'"`, project: testProject, want: `status = "'x'" AND project = "PRJ"`},
		"mismatched pair":  {status: `'x"`, project: testProject, want: `status = "'x\"" AND project = "PRJ"`},
		"lone quote":       {status: `"`, project: testProject, want: `status = "\"" AND project = "PRJ"`},
		"inner quote only": {status: `It's`, project: testProject, want: `status = "It's" AND project = "PRJ"`},
		"empty pair":       {status: `''`, project: testProject, want: `status = "" AND project = "PRJ"`},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			srv := newServer(t, replying(http.StatusOK, `{"issues":[]}`))
			c := newClient(t, srv.URL)
			c.cfg.Project = tc.project

			if _, err := c.SearchIssues(context.Background(), tc.status); err != nil {
				t.Fatal(err)
			}

			var body struct {
				JQL string `json:"jql"`
			}
			if err := json.Unmarshal([]byte(srv.requests()[0].body), &body); err != nil || body.JQL != tc.want {
				t.Errorf("jql = %q, %v, want %q", body.JQL, err, tc.want)
			}
		})
	}
}

// TestTransitionIsPostedOnce checks that a failing POST is not retried, where a failing search is.
func TestTransitionIsPostedOnce(t *testing.T) {
	srv := newServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			_, _ = io.WriteString(w, transitionsJSON)
			return
		}
		w.WriteHeader(http.StatusServiceUnavailable)
	})
	c := newClient(t, srv.URL)

	err := c.TransitionIssue(context.Background(), "PRJ-7", statusDone)

	if err == nil || !strings.Contains(err.Error(), "while transitioning issue") {
		t.Errorf("err = %v", err)
	}
	if n := len(srv.requests()); n != 2 {
		t.Errorf("%d requests, want the GET and one POST", n)
	}
}
