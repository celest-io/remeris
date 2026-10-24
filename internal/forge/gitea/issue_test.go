package gitea

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/celest-io/remeris/internal/diag"
)

const issuesPath = "/api/v1/repos/o/r/issues"

func TestListIssuesRequest(t *testing.T) {
	cases := map[string]struct {
		labels   []string
		wantURI  string
		wantList []Issue
	}{
		"any labels": {
			labels:  nil,
			wantURI: issuesPath + "?limit=30&state=open&type=issues",
		},
		"empty labels": {
			labels:  []string{},
			wantURI: issuesPath + "?limit=30&state=open&type=issues",
		},
		"labels are comma joined": {
			labels:  []string{"a b", "c"},
			wantURI: issuesPath + "?labels=a+b%2Cc&limit=30&state=open&type=issues",
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			srv := newServer(
				t,
				replying(http.StatusOK, `[{"number":4,"title":"first","state":"open"},{"number":9,"title":"second"}]`),
			)
			c := newClient(t, srv.URL)

			got, err := c.ListIssues(context.Background(), tc.labels)

			if err != nil {
				t.Fatal(err)
			}
			if len(got) != 2 || got[0] != (Issue{Number: 4, Title: "first"}) || got[1] != (Issue{Number: 9, Title: "second"}) {
				t.Errorf("issues = %v", got)
			}
			reqs := srv.requests()
			if len(reqs) != 1 {
				t.Fatalf("requests = %v", reqs)
			}
			r := reqs[0]
			if r.method != http.MethodGet || r.uri != tc.wantURI || r.auth != "token "+testToken ||
				r.accept != mimeJSON {
				t.Errorf("request = %+v, want GET %s", r, tc.wantURI)
			}
			if strings.Contains(r.uri, testToken) {
				t.Errorf("the token is in the URL: %s", r.uri)
			}
		})
	}
}

func TestListIssuesErrors(t *testing.T) {
	cases := map[string]struct {
		status  int
		body    string
		want    string
		wantErr string
	}{
		"status": {
			status: http.StatusInternalServerError, body: `{"message":"bad token ` + testToken + `"}`,
			want: "Trouble communicating with", wantErr: "while listing issues",
		},
		"not found": {
			status: http.StatusNotFound, body: `not found`,
			want: "Trouble communicating with", wantErr: "while listing issues",
		},
		"bad json": {
			status: http.StatusOK, body: `{"number":1}`,
			want: "Trouble decoding the response from Gitea while listing issues", wantErr: "",
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			srv := newServer(t, replying(tc.status, tc.body))
			c := newClient(t, srv.URL)

			_, err := c.ListIssues(context.Background(), nil)

			if err == nil || !strings.Contains(err.Error(), tc.want) || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("err = %v, want %q and %q", err, tc.want, tc.wantErr)
			}
			var rendered strings.Builder
			diag.Render(&rendered, err)
			for where, text := range map[string]string{"message": err.Error(), "help": helpOf(err), "rendering": rendered.String()} {
				if strings.Contains(text, testToken) {
					t.Errorf("the token is in the %s: %q", where, text)
				}
			}
		})
	}
}
