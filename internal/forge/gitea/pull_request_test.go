package gitea

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

const (
	pullsPath      = "/api/v1/repos/o/r/pulls"
	pullPath       = pullsPath + "/7"
	mainBranch     = "main"
	featureBranch  = "feature/x"
	pullTitle      = "chore: Release 1.0.0"
	pullBody       = "a <b> & c\n\nline"
	wantList       = "base_branch=main&limit=50&page=1&state=open"
	emptyList      = "[]"
	nullJSON       = "null"
	mimeJSON       = "application/json"
	pullCreatedAny = `{"unexpected":[1,2]}`
)

// pr builds one entry of the list response.
func pr(number int, head, owner, base string) string {
	repo := nullJSON
	if owner != "" {
		repo = fmt.Sprintf(`{"owner":{"login":%q},"extra":1}`, owner)
	}
	return fmt.Sprintf(`{"number":%d,"url":"https://g/api/v1/repos/o/r/pulls/%[1]d","head":{"ref":%q,"repo":%s},`+
		`"base":{"ref":%q},"unknown":true}`, number, head, repo, base)
}

func list(items ...string) string { return "[" + strings.Join(items, ",") + "]" }

// pullAPI answers the list with pages[page-1] (an empty page past the end) and every write with writeStatus.
func pullAPI(pages []string, writeStatus int, writeBody string) func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			var page int
			_, _ = fmt.Sscan(r.URL.Query().Get("page"), &page)
			if page < 1 || page > len(pages) {
				_, _ = io.WriteString(w, emptyList)
				return
			}
			_, _ = io.WriteString(w, pages[page-1])
			return
		}
		w.WriteHeader(writeStatus)
		_, _ = io.WriteString(w, writeBody)
	}
}

func TestCreateOrUpdatePullRequest(t *testing.T) {
	cases := map[string]struct {
		branch   string
		pages    []string
		wantReqs []string
		wantBody string
	}{
		"no pull request creates": {
			branch:   mainBranch,
			pages:    []string{emptyList},
			wantReqs: []string{"GET " + pullsPath + "?" + wantList, "POST " + pullsPath},
			wantBody: `{"title":"` + pullTitle + `","body":"a <b> & c\n\nline","head":"main","base":"main"}`,
		},
		"match updates": {
			branch:   mainBranch,
			pages:    []string{list(pr(7, mainBranch, "o", mainBranch))},
			wantReqs: []string{"GET " + pullsPath + "?" + wantList, "PATCH " + pullPath},
			wantBody: `{"body":"a <b> & c\n\nline","title":"` + pullTitle + `"}`,
		},
		"owner is case-insensitive": {
			branch:   mainBranch,
			pages:    []string{list(pr(7, mainBranch, "O", mainBranch))},
			wantReqs: []string{"GET " + pullsPath + "?" + wantList, "PATCH " + pullPath},
			wantBody: `{"body":"a <b> & c\n\nline","title":"` + pullTitle + `"}`,
		},
		"branch with a slash": {
			branch:   featureBranch,
			pages:    []string{emptyList},
			wantReqs: []string{"GET " + pullsPath + "?" + wantList, "POST " + pullsPath},
			wantBody: `{"title":"` + pullTitle + `","body":"a <b> & c\n\nline","head":"feature/x","base":"main"}`,
		},
		"slash branch matches whole name": {
			branch:   featureBranch,
			pages:    []string{list(pr(3, "x", "o", mainBranch), pr(7, featureBranch, "o", mainBranch))},
			wantReqs: []string{"GET " + pullsPath + "?" + wantList, "PATCH " + pullPath},
			wantBody: `{"body":"a <b> & c\n\nline","title":"` + pullTitle + `"}`,
		},
		"others are ignored": {
			branch: mainBranch,
			pages: []string{list(
				pr(1, "other", "o", mainBranch),
				pr(2, mainBranch, "o", "develop"),
				pr(3, mainBranch, "fork", mainBranch),
				pr(4, mainBranch, "", mainBranch),
				`{"number":5}`,
				`{"number":6,"head":{"ref":"main","repo":{"owner":null}},"base":{"ref":"main"}}`,
			)},
			wantReqs: []string{
				"GET " + pullsPath + "?" + wantList,
				"GET " + pullsPath + "?base_branch=main&limit=50&page=2&state=open",
				"POST " + pullsPath,
			},
			wantBody: `{"title":"` + pullTitle + `","body":"a <b> & c\n\nline","head":"main","base":"main"}`,
		},
		"first match wins": {
			branch: mainBranch,
			pages: []string{list(
				pr(3, mainBranch, "fork", mainBranch), pr(7, mainBranch, "o", mainBranch), pr(9, mainBranch, "o", mainBranch),
			)},
			wantReqs: []string{"GET " + pullsPath + "?" + wantList, "PATCH " + pullPath},
			wantBody: `{"body":"a <b> & c\n\nline","title":"` + pullTitle + `"}`,
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			s := newServer(t, pullAPI(tc.pages, http.StatusCreated, pullCreatedAny))
			c := newClient(t, s.URL)
			err := c.CreateOrUpdatePullRequest(context.Background(), pullTitle, pullBody, mainBranch, tc.branch)
			if err != nil {
				t.Fatal(err)
			}
			reqs := s.requests()
			if len(reqs) != len(tc.wantReqs) {
				t.Fatalf("requests = %v, want %v", reqs, tc.wantReqs)
			}
			for i, want := range tc.wantReqs {
				if got := reqs[i].method + " " + reqs[i].uri; got != want {
					t.Errorf("request %d = %q, want %q", i, got, want)
				}
				if reqs[i].auth != "token "+testToken || reqs[i].accept != mimeJSON {
					t.Errorf("request %d headers: auth %q, accept %q", i, reqs[i].auth, reqs[i].accept)
				}
				if strings.Contains(reqs[i].uri, testToken) || strings.Contains(reqs[i].uri, "head=") {
					t.Errorf("request %d URI %q holds the token or a head filter", i, reqs[i].uri)
				}
			}
			if got := reqs[len(reqs)-1].body; got != tc.wantBody {
				t.Errorf("body = %q, want %q", got, tc.wantBody)
			}
			if reqs[0].body != "" {
				t.Errorf("the list sent a body: %q", reqs[0].body)
			}
		})
	}
}

