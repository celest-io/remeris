package github_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/celest-io/remeris/internal/credentials"
	"github.com/celest-io/remeris/internal/forge/github"
)

const issuesPath = "/api/v3/repos/o/r/issues"
const defaultQuery = "direction=asc&per_page=30&sort=created&state=open"

// issuesPage renders count issues numbered from first; every third one is a pull request.
func issuesPage(first, count int) string {
	items := make([]string, count)
	for i := range items {
		n := first + i
		pr := ""
		if i%3 == 2 {
			pr = `,"pull_request":{"url":"https://example.test/pr"}`
		}
		items[i] = fmt.Sprintf(`{"number":%d,"title":"issue %d"%s}`, n, n, pr)
	}
	return "[" + strings.Join(items, ",") + "]"
}

func TestListOpenIssues(t *testing.T) {
	t.Parallel()
	tests := map[string]struct {
		labels      []string
		status      int
		pages       map[string]string
		wantQuery   string
		wantNumbers []int
		wantCalls   int
		wantErr     string
	}{
		"pull requests are skipped": {
			status: http.StatusOK,
			pages: map[string]string{
				"": `[{"number":1,"title":"a"},{"number":2,"title":"pr","pull_request":{}},{"number":3,"title":"c"}]`,
			},
			wantQuery:   defaultQuery,
			wantNumbers: []int{1, 3},
			wantCalls:   1,
		},
		"labels are joined": {
			labels:      []string{"a", "b"},
			status:      http.StatusOK,
			pages:       map[string]string{"": `[]`},
			wantQuery:   "direction=asc&labels=a%2Cb&per_page=30&sort=created&state=open",
			wantNumbers: nil,
			wantCalls:   1,
		},
		"pages until 30": {
			status: http.StatusOK,
			pages: map[string]string{
				"":  issuesPage(1, 30),
				"2": issuesPage(31, 30),
				"3": issuesPage(61, 30),
			},
			wantQuery: defaultQuery,
			// 20 issues per page of 30: the third page is needed to reach 30, and it is cut at 30.
			wantNumbers: append(seq(1, 30, 3), seq(31, 60, 3)...)[:30],
			wantCalls:   2,
		},
		"error": {
			status:    http.StatusNotFound,
			pages:     map[string]string{"": `{"message":"Not Found"}`},
			wantQuery: defaultQuery,
			wantCalls: 1,
			wantErr:   "Trouble communicating with GitHub while loading issues",
		},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			var mu sync.Mutex
			var queries []string
			var srv *httptest.Server
			srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				mu.Lock()
				queries = append(queries, r.URL.Path+"?"+r.URL.Query().Encode())
				mu.Unlock()
				page := r.URL.Query().Get("page")
				if next := nextPage(page); next != "" && tc.pages[next] != "" {
					w.Header().Set("Link", fmt.Sprintf(`<%s%s?page=%s>; rel="next"`, srv.URL, issuesPath, next))
				}
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.pages[page]))
			}))
			t.Cleanup(srv.Close)
			f := github.New(github.Config{Owner: "o", Repo: "r", TrustedAPIURL: srv.URL + "/api/v3/"}, nil,
				&credentials.Store{Dir: t.TempDir(), Getenv: func(string) string { return "" }})
			client, err := f.MaybeAuthenticated(context.Background())
			if err != nil {
				t.Fatal(err)
			}

			got, err := client.ListOpenIssues(context.Background(), tc.labels)

			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("err = %v, want %q", err, tc.wantErr)
				}
			} else if err != nil {
				t.Fatal(err)
			}
			if len(queries) != tc.wantCalls {
				t.Fatalf("requests = %q, want %d", queries, tc.wantCalls)
			}
			if !strings.HasPrefix(queries[0], issuesPath+"?") || !strings.Contains(queries[0], tc.wantQuery) {
				t.Errorf("first request = %q, want %q", queries[0], tc.wantQuery)
			}
			var numbers []int
			for _, issue := range got {
				numbers = append(numbers, issue.Number)
			}
			if fmt.Sprint(numbers) != fmt.Sprint(tc.wantNumbers) {
				t.Errorf("numbers = %v, want %v", numbers, tc.wantNumbers)
			}
		})
	}
}

// nextPage is the page after page ("" is the first).
func nextPage(page string) string {
	switch page {
	case "":
		return "2"
	case "2":
		return "3"
	default:
		return ""
	}
}

// seq lists the issue numbers in [from, to] that issuesPage does not make pull requests (every third is one).
func seq(from, to, _ int) []int {
	var out []int
	for n := from; n <= to; n++ {
		if (n-from)%3 != 2 {
			out = append(out, n)
		}
	}
	return out
}
