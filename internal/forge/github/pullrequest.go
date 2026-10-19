package github

import "context"

// PullRequestForCommit returns the number and the author login of the first pull request that holds the commit
// (spec F16.4): GET /repos/{owner}/{repo}/commits/{sha}/pulls. sha is sent as given; Knope sends the short hash of
// the commit. found is false when the list is empty. login is "" when GitHub gives the pull request no user.
//
// Knope also sends X-GitHub-Api-Version: 2026-03-10; go-github sets the version header it was written for, which is
// not changed here.
func (c *Client) PullRequestForCommit(ctx context.Context, sha string) (number int, login string, found bool, err error) {
	pulls, _, err := c.PullRequests.ListPullRequestsWithCommit(ctx, c.Owner, c.Repo, sha, nil)
	if err != nil {
		return 0, "", false, WrapError("fetching PRs for commit "+sha, err)
	}
	if len(pulls) == 0 {
		return 0, "", false, nil
	}
	return pulls[0].GetNumber(), pulls[0].GetUser().GetLogin(), true, nil
}