func TestCreateOrUpdatePullRequestPaging(t *testing.T) {
	full := make([]string, 50)
	for i := range full {
		full[i] = pr(100+i, "other", "o", mainBranch)
	}
	cases := map[string]struct {
		pages    []string
		wantReqs []string
	}{
		"match on page 2": {
			pages: []string{list(full...), list(pr(7, mainBranch, "o", mainBranch))},
			wantReqs: []string{
				"GET " + pullsPath + "?" + wantList,
				"GET " + pullsPath + "?base_branch=main&limit=50&page=2&state=open",
				"PATCH " + pullPath,
			},
		},
		"a short page does not end the search": {
			pages: []string{list(full...), list(pr(8, "other", "o", mainBranch))},
			wantReqs: []string{
				"GET " + pullsPath + "?" + wantList,
				"GET " + pullsPath + "?base_branch=main&limit=50&page=2&state=open",
				"GET " + pullsPath + "?base_branch=main&limit=50&page=3&state=open",
				"POST " + pullsPath,
			},
		},
		"a server with a smaller page size": {
			pages: []string{list(full[:30]...), list(pr(7, mainBranch, "o", mainBranch))},
			wantReqs: []string{
				"GET " + pullsPath + "?" + wantList,
				"GET " + pullsPath + "?base_branch=main&limit=50&page=2&state=open",
				"PATCH " + pullPath,
			},
		},
		"empty page 2 ends the search": {
			pages: []string{list(full...)},
			wantReqs: []string{
				"GET " + pullsPath + "?" + wantList,
				"GET " + pullsPath + "?base_branch=main&limit=50&page=2&state=open",
				"POST " + pullsPath,
			},
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			s := newServer(t, pullAPI(tc.pages, http.StatusCreated, pullCreatedAny))
			if err := newClient(t, s.URL).CreateOrUpdatePullRequest(
				context.Background(), pullTitle, pullBody, mainBranch, mainBranch,
			); err != nil {
				t.Fatal(err)
			}
			var got []string
			for _, r := range s.requests() {
				got = append(got, r.method+" "+r.uri)
			}
			if strings.Join(got, "\n") != strings.Join(tc.wantReqs, "\n") {
				t.Errorf("requests = %q, want %q", got, tc.wantReqs)
			}
		})
	}
}

