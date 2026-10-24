package gitea

import (
	"context"
	"net/http"
	"net/url"
	"strings"
)

const (
	listIssuesActivity = "listing issues"
	// issuesLimit is the number of issues asked for, as in Knope.
	issuesLimit = "30"
)

// Issue is an issue as the list endpoint returns it. Other fields, and missing ones, are ignored.
type Issue struct {
	Number int    `json:"number"`
	Title  string `json:"title"`
}

// issuesURL is the path of the issues of the repository, relative to the API base.
func (c Config) issuesURL() string { return c.repoPath() + "/issues" }

// ListIssues returns up to 30 open issues of the repository, narrowed to those that carry all the labels when there
// are some (spec F19.2). Call it only in a real run.
//
// Gitea's issues endpoint also lists pull requests unless it is given `type=issues`. Knope does not send it and so
// offers pull requests as issues; remeris sends it (see testdata/deviations.md).
func (c *Client) ListIssues(ctx context.Context, labels []string) ([]Issue, error) {
	query := url.Values{
		"state": {"open"},
		"type":  {"issues"},
		"limit": {issuesLimit},
	}
	if len(labels) > 0 {
		query.Set("labels", strings.Join(labels, ","))
	}
	var issues []Issue
	if err := c.do(ctx, http.MethodGet, c.cfg.issuesURL(), query, nil, &issues, listIssuesActivity); err != nil {
		return nil, err
	}
	return issues, nil
}
