package steps

import (
	"context"
	"log/slog"
	"strconv"

	"github.com/celest-io/remeris/internal/forge/github"
	"github.com/celest-io/remeris/pkg/versioning/changes"
)

const (
	// dryRunPRNumber and dryRunPRLogin are what a dry run puts in the pull request variables, as Knope does.
	dryRunPRNumber = 1234
	dryRunPRLogin  = "some-user"
)

// enrichGitInfo sets the pull request number and author login of the changes that have a commit (spec F16.4),
// the data of the `$pr_number` and `$pr_author_login` variables. Changes from commits and from change files are
// treated alike. forge must not be nil.
//
// A dry run reaches no network and builds no client: it logs what would happen and uses fixed values. A real run
// asks GitHub for the first pull request of each commit, by its short hash, with the token when there is one and
// anonymously otherwise (never a prompt). A commit that cannot be looked up gives a warning and is left as it
// is; so does a failure to build the client, as in Knope.
func enrichGitInfo(ctx context.Context, forge *github.Forge, list []changes.Change, dryRun bool) {
	cfg := forge.Config()
	if dryRun {
		slog.InfoContext(ctx, "Would fetch Pull Request info from GitHub repo "+cfg.Owner+"/"+cfg.Repo+" for "+
			strconv.Itoa(len(list))+" changes")
		for i := range list {
			if info := list[i].GitInfo; info != nil {
				number, login := dryRunPRNumber, dryRunPRLogin
				info.PRNumber, info.PRAuthorLogin = &number, &login
			}
		}
		return
	}

	client, err := forge.MaybeAuthenticated(ctx)
	if err != nil {
		slog.WarnContext(ctx, "Failed to enrich git info from GitHub: "+err.Error())
		return
	}
	for i := range list {
		info := list[i].GitInfo
		if info == nil {
			continue
		}
		number, login, found, err := client.PullRequestForCommit(ctx, info.Hash)
		switch {
		case err != nil:
			slog.WarnContext(ctx, "Failed to fetch PR info for commit "+info.Hash+": "+err.Error())
		case !found:
			slog.DebugContext(ctx, "No PR found for commit "+info.Hash)
		default:
			info.PRNumber = &number
			if login != "" {
				info.PRAuthorLogin = &login
			}
		}
	}
}