func TestCreateOrUpdatePullRequestPageCap(t *testing.T) {
	full := make([]string, 50)
	for i := range full {
		full[i] = pr(100+i, "other", "o", mainBranch)
	}
	page := list(full...)
	s := newServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			_, _ = io.WriteString(w, page)
			return
		}
		w.WriteHeader(http.StatusCreated)
		_, _ = io.WriteString(w, "{}")
	})

	if err := newClient(t, s.URL).CreateOrUpdatePullRequest(
		context.Background(), pullTitle, pullBody, mainBranch, mainBranch,
	); err != nil {
		t.Fatal(err)
	}

	reqs := s.requests()
	if len(reqs) != maxPullsPages+1 || reqs[len(reqs)-1].method != http.MethodPost {
		t.Errorf("%d requests, want %d lists and a create", len(reqs), maxPullsPages)
	}
}

func TestCreateOrUpdatePullRequestErrors(t *testing.T) {
	existing := list(pr(7, mainBranch, "o", mainBranch))
	cases := map[string]struct {
		pages       []string
		writeStatus int
		wantMsg     string
		wantCode    string
		wantReqs    int
	}{
		"list 4xx": {
			pages: nil, writeStatus: http.StatusOK, wantMsg: "while fetching existing pull requests", wantReqs: 1,
		},
		"update 5xx": {
			pages: []string{existing}, writeStatus: http.StatusInternalServerError,
			wantMsg: "while updating pull request", wantReqs: 0, // a 5xx is retried
		},
		"create 4xx": {
			pages: []string{emptyList}, writeStatus: http.StatusUnprocessableEntity,
			wantMsg: "while creating pull request", wantReqs: 2,
		},
		"create invalid JSON": {
			pages: []string{emptyList}, writeStatus: http.StatusCreated, wantMsg: "while creating pull request",
			wantCode: codeAPIResponse, wantReqs: 2,
		},
		"list invalid JSON": {
			pages: []string{"not json"}, writeStatus: http.StatusOK, wantMsg: "while fetching existing pull requests",
			wantCode: codeAPIResponse, wantReqs: 1,
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			body := "nope"
			if name == "create invalid JSON" {
				body = "<html>"
			}
			handler := pullAPI(tc.pages, tc.writeStatus, body)
			if name == "list 4xx" {
				handler = replying(http.StatusForbidden, "denied")
			}
			s := newServer(t, handler)

			c := newClient(t, s.URL)
			err := c.CreateOrUpdatePullRequest(context.Background(), pullTitle, pullBody, mainBranch, mainBranch)

			if err == nil || !strings.Contains(err.Error(), tc.wantMsg) {
				t.Fatalf("error = %v, want %q", err, tc.wantMsg)
			}
			if strings.Contains(err.Error(), testToken) {
				t.Errorf("the token leaked: %v", err)
			}
			if tc.wantCode != "" {
				if got := codeOf(t, err); got != tc.wantCode {
					t.Errorf("code = %q, want %q", got, tc.wantCode)
				}
			}
			if n := len(s.requests()); tc.wantReqs != 0 && n != tc.wantReqs {
				t.Errorf("%d requests, want %d", n, tc.wantReqs)
			}
		})
	}
}

func TestDescribePullRequest(t *testing.T) {
	got := DescribePullRequest("T", "B\nC", mainBranch, featureBranch)
	want := []string{"Would create or update a pull request from feature/x to main:", "\tTitle: T", "\tBody: B\nC"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Errorf("lines = %q, want %q", got, want)
	}
}

func TestPullListQueryIsEncoded(t *testing.T) {
	s := newServer(t, pullAPI([]string{emptyList}, http.StatusCreated, "{}"))
	if err := newClient(t, s.URL).CreateOrUpdatePullRequest(
		context.Background(), "t", "b", "release/1 2", mainBranch,
	); err != nil {
		t.Fatal(err)
	}
	q, err := url.ParseQuery(strings.SplitN(s.requests()[0].uri, "?", 2)[1])
	if err != nil || q.Get("base_branch") != "release/1 2" {
		t.Errorf("query = %v, err = %v", q, err)
	}
}
