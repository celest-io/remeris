package config

import "strings"

// GitHubFromRemote ports the GitHub detection of Knope's config::generate (spec F20.6) on the URL of the first
// remote. The URL must contain "github.com"; it is split on "/", the owner is the second-to-last part without a
// "git@github.com:" prefix and the repo is the last part without ".git". The quirks stay: "https://github.com/r"
// gives the owner "github.com", and a trailing slash gives an empty repo.
//
// A URL with a single part, such as "git@github.com:r.git", has no second-to-last part. Knope computes
// parts.len() - 2 on a usize there: it panics in a debug build and wraps to "no owner" in a release build. The
// release behaviour is kept, so there is no detection and no panic.
func GitHubFromRemote(url string) (*GitHub, bool) {
	if !strings.Contains(url, "github.com") {
		return nil, false
	}
	parts := strings.Split(url, "/")
	if len(parts) < 2 {
		return nil, false
	}
	owner := strings.TrimPrefix(parts[len(parts)-2], "git@github.com:")
	repo := strings.TrimSuffix(parts[len(parts)-1], ".git")
	return &GitHub{Owner: owner, Repo: repo}, true
}

// RemoteFunc returns the URL of the first Git remote, false when there is none or the repository cannot be read.
// It is a function so that config does not depend on the Git access and tests can use a fake.
type RemoteFunc func() (string, bool)

// knownPublicGiteaHosts are the Gitea hosts that are detected without a configuration (Knope's
// KNOWN_PUBLIC_GITEA_HOSTS).
var knownPublicGiteaHosts = []string{"codeberg.org"}

// GiteaFromRemote ports the Gitea detection of Knope's config::generate and Gitea::try_from_remote (spec F17,
// F20.6) on the URL of the first remote. It returns nil unless the remote is formatted as
// "git@{host}:{/}{owner}/{repo}" or "https://{host}/{owner}/{repo}" and its host is a known public Gitea host.
//
// Two deviations from Knope, both in testdata/deviations.md:
//   - Knope tests remote.contains("codeberg.org"), so "https://evil.example/codeberg.org/r" matches. Here the
//     parsed host must equal a known host.
//   - Knope keeps the userinfo of "https://user:token@codeberg.org/o/r" in the host. Here it is dropped, so a
//     credential never reaches a configuration, a log line or a request.
func GiteaFromRemote(url string) *Gitea {
	g := parseGiteaRemote(url)
	if g == nil {
		return nil
	}
	host := strings.TrimPrefix(g.Host, "https://")
	for _, known := range knownPublicGiteaHosts {
		if host == known {
			return g
		}
	}
	return nil
}

// parseGiteaRemote is Gitea::try_from_remote without the known-host gate, with the userinfo dropped. The host is
// lower-cased because host names are case-insensitive and the gate compares them.
func parseGiteaRemote(remote string) *Gitea {
	scheme, path, ok := strings.Cut(remote, ":")
	if !ok {
		return nil
	}
	if strings.Contains(scheme, "git@") {
		// scp style: git@host:/owner/repo.git or git@host:owner/repo.git
		host, ok := strings.CutPrefix(scheme, "git@")
		if !ok {
			return nil
		}
		owner, repo, ok := strings.Cut(strings.TrimPrefix(path, "/"), "/")
		if !ok {
			return nil
		}
		return newGitea(host, owner, repo)
	}
	// http(s) and ssh:// remotes: //host/owner/repo
	rest, ok := strings.CutPrefix(path, "//")
	if !ok {
		return nil
	}
	parts := strings.SplitN(rest, "/", 3)
	if len(parts) != 3 {
		return nil
	}
	return newGitea(parts[0], parts[1], parts[2])
}

func newGitea(host, owner, repo string) *Gitea {
	if i := strings.LastIndex(host, "@"); i >= 0 {
		host = host[i+1:]
	}
	return &Gitea{
		Owner: owner,
		Repo:  strings.TrimSuffix(repo, ".git"),
		Host:  "https://" + strings.ToLower(host),
	}
}
