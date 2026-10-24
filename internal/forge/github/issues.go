package github

import (
	"context"
	"net/url"
	"strings"

	"github.com/shurcooL/githubv4"

	"github.com/celest-io/remeris/internal/diag"
)

const (
	codeIssuesAPI    = "remeris::issues::github::api"
	codeIssuesURL    = "remeris::issues::github::graphql_url"
	issuesAPIHelp    = "Check your network connection and GitHub configuration"
	graphqlPath      = "graphql"
	enterpriseSuffix = "/api/v3"
)

// Issue is an open issue of the repository, as much of it as the issue steps need.
type Issue struct {
	Number int
	Title  string
}

// issuesQuery is Knope's query (crates/knope/src/step/issues/github.rs), written as a githubv4 struct:
//
//	query($repo: String!, $owner: String!, $labels: [String!]) {
//	  repository(name:$repo, owner:$owner) {
//	    issues(states:OPEN, first: 30, labels: $labels) { nodes { number, title } }
//	  }
//	}
//
// The query has no orderBy, so GitHub's default order applies, as in Knope. Pull requests are not issues for GraphQL.
// The labels are a variable, never part of the query text.
type issuesQuery struct {
	Repository struct {
		Issues struct {
			Nodes []struct {
				Number int
				Title  string
			}
		} `graphql:"issues(states: OPEN, first: 30, labels: $labels)"`
	} `graphql:"repository(name: $repo, owner: $owner)"`
}

// ListOpenIssues returns the first 30 open issues of the repository with any of the labels (spec F19.1), through
// the GraphQL API with Knope's query. No labels means any issue. There is no paging.
//
// The request goes through the HTTP client of the REST client, so the credential guard, the retry policy and the
// redirect policy apply, and the GraphQL endpoint is derived from the API URL the client was built for.
func (c *Client) ListOpenIssues(ctx context.Context, labels []string) ([]Issue, error) {
	endpoint, err := graphqlEndpoint(c.BaseURL())
	if err != nil {
		return nil, err
	}
	var gqlLabels *[]githubv4.String
	if labels != nil {
		converted := make([]githubv4.String, len(labels))
		for i, l := range labels {
			converted[i] = githubv4.String(l)
		}
		gqlLabels = &converted
	}
	variables := map[string]any{
		"repo":   githubv4.String(c.Repo),
		"owner":  githubv4.String(c.Owner),
		"labels": gqlLabels,
	}
	var query issuesQuery
	client := githubv4.NewEnterpriseClient(endpoint, c.Client.Client())
	if err := client.Query(ctx, &query, variables); err != nil {
		// Knope: "Could not communicate with GitHub while loading issues: {source}". The error of the GraphQL
		// client holds the status and the response body or the GraphQL messages, never a request header.
		return nil, diag.New("Could not communicate with GitHub while loading issues: " + err.Error()).
			WithCode(codeIssuesAPI).
			WithHelp(issuesAPIHelp).
			WithCause(err)
	}
	nodes := query.Repository.Issues.Nodes
	out := make([]Issue, len(nodes))
	for i, n := range nodes {
		out[i] = Issue{Number: n.Number, Title: n.Title}
	}
	return out, nil
}

// graphqlEndpoint maps the API base URL of the REST client to the GraphQL endpoint of the same instance:
//
//	https://api.github.com/             -> https://api.github.com/graphql
//	https://HOST/api/v3/                -> https://HOST/api/graphql   (GitHub Enterprise Server)
//	https://api.HOST/ (no path)         -> https://api.HOST/graphql   (GitHub Enterprise Cloud with data residency)
//
// Any other path has no known GraphQL endpoint and is refused, so that a request never goes to a guessed URL.
// The endpoint has the scheme, host and port of the API URL, which the credential guard already trusts.
func graphqlEndpoint(apiBase string) (string, error) {
	u, err := url.Parse(apiBase)
	if err != nil || u.Host == "" {
		// The error of url.Parse repeats the URL as written; it is not echoed.
		return "", graphqlURLError()
	}
	path := strings.TrimRight(u.Path, "/")
	switch {
	case path == "":
		u.Path = "/" + graphqlPath
	case strings.HasSuffix(path, enterpriseSuffix):
		u.Path = strings.TrimSuffix(path, "v3") + graphqlPath
	default:
		return "", graphqlURLError()
	}
	u.RawPath, u.RawQuery, u.Fragment = "", "", ""
	return u.String(), nil
}

func graphqlURLError() error {
	return diag.New("The GitHub API URL has no known GraphQL endpoint").
		WithCode(codeIssuesURL).
		WithHelp("SelectGitHubIssue needs an API URL like https://HOST/api/v3/ (GitHub Enterprise Server) or " +
			"https://api.HOST/ (GitHub.com and data residency).")
}
