package gitea

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

const (
	listPullsActivity  = "fetching existing pull requests"
	updatePullActivity = "updating pull request"
	createPullActivity = "creating pull request"
	pullsPageSize      = 50
	maxPullsPages      = 20
)

// pullSummary is the part of a pull request the lookup reads. Other fields, and missing ones, are ignored.
type pullSummary struct {
	Number int    `json:"number"`
	URL    string `json:"url"`
	Head   struct {
		Ref  string `json:"ref"`
		Repo *struct {
			Owner *struct {
				Login string `json:"login"`
			} `json:"owner"`
		} `json:"repo"`
	} `json:"head"`
	Base struct {
		Ref string `json:"ref"`
	} `json:"base"`
}

// matches reports whether the pull request goes from branch of a repository owned by owner into base. A pull request
// whose head repository is gone (a deleted fork) never matches.
func (p *pullSummary) matches(owner, branch, base string) bool {
	repo := p.Head.Repo
	if repo == nil || repo.Owner == nil {
		return false
	}
	return p.Head.Ref == branch && strings.EqualFold(repo.Owner.Login, owner) && p.Base.Ref == base
}

// updatePullRequest is the JSON body of the update. The order of the fields is the one of Knope.
type updatePullRequest struct {
	Body  string `json:"body"`
	Title string `json:"title"`
}

// createPullRequest is the JSON body of the creation. The order of the fields is the one of Knope.
type createPullRequest struct {
	Title string `json:"title"`
	Body  string `json:"body"`
	Head  string `json:"head"`
	Base  string `json:"base"`
}

// CreateOrUpdatePullRequest updates the open pull request from branch into base, or creates it when there is none
// (spec F18). branch is the full short name of the branch, such as feature/x. Call it only in a real run.
//
// Gitea's list endpoint has no `head` filter (Knope sends one, a server ignores it, and Knope may then update an
// unrelated pull request). The open pull requests into base are therefore listed, 50 per page (a server may give
// fewer) until a page is empty and at most 20 pages,
// and the first whose head is branch of a repository owned by the configured owner is chosen. When none of the 20
// pages holds it, a new pull request is created.
func (c *Client) CreateOrUpdatePullRequest(ctx context.Context, title, body, base, branch string) error {
	existing, err := c.findPullRequest(ctx, base, branch)
	if err != nil {
		return err
	}
	if existing != nil {
		slog.DebugContext(ctx, "Updating existing pull request: "+existing.URL)
		return c.do(ctx, http.MethodPatch, c.cfg.pullURL(existing.Number), nil,
			updatePullRequest{Body: body, Title: title}, nil, updatePullActivity)
	}
	slog.DebugContext(ctx, "No matching existing pull request found, creating a new one.")
	// Any JSON is accepted; the URL is only logged when it is there.
	var created json.RawMessage
	req := createPullRequest{Title: title, Body: body, Head: branch, Base: base}
	if err := c.do(ctx, http.MethodPost, c.cfg.pullsURL(), nil, req, &created, createPullActivity); err != nil {
		return err
	}
	var pr pullSummary
	_ = json.Unmarshal(created, &pr) // lenient: a response of another shape only means no URL to log
	slog.DebugContext(ctx, "Created new pull request: "+pr.URL)
	return nil
}

// findPullRequest returns the first open pull request that matches, or nil.
func (c *Client) findPullRequest(ctx context.Context, base, branch string) (*pullSummary, error) {
	for page := 1; page <= maxPullsPages; page++ {
		query := url.Values{
			"state":       {"open"},
			"base_branch": {base},
			"limit":       {strconv.Itoa(pullsPageSize)},
			"page":        {strconv.Itoa(page)},
		}
		var pulls []pullSummary
		if err := c.do(ctx, http.MethodGet, c.cfg.pullsURL(), query, nil, &pulls, listPullsActivity); err != nil {
			return nil, err
		}
		for i := range pulls {
			if pulls[i].matches(c.cfg.Owner, branch, base) {
				return &pulls[i], nil
			}
		}
		// Only an empty page ends the search: a server may cap the page size below what was asked for.
		if len(pulls) == 0 {
			break
		}
	}
	return nil, nil //nolint:nilnil // nil means "no pull request"; an error is a failed request
}

// DescribePullRequest returns the lines of a dry run (spec F18): nothing is sent and no client is needed.
func DescribePullRequest(title, body, base, branch string) []string {
	return []string{
		"Would create or update a pull request from " + branch + " to " + base + ":",
		"\tTitle: " + title,
		"\tBody: " + body,
	}
}
