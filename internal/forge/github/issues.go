package github

import (
	"context"

	gh "github.com/google/go-github/v92/github"
)

// maxIssues is the number of issues SelectGitHubIssue offers, as in Knope (`issues(first: 30)`).
const maxIssues = 30

// Issue is an open issue of the repository, as much of it as the issue steps need.
type Issue struct {
	Number int
	Title  string
}

// ListOpenIssues returns up to 30 open issues of the repository, oldest first, skipping pull requests (spec F19.1):
// GET /repos/{owner}/{repo}/issues?state=open&sort=created&direction=asc. The REST endpoint lists pull requests as
// issues too, so a page can hold fewer than 30 issues and the next page is read until 30 are collected.
//
// Deviations from Knope, which uses GraphQL: labels given together must all be on an issue (REST "labels=a,b"),
// where GraphQL takes any of them; and the oldest-first order is requested explicitly because REST defaults to the
// newest first. Empty labels mean any issue.
func (c *Client) ListOpenIssues(ctx context.Context, labels []string) ([]Issue, error) {
	opts := &gh.IssueListByRepoOptions{
		State:       "open",
		Labels:      labels,
		Sort:        "created",
		Direction:   "asc",
		ListOptions: gh.ListOptions{PerPage: maxIssues},
	}
	var out []Issue
	for len(out) < maxIssues {
		page, resp, err := c.Issues.ListByRepo(ctx, c.Owner, c.Repo, opts)
		if err != nil {
			return nil, WrapError("loading issues", err)
		}
		for _, issue := range page {
			if issue.IsPullRequest() {
				continue
			}
			out = append(out, Issue{Number: issue.GetNumber(), Title: issue.GetTitle()})
			if len(out) == maxIssues {
				break
			}
		}
		if resp.NextPage == 0 {
			break
		}
		opts.ListOptions.Page = resp.NextPage
	}
	return out, nil
}
