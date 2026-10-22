package github

import (
	"context"
	"log/slog"
	"strconv"

	gh "github.com/google/go-github/v92/github"
)

// CreateOrUpdatePullRequest opens a pull request from head to base, or updates the title and body of the open one
// that already has this head and base (spec F18). head is the short name of the branch, such as "feature/x"
// (Quirk Q3: Knope sends "refs/heads/feature/x").
//
// The lookup is GET /repos/{owner}/{repo}/pulls?head={owner}:{head}&base={base}; like Knope it does not send a
// state, so GitHub's default (open) applies. The first match is edited with PATCH, otherwise the pull request is
// created with POST.
func (c *Client) CreateOrUpdatePullRequest(ctx context.Context, title, body, head, base string) error {
	const lookup = "fetching existing pull requests"
	existing, _, err := c.PullRequests.List(ctx, c.Owner, c.Repo, &gh.PullRequestListOptions{
		Head: c.Owner + ":" + head,
		Base: base,
	})
	if err != nil {
		return WrapError(lookup, err)
	}
	if len(existing) > 0 {
		slog.DebugContext(ctx, "Updating existing pull request: "+existing[0].GetURL())
		_, _, err = c.PullRequests.Edit(ctx, c.Owner, c.Repo, existing[0].GetNumber(), &gh.PullRequest{
			Title: &title,
			Body:  &body,
		})
		return WrapError("updating pull request", err)
	}
	slog.DebugContext(ctx, "No matching existing pull request found, creating a new one.")
	created, _, err := c.PullRequests.Create(ctx, c.Owner, c.Repo, gh.CreatePullRequest{
		Title: &title,
		Body:  &body,
		Head:  head,
		Base:  base,
	})
	if err != nil {
		return WrapError("creating pull request", err)
	}
	if created.URL != nil {
		slog.DebugContext(ctx, "Created new pull request: "+strconv.Quote(created.GetURL()))
	}
	return nil
}
